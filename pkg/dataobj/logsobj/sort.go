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

// rankedSortKey holds the sort key of one stream and its rank, which is its
// new stream ID after the sort in [Builder.FlushAndSort]. Independently
// written objects do not share ranks.
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
//
// A nil streamIDs merges every record. Otherwise it merges only the records of
// those streams, given by their stream IDs in sections.
func mergeAndRemapLogsIter(ctx context.Context, sections []*dataobj.Section, remap []rankedSortKey, streamIDs []int64) (result.Seq[logs.Record], error) {
	var (
		iter result.Seq[logs.Record]
		err  error
	)
	if streamIDs == nil {
		iter, err = sortmerge.SchemaSortedIterator(ctx, sections, sortKeys(remap))
	} else {
		iter, err = sortmerge.SchemaSortedStreamsIterator(ctx, sections, sortKeys(remap), streamIDs)
	}
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
