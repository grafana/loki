package logsobj

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sortmerge"
)

// streamRemap is the CopyAndSort sidecar for one stream, indexed by that
// object's old stream ID. newID is the rank in this object after sorting by
// StreamOrderKey; independently written objects do not share newID.
type streamRemap struct {
	newID int64
	logs.StreamSort
}

// schemaRemap builds the CopyAndSort sidecar for one source: old ID -> new ID
// plus StreamSort, so the k-way merge can compare [shard, schema, hash]
// before records carry the new IDs.
func schemaRemap(ranks *StreamRanks, sourceIdx int) []streamRemap {
	var maxOld int64
	for oldID := range ranks.remap[sourceIdx] {
		if oldID > maxOld {
			maxOld = oldID
		}
	}
	out := make([]streamRemap, maxOld+1)
	for oldID, newID := range ranks.remap[sourceIdx] {
		out[oldID] = streamRemap{
			newID:      newID,
			StreamSort: ranks.byNewID[newID].Key.streamSort(),
		}
	}
	return out
}

// streamSorts extracts the sort-tuple column for the k-way merge comparator.
func streamSorts(remap []streamRemap) []logs.StreamSort {
	out := make([]logs.StreamSort, len(remap))
	for i, e := range remap {
		out[i] = e.StreamSort
	}
	return out
}

// sortedSchemaIter merges schema-sorted input sections, injects schema sort
// keys, remaps stream IDs, and returns an iterator suitable for AppendOrdered.
func sortedSchemaIter(ctx context.Context, sections []*dataobj.Section, remap []streamRemap) (result.Seq[logs.Record], error) {
	iter, err := sortmerge.IteratorForSchema(ctx, sections, streamSorts(remap))
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
			if e.newID == 0 {
				return fmt.Errorf("missing stream ID remap for stream ID %d", oldStreamID)
			}
			rec.SortKey = e.Key
			rec.ShardBucket = e.Shard
			rec.StreamHash = e.Hash
			rec.StreamID = e.newID
			if !yield(rec) {
				return nil
			}
		}
		return nil
	}), nil
}
