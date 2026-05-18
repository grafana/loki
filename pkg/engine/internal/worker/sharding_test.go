package worker

import (
	"slices"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// Helper function to create a record batch with timestamp and label columns
func createTestRecordBatch(t *testing.T, numRows int, timestamps []time.Time, labels map[string][]string) arrow.RecordBatch {
	t.Helper()

	alloc := memory.NewGoAllocator()

	// Build schema fields
	var fields []arrow.Field
	var builders []array.Builder

	// Add timestamp column
	timestampFQN := semconv.ColumnIdentTimestamp.FQN()
	fields = append(fields, arrow.Field{Name: timestampFQN, Type: arrow.FixedWidthTypes.Timestamp_ns})
	builders = append(builders, array.NewTimestampBuilder(alloc, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType)))

	// Sort label names for deterministic column order
	labelNames := make([]string, 0, len(labels))
	for labelName := range labels {
		labelNames = append(labelNames, labelName)
	}
	slices.Sort(labelNames)

	// Add label columns in sorted order
	for _, labelName := range labelNames {
		ident := semconv.NewIdentifier(labelName, types.ColumnTypeLabel, types.Loki.String)
		fqn := ident.FQN()
		fields = append(fields, arrow.Field{Name: fqn, Type: arrow.BinaryTypes.String})
		builders = append(builders, array.NewStringBuilder(alloc))
	}

	schema := arrow.NewSchema(fields, nil)

	// Populate data
	for i := 0; i < numRows; i++ {
		// Timestamp
		tsBuilder := builders[0].(*array.TimestampBuilder)
		if i < len(timestamps) {
			tsBuilder.Append(arrow.Timestamp(timestamps[i].UnixNano()))
		} else {
			tsBuilder.AppendNull()
		}

		// Labels in sorted order
		labelIdx := 1
		for _, labelName := range labelNames {
			values := labels[labelName]
			strBuilder := builders[labelIdx].(*array.StringBuilder)
			if i < len(values) && values[i] != "" {
				strBuilder.Append(values[i])
			} else {
				strBuilder.AppendNull()
			}
			labelIdx++
		}
	}

	// Build arrays
	var cols []arrow.Array
	for _, builder := range builders {
		cols = append(cols, builder.NewArray())
	}

	return array.NewRecordBatch(schema, cols, int64(numRows))
}

// Helper function to create a simple record batch with just timestamps
func createTimestampRecordBatch(t *testing.T, timestamps []time.Time) arrow.RecordBatch {
	t.Helper()

	alloc := memory.NewGoAllocator()
	timestampFQN := semconv.ColumnIdentTimestamp.FQN()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: timestampFQN, Type: arrow.FixedWidthTypes.Timestamp_ns},
	}, nil)

	tsBuilder := array.NewTimestampBuilder(alloc, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType))
	for _, ts := range timestamps {
		tsBuilder.Append(arrow.Timestamp(ts.UnixNano()))
	}

	return array.NewRecordBatch(schema, []arrow.Array{tsBuilder.NewArray()}, int64(len(timestamps)))
}

func TestPartitionRecordBatch_NoSharding(t *testing.T) {
	rec := createTimestampRecordBatch(t, []time.Time{
		time.Unix(10, 0),
		time.Unix(20, 0),
		time.Unix(30, 0),
	})
	defer rec.Release()

	// Test with numShards = 1
	results, err := partitionRecordBatch(rec, &workflow.SinkRouting{}, 1)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, int64(3), results[0].NumRows())

	// Test with numShards = 0
	results, err = partitionRecordBatch(rec, &workflow.SinkRouting{}, 0)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, int64(3), results[0].NumRows())
}

func TestPartitionRecordBatch_LabelHash(t *testing.T) {
	// Create a record batch with 6 rows and 2 label columns
	timestamps := []time.Time{
		time.Unix(10, 0), time.Unix(20, 0), time.Unix(30, 0),
		time.Unix(40, 0), time.Unix(50, 0), time.Unix(60, 0),
	}

	labels := map[string][]string{
		"app": {"web", "web", "api", "api", "db", "db"},
		"env": {"prod", "prod", "prod", "prod", "dev", "dev"},
	}

	rec := createTestRecordBatch(t, 6, timestamps, labels)
	defer rec.Release()

	routing := &workflow.SinkRouting{
		Strategy: workflow.SinkRoutingStrategyLabelHash,
		Grouping: physical.Grouping{
			Columns: []physical.ColumnExpression{
				&physical.ColumnExpr{Ref: types.ColumnRef{Column: "app", Type: types.ColumnTypeLabel}},
				&physical.ColumnExpr{Ref: types.ColumnRef{Column: "env", Type: types.ColumnTypeLabel}},
			},
		},
	}

	results, err := partitionRecordBatch(rec, routing, 2)
	require.NoError(t, err)
	require.Len(t, results, 2)

	// Verify that rows were distributed across shards
	totalRows := int64(0)
	for _, result := range results {
		if result != nil {
			totalRows += result.NumRows()
			result.Release()
		}
	}
	require.Equal(t, int64(6), totalRows)
}

func TestPartitionRecordBatch_TimeShard(t *testing.T) {
	// Create a record batch with timestamps spanning different time ranges
	timestamps := []time.Time{
		time.Unix(10, 0), // Should go to shard 0
		time.Unix(20, 0), // Should go to shard 0
		time.Unix(35, 0), // Should go to shard 1
		time.Unix(45, 0), // Should go to shard 1
		time.Unix(70, 0), // Should go to shard 2
	}

	rec := createTimestampRecordBatch(t, timestamps)
	defer rec.Release()

	routing := &workflow.SinkRouting{
		Strategy: workflow.SinkRoutingStrategyTimeShard,
		TimeRanges: []physical.TimeRange{
			{Start: time.Unix(0, 0), End: time.Unix(30, 0)},  // Shard 0
			{Start: time.Unix(30, 0), End: time.Unix(60, 0)}, // Shard 1
			{Start: time.Unix(60, 0), End: time.Unix(90, 0)}, // Shard 2
		},
	}

	results, err := partitionRecordBatch(rec, routing, 3)
	require.NoError(t, err)
	require.Len(t, results, 3)

	// Verify row distribution
	require.Equal(t, int64(2), results[0].NumRows()) // 10, 20
	require.Equal(t, int64(2), results[1].NumRows()) // 35, 45
	require.Equal(t, int64(1), results[2].NumRows()) // 70

	for _, result := range results {
		result.Release()
	}
}

func TestComputeLabelHashShards_NoGrouping(t *testing.T) {
	rec := createTimestampRecordBatch(t, []time.Time{
		time.Unix(10, 0),
		time.Unix(20, 0),
		time.Unix(30, 0),
	})
	defer rec.Release()

	shardIndices := make([]int, rec.NumRows())

	err := computeLabelHashShards(rec, physical.Grouping{}, 2, shardIndices)
	require.Error(t, err)
}

func TestComputeLabelHashShards_SingleLabel(t *testing.T) {
	labels := map[string][]string{
		"app": {"web", "api", "db", "web"},
	}

	rec := createTestRecordBatch(t, 4, []time.Time{
		time.Unix(10, 0), time.Unix(20, 0),
		time.Unix(30, 0), time.Unix(40, 0),
	}, labels)
	defer rec.Release()

	shardIndices := make([]int, rec.NumRows())

	grouping := physical.Grouping{
		Columns: []physical.ColumnExpression{
			&physical.ColumnExpr{Ref: types.ColumnRef{Column: "app", Type: types.ColumnTypeLabel}},
		},
	}

	err := computeLabelHashShards(rec, grouping, 2, shardIndices)
	require.NoError(t, err)

	// Verify that rows with same label value go to same shard
	require.Equal(t, shardIndices[0], shardIndices[3]) // Both "web"

	// Verify all indices are valid
	for _, idx := range shardIndices {
		require.GreaterOrEqual(t, idx, 0)
		require.Less(t, idx, 2)
	}
}

func TestComputeLabelHashShards_MultipleLabels(t *testing.T) {
	labels := map[string][]string{
		"app":    {"web", "web", "api", "api"},
		"env":    {"prod", "dev", "prod", "dev"},
		"region": {"us", "us", "eu", "eu"},
	}

	rec := createTestRecordBatch(t, 4, []time.Time{
		time.Unix(10, 0), time.Unix(20, 0),
		time.Unix(30, 0), time.Unix(40, 0),
	}, labels)
	defer rec.Release()

	shardIndices := make([]int, rec.NumRows())

	grouping := physical.Grouping{
		Columns: []physical.ColumnExpression{
			&physical.ColumnExpr{Ref: types.ColumnRef{Column: "app", Type: types.ColumnTypeLabel}},
			&physical.ColumnExpr{Ref: types.ColumnRef{Column: "env", Type: types.ColumnTypeLabel}},
			&physical.ColumnExpr{Ref: types.ColumnRef{Column: "region", Type: types.ColumnTypeLabel}},
		},
	}

	err := computeLabelHashShards(rec, grouping, 3, shardIndices)
	require.NoError(t, err)

	// Each row has unique combination of labels, so they should be distributed
	// Verify all indices are valid
	for _, idx := range shardIndices {
		require.GreaterOrEqual(t, idx, 0)
		require.Less(t, idx, 3)
	}
}

func TestComputeTimeShards_NoTimeRanges(t *testing.T) {
	rec := createTimestampRecordBatch(t, []time.Time{
		time.Unix(10, 0),
		time.Unix(20, 0),
		time.Unix(30, 0),
	})
	defer rec.Release()

	shardIndices := make([]int, rec.NumRows())

	// Empty time ranges should result in all rows going to shard 0
	err := computeTimeShards(rec, []physical.TimeRange{}, shardIndices)
	require.NoError(t, err)

	for _, idx := range shardIndices {
		require.Equal(t, 0, idx)
	}
}

func TestComputeTimeShards_SingleRange(t *testing.T) {
	timestamps := []time.Time{
		time.Unix(10, 0),
		time.Unix(20, 0),
		time.Unix(30, 0),
	}

	rec := createTimestampRecordBatch(t, timestamps)
	defer rec.Release()

	shardIndices := make([]int, rec.NumRows())

	timeRanges := []physical.TimeRange{
		{Start: time.Unix(0, 0), End: time.Unix(100, 0)},
	}

	err := computeTimeShards(rec, timeRanges, shardIndices)
	require.NoError(t, err)

	// All timestamps fall in the single range
	for _, idx := range shardIndices {
		require.Equal(t, 0, idx)
	}
}

func TestComputeTimeShards_MultipleRanges(t *testing.T) {
	timestamps := []time.Time{
		time.Unix(5, 0),  // Shard 0
		time.Unix(15, 0), // Shard 0
		time.Unix(25, 0), // Shard 1
		time.Unix(35, 0), // Shard 1
		time.Unix(45, 0), // Shard 2
	}

	rec := createTimestampRecordBatch(t, timestamps)
	defer rec.Release()

	shardIndices := make([]int, rec.NumRows())

	timeRanges := []physical.TimeRange{
		{Start: time.Unix(0, 0), End: time.Unix(20, 0)},  // Shard 0
		{Start: time.Unix(20, 0), End: time.Unix(40, 0)}, // Shard 1
		{Start: time.Unix(40, 0), End: time.Unix(60, 0)}, // Shard 2
	}

	err := computeTimeShards(rec, timeRanges, shardIndices)
	require.NoError(t, err)

	require.Equal(t, 0, shardIndices[0])
	require.Equal(t, 0, shardIndices[1])
	require.Equal(t, 1, shardIndices[2])
	require.Equal(t, 1, shardIndices[3])
	require.Equal(t, 2, shardIndices[4])
}

func TestComputeTimeShards_EdgeCases(t *testing.T) {
	timestamps := []time.Time{
		time.Unix(20, 0), // Exactly at boundary - should be in shard 1
		time.Unix(40, 0), // Exactly at boundary - should be in shard 2
	}

	rec := createTimestampRecordBatch(t, timestamps)
	defer rec.Release()

	shardIndices := make([]int, rec.NumRows())

	timeRanges := []physical.TimeRange{
		{Start: time.Unix(0, 0), End: time.Unix(20, 0)},  // Shard 0
		{Start: time.Unix(20, 0), End: time.Unix(40, 0)}, // Shard 1
		{Start: time.Unix(40, 0), End: time.Unix(60, 0)}, // Shard 2
	}

	err := computeTimeShards(rec, timeRanges, shardIndices)
	require.NoError(t, err)

	// Timestamps at start boundary should be included in that range
	require.Equal(t, 1, shardIndices[0])
	require.Equal(t, 2, shardIndices[1])
}

func TestFindTimestampColumn(t *testing.T) {
	t.Run("timestamp column exists", func(t *testing.T) {
		rec := createTimestampRecordBatch(t, []time.Time{time.Unix(10, 0)})
		defer rec.Release()

		tsCol, err := findTimestampColumn(rec)
		require.NoError(t, err)
		require.NotNil(t, tsCol)
		require.Equal(t, int64(1), int64(tsCol.Len()))
	})

	t.Run("no timestamp column", func(t *testing.T) {
		alloc := memory.NewGoAllocator()
		schema := arrow.NewSchema([]arrow.Field{
			{Name: "some_column", Type: arrow.BinaryTypes.String},
		}, nil)

		strBuilder := array.NewStringBuilder(alloc)
		strBuilder.Append("test")

		rec := array.NewRecordBatch(schema, []arrow.Array{strBuilder.NewArray()}, 1)
		defer rec.Release()

		tsCol, err := findTimestampColumn(rec)
		require.Error(t, err)
		require.Nil(t, tsCol)
	})
}

func TestSplitRecordByShards_EvenDistribution(t *testing.T) {
	// Create a record batch with 6 rows
	timestamps := []time.Time{
		time.Unix(10, 0), time.Unix(20, 0), time.Unix(30, 0),
		time.Unix(40, 0), time.Unix(50, 0), time.Unix(60, 0),
	}

	rec := createTimestampRecordBatch(t, timestamps)
	defer rec.Release()

	// Manually create shard indices: 2 rows per shard
	shardIndices := []int{0, 0, 1, 1, 2, 2}

	results, err := splitRecordByShards(rec, shardIndices, 3)
	require.NoError(t, err)
	require.Len(t, results, 3)

	for i, result := range results {
		require.Equal(t, int64(2), result.NumRows(), "shard %d should have 2 rows", i)
		result.Release()
	}
}

func TestSplitRecordByShards_UnevenDistribution(t *testing.T) {
	timestamps := []time.Time{
		time.Unix(10, 0), time.Unix(20, 0), time.Unix(30, 0),
		time.Unix(40, 0), time.Unix(50, 0),
	}

	rec := createTimestampRecordBatch(t, timestamps)
	defer rec.Release()

	// Shard 0: 3 rows, Shard 1: 2 rows, Shard 2: 0 rows
	shardIndices := []int{0, 0, 0, 1, 1}

	results, err := splitRecordByShards(rec, shardIndices, 3)
	require.NoError(t, err)
	require.Len(t, results, 3)

	require.Equal(t, int64(3), results[0].NumRows())
	require.Equal(t, int64(2), results[1].NumRows())
	require.Equal(t, int64(0), results[2].NumRows())

	for _, result := range results {
		result.Release()
	}
}

func TestSplitRecordByShards_AllToOneShard(t *testing.T) {
	timestamps := []time.Time{
		time.Unix(10, 0), time.Unix(20, 0), time.Unix(30, 0),
	}

	rec := createTimestampRecordBatch(t, timestamps)
	defer rec.Release()

	// All rows go to shard 1
	shardIndices := []int{1, 1, 1}

	results, err := splitRecordByShards(rec, shardIndices, 3)
	require.NoError(t, err)
	require.Len(t, results, 3)

	require.Equal(t, int64(0), results[0].NumRows())
	require.Equal(t, int64(3), results[1].NumRows())
	require.Equal(t, int64(0), results[2].NumRows())

	for _, result := range results {
		result.Release()
	}
}

func TestSplitRecordByShards_MultipleColumns(t *testing.T) {
	// Create a record batch with multiple column types
	alloc := memory.NewGoAllocator()

	timestampFQN := semconv.ColumnIdentTimestamp.FQN()
	labelFQN := semconv.NewIdentifier("app", types.ColumnTypeLabel, types.Loki.String).FQN()
	valueFQN := semconv.NewIdentifier("value", types.ColumnTypeGenerated, types.Loki.Float).FQN()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: timestampFQN, Type: arrow.FixedWidthTypes.Timestamp_ns},
		{Name: labelFQN, Type: arrow.BinaryTypes.String},
		{Name: valueFQN, Type: arrow.PrimitiveTypes.Float64},
	}, nil)

	tsBuilder := array.NewTimestampBuilder(alloc, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType))
	strBuilder := array.NewStringBuilder(alloc)
	float64Builder := array.NewFloat64Builder(alloc)

	// Add 4 rows
	timestamps := []time.Time{
		time.Unix(10, 0), time.Unix(20, 0),
		time.Unix(30, 0), time.Unix(40, 0),
	}
	labels := []string{"web", "api", "db", "cache"}
	values := []float64{1.0, 2.0, 3.0, 4.0}

	for i := 0; i < 4; i++ {
		tsBuilder.Append(arrow.Timestamp(timestamps[i].UnixNano()))
		strBuilder.Append(labels[i])
		float64Builder.Append(values[i])
	}

	rec := array.NewRecordBatch(schema, []arrow.Array{
		tsBuilder.NewArray(),
		strBuilder.NewArray(),
		float64Builder.NewArray(),
	}, 4)
	defer rec.Release()

	// Split: shard 0 gets rows 0,2; shard 1 gets rows 1,3
	shardIndices := []int{0, 1, 0, 1}

	results, err := splitRecordByShards(rec, shardIndices, 2)
	require.NoError(t, err)
	require.Len(t, results, 2)

	// Verify shard 0 has correct data
	require.Equal(t, int64(2), results[0].NumRows())
	shard0StrCol := results[0].Column(1).(*array.String)
	require.Equal(t, "web", shard0StrCol.Value(0))
	require.Equal(t, "db", shard0StrCol.Value(1))

	// Verify shard 1 has correct data
	require.Equal(t, int64(2), results[1].NumRows())
	shard1StrCol := results[1].Column(1).(*array.String)
	require.Equal(t, "api", shard1StrCol.Value(0))
	require.Equal(t, "cache", shard1StrCol.Value(1))

	for _, result := range results {
		result.Release()
	}
}

func TestSplitRecordByShards_WithNulls(t *testing.T) {
	alloc := memory.NewGoAllocator()

	timestampFQN := semconv.ColumnIdentTimestamp.FQN()
	labelFQN := semconv.NewIdentifier("app", types.ColumnTypeLabel, types.Loki.String).FQN()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: timestampFQN, Type: arrow.FixedWidthTypes.Timestamp_ns},
		{Name: labelFQN, Type: arrow.BinaryTypes.String},
	}, nil)

	tsBuilder := array.NewTimestampBuilder(alloc, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType))
	strBuilder := array.NewStringBuilder(alloc)

	// Add 3 rows with one null label
	tsBuilder.Append(arrow.Timestamp(time.Unix(10, 0).UnixNano()))
	strBuilder.Append("web")

	tsBuilder.Append(arrow.Timestamp(time.Unix(20, 0).UnixNano()))
	strBuilder.AppendNull() // Null label

	tsBuilder.Append(arrow.Timestamp(time.Unix(30, 0).UnixNano()))
	strBuilder.Append("api")

	rec := array.NewRecordBatch(schema, []arrow.Array{
		tsBuilder.NewArray(),
		strBuilder.NewArray(),
	}, 3)
	defer rec.Release()

	// All rows go to shard 0
	shardIndices := []int{0, 0, 0}

	results, err := splitRecordByShards(rec, shardIndices, 2)
	require.NoError(t, err)
	require.Len(t, results, 2)

	// Verify shard 0 has all rows including the null
	require.Equal(t, int64(3), results[0].NumRows())
	shard0StrCol := results[0].Column(1).(*array.String)
	require.False(t, shard0StrCol.IsNull(0))
	require.True(t, shard0StrCol.IsNull(1)) // Null preserved
	require.False(t, shard0StrCol.IsNull(2))

	// Verify shard 1 is empty
	require.Equal(t, int64(0), results[1].NumRows())

	for _, result := range results {
		result.Release()
	}
}

func TestSimpleEvaluatorForSharding(t *testing.T) {
	labels := map[string][]string{
		"app": {"web", "api"},
	}

	rec := createTestRecordBatch(t, 2, []time.Time{
		time.Unix(10, 0), time.Unix(20, 0),
	}, labels)
	defer rec.Release()

	eval := &simpleEvaluatorForSharding{}

	t.Run("column expression returns array", func(t *testing.T) {
		colExpr := &physical.ColumnExpr{
			Ref: types.ColumnRef{Column: "app", Type: types.ColumnTypeLabel},
		}

		arr, err := eval.EvalForGrouping(colExpr, rec)
		require.NoError(t, err)
		require.NotNil(t, arr)

		strArr := arr.(*array.String)
		require.Equal(t, "web", strArr.Value(0))
		require.Equal(t, "api", strArr.Value(1))
	})

	t.Run("non-column expression returns error", func(t *testing.T) {
		// Use a non-column expression
		literalExpr := physical.NewLiteral("test")

		arr, err := eval.EvalForGrouping(literalExpr, rec)
		require.Error(t, err)
		require.Nil(t, arr)
	})

	t.Run("non-existent column returns error", func(t *testing.T) {
		colExpr := &physical.ColumnExpr{
			Ref: types.ColumnRef{Column: "nonexistent", Type: types.ColumnTypeLabel},
		}

		arr, err := eval.EvalForGrouping(colExpr, rec)
		require.Error(t, err)
		require.Nil(t, arr)
	})
}

func TestComputeTimeShards_NoTimestampColumn(t *testing.T) {
	alloc := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "some_column", Type: arrow.BinaryTypes.String},
	}, nil)

	strBuilder := array.NewStringBuilder(alloc)
	strBuilder.AppendValues([]string{"first", "second"}, nil)

	rec := array.NewRecordBatch(schema, []arrow.Array{strBuilder.NewArray()}, 2)
	defer rec.Release()

	shardIndices := make([]int, rec.NumRows())
	timeRanges := []physical.TimeRange{
		{Start: time.Unix(0, 0), End: time.Unix(100, 0)},
		{Start: time.Unix(100, 0), End: time.Unix(200, 0)},
	}

	err := computeTimeShards(rec, timeRanges, shardIndices)
	require.NoError(t, err)
	require.Equal(t, []int{0, 0}, shardIndices)
}

func TestComputeLabelHashShards_WithoutGrouping(t *testing.T) {
	// Create a record with 4 rows and 3 label columns
	labels := map[string][]string{
		"app":    {"web", "web", "api", "api"},
		"env":    {"prod", "prod", "prod", "dev"},
		"region": {"us", "eu", "us", "us"},
	}

	rec := createTestRecordBatch(t, 4, []time.Time{
		time.Unix(10, 0),
		time.Unix(20, 0),
		time.Unix(30, 0),
		time.Unix(40, 0),
	}, labels)
	defer rec.Release()

	shardIndices := make([]int, rec.NumRows())

	// Use "without" grouping to exclude "app" column
	// This means rows with same env+region should go to same shard
	grouping := physical.Grouping{
		Without: true,
		Columns: []physical.ColumnExpression{
			&physical.ColumnExpr{Ref: types.ColumnRef{Column: "app", Type: types.ColumnTypeLabel}},
		},
	}

	err := computeLabelHashShards(rec, grouping, 4, shardIndices)
	require.NoError(t, err)

	// Rows 0 and 1 have same env=prod and region=us/eu, so they might differ
	// But row 0 (web, prod, us) and row 2 (api, prod, us) should go to the SAME shard
	// because they have the same env+region (everything except app)
	require.Equal(t, shardIndices[0], shardIndices[2], "rows with same env+region should go to same shard")

	// Verify all indices are valid
	for _, idx := range shardIndices {
		require.GreaterOrEqual(t, idx, 0)
		require.Less(t, idx, 4)
	}
}
