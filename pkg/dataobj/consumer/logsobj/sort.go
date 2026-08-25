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

// streamRemap is the CopyAndSort sidecar for one stream, indexed by that
// object's old stream ID. newID is the rank in this object after sorting by
// StreamOrderKey; independently written objects do not share newID.
type streamRemap struct {
	newID int64
	logs.StreamSort
}

// newStreamRemap fills shard, schema key, and hash from one labels.StableHash.
// newID is left 0; sortAndRemapStreams assigns it after the object-wide sort.
func newStreamRemap(ls labels.Labels, schemaLabels []string) (streamRemap, error) {
	schemaKey, err := ComputeSortKey(ls, schemaLabels)
	if err != nil {
		return streamRemap{}, err
	}
	hash := labels.StableHash(ls)
	return streamRemap{
		StreamSort: logs.StreamSort{
			Shard: streams.ShardBucketFromHash(hash),
			Key:   schemaKey,
			Hash:  hash,
		},
	}, nil
}

func (r streamRemap) orderKey(ls labels.Labels) StreamOrderKey {
	return StreamOrderKey{
		Shard:     r.Shard,
		SchemaKey: r.Key,
		Hash:      r.Hash,
		Labels:    ls,
	}
}

// streamSorts extracts the sort-tuple column for the k-way merge comparator.
func streamSorts(remap []streamRemap) []logs.StreamSort {
	out := make([]logs.StreamSort, len(remap))
	for i, e := range remap {
		out[i] = e.StreamSort
	}
	return out
}

// sortedLogsIter merges schema-sorted input sections, injects schema sort
// keys, remaps stream IDs, and returns an iterator suitable for AppendOrdered.
func sortedLogsIter(ctx context.Context, sections []*dataobj.Section, remap []streamRemap) (result.Seq[logs.Record], error) {
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
