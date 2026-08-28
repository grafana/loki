package index

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"
)

type readerConstructor struct {
	name string
	open func(path string) (Reader, error)
}

func allReaderConstructors() []readerConstructor {
	return []readerConstructor{
		{name: "MmapReader", open: func(p string) (Reader, error) { return NewMmapFileReader(p) }},
		{name: "StreamReader", open: func(p string) (Reader, error) { return NewStreamFileReader(p, DefaultStreamOptions()) }},
	}
}

type seriesFixture struct {
	ls     labels.Labels
	chunks []ChunkMeta
}

// writeIndexFixture writes series into a new index file and returns its path.
//
// It derives the symbol table from the series' labels, so no fixture has to
// keep a symbol list in sync by hand, and it reorders series by fingerprint,
// which is the order AddSeries requires. Refs are assigned in that order,
// starting at 1.
func writeIndexFixture(t testing.TB, format int, series []seriesFixture) string {
	t.Helper()
	fileName := filepath.Join(t.TempDir(), IndexFilename)

	creator, err := NewWriter(context.Background(), format, fileName)
	require.NoError(t, err)

	symbolSet := map[string]struct{}{}
	for _, s := range series {
		s.ls.Range(func(l labels.Label) {
			symbolSet[l.Name] = struct{}{}
			symbolSet[l.Value] = struct{}{}
		})
	}
	symbols := make([]string, 0, len(symbolSet))
	for s := range symbolSet {
		symbols = append(symbols, s)
	}
	sort.Strings(symbols)
	for _, s := range symbols {
		require.NoError(t, creator.AddSymbol(s))
	}

	// AddSeries requires ascending fingerprint order.
	sort.Slice(series, func(i, j int) bool {
		return labels.StableHash(series[i].ls) < labels.StableHash(series[j].ls)
	})
	for i, s := range series {
		require.NoError(t, creator.AddSeries(
			storage.SeriesRef(i+1),
			s.ls,
			model.Fingerprint(labels.StableHash(s.ls)),
			s.chunks...,
		))
	}

	_, err = creator.Close(false)
	require.NoError(t, err)
	return fileName
}

// openBothReaders opens path with both reader implementations, registering
// cleanups that close them. It returns the concrete reader types because
// several tests compare their internals.
func openBothReaders(t testing.TB, path string) (*ByteSliceReader, *StreamReader) {
	t.Helper()

	mmap, err := NewMmapFileReader(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, mmap.Close()) })

	stream, err := NewStreamFileReader(path, DefaultStreamOptions())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stream.Close()) })

	return mmap, stream
}

// scanFor opens a series scan over r, registering a cleanup that closes it.
func scanFor(t testing.TB, r Reader) SeriesScan {
	t.Helper()

	scan := r.NewSeriesScan()
	t.Cleanup(func() { require.NoError(t, scan.Close()) })
	return scan
}

// scanBoth opens a series scan over each reader returned by openBothReaders.
func scanBoth(t testing.TB, mmap, stream Reader) (SeriesScan, SeriesScan) {
	t.Helper()
	return scanFor(t, mmap), scanFor(t, stream)
}

// writeCrossCheckFixture builds a small but non-trivial index used by
// the cross-check tests: multiple label names, multiple values per
// name, a couple of chunks per series.
func writeCrossCheckFixture(t testing.TB, format int) string {
	t.Helper()
	return writeIndexFixture(t, format, []seriesFixture{
		{ls: labels.FromStrings("a", "1", "b", "1", "c", "svcA"), chunks: []ChunkMeta{{Checksum: 1, MinTime: 0, MaxTime: 10, KB: 1, Entries: 1}}},
		{ls: labels.FromStrings("a", "1", "b", "2", "c", "svcA"), chunks: []ChunkMeta{{Checksum: 2, MinTime: 10, MaxTime: 20, KB: 1, Entries: 1}}},
		{ls: labels.FromStrings("a", "2", "b", "3", "c", "svcB"), chunks: []ChunkMeta{{Checksum: 3, MinTime: 20, MaxTime: 30, KB: 1, Entries: 1}}},
	})
}

// BenchmarkNewStreamFileReader reproduces the index-open hot path.
func BenchmarkNewStreamFileReader(b *testing.B) {
	path := writeCrossCheckFixture(b, FormatV4)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := NewStreamFileReader(path, DefaultStreamOptions())
		if err != nil {
			b.Fatal(err)
		}
		if err := r.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func writeBenchmarkFixture(t testing.TB, numSeries int, numChunksPerSeries int) string {
	t.Helper()
	chunks := make([]ChunkMeta, numChunksPerSeries)
	for i := range numChunksPerSeries {
		chunks[i] = ChunkMeta{Checksum: uint32(i), MinTime: int64(i * 10), MaxTime: int64(i*10 + 10)}
	}
	series := make([]seriesFixture, numSeries)
	for i := range numSeries {
		series[i].ls = labels.FromStrings(
			"id", fmt.Sprintf("series-%d", i),
			"pod", fmt.Sprintf("pod-%d", i),
			"a", strconv.Itoa(i%5),
			"b", strconv.Itoa(i%11),
			"c", strconv.Itoa(i%17),
		)
		series[i].chunks = chunks
	}
	return writeIndexFixture(t, FormatV4, series)
}

func BenchmarkPostings(b *testing.B) {
	path := writeBenchmarkFixture(b, 1_000_000, 3)
	r, err := NewStreamFileReader(path, DefaultStreamOptions())
	require.NoError(b, err)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		postings, err := r.Postings("c", nil, "3")
		if err != nil {
			b.Fatal(err)
		}
		for postings.Next() {
			_ = postings.At()
		}
		if err := postings.Err(); err != nil {
			b.Fatal(err)
		}
	}
}

// chunkSpan is the millisecond width of every chunk written by
// writeManyChunksFixture. Chunks are contiguous, so a series with n chunks
// covers [0, n*chunkSpan).
const chunkSpan = 100

// writeManyChunksFixture builds an index whose series carry chunk counts
// straddling both ChunkPageSize (16) and DefaultMaxChunksToBypassMarkerLookup
// (64), so decoding takes the linear-scan path for the short series and the
// chunk-page-marker path for the long ones. Series carry several labels drawn
// from a pool of well over symbolFactor symbols, so resolving a series' labels
// walks the sparse symbol table rather than hitting only its first block.
//
// It returns the file name and the exclusive end of the time range covered.
func writeManyChunksFixture(t *testing.T, format int) (string, int64) {
	t.Helper()

	// Straddle the linear-scan/marker-lookup boundary (64) and the page size
	// (16) from both sides, and include a series long enough to span many
	// pages.
	chunkCounts := []int{1, 2, 15, 16, 17, 63, 64, 65, 200}

	var (
		series  = make([]seriesFixture, 0, len(chunkCounts))
		through int64
	)
	for i, chunkCount := range chunkCounts {
		ls := labels.FromStrings(
			"app", fmt.Sprintf("app%02d", i%4),
			"env", []string{"dev", "prod", "staging"}[i%3],
			"id", fmt.Sprintf("series%03d", i),
			"pod", fmt.Sprintf("pod%03d", (i*7)%len(chunkCounts)),
			"tenant", "loki",
		)

		chunks := make([]ChunkMeta, 0, chunkCount)
		for c := range chunkCount {
			minTime := int64(c) * chunkSpan
			maxTime := minTime + chunkSpan - 1
			chunk := ChunkMeta{
				Checksum: uint32(i*1000 + c),
				MinTime:  minTime,
				MaxTime:  maxTime,
				KB:       uint32(c + 1),
				Entries:  uint32(2*c + 1),
			}
			// Only FormatV4 encodes IngestedAt. Stamp it on every other series
			// so both the set and unset encodings are covered.
			if i%2 == 0 {
				chunk.IngestedAt = maxTime + int64(c%3+1)*ingestedAtDayMilliseconds
			}
			chunks = append(chunks, chunk)
			if maxTime+1 > through {
				through = maxTime + 1
			}
		}
		series = append(series, seriesFixture{ls: ls, chunks: chunks})
	}

	return writeIndexFixture(t, format, series), through
}

// TestReaders_CrossCheck opens the same fixture with every Reader
// implementation and asserts each method returns identical results to the
// ByteSliceReader baseline.
func TestReaders_CrossCheck(t *testing.T) {
	for _, format := range []int{FormatV3, FormatV4} {
		t.Run(fmt.Sprintf("format=%d", format), func(t *testing.T) {
			mmap, stream := openBothReaders(t, writeCrossCheckFixture(t, format))

			require.Equal(t, mmap.Version(), stream.Version())
			require.Equal(t, mmap.Checksum(), stream.Checksum())
			require.Equal(t, mmap.Size(), stream.Size())

			baseMin, baseMax := mmap.Bounds()
			rMin, rMax := stream.Bounds()
			require.Equal(t, baseMin, rMin)
			require.Equal(t, baseMax, rMax)

			requireLabelsEqual(t, mmap, stream)
			requirePostingsSeriesEqual(t, mmap, stream)
		})
	}
}

// TestStreamReader_SeriesMatchesMmap cross-checks the streaming Series and
// ChunkStats implementations against the mmap reader over series with widely
// varying chunk counts and a range of query windows, so both the linear chunk
// scan and the chunk-page-marker lookup are exercised, along with windows that
// select all, some, and none of a series' chunks.
func TestStreamReader_SeriesMatchesMmap(t *testing.T) {
	for _, format := range []int{FormatV3, FormatV4} {
		t.Run(fmt.Sprintf("format=%d", format), func(t *testing.T) {
			path, through := writeManyChunksFixture(t, format)
			mmap, stream := openBothReaders(t, path)
			mmapScan, streamScan := scanBoth(t, mmap, stream)

			refs := allSeriesRefs(t, mmap)
			require.NotEmpty(t, refs)

			windows := []struct {
				name          string
				from, through int64
			}{
				{"everything", 0, math.MaxInt64},
				{"first chunk only", 0, chunkSpan},
				{"single mid-range chunk", 20 * chunkSpan, 21 * chunkSpan},
				{"across page boundaries", 20*chunkSpan + 50, 40*chunkSpan + 50},
				{"tail", 150 * chunkSpan, math.MaxInt64},
				{"past the end", through, through + chunkSpan},
				{"before the start", -2 * chunkSpan, 0},
			}

			for _, w := range windows {
				t.Run(w.name, func(t *testing.T) {
					for _, ref := range refs {
						requireStreamSeriesEqual(t, mmapScan, streamScan, ref, w.from, w.through)
					}
				})
			}
		})
	}
}

// requireStreamSeriesEqual asserts that Series and ChunkStats agree between the
// two readers' scans for one series ref and one query window, including the
// labels-free Series path.
func requireStreamSeriesEqual(t *testing.T, mmap, stream SeriesScan, ref storage.SeriesRef, from, through int64) {
	t.Helper()

	var mmapLabels, streamLabels labels.Labels
	var mmapChunks, streamChunks []ChunkMeta

	mmapFingerprint, err := mmap.Series(ref, from, through, &mmapLabels, &mmapChunks)
	require.NoError(t, err)
	streamFingerprint, err := stream.Series(ref, from, through, &streamLabels, &streamChunks)
	require.NoError(t, err)
	require.Equal(t, mmapFingerprint, streamFingerprint)
	require.Equal(t, mmapLabels, streamLabels)
	require.Equal(t, mmapChunks, streamChunks)

	// Series with nil labels takes the skip-labels decode path.
	var mmapChunksNoLabels, streamChunksNoLabels []ChunkMeta
	mmapFingerprint, err = mmap.Series(ref, from, through, nil, &mmapChunksNoLabels)
	require.NoError(t, err)
	streamFingerprint, err = stream.Series(ref, from, through, nil, &streamChunksNoLabels)
	require.NoError(t, err)
	require.Equal(t, mmapFingerprint, streamFingerprint)
	require.Equal(t, mmapChunks, streamChunksNoLabels)
	require.Equal(t, mmapChunksNoLabels, streamChunksNoLabels)

	// ChunkStats both without and with a `by` set.
	for _, by := range []map[string]struct{}{nil, {"app": {}, "env": {}}} {
		var mmapStatsLabels, streamStatsLabels labels.Labels
		mmapFingerprint, mmapStats, err := mmap.ChunkStats(ref, from, through, &mmapStatsLabels, by)
		require.NoError(t, err)
		streamFingerprint, streamStats, err := stream.ChunkStats(ref, from, through, &streamStatsLabels, by)
		require.NoError(t, err)
		require.Equal(t, mmapFingerprint, streamFingerprint)
		require.Equal(t, mmapStats, streamStats)
		require.Equal(t, mmapStatsLabels, streamStatsLabels)
	}
}

// allSeriesRefs returns every series ref in the index, in ascending order. The
// fixtures give each series a unique "id" value, so the union of that label's
// postings covers all of them.
func allSeriesRefs(t *testing.T, r Reader) []storage.SeriesRef {
	t.Helper()
	values, err := r.LabelValues("id")
	require.NoError(t, err)
	return mustPostingsAndDrain(t, r, nil, "id", values...)
}

func TestReaders_RejectsBadMagic(t *testing.T) {
	for _, rc := range allReaderConstructors() {
		t.Run(rc.name, func(t *testing.T) {
			path := writeCrossCheckFixture(t, FormatV3)
			corruptFileBytes(t, path, func(b []byte) { b[0] ^= 0xFF })

			_, err := rc.open(path)
			require.Error(t, err)
			require.Contains(t, err.Error(), "invalid magic number")
		})
	}
}

func TestReaders_RejectsUnknownVersion(t *testing.T) {
	for _, rc := range allReaderConstructors() {
		t.Run(rc.name, func(t *testing.T) {
			path := writeCrossCheckFixture(t, FormatV3)
			// The version byte lives immediately after the 4-byte magic.
			corruptFileBytes(t, path, func(b []byte) { b[4] = 0xFE })

			_, err := rc.open(path)
			require.Error(t, err)
			require.Contains(t, err.Error(), "unknown index file version")
		})
	}
}

func TestReaders_RejectsTruncatedHeader(t *testing.T) {
	for _, rc := range allReaderConstructors() {
		t.Run(rc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "truncated.tsdb")
			require.NoError(t, os.WriteFile(path, []byte{0xBA, 0xAA}, 0600))

			_, err := rc.open(path)
			require.Error(t, err)
			require.Contains(t, err.Error(), "invalid size")
		})
	}
}

func TestReaders_RejectsTruncatedTOC(t *testing.T) {
	// Reject an index that has a valid header but is too short to contain the fixed-size TOC record.
	for _, rc := range allReaderConstructors() {
		t.Run(rc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "truncated.tsdb")
			// Valid magic + version, then nothing.
			buf := make([]byte, HeaderLen)
			binary.BigEndian.PutUint32(buf[0:4], MagicIndex)
			buf[4] = FormatV3
			require.NoError(t, os.WriteFile(path, buf, 0600))

			_, err := rc.open(path)
			require.Error(t, err)
			require.Contains(t, err.Error(), "invalid size")
		})
	}
}

func TestReaders_RejectsCorruptTOCChecksum(t *testing.T) {
	for _, rc := range allReaderConstructors() {
		t.Run(rc.name, func(t *testing.T) {
			path := writeCrossCheckFixture(t, FormatV3)
			// The TOC lives in the last 72+4 bytes of the file.
			// Flip byte at offset -8, which is in the content of the TOC,
			// so changing it should make the checksum invalid.
			corruptFileBytes(t, path, func(b []byte) {
				off := len(b) - crc32.Size - 8
				b[off] ^= 0x01
			})

			_, err := rc.open(path)
			require.Error(t, err)
			require.Contains(t, err.Error(), "invalid checksum")
		})
	}
}

// TestReaders_RejectsCorruptSectionChecksum asserts that every section a reader
// validates while opening is checked, for each of the sections the readers scan
// up front.
func TestReaders_RejectsCorruptSectionChecksum(t *testing.T) {
	sections := map[string]func(toc *TOC) int{
		"symbols":             func(toc *TOC) int { return int(toc.Symbols) },
		"postings table":      func(toc *TOC) int { return int(toc.PostingsTable) },
		"fingerprint offsets": func(toc *TOC) int { return int(toc.FingerprintOffsets) },
	}

	for name, sectionOffset := range sections {
		t.Run(name, func(t *testing.T) {
			for _, rc := range allReaderConstructors() {
				t.Run(rc.name, func(t *testing.T) {
					path := writeCrossCheckFixture(t, FormatV3)

					// Locate the section from the TOC
					reader, err := NewStreamFileReader(path, DefaultStreamOptions())
					require.NoError(t, err)
					offset := sectionOffset(reader.toc)
					require.NoError(t, reader.Close())

					corruptFileBytes(t, path, func(b []byte) {
						// Flip the first byte of the section's content (skipping the first 4 bytes,
						// which is the size of the section).
						b[offset+4] ^= 0x01
					})

					_, err = rc.open(path)
					require.Error(t, err)
					require.Contains(t, err.Error(), "invalid checksum")
				})
			}
		})
	}
}

// TestReaders_RejectsCorruptSeriesRecord asserts that a series record whose
// content no longer matches its trailing CRC32 is rejected rather than
// silently decoded into corrupt labels or chunks.
func TestReaders_RejectsCorruptSeriesRecord(t *testing.T) {
	for _, format := range []int{FormatV3, FormatV4} {
		t.Run(fmt.Sprintf("format=%d", format), func(t *testing.T) {
			path, _ := writeManyChunksFixture(t, format)

			mmap, err := NewMmapFileReader(path)
			require.NoError(t, err)
			ref := allSeriesRefs(t, mmap)[0]
			require.NoError(t, mmap.Close())

			corruptSeriesRecord(t, path, ref)

			for _, rc := range allReaderConstructors() {
				t.Run(rc.name, func(t *testing.T) {
					r, err := rc.open(path)
					require.NoError(t, err)
					t.Cleanup(func() { require.NoError(t, r.Close()) })

					scan := scanFor(t, r)

					var lbls labels.Labels
					var chunks []ChunkMeta
					_, err = scan.Series(ref, 0, math.MaxInt64, &lbls, &chunks)
					require.ErrorContains(t, err, "invalid checksum")

					_, _, err = scan.ChunkStats(ref, 0, math.MaxInt64, &lbls, nil)
					require.ErrorContains(t, err, "invalid checksum")
				})
			}
		})
	}
}

// corruptSeriesRecord flips a bit in the content of the series record for ref,
// leaving its uvarint length prefix and its trailing CRC32 intact so the record
// still parses but no longer matches its checksum.
func corruptSeriesRecord(t *testing.T, path string, ref storage.SeriesRef) {
	t.Helper()
	corruptFileBytes(t, path, func(b []byte) {
		offset := seriesOffset(ref)
		_, lenBytes := binary.Uvarint(b[offset:])
		require.Positive(t, lenBytes)
		b[offset+lenBytes] ^= 0x01
	})
}

// TestReaders_RejectsOutOfRangeSeriesRef asserts that a series ref pointing
// past the end of the index is rejected rather than read as garbage.
func TestReaders_RejectsOutOfRangeSeriesRef(t *testing.T) {
	path, _ := writeManyChunksFixture(t, FormatV4)

	fileInfo, err := os.Stat(path)
	require.NoError(t, err)
	// Series refs are file offsets divided by 16, so this one addresses well
	// past the end of the file.
	ref := storage.SeriesRef(fileInfo.Size()/16 + 100)

	for _, rc := range allReaderConstructors() {
		t.Run(rc.name, func(t *testing.T) {
			r, err := rc.open(path)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, r.Close()) })

			scan := scanFor(t, r)

			var lbls labels.Labels
			var chunks []ChunkMeta
			_, err = scan.Series(ref, 0, math.MaxInt64, &lbls, &chunks)
			require.ErrorContains(t, err, "invalid size")

			_, _, err = scan.ChunkStats(ref, 0, math.MaxInt64, &lbls, nil)
			require.ErrorContains(t, err, "invalid size")
		})
	}
}

// TestReaders_RawFileReaderIndependence verifies that RawFileReader
// returns an independent reader each call.
func TestReaders_RawFileReaderIndependence(t *testing.T) {
	for _, rc := range allReaderConstructors() {
		t.Run(rc.name, func(t *testing.T) {
			path := writeCrossCheckFixture(t, FormatV3)

			r, err := rc.open(path)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, r.Close()) })

			want, err := os.ReadFile(path)
			require.NoError(t, err)

			rf1, err := r.RawFileReader()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, rf1.Close()) })
			rf2, err := r.RawFileReader()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, rf2.Close()) })

			// Advancing rf1 must not consume from rf2 — they're
			// independent cursors over the same file content.
			half := len(want) / 2
			buf := make([]byte, half)
			n, err := io.ReadFull(rf1, buf)
			require.NoError(t, err)
			require.Equal(t, half, n)
			require.Equal(t, want[:half], buf)

			got2, err := io.ReadAll(rf2)
			require.NoError(t, err)
			require.Equal(t, want, got2)

			// The reader itself must still work after the raw readers
			// have been used — the underlying access path is separate.
			require.Equal(t, FormatV3, r.Version())
		})
	}
}

// TestStreamLabels_MatchesMmap cross-checks the streaming LabelNames,
// LabelValues, LabelValueFor and LabelNamesFor against the mmap reader.
func TestStreamLabels_MatchesMmap(t *testing.T) {
	fixtures := map[string]func(t *testing.T, format int) string{
		"many values for one name": writeManySymbolsFixture,
		"several names": func(t *testing.T, format int) string {
			path, _ := writeManyChunksFixture(t, format)
			return path
		},
	}

	for name, writeFixture := range fixtures {
		t.Run(name, func(t *testing.T) {
			for _, format := range []int{FormatV3, FormatV4} {
				t.Run(fmt.Sprintf("format=%d", format), func(t *testing.T) {
					mmap, stream := openBothReaders(t, writeFixture(t, format))
					mmapScan, streamScan := scanBoth(t, mmap, stream)

					mmapNames, err := mmap.LabelNames()
					require.NoError(t, err)
					streamNames, err := stream.LabelNames()
					require.NoError(t, err)
					require.Equal(t, mmapNames, streamNames)
					require.NotEmpty(t, streamNames)

					// Every name the index holds, plus the all-postings key
					// (which LabelNames omits but LabelValues still answers for)
					// and a name that doesn't exist.
					for _, labelName := range slices.Concat(mmapNames, []string{"", "does-not-exist"}) {
						mmapValues, err := mmap.LabelValues(labelName)
						require.NoError(t, err)
						streamValues, err := stream.LabelValues(labelName)
						require.NoError(t, err)
						require.Equal(t, mmapValues, streamValues)
					}

					refs := allSeriesRefs(t, mmap)
					require.NotEmpty(t, refs)

					// LabelNamesFor over every ref at once
					mmapNamesFor, err := mmapScan.LabelNamesFor(refs...)
					require.NoError(t, err)
					streamNamesFor, err := streamScan.LabelNamesFor(refs...)
					require.NoError(t, err)
					require.Equal(t, mmapNamesFor, streamNamesFor)

					for _, ref := range refs {
						// LabelNamesFor over every ref at once
						mmapNamesFor, err := mmapScan.LabelNamesFor(ref)
						require.NoError(t, err)
						streamNamesFor, err := streamScan.LabelNamesFor(ref)
						require.NoError(t, err)
						require.Equal(t, mmapNamesFor, streamNamesFor)

						// Both a label the series carries and one it doesn't,
						// the latter being reported as storage.ErrNotFound.
						for _, labelName := range slices.Concat(mmapNames, []string{"does-not-exist"}) {
							mmapValue, mmapErr := mmapScan.LabelValueFor(ref, labelName)
							streamValue, streamErr := streamScan.LabelValueFor(ref, labelName)
							require.Equal(t, mmapErr, streamErr)
							require.Equal(t, mmapValue, streamValue)
						}
					}

					// LabelNamesFor rejects a ref past the end of the file the
					// same way in both readers.
					_, err = streamScan.LabelNamesFor(refs[len(refs)-1] + 1<<20)
					require.Error(t, err)
					_, err = mmapScan.LabelNamesFor(refs[len(refs)-1] + 1<<20)
					require.Error(t, err)
				})
			}
		})
	}
}

func TestStreamLabels_RejectsMatchers(t *testing.T) { // Neither implementation supports matchers
	mmap, stream := openBothReaders(t, writeManySymbolsFixture(t, FormatV4))

	matcher := labels.MustNewMatcher(labels.MatchEqual, "id", "v0000")

	_, mmapErr := mmap.LabelValues("id", matcher)
	_, streamErr := stream.LabelValues("id", matcher)
	require.Error(t, streamErr)
	require.Equal(t, mmapErr.Error(), streamErr.Error())

	_, mmapErr = mmap.LabelNames(matcher)
	_, streamErr = stream.LabelNames(matcher)
	require.Error(t, streamErr)
	require.Equal(t, mmapErr.Error(), streamErr.Error())
}

func TestStreamLabels_RejectsCorruptSeriesRecord(t *testing.T) {
	path := writeManySymbolsFixture(t, FormatV4)

	// Pick a ref from the intact file, then corrupt it before opening the
	// readers under test.
	reader, err := NewMmapFileReader(path)
	require.NoError(t, err)
	ref := allSeriesRefs(t, reader)[0]
	require.NoError(t, reader.Close())
	corruptSeriesRecord(t, path, ref)

	mmap, stream := openBothReaders(t, path)
	mmapScan, streamScan := scanBoth(t, mmap, stream)

	_, mmapErr := mmapScan.LabelNamesFor(ref)
	_, streamErr := streamScan.LabelNamesFor(ref)
	require.ErrorContains(t, mmapErr, "invalid checksum")
	require.ErrorContains(t, streamErr, "invalid checksum")
	_, mmapErr = mmapScan.LabelValueFor(ref, "id")
	_, streamErr = streamScan.LabelValueFor(ref, "id")
	require.ErrorContains(t, mmapErr, "invalid checksum")
	require.ErrorContains(t, streamErr, "invalid checksum")

}

// TestStreamReader_SeriesScanRefOrders asserts a scan agrees with the mmap
// reader whatever order the refs arrive in.
func TestStreamReader_SeriesScanRefOrders(t *testing.T) {
	for _, format := range []int{FormatV3, FormatV4} {
		t.Run(fmt.Sprintf("format=%d", format), func(t *testing.T) {
			path, _ := writeManyChunksFixture(t, format)
			mmap, stream := openBothReaders(t, path)
			mmapScan := scanFor(t, mmap)

			ascending := allSeriesRefs(t, mmap)
			require.NotEmpty(t, ascending)

			descending := slices.Clone(ascending)
			slices.Reverse(descending)

			// A fixed shuffle, plus repeats, so the scan sees both backwards
			// steps and a ref it has already read.
			shuffled := slices.Clone(ascending)
			rnd := rand.New(rand.NewSource(1))
			rnd.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
			shuffled = append(shuffled, ascending[0], ascending[len(ascending)-1], ascending[0])

			for name, refs := range map[string][]storage.SeriesRef{
				"ascending":  ascending,
				"descending": descending,
				"shuffled":   shuffled,
			} {
				t.Run(name, func(t *testing.T) {
					scan := stream.NewSeriesScan()
					defer func() { require.NoError(t, scan.Close()) }()

					// Collect everything the scan produces before comparing, so a
					// record whose bytes were invalidated by a later read shows up
					// as a mismatch rather than being compared while still valid.
					type result struct {
						fingerprint uint64
						lbls        labels.Labels
						chunks      []ChunkMeta
						stats       ChunkStats
						labelValue  string
					}
					got := make([]result, 0, len(refs))
					for _, ref := range refs {
						var r result
						var err error
						r.fingerprint, err = scan.Series(ref, 0, math.MaxInt64, &r.lbls, &r.chunks)
						require.NoError(t, err)
						r.chunks = slices.Clone(r.chunks)

						var statsLabels labels.Labels
						_, r.stats, err = scan.ChunkStats(ref, 0, math.MaxInt64, &statsLabels, nil)
						require.NoError(t, err)

						r.labelValue, err = scan.LabelValueFor(ref, "id")
						require.NoError(t, err)

						got = append(got, r)
					}

					for i, ref := range refs {
						var wantLabels labels.Labels
						var wantChunks []ChunkMeta
						wantFingerprint, err := mmapScan.Series(ref, 0, math.MaxInt64, &wantLabels, &wantChunks)
						require.NoError(t, err)
						require.Equal(t, wantFingerprint, got[i].fingerprint)
						require.Equal(t, wantLabels, got[i].lbls)
						require.Equal(t, wantChunks, got[i].chunks)

						var statsLabels labels.Labels
						_, wantStats, err := mmapScan.ChunkStats(ref, 0, math.MaxInt64, &statsLabels, nil)
						require.NoError(t, err)
						require.Equal(t, wantStats, got[i].stats)

						wantValue, err := mmapScan.LabelValueFor(ref, "id")
						require.NoError(t, err)
						require.Equal(t, wantValue, got[i].labelValue)
					}
				})
			}
		})
	}
}

// TestStreamReader_SeriesScanRecoversFromError asserts a scan that hits a bad
// record keeps working for the records after it.
func TestStreamReader_SeriesScanRecoversFromError(t *testing.T) {
	path, _ := writeManyChunksFixture(t, FormatV4)

	mmap, err := NewMmapFileReader(path)
	require.NoError(t, err)
	refs := allSeriesRefs(t, mmap)
	require.NoError(t, mmap.Close())
	require.Greater(t, len(refs), 2)

	corruptSeriesRecord(t, path, refs[0])

	mmap, stream := openBothReaders(t, path)
	mmapScan, scan := scanBoth(t, mmap, stream)

	var lbls labels.Labels
	var chunks []ChunkMeta

	// A corrupt record, then a ref past the end of the file, then good records:
	// each failure mode must leave the scan usable.
	_, err = scan.Series(refs[0], 0, math.MaxInt64, &lbls, &chunks)
	require.ErrorContains(t, err, "invalid checksum")

	_, err = scan.Series(refs[len(refs)-1]+1<<20, 0, math.MaxInt64, &lbls, &chunks)
	require.ErrorContains(t, err, "invalid size")

	for _, ref := range refs[1:] {
		var wantLabels labels.Labels
		var wantChunks []ChunkMeta
		wantFingerprint, err := mmapScan.Series(ref, 0, math.MaxInt64, &wantLabels, &wantChunks)
		require.NoError(t, err)

		gotFingerprint, err := scan.Series(ref, 0, math.MaxInt64, &lbls, &chunks)
		require.NoError(t, err)
		require.Equal(t, wantFingerprint, gotFingerprint)
		require.Equal(t, wantLabels, lbls)
		require.Equal(t, wantChunks, chunks)
	}
}

// TestStreamReader_ConcurrentSeriesScans asserts that scans taken from one
// reader are independent.
func TestStreamReader_ConcurrentSeriesScans(t *testing.T) {
	path, _ := writeManyChunksFixture(t, FormatV4)
	mmap, stream := openBothReaders(t, path)

	refs := allSeriesRefs(t, mmap)
	require.NotEmpty(t, refs)

	mmapScan := scanFor(t, mmap)
	want := make([]labels.Labels, len(refs))
	for i, ref := range refs {
		var chunks []ChunkMeta
		_, err := mmapScan.Series(ref, 0, math.MaxInt64, &want[i], &chunks)
		require.NoError(t, err)
	}

	const goroutines = 8
	errs := make(chan error, goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			errs <- func() error {
				scan := stream.NewSeriesScan()
				defer func() { _ = scan.Close() }()

				for round := 0; round < 20; round++ {
					for i, ref := range refs {
						var lbls labels.Labels
						var chunks []ChunkMeta
						if _, err := scan.Series(ref, 0, math.MaxInt64, &lbls, &chunks); err != nil {
							return err
						}
						if !labels.Equal(want[i], lbls) {
							return fmt.Errorf("ref %d: got %s, want %s", ref, lbls, want[i])
						}
					}
				}
				return nil
			}()
		}()
	}
	for g := 0; g < goroutines; g++ {
		require.NoError(t, <-errs)
	}
}

// BenchmarkSeriesIteration reads every series matched by a broad postings list.
func BenchmarkSeriesIteration(b *testing.B) {
	path := writeBenchmarkFixture(b, 200_000, 3)
	r, err := NewStreamFileReader(path, DefaultStreamOptions())
	require.NoError(b, err)
	b.Cleanup(func() { require.NoError(b, r.Close()) })

	postings, err := r.Postings("c", nil, "3")
	require.NoError(b, err)
	var refs []storage.SeriesRef
	for postings.Next() {
		refs = append(refs, postings.At())
	}
	require.NoError(b, postings.Err())
	require.NotEmpty(b, refs)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scan := r.NewSeriesScan()
		var lbls labels.Labels
		chunks := make([]ChunkMeta, 0, 8)
		for _, ref := range refs {
			chunks = chunks[:0]
			if _, err := scan.Series(ref, 0, math.MaxInt64, &lbls, &chunks); err != nil {
				b.Fatal(err)
			}
		}
		if err := scan.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

// corruptFileBytes reads the whole file, hands the bytes to mutate, and
// writes them back. Used to inject targeted corruption into fixture
// files for the reject-* tests.
func corruptFileBytes(t *testing.T, path string, mutate func(b []byte)) {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	mutate(b)
	require.NoError(t, os.WriteFile(path, b, 0600))
}

func requireLabelsEqual(t *testing.T, mmap, stream Reader) {
	t.Helper()
	mmapLabelNames, err := mmap.LabelNames()
	require.NoError(t, err)
	streamLabelNames, err := stream.LabelNames()
	require.NoError(t, err)
	require.Equal(t, mmapLabelNames, streamLabelNames)

	for _, labelName := range mmapLabelNames {
		mmapLabelValues, err := mmap.LabelValues(labelName)
		require.NoError(t, err)
		streamLabelValues, err := stream.LabelValues(labelName)
		require.NoError(t, err)
		require.Equal(t, mmapLabelValues, streamLabelValues)
	}
}

func requirePostingsSeriesEqual(t *testing.T, mmap, stream Reader) {
	t.Helper()
	mmapScan, streamScan := scanBoth(t, mmap, stream)

	labelNames, err := mmap.LabelNames()
	require.NoError(t, err)
	for _, labelName := range labelNames {
		labelValues, err := mmap.LabelValues(labelName)
		require.NoError(t, err)
		for _, labelValue := range labelValues {
			mmapSeriesRefs := mustPostingsAndDrain(t, mmap, nil, labelName, labelValue)
			streamSeriesRefs := mustPostingsAndDrain(t, stream, nil, labelName, labelValue)
			require.Equal(t, mmapSeriesRefs, streamSeriesRefs)

			for _, seriesRef := range mmapSeriesRefs {
				requireSeriesEqual(t, mmapScan, streamScan, seriesRef, labelName)
			}
		}
	}
}

func requireSeriesEqual(t *testing.T, mmap, stream SeriesScan, seriesRef storage.SeriesRef, labelName string) {
	t.Helper()

	var mmapLabels, streamLabels labels.Labels
	var mmapChecksums, streamChecksums []ChunkMeta
	mmapFingerprint, err := mmap.Series(seriesRef, 0, math.MaxInt64, &mmapLabels, &mmapChecksums)
	require.NoError(t, err)
	streamFingerprint, err := stream.Series(seriesRef, 0, math.MaxInt64, &streamLabels, &streamChecksums)
	require.NoError(t, err)
	require.Equal(t, mmapFingerprint, streamFingerprint)
	require.Equal(t, mmapLabels, streamLabels)
	require.Equal(t, mmapChecksums, streamChecksums)

	mmapLabelValue, err := mmap.LabelValueFor(seriesRef, labelName)
	require.NoError(t, err)
	streamLabelValue, err := stream.LabelValueFor(seriesRef, labelName)
	require.NoError(t, err)
	require.Equal(t, mmapLabelValue, streamLabelValue)

	mmapLabelNames, err := mmap.LabelNamesFor(seriesRef)
	require.NoError(t, err)
	streamLabelNames, err := stream.LabelNamesFor(seriesRef)
	require.NoError(t, err)
	require.Equal(t, mmapLabelNames, streamLabelNames)

	requireChunkStatsEqual(t, mmap, stream, seriesRef, labelName)
}

func requireChunkStatsEqual(t *testing.T, mmap, stream SeriesScan, seriesRef storage.SeriesRef, labelName string) {
	for _, by := range []map[string]struct{}{nil, {labelName: {}}} {
		var mmapLabels, streamLabels labels.Labels
		mmapFingerprints, mmapChunkStats, err := mmap.ChunkStats(seriesRef, 0, math.MaxInt64, &mmapLabels, by)
		require.NoError(t, err)
		streamFingerprints, streamChunkStats, err := stream.ChunkStats(seriesRef, 0, math.MaxInt64, &streamLabels, by)
		require.NoError(t, err)
		require.Equal(t, mmapFingerprints, streamFingerprints)
		require.Equal(t, mmapChunkStats, streamChunkStats)
		require.Equal(t, mmapLabels, streamLabels)
	}
}
