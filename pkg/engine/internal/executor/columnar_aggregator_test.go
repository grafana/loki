package executor

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/cespare/xxhash/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

var (
	columnarGroupBy = []arrow.Field{
		semconv.FieldFromIdent(semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String), true),
		semconv.FieldFromIdent(semconv.NewIdentifier("service", types.ColumnTypeLabel, types.Loki.String), true),
	}
)

func makeTimestampArray(mem memory.Allocator, values []int64) *array.Timestamp {
	builder := array.NewTimestampBuilder(mem, &arrow.TimestampType{Unit: arrow.Nanosecond})
	defer builder.Release()
	for _, v := range values {
		builder.Append(arrow.Timestamp(v))
	}
	return builder.NewTimestampArray()
}

func makeFloat64Array(mem memory.Allocator, values []float64) *array.Float64 {
	builder := array.NewFloat64Builder(mem)
	defer builder.Release()
	for _, v := range values {
		builder.Append(v)
	}
	return builder.NewFloat64Array()
}

func makeStringArray(mem memory.Allocator, values []string) *array.String {
	builder := array.NewStringBuilder(mem)
	defer builder.Release()
	for _, v := range values {
		builder.Append(v)
	}
	return builder.NewStringArray()
}

func TestColumnarAggregator(t *testing.T) {
	colTs := semconv.ColumnIdentTimestamp.FQN()
	colVal := semconv.ColumnIdentValue.FQN()
	colEnv := semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String).FQN()
	colSvc := semconv.NewIdentifier("service", types.ColumnTypeLabel, types.Loki.String).FQN()

	mem := memory.NewGoAllocator()

	ts1 := int64(1704103200000000000) // 2024-01-01 10:00:00 UTC
	ts2 := int64(1704103260000000000) // 2024-01-01 10:01:00 UTC

	t.Run("basic SUM aggregation", func(t *testing.T) {
		agg := newColumnarAggregator(10, aggregationOperationSum, columnarGroupBy, nil)

		timestamps := makeTimestampArray(mem, []int64{
			ts1, ts1, ts1,
			ts2, ts2, ts2,
			ts1, ts2,
		})
		defer timestamps.Release()

		values := makeFloat64Array(mem, []float64{
			10, 20, 30,
			15, 25, 35,
			5, 10,
		})
		defer values.Release()

		envCol := makeStringArray(mem, []string{
			"prod", "prod", "dev",
			"prod", "prod", "dev",
			"prod", "prod",
		})
		defer envCol.Release()

		svcCol := makeStringArray(mem, []string{
			"app1", "app2", "app1",
			"app1", "app2", "app2",
			"app1", "app1",
		})
		defer svcCol.Release()

		err := agg.AddBatch(timestamps, values, []*array.String{envCol, svcCol})
		require.NoError(t, err)

		record, err := agg.BuildRecord()
		require.NoError(t, err)

		expect := arrowtest.Rows{
			{colTs: arrow.Timestamp(ts1).ToTime(arrow.Nanosecond), colVal: float64(15), colEnv: "prod", colSvc: "app1"},
			{colTs: arrow.Timestamp(ts1).ToTime(arrow.Nanosecond), colVal: float64(20), colEnv: "prod", colSvc: "app2"},
			{colTs: arrow.Timestamp(ts1).ToTime(arrow.Nanosecond), colVal: float64(30), colEnv: "dev", colSvc: "app1"},

			{colTs: arrow.Timestamp(ts2).ToTime(arrow.Nanosecond), colVal: float64(25), colEnv: "prod", colSvc: "app1"},
			{colTs: arrow.Timestamp(ts2).ToTime(arrow.Nanosecond), colVal: float64(25), colEnv: "prod", colSvc: "app2"},
			{colTs: arrow.Timestamp(ts2).ToTime(arrow.Nanosecond), colVal: float64(35), colEnv: "dev", colSvc: "app2"},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")
		require.Equal(t, len(expect), len(rows), "number of rows should match")
		require.ElementsMatch(t, expect, rows)
	})

	t.Run("basic AVG aggregation", func(t *testing.T) {
		agg := newColumnarAggregator(10, aggregationOperationAvg, columnarGroupBy, nil)

		timestamps := makeTimestampArray(mem, []int64{ts1, ts1})
		defer timestamps.Release()

		values := makeFloat64Array(mem, []float64{10, 30})
		defer values.Release()

		envCol := makeStringArray(mem, []string{"prod", "prod"})
		defer envCol.Release()

		svcCol := makeStringArray(mem, []string{"app1", "app1"})
		defer svcCol.Release()

		err := agg.AddBatch(timestamps, values, []*array.String{envCol, svcCol})
		require.NoError(t, err)

		record, err := agg.BuildRecord()
		require.NoError(t, err)

		expect := arrowtest.Rows{
			{colTs: arrow.Timestamp(ts1).ToTime(arrow.Nanosecond), colVal: float64(20), colEnv: "prod", colSvc: "app1"},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err)
		require.Equal(t, len(expect), len(rows))
		require.ElementsMatch(t, expect, rows)
	})

	t.Run("basic COUNT aggregation", func(t *testing.T) {
		agg := newColumnarAggregator(10, aggregationOperationCount, columnarGroupBy, nil)

		timestamps := makeTimestampArray(mem, []int64{ts1, ts1, ts1})
		defer timestamps.Release()

		values := makeFloat64Array(mem, []float64{10, 20, 30})
		defer values.Release()

		envCol := makeStringArray(mem, []string{"prod", "prod", "prod"})
		defer envCol.Release()

		svcCol := makeStringArray(mem, []string{"app1", "app1", "app2"})
		defer svcCol.Release()

		err := agg.AddBatch(timestamps, values, []*array.String{envCol, svcCol})
		require.NoError(t, err)

		record, err := agg.BuildRecord()
		require.NoError(t, err)

		expect := arrowtest.Rows{
			{colTs: arrow.Timestamp(ts1).ToTime(arrow.Nanosecond), colVal: float64(2), colEnv: "prod", colSvc: "app1"},
			{colTs: arrow.Timestamp(ts1).ToTime(arrow.Nanosecond), colVal: float64(1), colEnv: "prod", colSvc: "app2"},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err)
		require.Equal(t, len(expect), len(rows))
		require.ElementsMatch(t, expect, rows)
	})

	t.Run("basic MAX aggregation", func(t *testing.T) {
		agg := newColumnarAggregator(10, aggregationOperationMax, columnarGroupBy, nil)

		timestamps := makeTimestampArray(mem, []int64{ts1, ts1, ts1})
		defer timestamps.Release()

		values := makeFloat64Array(mem, []float64{10, 30, 20})
		defer values.Release()

		envCol := makeStringArray(mem, []string{"prod", "prod", "prod"})
		defer envCol.Release()

		svcCol := makeStringArray(mem, []string{"app1", "app1", "app1"})
		defer svcCol.Release()

		err := agg.AddBatch(timestamps, values, []*array.String{envCol, svcCol})
		require.NoError(t, err)

		record, err := agg.BuildRecord()
		require.NoError(t, err)

		expect := arrowtest.Rows{
			{colTs: arrow.Timestamp(ts1).ToTime(arrow.Nanosecond), colVal: float64(30), colEnv: "prod", colSvc: "app1"},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err)
		require.Equal(t, len(expect), len(rows))
		require.ElementsMatch(t, expect, rows)
	})

	t.Run("basic MIN aggregation", func(t *testing.T) {
		agg := newColumnarAggregator(10, aggregationOperationMin, columnarGroupBy, nil)

		timestamps := makeTimestampArray(mem, []int64{ts1, ts1, ts1})
		defer timestamps.Release()

		values := makeFloat64Array(mem, []float64{30, 10, 20})
		defer values.Release()

		envCol := makeStringArray(mem, []string{"prod", "prod", "prod"})
		defer envCol.Release()

		svcCol := makeStringArray(mem, []string{"app1", "app1", "app1"})
		defer svcCol.Release()

		err := agg.AddBatch(timestamps, values, []*array.String{envCol, svcCol})
		require.NoError(t, err)

		record, err := agg.BuildRecord()
		require.NoError(t, err)

		expect := arrowtest.Rows{
			{colTs: arrow.Timestamp(ts1).ToTime(arrow.Nanosecond), colVal: float64(10), colEnv: "prod", colSvc: "app1"},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err)
		require.Equal(t, len(expect), len(rows))
		require.ElementsMatch(t, expect, rows)
	})

	t.Run("SUM aggregation with empty groupBy", func(t *testing.T) {
		agg := newColumnarAggregator(1, aggregationOperationSum, nil, nil)

		timestamps := makeTimestampArray(mem, []int64{ts1, ts1, ts1, ts2, ts2})
		defer timestamps.Release()

		values := makeFloat64Array(mem, []float64{10, 20, 30, 15, 25})
		defer values.Release()

		err := agg.AddBatch(timestamps, values, []*array.String{})
		require.NoError(t, err)

		record, err := agg.BuildRecord()
		require.NoError(t, err)

		expect := arrowtest.Rows{
			{colTs: arrow.Timestamp(ts1).ToTime(arrow.Nanosecond), colVal: float64(60)},
			{colTs: arrow.Timestamp(ts2).ToTime(arrow.Nanosecond), colVal: float64(40)},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err)
		require.Equal(t, len(expect), len(rows))
		require.ElementsMatch(t, expect, rows)
	})

	t.Run("series limit enforcement", func(t *testing.T) {
		agg := newColumnarAggregator(10, aggregationOperationSum, columnarGroupBy, nil)
		agg.SetMaxSeries(2)

		timestamps1 := makeTimestampArray(mem, []int64{ts1, ts1})
		defer timestamps1.Release()

		values1 := makeFloat64Array(mem, []float64{10, 20})
		defer values1.Release()

		envCol1 := makeStringArray(mem, []string{"prod", "prod"})
		defer envCol1.Release()

		svcCol1 := makeStringArray(mem, []string{"app1", "app2"})
		defer svcCol1.Release()

		err := agg.AddBatch(timestamps1, values1, []*array.String{envCol1, svcCol1})
		require.NoError(t, err)

		timestamps2 := makeTimestampArray(mem, []int64{ts2})
		defer timestamps2.Release()

		values2 := makeFloat64Array(mem, []float64{30})
		defer values2.Release()

		envCol2 := makeStringArray(mem, []string{"dev"})
		defer envCol2.Release()

		svcCol2 := makeStringArray(mem, []string{"app1"})
		defer svcCol2.Release()

		err = agg.AddBatch(timestamps2, values2, []*array.String{envCol2, svcCol2})
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrSeriesLimitExceeded))

		timestamps3 := makeTimestampArray(mem, []int64{ts2})
		defer timestamps3.Release()

		values3 := makeFloat64Array(mem, []float64{15})
		defer values3.Release()

		envCol3 := makeStringArray(mem, []string{"prod"})
		defer envCol3.Release()

		svcCol3 := makeStringArray(mem, []string{"app1"})
		defer svcCol3.Release()

		agg.Reset()
		agg.SetMaxSeries(0)

		err = agg.AddBatch(timestamps3, values3, []*array.String{envCol3, svcCol3})
		require.NoError(t, err)
	})

	t.Run("multiple batches", func(t *testing.T) {
		agg := newColumnarAggregator(10, aggregationOperationSum, columnarGroupBy, nil)

		timestamps1 := makeTimestampArray(mem, []int64{ts1, ts1})
		defer timestamps1.Release()

		values1 := makeFloat64Array(mem, []float64{10, 20})
		defer values1.Release()

		envCol1 := makeStringArray(mem, []string{"prod", "prod"})
		defer envCol1.Release()

		svcCol1 := makeStringArray(mem, []string{"app1", "app2"})
		defer svcCol1.Release()

		err := agg.AddBatch(timestamps1, values1, []*array.String{envCol1, svcCol1})
		require.NoError(t, err)

		timestamps2 := makeTimestampArray(mem, []int64{ts1, ts1})
		defer timestamps2.Release()

		values2 := makeFloat64Array(mem, []float64{5, 10})
		defer values2.Release()

		envCol2 := makeStringArray(mem, []string{"prod", "prod"})
		defer envCol2.Release()

		svcCol2 := makeStringArray(mem, []string{"app1", "app2"})
		defer svcCol2.Release()

		err = agg.AddBatch(timestamps2, values2, []*array.String{envCol2, svcCol2})
		require.NoError(t, err)

		record, err := agg.BuildRecord()
		require.NoError(t, err)

		expect := arrowtest.Rows{
			{colTs: arrow.Timestamp(ts1).ToTime(arrow.Nanosecond), colVal: float64(15), colEnv: "prod", colSvc: "app1"},
			{colTs: arrow.Timestamp(ts1).ToTime(arrow.Nanosecond), colVal: float64(30), colEnv: "prod", colSvc: "app2"},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err)
		require.Equal(t, len(expect), len(rows))
		require.ElementsMatch(t, expect, rows)
	})

	t.Run("with window function", func(t *testing.T) {
		windowStart := arrow.Timestamp(ts1).ToTime(arrow.Nanosecond)

		w0End := windowStart.UnixNano()
		w0Start := windowStart.Add(-60 * 1e9).UnixNano()

		matchFunc := columnarWindowMatchFunc(func(inputTs, outTs []int64, validMask []bool, numRows int) {
			for i := range numRows {
				if !validMask[i] {
					continue
				}
				ts := inputTs[i]
				if ts > w0Start && ts <= w0End {
					outTs[i] = w0End
				} else {
					validMask[i] = false
				}
			}
		})

		agg := newColumnarAggregator(10, aggregationOperationSum, columnarGroupBy, matchFunc)

		tsInWindow := windowStart.Add(-15 * 1e9).UnixNano()

		timestamps := makeTimestampArray(mem, []int64{tsInWindow})
		defer timestamps.Release()

		values := makeFloat64Array(mem, []float64{100})
		defer values.Release()

		envCol := makeStringArray(mem, []string{"prod"})
		defer envCol.Release()

		svcCol := makeStringArray(mem, []string{"app1"})
		defer svcCol.Release()

		err := agg.AddBatch(timestamps, values, []*array.String{envCol, svcCol})
		require.NoError(t, err)

		record, err := agg.BuildRecord()
		require.NoError(t, err)

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err)

		require.Equal(t, 1, len(rows))
		require.Equal(t, float64(100), rows[0][colVal])
		require.Equal(t, "prod", rows[0][colEnv])
		require.Equal(t, "app1", rows[0][colSvc])
		require.Equal(t, time.Unix(0, w0End).UTC(), rows[0][colTs])
	})

	t.Run("with window function filtering out-of-range", func(t *testing.T) {
		matchFunc := columnarWindowMatchFunc(func(_, _ []int64, validMask []bool, numRows int) {
			for i := range numRows {
				validMask[i] = false
			}
		})

		agg := newColumnarAggregator(10, aggregationOperationSum, columnarGroupBy, matchFunc)

		timestamps := makeTimestampArray(mem, []int64{ts1, ts2})
		defer timestamps.Release()

		values := makeFloat64Array(mem, []float64{100, 200})
		defer values.Release()

		envCol := makeStringArray(mem, []string{"prod", "prod"})
		defer envCol.Release()

		svcCol := makeStringArray(mem, []string{"app1", "app1"})
		defer svcCol.Release()

		err := agg.AddBatch(timestamps, values, []*array.String{envCol, svcCol})
		require.NoError(t, err)

		record, err := agg.BuildRecord()
		require.NoError(t, err)

		require.Equal(t, int64(0), record.NumRows(), "all timestamps should be filtered out")
	})

	t.Run("composite key correctness", func(t *testing.T) {
		windowStart := arrow.Timestamp(ts1).ToTime(arrow.Nanosecond)

		w0Start := windowStart.Add(-120 * 1e9).UnixNano()
		w0End := windowStart.Add(-60 * 1e9).UnixNano()
		w1Start := windowStart.Add(-60 * 1e9).UnixNano()
		w1End := windowStart.UnixNano()

		matchFunc := columnarWindowMatchFunc(func(inputTs, outTs []int64, validMask []bool, numRows int) {
			for i := range numRows {
				if !validMask[i] {
					continue
				}
				ts := inputTs[i]
				if ts > w0Start && ts <= w0End {
					outTs[i] = w0End
				} else if ts > w1Start && ts <= w1End {
					outTs[i] = w1End
				} else {
					validMask[i] = false
				}
			}
		})

		agg := newColumnarAggregator(10, aggregationOperationSum, columnarGroupBy, matchFunc)

		tsW0 := windowStart.Add(-90 * 1e9).UnixNano() // falls in window 0
		tsW1 := windowStart.Add(-30 * 1e9).UnixNano() // falls in window 1

		timestamps := makeTimestampArray(mem, []int64{tsW0, tsW1})
		defer timestamps.Release()

		values := makeFloat64Array(mem, []float64{10, 20})
		defer values.Release()

		envCol := makeStringArray(mem, []string{"prod", "prod"})
		defer envCol.Release()

		svcCol := makeStringArray(mem, []string{"app1", "app1"})
		defer svcCol.Release()

		err := agg.AddBatch(timestamps, values, []*array.String{envCol, svcCol})
		require.NoError(t, err)

		record, err := agg.BuildRecord()
		require.NoError(t, err)

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err)

		require.Equal(t, 2, len(rows), "same series in different windows gets different groups")

		for _, row := range rows {
			require.Equal(t, "prod", row[colEnv])
			require.Equal(t, "app1", row[colSvc])
		}

		valSet := map[float64]bool{}
		for _, row := range rows {
			valSet[row[colVal].(float64)] = true
		}
		require.True(t, valSet[10], "window 0 should have value 10")
		require.True(t, valSet[20], "window 1 should have value 20")
	})
}

func TestCombineHashes(t *testing.T) {
	h1 := xxhash.Sum64String("foo")
	h2 := xxhash.Sum64String("bar")

	combined1 := combineHashes(h1, h2)
	combined2 := combineHashes(h2, h1)

	require.NotEqual(t, combined1, combined2, "hash combine should be order-dependent")

	combined3 := combineHashes(h1, h2)
	require.Equal(t, combined1, combined3, "hash combine should be deterministic")
}

func TestGroupKey(t *testing.T) {
	h := xxhash.Sum64String("test-series")

	k1 := groupKey{tsNano: 100, seriesHash: h}
	k2 := groupKey{tsNano: 200, seriesHash: h}
	k3 := groupKey{tsNano: 100, seriesHash: h}
	k4 := groupKey{tsNano: 100, seriesHash: h + 1}

	require.NotEqual(t, k1, k2, "different timestamps should produce different keys")
	require.Equal(t, k1, k3, "same inputs should produce same keys")
	require.NotEqual(t, k1, k4, "different series hashes should produce different keys")
}

// benchBatch holds pre-built Arrow arrays for one batch in a benchmark scenario.
type benchBatch struct {
	ts   *array.Timestamp
	vals *array.Float64
	cols []*array.String
}

func (bb benchBatch) Release() {
	bb.ts.Release()
	bb.vals.Release()
	for _, c := range bb.cols {
		c.Release()
	}
}

func buildBenchBatches(mem memory.Allocator, numRows, numBatches, envMod, clusterMod, serviceMod int) []benchBatch {
	baseTs := int64(1704103200000000000)
	batches := make([]benchBatch, numBatches)

	for bi := range numBatches {
		batchOffset := int64(bi) * int64(numRows) * 1_000_000_000

		timestamps := make([]int64, numRows)
		values := make([]float64, numRows)
		envs := make([]string, numRows)
		clusters := make([]string, numRows)
		services := make([]string, numRows)

		for i := range numRows {
			timestamps[i] = baseTs + batchOffset + int64(i%100)*1_000_000_000
			values[i] = float64(i)
			envs[i] = fmt.Sprintf("env-%d", i%envMod)
			clusters[i] = fmt.Sprintf("cluster-%d", i%clusterMod)
			services[i] = fmt.Sprintf("service-%d", i%serviceMod)
		}

		batches[bi] = benchBatch{
			ts:   makeTimestampArray(mem, timestamps),
			vals: makeFloat64Array(mem, values),
			cols: []*array.String{
				makeStringArray(mem, envs),
				makeStringArray(mem, clusters),
				makeStringArray(mem, services),
			},
		}
	}
	return batches
}

// BenchmarkColumnarAggregator measures the per-batch steady-state cost of
// AddBatch. The aggregator is warmed up with one batch so the group map is
// populated, then each b.N iteration processes a single batch.
func BenchmarkColumnarAggregator(b *testing.B) {
	mem := memory.NewGoAllocator()

	fields := []arrow.Field{
		semconv.FieldFromIdent(semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String), true),
		semconv.FieldFromIdent(semconv.NewIdentifier("cluster", types.ColumnTypeLabel, types.Loki.String), true),
		semconv.FieldFromIdent(semconv.NewIdentifier("service", types.ColumnTypeLabel, types.Loki.String), true),
	}

	for _, tc := range []struct {
		name       string
		numRows    int
		envMod     int
		clusterMod int
		serviceMod int
	}{
		{"100rows_low_card", 100, 2, 3, 2},
		{"1k_rows_medium_card", 1_000, 3, 10, 7},
		{"10k_rows_high_card", 10_000, 10, 50, 20},
		{"10k_rows_low_card", 10_000, 2, 3, 2},
	} {
		b.Run(tc.name, func(b *testing.B) {
			batches := buildBenchBatches(mem, tc.numRows, 1, tc.envMod, tc.clusterMod, tc.serviceMod)
			batch := batches[0]
			defer batch.Release()

			agg := newColumnarAggregator(100, aggregationOperationSum, fields, nil)
			_ = agg.AddBatch(batch.ts, batch.vals, batch.cols)

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = agg.AddBatch(batch.ts, batch.vals, batch.cols)
			}
			b.ReportMetric(float64(tc.numRows)*float64(b.N)/b.Elapsed().Seconds(), "rows/s")
		})
	}
}

// BenchmarkAggregator_Comparison compares per-batch steady-state throughput
// of the columnar aggregator against the row-based aggregator.
func BenchmarkAggregator_Comparison(b *testing.B) {
	mem := memory.NewGoAllocator()

	fields := []arrow.Field{
		semconv.FieldFromIdent(semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String), true),
		semconv.FieldFromIdent(semconv.NewIdentifier("cluster", types.ColumnTypeLabel, types.Loki.String), true),
		semconv.FieldFromIdent(semconv.NewIdentifier("service", types.ColumnTypeLabel, types.Loki.String), true),
	}

	for _, tc := range []struct {
		name       string
		numRows    int
		envMod     int
		clusterMod int
		serviceMod int
	}{
		{"1k_rows_medium_card", 1_000, 3, 10, 7},
		{"10k_rows_high_card", 10_000, 10, 50, 20},
		{"10k_rows_low_card", 10_000, 2, 3, 2},
	} {
		b.Run(tc.name, func(b *testing.B) {
			batches := buildBenchBatches(mem, tc.numRows, 1, tc.envMod, tc.clusterMod, tc.serviceMod)
			batch := batches[0]
			defer batch.Release()

			baseTs := int64(1704103200000000000)
			timestamps := make([]int64, tc.numRows)
			values := make([]float64, tc.numRows)
			envs := make([]string, tc.numRows)
			clusters := make([]string, tc.numRows)
			services := make([]string, tc.numRows)
			for i := range tc.numRows {
				timestamps[i] = baseTs + int64(i%100)*1_000_000_000
				values[i] = float64(i)
				envs[i] = fmt.Sprintf("env-%d", i%tc.envMod)
				clusters[i] = fmt.Sprintf("cluster-%d", i%tc.clusterMod)
				services[i] = fmt.Sprintf("service-%d", i%tc.serviceMod)
			}

			b.Run("columnar", func(b *testing.B) {
				agg := newColumnarAggregator(100, aggregationOperationSum, fields, nil)
				_ = agg.AddBatch(batch.ts, batch.vals, batch.cols)

				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_ = agg.AddBatch(batch.ts, batch.vals, batch.cols)
				}
				b.ReportMetric(float64(tc.numRows)*float64(b.N)/b.Elapsed().Seconds(), "rows/s")
			})

			b.Run("row-based", func(b *testing.B) {
				agg := newAggregator(100, aggregationOperationSum)
				agg.AddLabels(fields)
				for row := range tc.numRows {
					ts := arrow.Timestamp(timestamps[row]).ToTime(arrow.Nanosecond)
					_ = agg.Add(ts, values[row], fields, []string{envs[row], clusters[row], services[row]})
				}

				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					for row := range tc.numRows {
						ts := arrow.Timestamp(timestamps[row]).ToTime(arrow.Nanosecond)
						_ = agg.Add(ts, values[row], fields, []string{envs[row], clusters[row], services[row]})
					}
				}
				b.ReportMetric(float64(tc.numRows)*float64(b.N)/b.Elapsed().Seconds(), "rows/s")
			})
		})
	}
}
