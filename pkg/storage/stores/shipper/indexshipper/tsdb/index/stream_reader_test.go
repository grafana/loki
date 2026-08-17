package index

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
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
		{name: "StreamReader", open: func(p string) (Reader, error) { return NewStreamFileReader(p) }},
	}
}

// writeCrossCheckFixture builds a small but non-trivial index used by
// the cross-check tests: multiple label names, multiple values per
// name, a couple of chunks per series.
func writeCrossCheckFixture(t *testing.T, format int) string {
	t.Helper()
	dir := t.TempDir()
	fileName := filepath.Join(dir, IndexFilename)

	creator, err := NewWriter(context.Background(), format, fileName)
	require.NoError(t, err)

	symbols := []string{"1", "2", "3", "a", "b", "c", "svcA", "svcB"}
	for _, s := range symbols {
		require.NoError(t, creator.AddSymbol(s))
	}

	type entry struct {
		ls     labels.Labels
		chunks []ChunkMeta
	}
	entries := []entry{
		{ls: labels.FromStrings("a", "1", "b", "1", "c", "svcA"), chunks: []ChunkMeta{{Checksum: 1, MinTime: 0, MaxTime: 10, KB: 1, Entries: 1}}},
		{ls: labels.FromStrings("a", "1", "b", "2", "c", "svcA"), chunks: []ChunkMeta{{Checksum: 2, MinTime: 10, MaxTime: 20, KB: 1, Entries: 1}}},
		{ls: labels.FromStrings("a", "2", "b", "3", "c", "svcB"), chunks: []ChunkMeta{{Checksum: 3, MinTime: 20, MaxTime: 30, KB: 1, Entries: 1}}},
	}
	// AddSeries requires ascending fingerprint order.
	sort.Slice(entries, func(i, j int) bool {
		return labels.StableHash(entries[i].ls) < labels.StableHash(entries[j].ls)
	})
	for i, e := range entries {
		require.NoError(t, creator.AddSeries(
			storage.SeriesRef(i+1),
			e.ls,
			model.Fingerprint(labels.StableHash(e.ls)),
			e.chunks...,
		))
	}

	_, err = creator.Close(false)
	require.NoError(t, err)
	return fileName
}

// TestReaders_CrossCheck opens the same fixture with every Reader
// implementation and asserts each method returns identical results to
// the ByteSliceReader baseline. Today StreamReader delegates to
// ByteSliceReader so this passes trivially; the check exists to catch
// regressions as StreamReader gains real, standalone implementations.
func TestReaders_CrossCheck(t *testing.T) {
	for _, format := range []int{FormatV3, FormatV4} {
		t.Run(fmt.Sprintf("format=%d", format), func(t *testing.T) {
			fn := writeCrossCheckFixture(t, format)

			mmap, err := NewMmapFileReader(fn)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, mmap.Close()) })

			stream, err := NewStreamFileReader(fn)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, stream.Close()) })

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

func TestReaders_RejectsCorruptSymbolsChecksum(t *testing.T) {
	for _, rc := range allReaderConstructors() {
		t.Run(rc.name, func(t *testing.T) {
			path := writeCrossCheckFixture(t, FormatV3)

			// Locate the symbols section from the TOC
			reader, err := NewStreamFileReader(path)
			require.NoError(t, err)
			symbolsOffset := int(reader.toc.Symbols)
			require.NoError(t, reader.Close())

			corruptFileBytes(t, path, func(b []byte) {
				// Flip the first byte of the section's content (skipping the first 4 bytes,
				// which is the size of the section).
				b[symbolsOffset+4] ^= 0x01
			})

			_, err = rc.open(path)
			require.Error(t, err)
			require.Contains(t, err.Error(), "invalid checksum")
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
	labelNames, err := mmap.LabelNames()
	require.NoError(t, err)
	for _, labelName := range labelNames {
		labelValues, err := mmap.LabelValues(labelName)
		require.NoError(t, err)
		for _, labelValue := range labelValues {
			mmapSeriesRefs := getPostings(t, mmap, labelName, labelValue)
			streamSeriesRefs := getPostings(t, stream, labelName, labelValue)
			require.Equal(t, mmapSeriesRefs, streamSeriesRefs)

			for _, seriesRef := range mmapSeriesRefs {
				requireSeriesEqual(t, mmap, stream, seriesRef, labelName)
			}
		}
	}
}

func getPostings(t *testing.T, reader Reader, labelName, labelValue string) []storage.SeriesRef {
	t.Helper()
	p, err := reader.Postings(labelName, nil, labelValue)
	require.NoError(t, err)
	var refs []storage.SeriesRef
	for p.Next() {
		refs = append(refs, p.At())
	}
	require.NoError(t, p.Err())
	return refs
}

func requireSeriesEqual(t *testing.T, mmap, stream Reader, seriesRef storage.SeriesRef, labelName string) {
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

func requireChunkStatsEqual(t *testing.T, mmap Reader, stream Reader, seriesRef storage.SeriesRef, labelName string) {
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
