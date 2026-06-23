package logsobj

import (
	"context"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sortmerge"
)

// sortedSchemaIter reads all records from the input sections, injects schema
// sort keys, sorts globally by schema key, and returns an iterator in schema
// order suitable for AppendOrdered.
func sortedSchemaIter(
	ctx context.Context, sections []*dataobj.Section, sortKeys map[int64]string, fallbackOrder logs.SortOrder,
) (result.Seq[logs.Record], error) {
	iter, err := sortmerge.Iterator(ctx, sections, fallbackOrder)
	if err != nil {
		return nil, err
	}
	var recs []logs.Record
	for res := range iter {
		rec, err := res.Value()
		if err != nil {
			return nil, err
		}
		rec.SortKey = sortKeys[rec.StreamID]
		recs = append(recs, rec)
	}
	logs.SortRecords(recs, logs.SortSchemaASC)
	return result.Iter(func(yield func(logs.Record) bool) error {
		for _, r := range recs {
			if !yield(r) {
				return nil
			}
		}
		return nil
	}), nil
}
