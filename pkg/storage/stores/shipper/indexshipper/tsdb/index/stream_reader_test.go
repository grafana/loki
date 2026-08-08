package index

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"
)

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
			require.Equal(t, mmap.SymbolTableSize(), stream.SymbolTableSize())
			require.Equal(t, mmap.Size(), stream.Size())

			baseMin, baseMax := mmap.Bounds()
			rMin, rMax := stream.Bounds()
			require.Equal(t, baseMin, rMin)
			require.Equal(t, baseMax, rMax)

			requireSymbolsEqual(t, mmap, stream)
			requireLabelsEqual(t, mmap, stream)
			requirePostingsSeriesEqual(t, mmap, stream)
			requirePostingsRangesEqual(t, mmap, stream)
		})
	}
}

func requireSymbolsEqual(t *testing.T, mmap, stream Reader) {
	t.Helper()
	mmapSymbols := getSymbols(t, mmap)
	streamSymbols := getSymbols(t, stream)
	require.Equal(t, mmapSymbols, streamSymbols)
}

func getSymbols(t *testing.T, reader Reader) []string {
	t.Helper()
	var out []string
	symbols := reader.Symbols()
	for symbols.Next() {
		out = append(out, symbols.At())
	}
	require.NoError(t, symbols.Err())
	return out
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

func requirePostingsRangesEqual(t *testing.T, base, other Reader) {
	t.Helper()
	baseRanges, err := base.PostingsRanges()
	require.NoError(t, err)
	otherRanges, err := other.PostingsRanges()
	require.NoError(t, err)
	require.Equal(t, baseRanges, otherRanges)
}
