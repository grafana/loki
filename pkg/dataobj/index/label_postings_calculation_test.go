package index

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestLabelPostingsCalculation_BasicPostings(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)
	calc := &labelPostingsCalculation{}

	require.NoError(t, calc.Prepare(context.Background(), calcCtx, nil, logs.Stats{}))

	ts1 := time.Unix(100, 0).UTC()
	ts2 := time.Unix(200, 0).UTC()
	ts3 := time.Unix(150, 0).UTC()

	// Stream 1: service_name=svcA, env=prod
	// Stream 2: service_name=svcB, env=dev
	batch := []logs.Record{
		{StreamID: 1, Timestamp: ts1, Line: []byte("hello")}, // svcA, prod
		{StreamID: 2, Timestamp: ts2, Line: []byte("world")}, // svcB, dev
		{StreamID: 1, Timestamp: ts3, Line: []byte("again")}, // svcA, prod again
	}

	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch))
	require.NoError(t, calc.Flush(context.Background(), calcCtx, builder))

	rows := flushAndReadAllPostingsRows(t, builder)

	// We expect 4 label postings: service_name=svcA, service_name=svcB, env=prod, env=dev.
	require.Len(t, rows, 4)

	// All should be label-kind.
	for _, row := range rows {
		require.Equal(t, postings.KindLabel, row.Kind)
		require.Equal(t, int64(streams.DefaultShardLayout().Buckets()), row.ShardBuckets)
		require.NotEmpty(t, row.LabelValue)
	}

	// Find svcA and svcB.
	i := findLabelRow(rows, "service_name", "svcA")
	require.NotEqual(t, -1, i, "expected svcA posting")
	svcARow := rows[i]

	j := findLabelRow(rows, "service_name", "svcB")
	require.NotEqual(t, -1, j, "expected svcB posting")

	// svcA should have bit 1 set.
	require.NotEmpty(t, rows[i].StreamIDBitmap)
	// svcB should have bit 2 set.
	require.NotEmpty(t, rows[j].StreamIDBitmap)

	// Verify timestamps for svcA (min=ts1, max=ts3).
	require.Equal(t, ts1.UnixNano(), svcARow.MinTimestamp)
	require.Equal(t, ts3.UnixNano(), svcARow.MaxTimestamp)

	// Verify env labels are also present.
	require.NotEqual(t, -1, findLabelRow(rows, "env", "prod"), "expected env=prod posting")
	require.NotEqual(t, -1, findLabelRow(rows, "env", "dev"), "expected env=dev posting")
}

func TestLabelPostingsCalculation_BitmapsNormalized(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)
	calc := &labelPostingsCalculation{}

	require.NoError(t, calc.Prepare(context.Background(), calcCtx, nil, logs.Stats{}))

	// Stream 1 = svcA/prod, Stream 2 = svcB/dev (from makeTestStreamLabels).
	batch := []logs.Record{
		{StreamID: 1, Timestamp: time.Unix(1, 0).UTC(), Line: []byte("a")}, // svcA, prod
		{StreamID: 2, Timestamp: time.Unix(2, 0).UTC(), Line: []byte("b")}, // svcB, dev
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch))
	require.NoError(t, calc.Flush(context.Background(), calcCtx, builder))

	rows := flushAndReadAllPostingsRows(t, builder)
	// 4 postings: service_name=svcA, service_name=svcB, env=prod, env=dev
	require.Len(t, rows, 4)

	i := findLabelRow(rows, "service_name", "svcA")
	require.NotEqual(t, -1, i)

	j := findLabelRow(rows, "service_name", "svcB")
	require.NotEqual(t, -1, j)

	// All bitmaps should be the same length after normalization.
	for k := 1; k < len(rows); k++ {
		require.Equal(t, len(rows[0].StreamIDBitmap), len(rows[k].StreamIDBitmap),
			"all bitmaps should be normalized to the same size")
	}

	// svcA posting should include stream 1 but not stream 2.
	require.True(t, isBitSet(rows[i].StreamIDBitmap, 1), "svcA bitmap should have bit 1 set")
	require.False(t, isBitSet(rows[i].StreamIDBitmap, 2), "svcA bitmap should not have bit 2 set")

	// svcB posting should include stream 2 but not stream 1.
	require.True(t, isBitSet(rows[j].StreamIDBitmap, 2), "svcB bitmap should have bit 2 set")
	require.False(t, isBitSet(rows[j].StreamIDBitmap, 1), "svcB bitmap should not have bit 1 set")
}

func TestLabelPostingsCalculation_TimestampsAndSizes(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)
	calc := &labelPostingsCalculation{}

	require.NoError(t, calc.Prepare(context.Background(), calcCtx, nil, logs.Stats{}))

	ts1 := time.Unix(10, 0).UTC()
	ts2 := time.Unix(20, 0).UTC()
	ts3 := time.Unix(30, 0).UTC()

	// All records are stream 1 (service_name=svcA, env=prod).
	batch := []logs.Record{
		{StreamID: 1, Timestamp: ts1, Line: []byte("hello")},  // svcA, ts=10
		{StreamID: 1, Timestamp: ts2, Line: []byte("world!")}, // svcA, ts=20
		{StreamID: 1, Timestamp: ts3, Line: []byte("mid")},    // svcA, ts=30
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch))
	require.NoError(t, calc.Flush(context.Background(), calcCtx, builder))

	rows := flushAndReadAllPostingsRows(t, builder)
	// 2 postings: service_name=svcA, env=prod (both from stream 1).
	require.Len(t, rows, 2)

	// Check timestamps and sizes on the service_name=svcA posting.
	i := findLabelRow(rows, "service_name", "svcA")
	require.NotEqual(t, -1, i)
	row := rows[i]
	require.Equal(t, ts1.UnixNano(), row.MinTimestamp)
	require.Equal(t, ts3.UnixNano(), row.MaxTimestamp)
	expectedSize := int64(len("hello") + len("world!") + len("mid"))
	require.Equal(t, expectedSize, row.UncompressedSize)
}

func TestLabelPostingsCalculation_ProcessBatch(t *testing.T) {
	ts10 := time.Unix(10, 0).UTC()
	ts20 := time.Unix(20, 0).UTC()
	ts30 := time.Unix(30, 0).UTC()

	runTests := []struct {
		name  string
		batch []logs.Record
	}{
		{
			name: "reports min and max timestamps when a run is in descending order",
			batch: []logs.Record{
				{StreamID: 1, Timestamp: ts30, Line: []byte("a")},
				{StreamID: 1, Timestamp: ts20, Line: []byte("bb")},
				{StreamID: 1, Timestamp: ts10, Line: []byte("ccc")},
			},
		},
		{
			name: "reports min and max timestamps when a run is unordered",
			batch: []logs.Record{
				{StreamID: 1, Timestamp: ts20, Line: []byte("a")},
				{StreamID: 1, Timestamp: ts10, Line: []byte("bb")},
				{StreamID: 1, Timestamp: ts30, Line: []byte("ccc")},
			},
		},
	}
	for _, tc := range runTests {
		t.Run(tc.name, func(t *testing.T) {
			builder := newTestIndexBuilder(t)
			calcCtx := makeTestCalcContext(builder)
			calc := &labelPostingsCalculation{}

			require.NoError(t, calc.Prepare(context.Background(), calcCtx, nil, logs.Stats{}))
			require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, tc.batch))
			require.NoError(t, calc.Flush(context.Background(), calcCtx, builder))

			rows := flushAndReadAllPostingsRows(t, builder)
			i := findLabelRow(rows, "service_name", "svcA")
			require.NotEqual(t, -1, i)
			row := rows[i]
			require.Equal(t, ts10.UnixNano(), row.MinTimestamp)
			require.Equal(t, ts30.UnixNano(), row.MaxTimestamp)
			require.Equal(t, int64(len("a")+len("bb")+len("ccc")), row.UncompressedSize)
		})
	}

	t.Run("produces the same postings as per-record observation for a random batch", func(t *testing.T) {
		rnd := rand.New(rand.NewSource(1))
		var batch []logs.Record
		for range 200 {
			streamID := int64(rnd.Intn(4) + 1)
			for range rnd.Intn(10) + 1 {
				batch = append(batch, logs.Record{
					StreamID:  streamID,
					Timestamp: time.Unix(int64(rnd.Intn(1000)), 0).UTC(),
					Line:      make([]byte, rnd.Intn(64)),
				})
			}
		}

		perRunBuilder := newTestIndexBuilder(t)
		perRunCtx := makeTestCalcContext(perRunBuilder)
		calc := &labelPostingsCalculation{}
		require.NoError(t, calc.Prepare(context.Background(), perRunCtx, nil, logs.Stats{}))
		require.NoError(t, calc.ProcessBatch(context.Background(), perRunCtx, batch))
		require.NoError(t, calc.Flush(context.Background(), perRunCtx, perRunBuilder))

		perRecordBuilder := newTestIndexBuilder(t)
		perRecordCtx := makeTestCalcContext(perRecordBuilder)
		for _, record := range batch {
			perRecordCtx.streamLabels[record.StreamID].Range(func(lbl labels.Label) {
				perRecordBuilder.ObserveLabelPosting(postings.LabelObservation{
					ObjectPath:       perRecordCtx.objectPath,
					ShardBuckets:     int64(perRecordCtx.shardLayout.Buckets()),
					SectionIndex:     perRecordCtx.sectionIdx,
					ColumnName:       lbl.Name,
					LabelValue:       lbl.Value,
					StreamID:         record.StreamID,
					Time:             postings.At(record.Timestamp),
					UncompressedSize: record.UncompressedSize(),
					ShardBucket:      perRecordCtx.streamShardBuckets[record.StreamID],
				})
			})
		}

		require.Equal(t, flushAndReadAllPostingsRows(t, perRecordBuilder), flushAndReadAllPostingsRows(t, perRunBuilder))
	})
}

func TestLabelPostingsCalculation_ShardBucketsHandling(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)
	calc := &labelPostingsCalculation{}

	require.NoError(t, calc.Prepare(context.Background(), calcCtx, nil, logs.Stats{}))

	ts1 := time.Unix(10, 0).UTC()

	batch := []logs.Record{
		{StreamID: 1, Timestamp: ts1, Line: []byte("")}, // shard: 30 (svcA, prod)
		{StreamID: 2, Timestamp: ts1, Line: []byte("")}, // shard: 25 (svcB, dev)
		{StreamID: 3, Timestamp: ts1, Line: []byte("")}, // shard: 5  (staging)
		{StreamID: 4, Timestamp: ts1, Line: []byte("")}, // shard: 15 (dev)
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch))
	require.NoError(t, calc.Flush(context.Background(), calcCtx, builder))

	rows := flushAndReadAllPostingsRows(t, builder)
	// 5 postings: service_name={svcA, svcB}, env={dev, staging, prod}
	require.Len(t, rows, 5)

	// Shard factor should be constant on all rows
	for _, row := range rows {
		require.Equal(t, int64(streams.DefaultShardLayout().Buckets()), row.ShardBuckets)
	}

	// Check min & max shard buckets are calculated correctly
	// Dev is derived from two records in the same object. Prod & staging have one record.
	type expectation struct {
		env      string
		minShard uint32
		maxShard uint32
	}
	expect := []expectation{
		{
			env:      "prod",
			minShard: 30,
			maxShard: 30,
		},
		{
			env:      "staging",
			minShard: 5,
			maxShard: 5,
		},
		{
			env:      "dev",
			minShard: 15,
			maxShard: 25,
		},
	}
	for _, exp := range expect {
		i := findLabelRow(rows, "env", exp.env)
		require.NotEqual(t, -1, i)
		row := rows[i]
		require.Equal(t, exp.minShard, row.MinShardBucket)
		require.Equal(t, exp.maxShard, row.MaxShardBucket)
	}
}

func TestLabelPostingsCalculation_SharedLabelAggregation(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)
	calc := &labelPostingsCalculation{}

	require.NoError(t, calc.Prepare(context.Background(), calcCtx, nil, logs.Stats{}))

	ts1 := time.Unix(10, 0).UTC()
	ts2 := time.Unix(20, 0).UTC()
	line2 := []byte("from-two")  // stream 2: service_name=svcB, env=dev
	line4 := []byte("from-four") // stream 4: env=dev
	line1 := []byte("from-one")  // stream 1: service_name=svcA, env=prod (must not mix into env=dev)

	batch := []logs.Record{
		{StreamID: 2, Timestamp: ts1, Line: line2},
		{StreamID: 4, Timestamp: ts2, Line: line4},
		{StreamID: 1, Timestamp: ts1, Line: line1},
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch))
	require.NoError(t, calc.Flush(context.Background(), calcCtx, builder))

	rows := flushAndReadAllPostingsRows(t, builder)
	// service_name=svcA, service_name=svcB, env=prod, env=dev
	require.Len(t, rows, 4)

	i := findLabelRow(rows, "env", "dev")
	require.NotEqual(t, -1, i, "expected env=dev posting aggregated across streams 2 and 4")
	row := rows[i]
	require.Equal(t, int64(len(line2)+len(line4)), row.UncompressedSize)
	require.Equal(t, ts1.UnixNano(), row.MinTimestamp)
	require.Equal(t, ts2.UnixNano(), row.MaxTimestamp)

	bitmap := row.StreamIDBitmap
	require.True(t, isBitSet(bitmap, 2), "env=dev bitmap should have stream 2 set")
	require.True(t, isBitSet(bitmap, 4), "env=dev bitmap should have stream 4 set")
	require.False(t, isBitSet(bitmap, 1), "env=dev bitmap should not include stream 1 (env=prod)")
}

func TestLabelPostingsCalculation_UnknownStreamID(t *testing.T) {
	tt := []struct {
		name     string
		streamID int64
		mutate   func(*logsCalculationContext)
	}{
		{
			name:     "stream not in labels or shards",
			streamID: 99,
		},
		{
			name:     "missing shard bucket",
			streamID: 1,
			mutate: func(ctx *logsCalculationContext) {
				delete(ctx.streamShardBuckets, 1)
			},
		},
		{
			name:     "missing stream labels",
			streamID: 1,
			mutate: func(ctx *logsCalculationContext) {
				delete(ctx.streamLabels, 1)
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			builder := newTestIndexBuilder(t)
			calcCtx := makeTestCalcContext(builder)
			if tc.mutate != nil {
				tc.mutate(calcCtx)
			}
			calc := &labelPostingsCalculation{}

			require.NoError(t, calc.Prepare(context.Background(), calcCtx, nil, logs.Stats{}))
			err := calc.ProcessBatch(context.Background(), calcCtx, []logs.Record{
				{StreamID: tc.streamID, Timestamp: time.Unix(10, 0).UTC(), Line: []byte("x")},
			})
			require.ErrorContains(t, err, "unknown stream ID")
		})
	}
}

func TestLabelPostingsCalculation_EmptyBatch(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)
	calc := &labelPostingsCalculation{}

	require.NoError(t, calc.Prepare(context.Background(), calcCtx, nil, logs.Stats{}))
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, nil))
	require.NoError(t, calc.Flush(context.Background(), calcCtx, builder))

	// No data → builder is empty, Flush returns ErrBuilderEmpty.
	_, _, err := builder.Flush()
	require.ErrorIs(t, err, indexobj.ErrBuilderEmpty, "expected builder to be empty after empty batch")
}

func TestLabelPostingsCalculation_MultipleBatches(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)
	calc := &labelPostingsCalculation{}

	require.NoError(t, calc.Prepare(context.Background(), calcCtx, nil, logs.Stats{}))

	batch1 := []logs.Record{
		{StreamID: 1, Timestamp: time.Unix(1, 0).UTC(), Line: []byte("a")},
	}
	batch2 := []logs.Record{
		{StreamID: 2, Timestamp: time.Unix(2, 0).UTC(), Line: []byte("bb")},
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch1))
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch2))
	require.NoError(t, calc.Flush(context.Background(), calcCtx, builder))

	rows := flushAndReadAllPostingsRows(t, builder)
	// 4 postings: service_name=svcA, service_name=svcB, env=prod, env=dev
	require.Len(t, rows, 4)

	// Verify that both svcA and svcB postings are present.
	require.NotEqual(t, -1, findLabelRow(rows, "service_name", "svcA"))
	require.NotEqual(t, -1, findLabelRow(rows, "service_name", "svcB"))
	// Verify env labels are also present.
	require.NotEqual(t, -1, findLabelRow(rows, "env", "prod"))
	require.NotEqual(t, -1, findLabelRow(rows, "env", "dev"))
}

// BenchmarkLabelPostingsCalculation_ProcessBatch benchmarks ProcessBatch with
// varying numbers of log records and distinct streams. This helps track
// performance since the calculation must iterate through all rows in a logs
// section.
func BenchmarkLabelPostingsCalculation_ProcessBatch(b *testing.B) {
	benchCases := []struct {
		records int
		streams int
	}{
		{records: 1_000, streams: 10},
		{records: 10_000, streams: 10},
		{records: 10_000, streams: 100},
		{records: 100_000, streams: 10},
		{records: 100_000, streams: 100},
		{records: 100_000, streams: 1_000},
	}

	for _, bc := range benchCases {
		b.Run(fmt.Sprintf("records=%d/streams=%d", bc.records, bc.streams), func(b *testing.B) {
			// Build stream labels map with multiple labels per stream.
			streamLabels := make(map[int64]labels.Labels, bc.streams)
			for i := range bc.streams {
				streamLabels[int64(i)] = labels.FromStrings(
					"service_name", fmt.Sprintf("svc-%d", i),
					"cluster", fmt.Sprintf("cluster-%d", i%5),
					"namespace", fmt.Sprintf("ns-%d", i%10),
				)
			}
			shardBuckets := makeTestShardBuckets(streamLabels)

			// Pre-build the batch: records are distributed round-robin across streams.
			batch := make([]logs.Record, bc.records)
			for i := range batch {
				batch[i] = logs.Record{
					StreamID:  int64(i % bc.streams),
					Timestamp: time.Unix(int64(i), 0).UTC(),
					Line:      []byte("log line payload for benchmarking"),
				}
			}

			b.ResetTimer()
			b.ReportAllocs()

			for range b.N {
				builder, err := indexobj.NewBuilder("bench-tenant", testCalculatorConfig, nil, indexobj.NewBuilderMetrics(nil), nil)
				if err != nil {
					b.Fatal(err)
				}
				calcCtx := &logsCalculationContext{
					objectPath:         "bench/path",
					sectionIdx:         0,
					streamLabels:       streamLabels,
					streamShardBuckets: shardBuckets,
					shardLayout:        streams.DefaultShardLayout(),
					builder:            builder,
				}
				calc := &labelPostingsCalculation{}

				if err := calc.Prepare(context.Background(), calcCtx, nil, logs.Stats{}); err != nil {
					b.Fatal(err)
				}
				if err := calc.ProcessBatch(context.Background(), calcCtx, batch); err != nil {
					b.Fatal(err)
				}
				if err := calc.Flush(context.Background(), calcCtx, builder); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// flushAndReadAllPostingsRows flushes the builder and reads the rows of all its
// postings sections, in section order.
func flushAndReadAllPostingsRows(t *testing.T, builder *indexobj.Builder) []postings.Row {
	t.Helper()
	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	var rows []postings.Row
	for _, s := range obj.Sections() {
		if !postings.CheckSection(s) {
			continue
		}
		sec, err := postings.Open(t.Context(), s)
		require.NoError(t, err)

		r := postings.NewReader(postings.ReaderOptions{Columns: sec.Columns()})
		require.NoError(t, r.Open(t.Context()))

		rowReader := postings.NewRowReader(t.Context(), r)
		for rowReader.Next() {
			rows = append(rows, rowReader.At())
		}
		require.NoError(t, rowReader.Err())
		require.NoError(t, rowReader.Close())
	}
	return rows
}

// findLabelRow returns the index of the first label row with the given column
// name and label value. It returns -1 if there is none.
func findLabelRow(rows []postings.Row, columnName, labelValue string) int {
	for i, row := range rows {
		if row.Kind == postings.KindLabel && row.ColumnName == columnName && row.LabelValue == labelValue {
			return i
		}
	}
	return -1
}

// findBloomRow returns the index of the first bloom row with the given column
// name. It returns -1 if there is none.
func findBloomRow(rows []postings.Row, columnName string) int {
	for i, row := range rows {
		if row.Kind == postings.KindBloom && row.ColumnName == columnName {
			return i
		}
	}
	return -1
}

// isBitSet checks if bit n is set in a LSB-numbered bitmap.
func isBitSet(bitmap []byte, n int) bool {
	if n/8 >= len(bitmap) {
		return false
	}
	return bitmap[n/8]&(1<<(n%8)) != 0
}
