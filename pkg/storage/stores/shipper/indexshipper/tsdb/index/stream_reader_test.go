package index

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"
)

// buildStreamReaderFixture writes a small index into a temp dir and returns
// its path. Series are inserted in fingerprint order (Creator requires this).
// Kept as a helper so future streaming-reader tests can share the same setup.
func buildStreamReaderFixture(t *testing.T, version int) string {
	t.Helper()

	dir := t.TempDir()
	fn := filepath.Join(dir, IndexFilename)

	iw, err := NewWriter(context.Background(), version, fn)
	require.NoError(t, err)

	series := []labels.Labels{
		labels.FromStrings("a", "1", "b", "1"),
		labels.FromStrings("a", "1", "b", "2"),
		labels.FromStrings("a", "1", "b", "3"),
		labels.FromStrings("a", "2", "b", "1"),
		labels.FromStrings("a", "2", "b", "2"),
	}
	sort.Slice(series, func(i, j int) bool {
		return labels.StableHash(series[i]) < labels.StableHash(series[j])
	})

	for _, s := range []string{"1", "2", "3", "a", "b"} {
		require.NoError(t, iw.AddSymbol(s))
	}
	for i, s := range series {
		require.NoError(t, iw.AddSeries(storage.SeriesRef(i+1), s, model.Fingerprint(labels.StableHash(s))))
	}

	_, err = iw.Close(false)
	require.NoError(t, err)
	return fn
}

// TestStreamReader_MetadataMatchesMmap covers Phase 2 Bucket A proposals
// P2.A1 + P2.A2: the streaming reader must open, expose file format
// version, and return the same TOC-derived bounds/checksum as the mmap
// reader. Query surface methods are covered by later tests as they land.
func TestStreamReader_MetadataMatchesMmap(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version int
	}{
		{"V3", FormatV3},
		{"V4", FormatV4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := buildStreamReaderFixture(t, tc.version)

			mmap, err := NewFileReader(path)
			require.NoError(t, err)
			t.Cleanup(func() { _ = mmap.Close() })

			stream, err := NewStreamFileReader(path)
			require.NoError(t, err)
			t.Cleanup(func() { _ = stream.Close() })

			require.Equal(t, mmap.Version(), stream.Version(), "version")
			require.Equal(t, mmap.Checksum(), stream.Checksum(), "checksum")

			mFrom, mThrough := mmap.Bounds()
			sFrom, sThrough := stream.Bounds()
			require.Equal(t, mFrom, sFrom, "bounds from")
			require.Equal(t, mThrough, sThrough, "bounds through")
		})
	}
}

// TestStreamReader_SymbolsMatchesMmap covers P2.A3: symbol iteration,
// lookup by ordinal, reverse lookup by string, and the reported symbol
// table size all match the mmap reader.
func TestStreamReader_SymbolsMatchesMmap(t *testing.T) {
	path := buildStreamReaderFixture(t, FormatV3)

	mmap, err := NewFileReader(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = mmap.Close() })

	stream, err := NewStreamFileReader(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	// Iteration order must match.
	var mmapSyms, streamSyms []string
	mIter := mmap.Symbols()
	for mIter.Next() {
		mmapSyms = append(mmapSyms, mIter.At())
	}
	require.NoError(t, mIter.Err())

	sIter := stream.Symbols()
	for sIter.Next() {
		streamSyms = append(streamSyms, sIter.At())
	}
	require.NoError(t, sIter.Err())
	require.Equal(t, mmapSyms, streamSyms, "symbol iteration")

	// Lookup by ordinal.
	for i, want := range mmapSyms {
		got, err := stream.lookupSymbol(uint32(i))
		require.NoError(t, err, "lookup ordinal %d (%q)", i, want)
		require.Equal(t, want, got, "lookup ordinal %d", i)
	}

	// Reverse lookup.
	for i, sym := range mmapSyms {
		ord, err := stream.symbols.ReverseLookup(sym)
		require.NoError(t, err, "reverse %q", sym)
		require.Equal(t, uint32(i), ord, "reverse ordinal for %q", sym)
	}

	// SymbolTableSize matches (on-heap footprint of sparse offsets).
	require.Equal(t, mmap.SymbolTableSize(), stream.SymbolTableSize(), "SymbolTableSize")
}

// TestStreamReader_SymbolsScaling exercises the sparse-index scan across
// symbolFactor boundaries. We seed just over 2*symbolFactor unique symbols
// so the offset table has multiple entries and the intra-group scan runs.
func TestStreamReader_SymbolsScaling(t *testing.T) {
	dir := t.TempDir()
	fn := filepath.Join(dir, IndexFilename)

	iw, err := NewWriter(context.Background(), FormatV3, fn)
	require.NoError(t, err)

	const numSyms = 2*symbolFactor + 7
	syms := make([]string, 0, numSyms+2)
	for i := 0; i < numSyms; i++ {
		s := "sym-" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+i))
		syms = append(syms, s)
	}
	// Plus the symbols we need for the single series below.
	syms = append(syms, "__name__", "val")
	sort.Strings(syms)
	for _, s := range syms {
		require.NoError(t, iw.AddSymbol(s))
	}
	// Creator refuses an empty index; add one series to make the file valid.
	require.NoError(t, iw.AddSeries(1, labels.FromStrings("__name__", "val"), model.Fingerprint(labels.StableHash(labels.FromStrings("__name__", "val")))))

	_, err = iw.Close(false)
	require.NoError(t, err)

	stream, err := NewStreamFileReader(fn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	// Enumerate all symbols and cross-check both lookup directions.
	var allSyms []string
	it := stream.Symbols()
	for it.Next() {
		allSyms = append(allSyms, it.At())
	}
	require.NoError(t, it.Err())
	require.GreaterOrEqual(t, len(allSyms), numSyms)

	for i, s := range allSyms {
		got, err := stream.lookupSymbol(uint32(i))
		require.NoError(t, err, "lookupSymbol(%d) = ? (want %q)", i, s)
		require.Equal(t, s, got)

		ord, err := stream.symbols.ReverseLookup(s)
		require.NoError(t, err, "reverse %q", s)
		require.Equal(t, uint32(i), ord)
	}
}

// TestStreamReader_PostingsMatchesMmap covers P2.A4 — every label/value
// combination produces the same postings iterator as the mmap reader.
func TestStreamReader_PostingsMatchesMmap(t *testing.T) {
	path := buildStreamReaderFixture(t, FormatV3)

	mmap, err := NewFileReader(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = mmap.Close() })

	stream, err := NewStreamFileReader(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	mmapNames, err := mmap.LabelNames()
	require.NoError(t, err)

	for _, name := range mmapNames {
		vals, err := mmap.LabelValues(name)
		require.NoError(t, err)
		for _, v := range vals {
			mmapP, err := mmap.Postings(name, nil, v)
			require.NoError(t, err)
			streamP, err := stream.Postings(name, nil, v)
			require.NoError(t, err)

			var mmapRefs, streamRefs []storage.SeriesRef
			for mmapP.Next() {
				mmapRefs = append(mmapRefs, mmapP.At())
			}
			require.NoError(t, mmapP.Err())
			for streamP.Next() {
				streamRefs = append(streamRefs, streamP.At())
			}
			require.NoError(t, streamP.Err())
			require.Equal(t, mmapRefs, streamRefs, "postings for %s=%s differ", name, v)
		}
	}

	// Missing label should return EmptyPostings, not an error.
	sp, err := stream.Postings("no-such-name", nil, "x")
	require.NoError(t, err)
	require.False(t, sp.Next())
	require.NoError(t, sp.Err())

	// Missing value should return no refs (but a valid iterator).
	sp, err = stream.Postings("a", nil, "no-such-value")
	require.NoError(t, err)
	require.False(t, sp.Next())
	require.NoError(t, sp.Err())
}

// TestStreamReader_SeriesMatchesMmap covers P2.A5 — for every posting ref
// on the fixture, Series() and ChunkStats() return the same labels/chunks
// as the mmap reader. Runs on V3 and V4 fixtures to exercise the paged
// chunk-meta and IngestedAt encodings respectively.
func TestStreamReader_SeriesMatchesMmap(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version int
	}{
		{"V3", FormatV3},
		{"V4", FormatV4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := buildStreamReaderFixture(t, tc.version)

			mmap, err := NewFileReader(path)
			require.NoError(t, err)
			t.Cleanup(func() { _ = mmap.Close() })

			stream, err := NewStreamFileReader(path)
			require.NoError(t, err)
			t.Cleanup(func() { _ = stream.Close() })

			names, err := mmap.LabelNames()
			require.NoError(t, err)

			for _, name := range names {
				vals, err := mmap.LabelValues(name)
				require.NoError(t, err)
				for _, v := range vals {
					p, err := mmap.Postings(name, nil, v)
					require.NoError(t, err)

					for p.Next() {
						ref := p.At()

						var mLbls labels.Labels
						var mChks []ChunkMeta
						mFP, err := mmap.Series(ref, 0, 0, &mLbls, &mChks)
						require.NoError(t, err)

						var sLbls labels.Labels
						var sChks []ChunkMeta
						sFP, err := stream.Series(ref, 0, 0, &sLbls, &sChks)
						require.NoError(t, err)

						require.Equal(t, mFP, sFP, "fingerprint for ref %d", ref)
						require.Equal(t, mLbls, sLbls, "labels for ref %d", ref)
						require.Equal(t, mChks, sChks, "chunks for ref %d", ref)

						// ChunkStats with an all-by set that includes every
						// label captured on the series.
						by := make(map[string]struct{})
						mLbls.Range(func(l labels.Label) { by[l.Name] = struct{}{} })
						var mLbls2 labels.Labels
						mFP2, mStats, err := mmap.ChunkStats(ref, 0, int64(1<<62), &mLbls2, by)
						require.NoError(t, err)
						var sLbls2 labels.Labels
						sFP2, sStats, err := stream.ChunkStats(ref, 0, int64(1<<62), &sLbls2, by)
						require.NoError(t, err)
						require.Equal(t, mFP2, sFP2, "chunk-stats fingerprint for ref %d", ref)
						require.Equal(t, mStats, sStats, "chunk-stats for ref %d", ref)
						require.Equal(t, mLbls2, sLbls2, "chunk-stats labels for ref %d", ref)
					}
					require.NoError(t, p.Err())
				}
			}
		})
	}
}

// TestStreamReader_LabelsMatchesMmap covers P2.A6 — LabelNames,
// LabelValues, LabelValueFor, LabelNamesFor.
func TestStreamReader_LabelsMatchesMmap(t *testing.T) {
	path := buildStreamReaderFixture(t, FormatV3)

	mmap, err := NewFileReader(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = mmap.Close() })

	stream, err := NewStreamFileReader(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	// LabelNames.
	mmapNames, err := mmap.LabelNames()
	require.NoError(t, err)
	streamNames, err := stream.LabelNames()
	require.NoError(t, err)
	require.Equal(t, mmapNames, streamNames, "LabelNames")

	// LabelValues per name.
	for _, name := range mmapNames {
		mV, err := mmap.LabelValues(name)
		require.NoError(t, err)
		sV, err := stream.LabelValues(name)
		require.NoError(t, err)
		require.Equal(t, mV, sV, "LabelValues for %q", name)
	}

	// Missing name -> nil, nil.
	sV, err := stream.LabelValues("no-such-name")
	require.NoError(t, err)
	require.Nil(t, sV)

	// LabelValueFor + LabelNamesFor: pick every ref we can find.
	var refs []storage.SeriesRef
	for _, name := range mmapNames {
		vals, err := mmap.LabelValues(name)
		require.NoError(t, err)
		for _, v := range vals {
			p, err := mmap.Postings(name, nil, v)
			require.NoError(t, err)
			for p.Next() {
				refs = append(refs, p.At())
			}
			require.NoError(t, p.Err())
		}
	}
	// Dedup refs (postings from different labels return the same series).
	sort.Slice(refs, func(i, j int) bool { return refs[i] < refs[j] })
	out := refs[:0]
	for i, r := range refs {
		if i == 0 || r != refs[i-1] {
			out = append(out, r)
		}
	}
	refs = out

	for _, ref := range refs {
		var mLbls labels.Labels
		var mChks []ChunkMeta
		_, err := mmap.Series(ref, 0, 0, &mLbls, &mChks)
		require.NoError(t, err)

		mLbls.Range(func(l labels.Label) {
			mVal, mErr := mmap.LabelValueFor(ref, l.Name)
			sVal, sErr := stream.LabelValueFor(ref, l.Name)
			require.Equal(t, mErr, sErr, "LabelValueFor(%d,%q) err", ref, l.Name)
			require.Equal(t, mVal, sVal, "LabelValueFor(%d,%q) value", ref, l.Name)
		})

		mNames, err := mmap.LabelNamesFor(ref)
		require.NoError(t, err)
		sNames, err := stream.LabelNamesFor(ref)
		require.NoError(t, err)
		require.Equal(t, mNames, sNames, "LabelNamesFor(%d)", ref)
	}

	// LabelNamesFor over multiple refs at once.
	if len(refs) > 1 {
		mNames, err := mmap.LabelNamesFor(refs...)
		require.NoError(t, err)
		sNames, err := stream.LabelNamesFor(refs...)
		require.NoError(t, err)
		require.Equal(t, mNames, sNames, "LabelNamesFor(all)")
	}
}

// TestStreamReader_RejectsCorruptMagic ensures header validation runs.
func TestStreamReader_RejectsCorruptMagic(t *testing.T) {
	path := buildStreamReaderFixture(t, FormatV3)

	// Verify open works on a healthy fixture first.
	r, err := NewStreamFileReader(path)
	require.NoError(t, err)
	require.NoError(t, r.Close())

	// Stomp the magic bytes.
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteAt([]byte{0xDE, 0xAD, 0xBE, 0xEF}, 0)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	_, err = NewStreamFileReader(path)
	require.Error(t, err)
}
