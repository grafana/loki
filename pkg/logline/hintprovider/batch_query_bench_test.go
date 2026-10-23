package hintprovider

import (
	"fmt"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/logline/format"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

// benchRangeSink keeps benchmark results live so the compiler cannot drop the call.
var benchRangeSink []HintTimeRange

// BenchmarkRangesForDocIDs compares hint-range construction for a dense run of
// abutting 100ms documents. per_document is the previous expansion (one range
// and source string per document, then NormalizeRanges). merged_runs is
// rangesForDocIDs, which collapses that run before formatting sources.
func BenchmarkRangesForDocIDs(b *testing.B) {
	const (
		n      = 100_000
		bucket = 100 * time.Millisecond
	)
	t0 := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	meta := store.Meta{Date: "2026-03-02", StorageID: "abc"}
	docs := make([]format.DocumentMetadata, n)
	ids := make([]uint32, n)
	for i := range n {
		start := t0.Add(time.Duration(i) * bucket)
		docs[i] = format.DocumentMetadata{
			ID:          uint32(i),
			MinTimeUnix: start.UnixMilli(),
			MaxTimeUnix: start.Add(bucket).UnixMilli(),
		}
		ids[i] = uint32(i)
	}

	b.Run("per_document", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchRangeSink = NormalizeRanges(rangesForDocIDsPerDocument(meta, ids, docs))
		}
	})
	b.Run("merged_runs", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			benchRangeSink = NormalizeRanges(rangesForDocIDs(meta, ids, docs, time.Time{}, time.Time{}))
		}
	})
}

// rangesForDocIDsPerDocument is the pre-merge expansion. It exists so this
// benchmark can show the allocation difference in one run.
func rangesForDocIDsPerDocument(meta store.Meta, docIDs []uint32, docs []format.DocumentMetadata) []HintTimeRange {
	if len(docIDs) == 0 || len(docs) == 0 {
		return nil
	}

	docByID := make(map[uint32]format.DocumentMetadata, len(docs))
	for _, doc := range docs {
		docByID[doc.ID] = doc
	}

	ranges := make([]HintTimeRange, 0, len(docIDs))
	for _, id := range docIDs {
		doc, ok := docByID[id]
		if !ok {
			continue
		}
		minTS := time.UnixMilli(doc.MinTimeUnix).UTC()
		maxTS := time.UnixMilli(doc.MaxTimeUnix).UTC()
		ranges = append(ranges, HintTimeRange{
			Start: minTS,
			End:   maxTS,
			Source: fmt.Sprintf(
				"index=%s,doc=%d,min=%s,max=%s",
				meta.ID(),
				doc.ID,
				minTS.Format(time.RFC3339Nano),
				maxTS.Format(time.RFC3339Nano),
			),
		})
	}
	return ranges
}
