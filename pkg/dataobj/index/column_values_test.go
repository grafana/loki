package index

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// makeColumnValuesStats creates a logs.Stats with a set of metadata columns
// as if Prepare would receive them from a real logs section.
func makeColumnValuesStats(metadataColumns []string) logs.Stats {
	var stats logs.Stats
	// Non-metadata columns come first (streamid, timestamp, message).
	stats.Columns = append(stats.Columns, logs.ColumnStats{
		Name:        "stream_id",
		Type:        "stream_id",
		ColumnIndex: 0,
		Cardinality: 10,
	})
	stats.Columns = append(stats.Columns, logs.ColumnStats{
		Name:        "timestamp",
		Type:        "timestamp",
		ColumnIndex: 1,
		Cardinality: 100,
	})
	for i, name := range metadataColumns {
		stats.Columns = append(stats.Columns, logs.ColumnStats{
			Name:        name,
			Type:        "metadata",
			ColumnIndex: int64(2 + i),
			Cardinality: 5,
		})
	}
	stats.Columns = append(stats.Columns, logs.ColumnStats{
		Name:        "message",
		Type:        "message",
		ColumnIndex: int64(2 + len(metadataColumns)),
		Cardinality: 50,
	})
	return stats
}

func TestColumnValuesCalculation_BloomPostingAppended(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := &logsCalculationContext{
		tenantID:     "tenant-1",
		objectPath:   "test/path/obj1",
		sectionIdx:   0,
		streamLabels: makeTestStreamLabels(),
		builder:      builder,
	}

	calc := &columnValuesCalculation{}
	stat := makeColumnValuesStats([]string{"trace_id", "span_id"})
	require.NoError(t, calc.Prepare(context.Background(), nil, stat))

	ts1 := time.Unix(10, 0).UTC()
	ts2 := time.Unix(20, 0).UTC()
	ts3 := time.Unix(15, 0).UTC()

	line1 := []byte("hello from stream 1")
	line2 := []byte("hello again")

	// Stream 1 has both trace_id and span_id metadata.
	// Stream 2 has only trace_id metadata.
	batch := []logs.Record{
		{
			StreamID:  1,
			Timestamp: ts1,
			Line:      line1,
			Metadata:  labels.FromStrings("trace_id", "abc", "span_id", "111"),
		},
		{
			StreamID:  2,
			Timestamp: ts2,
			Line:      line2,
			Metadata:  labels.FromStrings("trace_id", "def"),
		},
		{
			StreamID:  1,
			Timestamp: ts3,
			Line:      line1,
			Metadata:  labels.FromStrings("trace_id", "ghi"),
		},
	}

	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch))
	require.NoError(t, calc.Flush(context.Background(), calcCtx))

	allPostings := readAllPostingsForTenant(t, builder, "tenant-1")

	// We expect 2 bloom postings: one for trace_id, one for span_id.
	var tracePosting, spanPosting *postings.Posting
	for i := range allPostings {
		if allPostings[i].Kind == postings.KindBloom {
			switch allPostings[i].ColumnName {
			case "trace_id":
				tracePosting = &allPostings[i]
			case "span_id":
				spanPosting = &allPostings[i]
			}
		}
	}

	require.NotNil(t, tracePosting, "expected bloom posting for trace_id")
	require.NotNil(t, spanPosting, "expected bloom posting for span_id")

	// Bloom filter bytes should be non-empty.
	require.NotEmpty(t, tracePosting.BloomFilter, "expected non-empty bloom filter for trace_id")
	require.NotEmpty(t, spanPosting.BloomFilter, "expected non-empty bloom filter for span_id")

	// Stream ID bitmap should be non-empty.
	require.NotEmpty(t, tracePosting.StreamIDBitmap, "expected non-empty stream bitmap for trace_id")
	require.NotEmpty(t, spanPosting.StreamIDBitmap, "expected non-empty stream bitmap for span_id")
}

func TestColumnValuesCalculation_TimestampsAndSizes(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := &logsCalculationContext{
		tenantID:     "tenant-1",
		objectPath:   "test/path/obj1",
		sectionIdx:   0,
		streamLabels: makeTestStreamLabels(),
		builder:      builder,
	}

	calc := &columnValuesCalculation{}
	stat := makeColumnValuesStats([]string{"trace_id"})
	require.NoError(t, calc.Prepare(context.Background(), nil, stat))

	ts1 := time.Unix(10, 0).UTC()
	ts2 := time.Unix(20, 0).UTC()
	ts3 := time.Unix(30, 0).UTC() // latest timestamp — proves third record is processed

	line1 := []byte("first")
	line2 := []byte("second line")
	line3 := []byte("third")

	batch := []logs.Record{
		{StreamID: 1, Timestamp: ts1, Line: line1, Metadata: labels.FromStrings("trace_id", "aaa")},
		{StreamID: 2, Timestamp: ts2, Line: line2, Metadata: labels.FromStrings("trace_id", "bbb")},
		{StreamID: 1, Timestamp: ts3, Line: line3, Metadata: labels.FromStrings("trace_id", "ccc")},
	}

	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch))
	require.NoError(t, calc.Flush(context.Background(), calcCtx))

	allPostings := readAllPostingsForTenant(t, builder, "tenant-1")

	var tracePosting *postings.Posting
	for i := range allPostings {
		if allPostings[i].Kind == postings.KindBloom && allPostings[i].ColumnName == "trace_id" {
			tracePosting = &allPostings[i]
			break
		}
	}
	require.NotNil(t, tracePosting, "expected bloom posting for trace_id")

	// Timestamps: min=ts1 (10s), max=ts3 (30s) — ts3 is the latest, proving the
	// third record's timestamp was tracked.
	require.Equal(t, ts1.UnixNano(), tracePosting.MinTimestamp)
	require.Equal(t, ts3.UnixNano(), tracePosting.MaxTimestamp)

	// Size: sum of line lengths for records that have trace_id metadata.
	expectedSize := int64(len(line1) + len(line2) + len(line3))
	require.Equal(t, expectedSize, tracePosting.UncompressedSize)
}

func TestColumnValuesCalculation_StreamIDBitmapBitsSet(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := &logsCalculationContext{
		tenantID:     "tenant-1",
		objectPath:   "test/path/obj1",
		sectionIdx:   0,
		streamLabels: makeTestStreamLabels(),
		builder:      builder,
	}

	calc := &columnValuesCalculation{}
	stat := makeColumnValuesStats([]string{"trace_id"})
	require.NoError(t, calc.Prepare(context.Background(), nil, stat))

	// Only stream 1 has trace_id metadata; stream 2 does not.
	batch := []logs.Record{
		{StreamID: 1, Timestamp: time.Unix(10, 0).UTC(), Line: []byte("a"), Metadata: labels.FromStrings("trace_id", "x")},
		{StreamID: 2, Timestamp: time.Unix(20, 0).UTC(), Line: []byte("b")}, // no trace_id
	}

	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch))
	require.NoError(t, calc.Flush(context.Background(), calcCtx))

	allPostings := readAllPostingsForTenant(t, builder, "tenant-1")

	var tracePosting *postings.Posting
	for i := range allPostings {
		if allPostings[i].Kind == postings.KindBloom && allPostings[i].ColumnName == "trace_id" {
			tracePosting = &allPostings[i]
			break
		}
	}
	require.NotNil(t, tracePosting, "expected bloom posting for trace_id")

	// Bitmap should have bit 1 set (stream ID 1 has trace_id).
	require.NotEmpty(t, tracePosting.StreamIDBitmap)
}

func TestColumnValuesCalculation_EmptyBatch(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := &logsCalculationContext{
		tenantID:     "tenant-1",
		objectPath:   "test/path/obj1",
		sectionIdx:   0,
		streamLabels: makeTestStreamLabels(),
		builder:      builder,
	}

	calc := &columnValuesCalculation{}
	stat := makeColumnValuesStats([]string{"trace_id"})
	require.NoError(t, calc.Prepare(context.Background(), nil, stat))
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, nil))
	require.NoError(t, calc.Flush(context.Background(), calcCtx))

	// The column was registered during Prepare, so Flush still appends a bloom
	// posting for trace_id — but with empty data since no records were processed.
	allPostings := readAllPostingsForTenant(t, builder, "tenant-1")

	var tracePosting *postings.Posting
	for i := range allPostings {
		if allPostings[i].Kind == postings.KindBloom && allPostings[i].ColumnName == "trace_id" {
			tracePosting = &allPostings[i]
			break
		}
	}
	require.NotNil(t, tracePosting, "expected bloom posting for trace_id even with empty batch")
	zeroTs := time.Time{}.UnixNano()
	require.Equal(t, zeroTs, tracePosting.MinTimestamp, "no records means Go zero-value timestamp")
	require.Equal(t, zeroTs, tracePosting.MaxTimestamp, "no records means Go zero-value timestamp")
	require.Equal(t, int64(0), tracePosting.UncompressedSize, "no records means zero size")
}

func TestColumnValuesCalculation_MultipleBatches(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := &logsCalculationContext{
		tenantID:     "tenant-1",
		objectPath:   "test/path/obj1",
		sectionIdx:   0,
		streamLabels: makeTestStreamLabels(),
		builder:      builder,
	}

	calc := &columnValuesCalculation{}
	stat := makeColumnValuesStats([]string{"trace_id"})
	require.NoError(t, calc.Prepare(context.Background(), nil, stat))

	ts1 := time.Unix(10, 0).UTC()
	ts2 := time.Unix(30, 0).UTC()

	batch1 := []logs.Record{
		{StreamID: 1, Timestamp: ts1, Line: []byte("first"), Metadata: labels.FromStrings("trace_id", "aaa")},
	}
	batch2 := []logs.Record{
		{StreamID: 2, Timestamp: ts2, Line: []byte("second"), Metadata: labels.FromStrings("trace_id", "bbb")},
	}

	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch1))
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch2))
	require.NoError(t, calc.Flush(context.Background(), calcCtx))

	allPostings := readAllPostingsForTenant(t, builder, "tenant-1")

	var tracePosting *postings.Posting
	for i := range allPostings {
		if allPostings[i].Kind == postings.KindBloom && allPostings[i].ColumnName == "trace_id" {
			tracePosting = &allPostings[i]
			break
		}
	}
	require.NotNil(t, tracePosting, "expected bloom posting for trace_id")

	// Timestamps should span both batches.
	require.Equal(t, ts1.UnixNano(), tracePosting.MinTimestamp)
	require.Equal(t, ts2.UnixNano(), tracePosting.MaxTimestamp)

	// Size: both lines.
	expectedSize := int64(len("first") + len("second"))
	require.Equal(t, expectedSize, tracePosting.UncompressedSize)
}
