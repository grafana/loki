package index

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
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
	calcCtx := makeTestCalcContext(builder)

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
	require.NoError(t, calc.Flush(context.Background(), calcCtx, builder))

	rows := flushAndReadAllPostingsRows(t, builder)

	// We expect 2 bloom postings: one for trace_id, one for span_id.
	i := findBloomRow(rows, "trace_id")
	require.NotEqual(t, -1, i, "expected bloom posting for trace_id")

	j := findBloomRow(rows, "span_id")
	require.NotEqual(t, -1, j, "expected bloom posting for span_id")

	require.Equal(t, int64(streams.DefaultShardLayout().Buckets()), rows[i].ShardBuckets)
	require.Equal(t, int64(streams.DefaultShardLayout().Buckets()), rows[j].ShardBuckets)

	// Bloom filter bytes should be non-empty.
	require.NotEmpty(t, rows[i].BloomFilter, "expected non-empty bloom filter for trace_id")
	require.NotEmpty(t, rows[j].BloomFilter, "expected non-empty bloom filter for span_id")

	// Stream ID bitmap should be non-empty.
	require.NotEmpty(t, rows[i].StreamIDBitmap, "expected non-empty stream bitmap for trace_id")
	require.NotEmpty(t, rows[j].StreamIDBitmap, "expected non-empty stream bitmap for span_id")
}

func TestColumnValuesCalculation_TimestampsAndSizes(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)

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
	require.NoError(t, calc.Flush(context.Background(), calcCtx, builder))

	rows := flushAndReadAllPostingsRows(t, builder)

	i := findBloomRow(rows, "trace_id")
	require.NotEqual(t, -1, i, "expected bloom posting for trace_id")

	row := rows[i]
	require.Equal(t, int64(streams.DefaultShardLayout().Buckets()), row.ShardBuckets)
	// Timestamps: min=ts1 (10s), max=ts3 (30s) — ts3 is the latest, proving the
	// third record's timestamp was tracked.
	require.Equal(t, ts1.UnixNano(), row.MinTimestamp)
	require.Equal(t, ts3.UnixNano(), row.MaxTimestamp)

	// Size: sum of line lengths for records that have trace_id metadata.
	expectedSize := int64(len(line1) + len(line2) + len(line3))
	require.Equal(t, expectedSize, row.UncompressedSize)
}

func TestColumnValuesCalculation_StreamIDBitmapBitsSet(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)

	calc := &columnValuesCalculation{}
	stat := makeColumnValuesStats([]string{"trace_id"})
	require.NoError(t, calc.Prepare(context.Background(), calcCtx, nil, stat))

	// Only stream 1 has trace_id metadata; stream 2 does not.
	batch := []logs.Record{
		{StreamID: 1, Timestamp: time.Unix(10, 0).UTC(), Line: []byte("a"), Metadata: labels.FromStrings("trace_id", "x")},
		{StreamID: 2, Timestamp: time.Unix(20, 0).UTC(), Line: []byte("b")}, // no trace_id
	}

	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch))
	require.NoError(t, calc.Flush(context.Background(), calcCtx, builder))

	rows := flushAndReadAllPostingsRows(t, builder)

	i := findBloomRow(rows, "trace_id")
	require.NotEqual(t, -1, i, "expected bloom posting for trace_id")
	require.Equal(t, int64(streams.DefaultShardLayout().Buckets()), rows[i].ShardBuckets)

	// Bitmap should have bit 1 set (stream ID 1 has trace_id).
	require.NotEmpty(t, rows[i].StreamIDBitmap)
}

func TestColumnValuesCalculation_EmptyBatch(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)

	calc := &columnValuesCalculation{}
	stat := makeColumnValuesStats([]string{"trace_id"})
	require.NoError(t, calc.Prepare(context.Background(), calcCtx, nil, stat))
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, nil))
	require.NoError(t, calc.Flush(context.Background(), calcCtx, builder))

	// The column was registered during Prepare, so the postings section still
	// holds a bloom posting for trace_id, with empty data because no records
	// were processed.
	rows := flushAndReadAllPostingsRows(t, builder)

	i := findBloomRow(rows, "trace_id")
	require.NotEqual(t, -1, i, "expected bloom posting for trace_id even with empty batch")

	row := rows[i]
	require.Equal(t, int64(streams.DefaultShardLayout().Buckets()), row.ShardBuckets)
	// With the bloom aggregator, an unobserved but prepared column uses sentinel values:
	// MinTimestamp = math.MaxInt64 and MaxTimestamp = math.MinInt64.
	require.Equal(t, int64(math.MaxInt64), row.MinTimestamp, "no records means sentinel max int64 for min timestamp")
	require.Equal(t, int64(math.MinInt64), row.MaxTimestamp, "no records means sentinel min int64 for max timestamp")
	require.Equal(t, int64(0), row.UncompressedSize, "no records means zero size")
}

func TestColumnValuesCalculation_MultipleBatches(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)

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
	require.NoError(t, calc.Flush(context.Background(), calcCtx, builder))

	rows := flushAndReadAllPostingsRows(t, builder)

	i := findBloomRow(rows, "trace_id")
	require.NotEqual(t, -1, i, "expected bloom posting for trace_id")

	row := rows[i]
	require.Equal(t, int64(streams.DefaultShardLayout().Buckets()), row.ShardBuckets)
	// Timestamps should span both batches.
	require.Equal(t, ts1.UnixNano(), row.MinTimestamp)
	require.Equal(t, ts2.UnixNano(), row.MaxTimestamp)

	// Size: both lines.
	expectedSize := int64(len("first") + len("second"))
	require.Equal(t, expectedSize, row.UncompressedSize)
}
