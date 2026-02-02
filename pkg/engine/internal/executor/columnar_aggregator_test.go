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

// Helper function to create timestamp array from nanosecond values
func makeTimestampArray(mem memory.Allocator, values []int64) *array.Timestamp {
	builder := array.NewTimestampBuilder(mem, &arrow.TimestampType{Unit: arrow.Nanosecond})
	defer builder.Release()
	for _, v := range values {
		builder.Append(arrow.Timestamp(v))
	}
	return builder.NewTimestampArray()
}

// Helper function to create float64 array
func makeFloat64Array(mem memory.Allocator, values []float64) *array.Float64 {
	builder := array.NewFloat64Builder(mem)
	defer builder.Release()
	for _, v := range values {
		builder.Append(v)
	}
	return builder.NewFloat64Array()
}

// Helper function to create string array
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

	// Timestamps in nanoseconds
	ts1 := int64(1704103200000000000) // 2024-01-01 10:00:00 UTC
	ts2 := int64(1704103260000000000) // 2024-01-01 10:01:00 UTC

	t.Run("basic SUM aggregation", func(t *testing.T) {
		agg := newColumnarAggregator(10, aggregationOperationSum, nil)
		agg.SetGroupByLabels(columnarGroupBy)

		// Create input arrays
		timestamps := makeTimestampArray(mem, []int64{
			ts1, ts1, ts1, // first 3 rows at ts1
			ts2, ts2, ts2, // next 3 rows at ts2
			ts1, ts2, // additional rows to test aggregation
		})
		defer timestamps.Release()

		values := makeFloat64Array(mem, []float64{
			10, 20, 30, // ts1: prod/app1=10, prod/app2=20, dev/app1=30
			15, 25, 35, // ts2: prod/app1=15, prod/app2=25, dev/app2=35
			5, 10, // ts1: prod/app1+=5, ts2: prod/app1+=10
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
		agg := newColumnarAggregator(10, aggregationOperationAvg, nil)
		agg.SetGroupByLabels(columnarGroupBy)

		timestamps := makeTimestampArray(mem, []int64{ts1, ts1})
		defer timestamps.Release()

		values := makeFloat64Array(mem, []float64{10, 30}) // avg should be 20
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
		agg := newColumnarAggregator(10, aggregationOperationCount, nil)
		agg.SetGroupByLabels(columnarGroupBy)

		timestamps := makeTimestampArray(mem, []int64{ts1, ts1, ts1})
		defer timestamps.Release()

		// For COUNT, values don't matter but we pass them anyway
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
		agg := newColumnarAggregator(10, aggregationOperationMax, nil)
		agg.SetGroupByLabels(columnarGroupBy)

		timestamps := makeTimestampArray(mem, []int64{ts1, ts1, ts1})
		defer timestamps.Release()

		values := makeFloat64Array(mem, []float64{10, 30, 20}) // max should be 30
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
		agg := newColumnarAggregator(10, aggregationOperationMin, nil)
		agg.SetGroupByLabels(columnarGroupBy)

		timestamps := makeTimestampArray(mem, []int64{ts1, ts1, ts1})
		defer timestamps.Release()

		values := makeFloat64Array(mem, []float64{30, 10, 20}) // min should be 10
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
		// Empty groupBy represents sum by () - all values aggregated into single group
		agg := newColumnarAggregator(1, aggregationOperationSum, nil)
		agg.SetGroupByLabels([]arrow.Field{})

		timestamps := makeTimestampArray(mem, []int64{ts1, ts1, ts1, ts2, ts2})
		defer timestamps.Release()

		values := makeFloat64Array(mem, []float64{10, 20, 30, 15, 25}) // ts1=60, ts2=40
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
		agg := newColumnarAggregator(10, aggregationOperationSum, nil)
		agg.SetGroupByLabels(columnarGroupBy)
		agg.SetMaxSeries(2) // Limit to 2 series

		// First batch with 2 unique series - should succeed
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

		// Second batch with a new series - should fail
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

		// Adding to existing series should succeed
		timestamps3 := makeTimestampArray(mem, []int64{ts2})
		defer timestamps3.Release()

		values3 := makeFloat64Array(mem, []float64{15})
		defer values3.Release()

		envCol3 := makeStringArray(mem, []string{"prod"})
		defer envCol3.Release()

		svcCol3 := makeStringArray(mem, []string{"app1"})
		defer svcCol3.Release()

		// Reset the aggregator first to clear the failed state
		agg.Reset()
		agg.SetMaxSeries(0) // Disable limit

		err = agg.AddBatch(timestamps3, values3, []*array.String{envCol3, svcCol3})
		require.NoError(t, err)
	})

	t.Run("multiple batches", func(t *testing.T) {
		agg := newColumnarAggregator(10, aggregationOperationSum, nil)
		agg.SetGroupByLabels(columnarGroupBy)

		// First batch
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

		// Second batch - same groups, different values
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
		// Create a simple window function that maps timestamps to specific windows
		// Simulate a 1-minute range with 30-second step, so timestamps can fall into multiple windows
		windowStart := arrow.Timestamp(ts1).ToTime(arrow.Nanosecond)

		windows := []window{
			{start: windowStart.Add(-60 * 1e9), end: windowStart},
			{start: windowStart.Add(-30 * 1e9), end: windowStart.Add(30 * 1e9)},
		}

		// Window function that returns matching windows
		windowFunc := func(t time.Time) []window {
			var result []window
			for _, w := range windows {
				if t.After(w.start) && !t.After(w.end) {
					result = append(result, w)
				}
			}
			return result
		}

		agg := newColumnarAggregator(10, aggregationOperationSum, windowFunc)
		agg.SetGroupByLabels(columnarGroupBy)

		// Create timestamps that fall into the windows
		// ts1 should fall into both windows (since it's exactly at windowStart)
		// Actually, window start is exclusive, end is inclusive
		// So a timestamp at windowStart won't be in window[0] (end=windowStart) but could be in window[1]
		tsInBothWindows := windowStart.Add(-15 * 1e9).UnixNano() // 15 seconds before windowStart

		timestamps := makeTimestampArray(mem, []int64{tsInBothWindows})
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

		// The timestamp should fall into both windows, so we expect 2 output rows
		// (one for each window's end time)
		require.Equal(t, 2, len(rows), "timestamp should match 2 windows")

		// Both rows should have the same value (100) and labels
		for _, row := range rows {
			require.Equal(t, float64(100), row[colVal])
			require.Equal(t, "prod", row[colEnv])
			require.Equal(t, "app1", row[colSvc])
		}

		// Verify the output timestamps correspond to window ends
		outputTs1 := rows[0][colTs].(time.Time)
		outputTs2 := rows[1][colTs].(time.Time)

		require.Contains(t, []time.Time{windows[0].end, windows[1].end}, outputTs1)
		require.Contains(t, []time.Time{windows[0].end, windows[1].end}, outputTs2)
		require.NotEqual(t, outputTs1, outputTs2, "output timestamps should be different")
	})

	t.Run("with window function filtering out-of-range", func(t *testing.T) {
		// Window function that returns empty for out-of-range timestamps
		windowFunc := func(t time.Time) []window {
			return nil // Always out of range
		}

		agg := newColumnarAggregator(10, aggregationOperationSum, windowFunc)
		agg.SetGroupByLabels(columnarGroupBy)

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

		// All timestamps are out of range, so we expect 0 output rows
		require.Equal(t, int64(0), record.NumRows(), "all timestamps should be filtered out")
	})
}

func TestCombineHashes(t *testing.T) {
	// Test that combine hashes produces different results for different orders
	h1 := xxhash.Sum64String("foo")
	h2 := xxhash.Sum64String("bar")

	combined1 := combineHashes(h1, h2)
	combined2 := combineHashes(h2, h1)

	require.NotEqual(t, combined1, combined2, "hash combine should be order-dependent")

	// Test that same inputs produce same output
	combined3 := combineHashes(h1, h2)
	require.Equal(t, combined1, combined3, "hash combine should be deterministic")
}

func TestColumnarHasher(t *testing.T) {
	mem := memory.NewGoAllocator()

	t.Run("hash single column", func(t *testing.T) {
		col := makeStringArray(mem, []string{"foo", "bar", "baz"})
		defer col.Release()

		hasher := NewColumnarHasher(10)
		hashes := hasher.HashStringColumns([]*array.String{col})

		require.Len(t, hashes, 3)
		// Verify each hash is computed correctly
		require.Equal(t, xxhash.Sum64String("foo"), hashes[0])
		require.Equal(t, xxhash.Sum64String("bar"), hashes[1])
		require.Equal(t, xxhash.Sum64String("baz"), hashes[2])
	})

	t.Run("hash multiple columns", func(t *testing.T) {
		col1 := makeStringArray(mem, []string{"a", "b"})
		defer col1.Release()

		col2 := makeStringArray(mem, []string{"x", "y"})
		defer col2.Release()

		hasher := NewColumnarHasher(10)
		hashes := hasher.HashStringColumns([]*array.String{col1, col2})

		require.Len(t, hashes, 2)

		// Verify combined hashes
		expectedHash0 := combineHashes(xxhash.Sum64String("a"), xxhash.Sum64String("x"))
		expectedHash1 := combineHashes(xxhash.Sum64String("b"), xxhash.Sum64String("y"))

		require.Equal(t, expectedHash0, hashes[0])
		require.Equal(t, expectedHash1, hashes[1])
	})

	t.Run("hash empty columns", func(t *testing.T) {
		hasher := NewColumnarHasher(10)
		hashes := hasher.HashStringColumns([]*array.String{})

		require.Len(t, hashes, 0)
	})

	t.Run("buffer reuse", func(t *testing.T) {
		col := makeStringArray(mem, []string{"foo", "bar"})
		defer col.Release()

		hasher := NewColumnarHasher(10)

		// First call
		hashes1 := hasher.HashStringColumns([]*array.String{col})
		require.Len(t, hashes1, 2)

		// Second call should reuse buffer
		hashes2 := hasher.HashStringColumns([]*array.String{col})
		require.Len(t, hashes2, 2)

		// Results should be the same
		require.Equal(t, hashes1[0], hashes2[0])
		require.Equal(t, hashes1[1], hashes2[1])
	})
}

func BenchmarkColumnarAggregator(b *testing.B) {
	mem := memory.NewGoAllocator()

	fields := []arrow.Field{
		semconv.FieldFromIdent(semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String), true),
		semconv.FieldFromIdent(semconv.NewIdentifier("cluster", types.ColumnTypeLabel, types.Loki.String), true),
		semconv.FieldFromIdent(semconv.NewIdentifier("service", types.ColumnTypeLabel, types.Loki.String), true),
	}

	// Create test data with 1000 rows
	numRows := 1000
	timestamps := make([]int64, numRows)
	values := make([]float64, numRows)
	envs := make([]string, numRows)
	clusters := make([]string, numRows)
	services := make([]string, numRows)

	baseTs := int64(1704103200000000000)
	for i := 0; i < numRows; i++ {
		timestamps[i] = baseTs + int64(i%100)*1000000000 // 100 unique timestamps
		values[i] = float64(i)
		envs[i] = fmt.Sprintf("env-%d", i%3)
		clusters[i] = fmt.Sprintf("cluster-%d", i%10)
		services[i] = fmt.Sprintf("service-%d", i%7)
	}

	// Create arrays once
	tsArr := makeTimestampArray(mem, timestamps)
	defer tsArr.Release()

	valArr := makeFloat64Array(mem, values)
	defer valArr.Release()

	envArr := makeStringArray(mem, envs)
	defer envArr.Release()

	clusterArr := makeStringArray(mem, clusters)
	defer clusterArr.Release()

	svcArr := makeStringArray(mem, services)
	defer svcArr.Release()

	cols := []*array.String{envArr, clusterArr, svcArr}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		agg := newColumnarAggregator(100, aggregationOperationSum, nil)
		agg.SetGroupByLabels(fields)

		_ = agg.AddBatch(tsArr, valArr, cols)
	}
}

func BenchmarkColumnarAggregator_HashOnly(b *testing.B) {
	mem := memory.NewGoAllocator()

	// Create test data with 1000 rows
	numRows := 1000
	envs := make([]string, numRows)
	clusters := make([]string, numRows)
	services := make([]string, numRows)

	for i := 0; i < numRows; i++ {
		envs[i] = fmt.Sprintf("env-%d", i%3)
		clusters[i] = fmt.Sprintf("cluster-%d", i%10)
		services[i] = fmt.Sprintf("service-%d", i%7)
	}

	envArr := makeStringArray(mem, envs)
	defer envArr.Release()

	clusterArr := makeStringArray(mem, clusters)
	defer clusterArr.Release()

	svcArr := makeStringArray(mem, services)
	defer svcArr.Release()

	cols := []*array.String{envArr, clusterArr, svcArr}

	hasher := NewColumnarHasher(numRows)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = hasher.HashStringColumns(cols)
	}
}

// Benchmark comparing old row-based vs new columnar approach
func BenchmarkAggregator_Comparison(b *testing.B) {
	mem := memory.NewGoAllocator()

	fields := []arrow.Field{
		semconv.FieldFromIdent(semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String), true),
		semconv.FieldFromIdent(semconv.NewIdentifier("cluster", types.ColumnTypeLabel, types.Loki.String), true),
		semconv.FieldFromIdent(semconv.NewIdentifier("service", types.ColumnTypeLabel, types.Loki.String), true),
	}

	numRows := 1000
	timestamps := make([]int64, numRows)
	values := make([]float64, numRows)
	envs := make([]string, numRows)
	clusters := make([]string, numRows)
	services := make([]string, numRows)

	baseTs := int64(1704103200000000000)
	for i := 0; i < numRows; i++ {
		timestamps[i] = baseTs + int64(i%100)*1000000000
		values[i] = float64(i)
		envs[i] = fmt.Sprintf("env-%d", i%3)
		clusters[i] = fmt.Sprintf("cluster-%d", i%10)
		services[i] = fmt.Sprintf("service-%d", i%7)
	}

	// Create arrays once
	tsArr := makeTimestampArray(mem, timestamps)
	defer tsArr.Release()

	valArr := makeFloat64Array(mem, values)
	defer valArr.Release()

	envArr := makeStringArray(mem, envs)
	defer envArr.Release()

	clusterArr := makeStringArray(mem, clusters)
	defer clusterArr.Release()

	svcArr := makeStringArray(mem, services)
	defer svcArr.Release()

	cols := []*array.String{envArr, clusterArr, svcArr}

	b.Run("columnar", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			agg := newColumnarAggregator(100, aggregationOperationSum, nil)
			agg.SetGroupByLabels(fields)
			_ = agg.AddBatch(tsArr, valArr, cols)
		}
	})

	b.Run("row-based", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			agg := newAggregator(100, aggregationOperationSum)
			agg.AddLabels(fields)

			for row := 0; row < numRows; row++ {
				ts := arrow.Timestamp(timestamps[row]).ToTime(arrow.Nanosecond)
				_ = agg.Add(ts, values[row], fields, []string{envs[row], clusters[row], services[row]})
			}
		}
	})
}
