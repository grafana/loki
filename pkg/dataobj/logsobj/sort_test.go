package logsobj

import (
	"slices"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestNewSortedStreams(t *testing.T) {
	seqOf := func(list ...streams.Stream) result.Seq[streams.Stream] {
		return result.Iter(func(yield func(streams.Stream) bool) error {
			for _, s := range list {
				if !yield(s) {
					return nil
				}
			}
			return nil
		})
	}
	stream := func(id int64, app string, rows int, size int64) streams.Stream {
		return streams.Stream{ID: id, Labels: labels.FromStrings("app", app), Rows: rows, UncompressedSize: size}
	}
	schema := []string{"label:app"}

	t.Run("gives the streams the new IDs 1 to N in sort key order", func(t *testing.T) {
		input := []streams.Stream{stream(1, "c", 0, 0), stream(2, "a", 0, 0), stream(3, "b", 0, 0)}
		keyOf := func(s streams.Stream) streams.SortKey {
			key, err := streamSortKey(s.Labels, schema, streams.DefaultShardLayout())
			require.NoError(t, err)
			return key
		}
		wantOrder := slices.Clone(input)
		slices.SortFunc(wantOrder, func(a, b streams.Stream) int { return streams.CompareSortKey(keyOf(a), keyOf(b)) })

		sortedStreams, err := newSortedStreams(seqOf(input...), "tenant", schema, streams.DefaultShardLayout(), 3)
		require.NoError(t, err)

		require.Len(t, sortedStreams, len(wantOrder))
		for i, want := range wantOrder {
			require.Equal(t, want.ID, sortedStreams[i].oldID, "old ID at position %d", i)
			require.Equal(t, int64(i+1), sortedStreams[i].ID, "new ID at position %d", i)
			require.Equal(t, keyOf(want), sortedStreams[i].key, "sort key at position %d", i)
		}
	})

	t.Run("starts the counters of the streams at zero", func(t *testing.T) {
		sortedStreams, err := newSortedStreams(seqOf(stream(1, "a", 4, 100)), "tenant", schema, streams.DefaultShardLayout(), 1)
		require.NoError(t, err)

		require.Zero(t, sortedStreams[0].Rows)
		require.Zero(t, sortedStreams[0].UncompressedSize)
	})

	t.Run("returns an error when an old ID is out of range", func(t *testing.T) {
		_, err := newSortedStreams(seqOf(stream(1, "a", 0, 0), stream(5, "b", 0, 0)), "tenant", schema, streams.DefaultShardLayout(), 2)
		require.ErrorContains(t, err, "stream id 5 out of range for tenant tenant with 2 streams")
	})

	t.Run("returns an error when the section yields fewer streams than it has rows", func(t *testing.T) {
		_, err := newSortedStreams(seqOf(stream(1, "a", 0, 0), stream(3, "b", 0, 0)), "tenant", schema, streams.DefaultShardLayout(), 3)
		require.ErrorContains(t, err, "the streams section of tenant tenant has 3 rows, but it holds 2 streams")
	})

	t.Run("returns an error when two streams have the same old ID", func(t *testing.T) {
		_, err := newSortedStreams(seqOf(stream(1, "a", 0, 0), stream(1, "b", 0, 0)), "tenant", schema, streams.DefaultShardLayout(), 2)
		require.ErrorContains(t, err, "duplicate stream id for tenant tenant: old id 1")
	})
}

func TestSortKeysByOldID(t *testing.T) {
	t.Run("indexes the sort keys by old ID and leaves index 0 empty", func(t *testing.T) {
		sortedStreams := []sortedStream{
			{oldID: 2, key: streams.SortKey{Hash: 20}},
			{oldID: 3, key: streams.SortKey{Hash: 30}},
			{oldID: 1, key: streams.SortKey{Hash: 10}},
		}

		require.Equal(t, []streams.SortKey{{}, {Hash: 10}, {Hash: 20}, {Hash: 30}}, sortKeysByOldID(sortedStreams))
	})
}
