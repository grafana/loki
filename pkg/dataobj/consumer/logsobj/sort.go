package logsobj

import (
	"bytes"
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
)

// sortedSchemaIter scans input sections, injects target-schema sort keys, and
// remaps stream IDs. The caller feeds the result to an AppendUnordered builder,
// which performs the actual target-layout sort.
func sortedSchemaIter(
	ctx context.Context, sections []*dataobj.Section, shards []uint32, sortKeys []string, streamIDs []int64,
) (result.Seq[logs.Record], error) {
	return result.Iter(func(yield func(logs.Record) bool) error {
		for _, section := range sections {
			opened, err := logs.Open(ctx, section)
			if err != nil {
				return err
			}
			for res := range logs.IterSection(ctx, opened) {
				rec, err := res.Value()
				if err != nil {
					return err
				}

				oldStreamID := rec.StreamID
				if oldStreamID <= 0 || oldStreamID >= int64(len(sortKeys)) {
					return fmt.Errorf("missing schema sort key for stream ID %d", oldStreamID)
				}
				sortKey := sortKeys[oldStreamID]

				if oldStreamID >= int64(len(streamIDs)) {
					return fmt.Errorf("missing stream ID remap for stream ID %d", oldStreamID)
				}
				streamID := streamIDs[oldStreamID]
				if streamID == 0 {
					return fmt.Errorf("missing stream ID remap for stream ID %d", oldStreamID)
				}
				rec.SortKey = sortKey
				rec.ShardHash = int64(shards[oldStreamID])
				rec.StreamID = streamID
				rec.Line = bytes.Clone(rec.Line)
				rec.Metadata = rec.Metadata.Copy()
				if !yield(rec) {
					return nil
				}
			}
		}
		return nil
	}), nil
}
