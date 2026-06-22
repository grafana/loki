package logsobj

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sortmerge"
)

// sortedSchemaIter merges schema-sorted input sections, injects schema sort
// keys, remaps stream IDs, and returns an iterator suitable for AppendOrdered.
func sortedSchemaIter(
	ctx context.Context, sections []*dataobj.Section, sortKeys map[int64]string, streamIDs map[int64]int64,
) (result.Seq[logs.Record], error) {
	iter, err := sortmerge.IteratorForSchema(ctx, sections, sortKeys)
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
			sortKey, ok := sortKeys[oldStreamID]
			if !ok {
				return fmt.Errorf("missing schema sort key for stream ID %d", oldStreamID)
			}
			streamID, ok := streamIDs[oldStreamID]
			if !ok {
				return fmt.Errorf("missing stream ID remap for stream ID %d", oldStreamID)
			}
			rec.SortKey = sortKey
			rec.StreamID = streamID
			if !yield(rec) {
				return nil
			}
		}
		return nil
	}), nil
}
