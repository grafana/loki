package logsobj

import (
	"context"
	"fmt"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/dataobj/sortmerge"
)

// rankedSortKey is the CopyAndSort sidecar for one stream. rank is the rank in this object after sorting by
// StreamOrderKey; independently written objects do not share rank.
type rankedSortKey struct {
	rank int64
	streams.SortKey
}

// emptyRankedSortKey returns an unranked sort key (rank set to 0) for a label set.
// SortKey fields are calculated from input labels ls.
func emptyRankedSortKey(ls labels.Labels, schemaLabels []string) (rankedSortKey, error) {
	schemaKey, err := ComputeSchemaKey(ls, schemaLabels)
	if err != nil {
		return rankedSortKey{}, err
	}

	return rankedSortKey{
		SortKey: streams.NewSortKey(ls, schemaKey),
	}, nil
}

// sortKeys extracts the sort-tuple column for the k-way merge comparator.
func sortKeys(remap []rankedSortKey) []streams.SortKey {
	out := make([]streams.SortKey, len(remap))
	for i, e := range remap {
		out[i] = e.SortKey
	}
	return out
}

// sortedLogsIter merges schema-sorted input sections, injects schema sort
// keys, remaps stream IDs, and returns an iterator suitable for AppendOrdered.
func sortedLogsIter(ctx context.Context, sections []*dataobj.Section, remap []rankedSortKey) (result.Seq[logs.Record], error) {
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
			if oldStreamID <= 0 || oldStreamID >= int64(len(remap)) {
				return fmt.Errorf("missing stream remap for stream ID %d", oldStreamID)
			}
			e := remap[oldStreamID]
			if e.rank == 0 {
				return fmt.Errorf("missing stream ID remap for stream ID %d", oldStreamID)
			}
			rec.SchemaKey = e.SchemaKey
			rec.ShardBucket = e.ShardBucket
			rec.StreamHash = e.Hash
			rec.StreamID = e.rank
			if !yield(rec) {
				return nil
			}
		}
		return nil
	}), nil
}
