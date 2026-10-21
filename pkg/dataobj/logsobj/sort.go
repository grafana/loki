package logsobj

import (
	"context"
	"fmt"
	"slices"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/dataobj/sortmerge"
)

// rankedSortKey is the sort sidecar of [Builder.FlushAndSort] for one stream. rank is the rank in this object after sorting by
// SortKey; independently written objects do not share rank.
type rankedSortKey struct {
	rank int64
	streams.SortKey
}

// emptyRankedSortKey returns an unranked sort key (rank set to 0) for a label set.
// SortKey fields are calculated from input labels ls.
func emptyRankedSortKey(ls labels.Labels, schemaLabels []string, shardLayout streams.ShardLayout) (rankedSortKey, error) {
	schemaKey, err := ComputeSchemaKey(ls, schemaLabels)
	if err != nil {
		return rankedSortKey{}, err
	}

	return rankedSortKey{
		SortKey: shardLayout.NewSortKey(ls, schemaKey),
	}, nil
}

// TargetSortLayout returns the physical logs layout produced for schemaLabels
// and shardLayout.
func TargetSortLayout(schemaLabels []string, shardLayout streams.ShardLayout) logs.SortLayout {
	return logs.SortLayout{
		SchemaLabels: slices.Clone(schemaLabels),
		StreamOrder:  logs.StreamOrderStableHashV1,
		ShardCount:   shardLayout.Buckets(),
	}
}

// EqualSortLayout reports whether two physical logs layouts are identical.
func EqualSortLayout(a, b logs.SortLayout) bool {
	return slices.Equal(a.SchemaLabels, b.SchemaLabels) &&
		a.StreamOrder == b.StreamOrder &&
		a.ShardCount == b.ShardCount
}

// sortKeys extracts the sort-tuple column for the k-way merge comparator.
func sortKeys(remap []rankedSortKey) []streams.SortKey {
	out := make([]streams.SortKey, len(remap))
	for i, entry := range remap {
		out[i] = entry.SortKey
	}
	return out
}

// mergeAndRemapLogsIter merges sections which are ordered by the sort keys in
// remap, injects sort-key sidecars, and rewrites their stream IDs to rank.
func mergeAndRemapLogsIter(ctx context.Context, sections []*dataobj.Section, remap []rankedSortKey) (result.Seq[logs.Record], error) {
	iter, err := sortmerge.SchemaSortedIterator(ctx, sections, sortKeys(remap))
	if err != nil {
		return nil, err
	}

	return result.Iter(func(yield func(logs.Record) bool) error {
		for res := range iter {
			rec, err := res.Value()
			if err != nil {
				return err
			}
			oldStreamID := rec.StreamID
			if oldStreamID <= 0 || oldStreamID >= int64(len(remap)) || remap[oldStreamID].rank == 0 {
				return fmt.Errorf("missing stream ID remap for stream ID %d", oldStreamID)
			}
			entry := remap[oldStreamID]
			rec.SchemaKey = entry.SchemaKey
			rec.ShardBucket = entry.ShardBucket
			rec.StreamHash = entry.Hash
			rec.StreamID = entry.rank
			if !yield(rec) {
				return nil
			}
		}
		return nil
	}), nil
}
