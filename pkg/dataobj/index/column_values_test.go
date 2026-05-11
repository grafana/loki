package index

import (
	"context"
	"math"
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
	require.NoError(t, calc.Prepare(context.Background(), calcCtx, nil, stat))

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

	tbl := flushAndReadAllPostingsTable(t, builder)

	// We expect 2 bloom postings: one for trace_id, one for span_id.
	i := findRow(tbl.rows, map[string]any{
		"kind.int64":       int64(postings.KindBloom),
		"column_name.utf8": "trace_id",
	})
	require.NotEqual(t, -1, i, "expected bloom posting for trace_id")

	j := findRow(tbl.rows, map[string]any{
		"kind.int64":       int64(postings.KindBloom),
		"column_name.utf8": "span_id",
	})
	require.NotEqual(t, -1, j, "expected bloom posting for span_id")

	// Bloom filter bytes should be non-empty.
	require.NotEmpty(t, tbl.opaque["bloom_filter.binary"][i], "expected non-empty bloom filter for trace_id")
	require.NotEmpty(t, tbl.opaque["bloom_filter.binary"][j], "expected non-empty bloom filter for span_id")

	// Stream ID bitmap should be non-empty.
	require.NotEmpty(t, tbl.opaque["stream_id_bitmap.binary"][i], "expected non-empty stream bitmap for trace_id")
	require.NotEmpty(t, tbl.opaque["stream_id_bitmap.binary"][j], "expected non-empty stream bitmap for span_id")
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
	require.NoError(t, calc.Prepare(context.Background(), calcCtx, nil, stat))

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

	tbl := flushAndReadAllPostingsTable(t, builder)

	i := findRow(tbl.rows, map[string]any{
		"kind.int64":       int64(postings.KindBloom),
		"column_name.utf8": "trace_id",
	})
	require.NotEqual(t, -1, i, "expected bloom posting for trace_id")

	row := tbl.rows[i]
	// Timestamps: min=ts1 (10s), max=ts3 (30s) — ts3 is the latest, proving the
	// third record's timestamp was tracked.
	require.Equal(t, ts1.UTC(), row["min_timestamp.timestamp"])
	require.Equal(t, ts3.UTC(), row["max_timestamp.timestamp"])

	// Size: sum of line lengths for records that have trace_id metadata.
	expectedSize := int64(len(line1) + len(line2) + len(line3))
	require.Equal(t, expectedSize, row["uncompressed_size.int64"])
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
	require.NoError(t, calc.Prepare(context.Background(), calcCtx, nil, stat))

	// Only stream 1 has trace_id metadata; stream 2 does not.
	batch := []logs.Record{
		{StreamID: 1, Timestamp: time.Unix(10, 0).UTC(), Line: []byte("a"), Metadata: labels.FromStrings("trace_id", "x")},
		{StreamID: 2, Timestamp: time.Unix(20, 0).UTC(), Line: []byte("b")}, // no trace_id
	}

	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch))
	require.NoError(t, calc.Flush(context.Background(), calcCtx))

	tbl := flushAndReadAllPostingsTable(t, builder)

	i := findRow(tbl.rows, map[string]any{
		"kind.int64":       int64(postings.KindBloom),
		"column_name.utf8": "trace_id",
	})
	require.NotEqual(t, -1, i, "expected bloom posting for trace_id")

	// Bitmap should have bit 1 set (stream ID 1 has trace_id).
	require.NotEmpty(t, tbl.opaque["stream_id_bitmap.binary"][i])
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
	require.NoError(t, calc.Prepare(context.Background(), calcCtx, nil, stat))
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, nil))
	require.NoError(t, calc.Flush(context.Background(), calcCtx))

	// The column was registered during Prepare, so Flush still appends a bloom
	// posting for trace_id — but with empty data since no records were processed.
	tbl := flushAndReadAllPostingsTable(t, builder)

	i := findRow(tbl.rows, map[string]any{
		"kind.int64":       int64(postings.KindBloom),
		"column_name.utf8": "trace_id",
	})
	require.NotEqual(t, -1, i, "expected bloom posting for trace_id even with empty batch")

	row := tbl.rows[i]
	// With the bloom aggregator, an unobserved but prepared column uses sentinel values:
	// MinTimestamp = math.MaxInt64 and MaxTimestamp = math.MinInt64.
	// These are stored and read back as time.Time values.
	sentinelMinTimestamp := time.Unix(0, math.MaxInt64).UTC() // unobserved min starts at max possible
	sentinelMaxTimestamp := time.Unix(0, math.MinInt64).UTC() // unobserved max starts at min possible
	require.Equal(t, sentinelMinTimestamp, row["min_timestamp.timestamp"], "no records means sentinel max int64 for min timestamp")
	require.Equal(t, sentinelMaxTimestamp, row["max_timestamp.timestamp"], "no records means sentinel min int64 for max timestamp")
	require.Equal(t, int64(0), row["uncompressed_size.int64"], "no records means zero size")
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
	require.NoError(t, calc.Prepare(context.Background(), calcCtx, nil, stat))

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

	tbl := flushAndReadAllPostingsTable(t, builder)

	i := findRow(tbl.rows, map[string]any{
		"kind.int64":       int64(postings.KindBloom),
		"column_name.utf8": "trace_id",
	})
	require.NotEqual(t, -1, i, "expected bloom posting for trace_id")

	row := tbl.rows[i]
	// Timestamps should span both batches.
	require.Equal(t, ts1.UTC(), row["min_timestamp.timestamp"])
	require.Equal(t, ts2.UTC(), row["max_timestamp.timestamp"])

	// Size: both lines.
	expectedSize := int64(len("first") + len("second"))
	require.Equal(t, expectedSize, row["uncompressed_size.int64"])
}
