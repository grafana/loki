package index

import (
	"context"
	"path/filepath"
	"sort"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"
)

// TestBufferedFileReader_MatchesMmap builds a small index and reads it back
// two ways — mmap-backed NewFileReader and heap-backed NewBufferedFileReader —
// then cross-checks that the same queries return the same results. This is the
// Phase 1 correctness gate: the two paths must be equivalent from a caller's
// perspective.
func TestBufferedFileReader_MatchesMmap(t *testing.T) {
	dir := t.TempDir()
	fn := filepath.Join(dir, IndexFilename)

	iw, err := NewWriter(context.Background(), FormatV3, fn)
	require.NoError(t, err)

	series := []labels.Labels{
		labels.FromStrings("a", "1", "b", "1"),
		labels.FromStrings("a", "1", "b", "2"),
		labels.FromStrings("a", "1", "b", "3"),
		labels.FromStrings("a", "2", "b", "1"),
		labels.FromStrings("a", "2", "b", "2"),
	}
	// Creator requires series added in ascending fingerprint order.
	sort.Slice(series, func(i, j int) bool {
		return labels.StableHash(series[i]) < labels.StableHash(series[j])
	})

	symbols := []string{"1", "2", "3", "a", "b"}
	for _, s := range symbols {
		require.NoError(t, iw.AddSymbol(s))
	}

	for i, s := range series {
		require.NoError(t, iw.AddSeries(storage.SeriesRef(i+1), s, model.Fingerprint(labels.StableHash(s))))
	}

	_, err = iw.Close(false)
	require.NoError(t, err)

	mmapReader, err := NewFileReader(fn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = mmapReader.Close() })

	bufReader, err := NewBufferedFileReader(fn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = bufReader.Close() })

	require.Equal(t, mmapReader.Version(), bufReader.Version())
	require.Equal(t, mmapReader.Checksum(), bufReader.Checksum())

	fromA, throughA := mmapReader.Bounds()
	fromB, throughB := bufReader.Bounds()
	require.Equal(t, fromA, fromB)
	require.Equal(t, throughA, throughB)

	mmapLabelNames, err := mmapReader.LabelNames()
	require.NoError(t, err)
	bufLabelNames, err := bufReader.LabelNames()
	require.NoError(t, err)
	require.Equal(t, mmapLabelNames, bufLabelNames)

	for _, name := range mmapLabelNames {
		mmapVals, err := mmapReader.LabelValues(name)
		require.NoError(t, err)
		bufVals, err := bufReader.LabelValues(name)
		require.NoError(t, err)
		require.Equal(t, mmapVals, bufVals, "label values for %q differ", name)

		for _, v := range mmapVals {
			mmapPostings, err := mmapReader.Postings(name, nil, v)
			require.NoError(t, err)
			bufPostings, err := bufReader.Postings(name, nil, v)
			require.NoError(t, err)

			var mmapRefs, bufRefs []storage.SeriesRef
			for mmapPostings.Next() {
				mmapRefs = append(mmapRefs, mmapPostings.At())
			}
			require.NoError(t, mmapPostings.Err())
			for bufPostings.Next() {
				bufRefs = append(bufRefs, bufPostings.At())
			}
			require.NoError(t, bufPostings.Err())
			require.Equal(t, mmapRefs, bufRefs, "postings for %s=%s differ", name, v)

			for _, ref := range mmapRefs {
				var mmapLbls labels.Labels
				var mmapChks []ChunkMeta
				_, err := mmapReader.Series(ref, 0, 0, &mmapLbls, &mmapChks)
				require.NoError(t, err)

				var bufLbls labels.Labels
				var bufChks []ChunkMeta
				_, err = bufReader.Series(ref, 0, 0, &bufLbls, &bufChks)
				require.NoError(t, err)

				require.Equal(t, mmapLbls, bufLbls, "series labels for ref %d differ", ref)
				require.Equal(t, mmapChks, bufChks, "series chunks for ref %d differ", ref)
			}
		}
	}
}
