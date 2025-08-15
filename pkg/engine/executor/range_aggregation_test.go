package executor

import (
	"slices"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

const arrowTimestampFormat = "2006-01-02T15:04:05.000000000Z"

func TestRangeAggregationPipeline_instant(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	// input schema with timestamp, partition-by columns and non-partition columns
	fields := []arrow.Field{
		{Name: types.ColumnNameBuiltinTimestamp, Type: datatype.Arrow.Timestamp, Metadata: datatype.ColumnMetadataBuiltinTimestamp},
		{Name: "env", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeLabel, datatype.Loki.String)},
		{Name: "service", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeLabel, datatype.Loki.String)},
		{Name: "severity", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeMetadata, datatype.Loki.String)}, // extra column not included in partition_by
	}
	schema := arrow.NewSchema(fields, nil)

	rowsPipelineA := []arrowtest.Rows{
		{
			{"timestamp": time.Unix(20, 0).UTC(), "env": "prod", "service": "app1", "severity": "error"}, // included
			{"timestamp": time.Unix(15, 0).UTC(), "env": "prod", "service": "app1", "severity": "info"},
			{"timestamp": time.Unix(10, 0).UTC(), "env": "prod", "service": "app1", "severity": "error"}, // excluded, open interval
			{"timestamp": time.Unix(12, 0).UTC(), "env": "prod", "service": "app2", "severity": "error"},
			{"timestamp": time.Unix(12, 0).UTC(), "env": "dev", "service": "", "severity": "error"},
		},
	}
	rowsPipelineB := []arrowtest.Rows{
		{
			{"timestamp": time.Unix(15, 0).UTC(), "env": "prod", "service": "app2", "severity": "info"},
			{"timestamp": time.Unix(12, 0).UTC(), "env": "prod", "service": "app2", "severity": "error"},
		},
		{
			{"timestamp": time.Unix(15, 0).UTC(), "env": "prod", "service": "app3", "severity": "info"},
			{"timestamp": time.Unix(12, 0).UTC(), "env": "prod", "service": "app3", "severity": "error"},
			{"timestamp": time.Unix(5, 0).UTC(), "env": "dev", "service": "app2", "severity": "error"}, // excluded, out of range
		},
	}

	opts := rangeAggregationOptions{
		partitionBy: []physical.ColumnExpression{
			&physical.ColumnExpr{
				Ref: types.ColumnRef{
					Column: "env",
					Type:   types.ColumnTypeAmbiguous,
				},
			},
			&physical.ColumnExpr{
				Ref: types.ColumnRef{
					Column: "service",
					Type:   types.ColumnTypeAmbiguous,
				},
			},
		},
		startTs:       time.Unix(20, 0).UTC(),
		endTs:         time.Unix(20, 0).UTC(),
		rangeInterval: 10 * time.Second,
	}

	inputA := NewArrowtestPipeline(alloc, schema, rowsPipelineA...)
	inputB := NewArrowtestPipeline(alloc, schema, rowsPipelineB...)
	pipeline, err := NewRangeAggregationPipeline([]Pipeline{inputA, inputB}, expressionEvaluator{}, opts)
	require.NoError(t, err)
	defer pipeline.Close()

	// Read the pipeline output
	err = pipeline.Read(t.Context())
	require.NoError(t, err)
	record, err := pipeline.Value()
	require.NoError(t, err)
	defer record.Release()

	expect := arrowtest.Rows{
		{"timestamp": time.Unix(20, 0).UTC(), "value": int64(2), "env": "prod", "service": "app1"},
		{"timestamp": time.Unix(20, 0).UTC(), "value": int64(3), "env": "prod", "service": "app2"},
		{"timestamp": time.Unix(20, 0).UTC(), "value": int64(2), "env": "prod", "service": "app3"},
		{"timestamp": time.Unix(20, 0).UTC(), "value": int64(1), "env": "dev", "service": nil},
	}

	rows, err := arrowtest.RecordRows(record)
	require.NoError(t, err, "should be able to convert record back to rows")
	require.Equal(t, len(expect), len(rows), "number of rows should match")
	require.ElementsMatch(t, expect, rows)
}

func TestRangeAggregationPipeline(t *testing.T) {
	// Test RangeAggregationPipeline for range queries (step > 0).
	// 1. Overlapping windows (range > step) - data points can appear in multiple windows
	// 2. Aligned windows (step = range) - each data point appears in exactly one window
	// 3. Non-overlapping windows (step > range) - gaps between windows
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	var (
		fields = []arrow.Field{
			{Name: types.ColumnNameBuiltinTimestamp, Type: datatype.Arrow.Timestamp, Metadata: datatype.ColumnMetadataBuiltinTimestamp},
			{Name: "env", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeMetadata, datatype.Loki.String)},
			{Name: "service", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeMetadata, datatype.Loki.String)},
			{Name: "severity", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeMetadata, datatype.Loki.String)},
		}

		schema = arrow.NewSchema(fields, nil)

		// two records from pipeline A and one from pipeline B
		rowsPipelineA = []arrowtest.Rows{
			{
				// time.Unix(0, 0) is not part of any window, it falls on the open interval of the first window
				{"timestamp": time.Unix(0, 0).UTC(), "env": "prod", "service": "app1", "severity": "info"},
				{"timestamp": time.Unix(2, 0).UTC(), "env": "prod", "service": "app1", "severity": "warn"},
				{"timestamp": time.Unix(4, 0).UTC(), "env": "prod", "service": "app1", "severity": "info"},
				{"timestamp": time.Unix(5, 0).UTC(), "env": "prod", "service": "app2", "severity": "error"},
			}, {
				{"timestamp": time.Unix(6, 0).UTC(), "env": "dev", "service": "app1", "severity": "info"},
				{"timestamp": time.Unix(8, 0).UTC(), "env": "prod", "service": "app1", "severity": "error"},
				{"timestamp": time.Unix(10, 0).UTC(), "env": "prod", "service": "app2", "severity": "info"},
				{"timestamp": time.Unix(12, 0).UTC(), "env": "prod", "service": "app1", "severity": "info"},
				{"timestamp": time.Unix(15, 0).UTC(), "env": "prod", "service": "app2", "severity": "error"},
			},
		}
		rowsPiplelineB = []arrowtest.Rows{{
			{"timestamp": time.Unix(20, 0).UTC(), "env": "dev", "service": "app1", "severity": "info"},
			{"timestamp": time.Unix(25, 0).UTC(), "env": "dev", "service": "app2", "severity": "error"},
			{"timestamp": time.Unix(28, 0).UTC(), "env": "dev", "service": "app1", "severity": "info"},
			{"timestamp": time.Unix(30, 0).UTC(), "env": "dev", "service": "app2", "severity": "info"},
		}}
	)

	partitionBy := []physical.ColumnExpression{
		&physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: "env",
				Type:   types.ColumnTypeAmbiguous,
			},
		},
		&physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: "service",
				Type:   types.ColumnTypeAmbiguous,
			},
		},
	}

	t.Run("aligned windows", func(t *testing.T) {
		opts := rangeAggregationOptions{
			partitionBy:   partitionBy,
			startTs:       time.Unix(10, 0),
			endTs:         time.Unix(40, 0),
			rangeInterval: 10 * time.Second,
			step:          10 * time.Second,
		}

		inputA := NewArrowtestPipeline(alloc, schema, rowsPipelineA...)
		inputB := NewArrowtestPipeline(alloc, schema, rowsPiplelineB...)
		pipeline, err := NewRangeAggregationPipeline([]Pipeline{inputA, inputB}, expressionEvaluator{}, opts)
		require.NoError(t, err)
		defer pipeline.Close()

		err = pipeline.Read(t.Context())
		require.NoError(t, err)
		record, err := pipeline.Value()
		require.NoError(t, err)
		defer record.Release()

		expect := arrowtest.Rows{
			// time.Unix(10, 0)
			{"timestamp": time.Unix(10, 0).UTC(), "env": "prod", "service": "app1", "value": int64(3)},
			{"timestamp": time.Unix(10, 0).UTC(), "env": "prod", "service": "app2", "value": int64(2)},
			{"timestamp": time.Unix(10, 0).UTC(), "env": "dev", "service": "app1", "value": int64(1)},

			// time.Unix(20, 0)
			{"timestamp": time.Unix(20, 0).UTC(), "env": "prod", "service": "app1", "value": int64(1)},
			{"timestamp": time.Unix(20, 0).UTC(), "env": "prod", "service": "app2", "value": int64(1)},
			{"timestamp": time.Unix(20, 0).UTC(), "env": "dev", "service": "app1", "value": int64(1)},

			// time.Unix(30, 0)
			{"timestamp": time.Unix(30, 0).UTC(), "env": "dev", "service": "app2", "value": int64(2)},
			{"timestamp": time.Unix(30, 0).UTC(), "env": "dev", "service": "app1", "value": int64(1)},

			// time.Unix(40, 0)
			{"timestamp": time.Unix(40, 0).UTC(), "env": nil, "service": nil, "value": nil},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")

		require.Equal(t, len(expect), len(rows), "number of rows should match")
		// rows are expected to be sorted by timestamp.
		// for a given timestamp, no ordering is enforced based on labels.
		require.True(t, slices.IsSortedFunc(rows, func(a, b arrowtest.Row) int {
			return a["timestamp"].(time.Time).Compare(b["timestamp"].(time.Time))
		}))
		require.ElementsMatch(t, expect, rows)
	})

	t.Run("overlapping windows", func(t *testing.T) {
		opts := rangeAggregationOptions{
			partitionBy:   partitionBy,
			startTs:       time.Unix(10, 0),
			endTs:         time.Unix(40, 0),
			rangeInterval: 10 * time.Second,
			step:          5 * time.Second,
		}

		inputA := NewArrowtestPipeline(alloc, schema, rowsPipelineA...)
		inputB := NewArrowtestPipeline(alloc, schema, rowsPiplelineB...)
		pipeline, err := NewRangeAggregationPipeline([]Pipeline{inputA, inputB}, expressionEvaluator{}, opts)
		require.NoError(t, err)
		defer pipeline.Close()

		err = pipeline.Read(t.Context())
		require.NoError(t, err)
		record, err := pipeline.Value()
		require.NoError(t, err)
		defer record.Release()

		expect := arrowtest.Rows{
			// time.Unix(10, 0)
			{"timestamp": time.Unix(10, 0).UTC(), "env": "prod", "service": "app1", "value": int64(3)},
			{"timestamp": time.Unix(10, 0).UTC(), "env": "prod", "service": "app2", "value": int64(2)},
			{"timestamp": time.Unix(10, 0).UTC(), "env": "dev", "service": "app1", "value": int64(1)},

			// time.Unix(15, 0)
			{"timestamp": time.Unix(15, 0).UTC(), "env": "prod", "service": "app2", "value": int64(2)},
			{"timestamp": time.Unix(15, 0).UTC(), "env": "prod", "service": "app1", "value": int64(2)},
			{"timestamp": time.Unix(15, 0).UTC(), "env": "dev", "service": "app1", "value": int64(1)},

			// time.Unix(20, 0)
			{"timestamp": time.Unix(20, 0).UTC(), "env": "prod", "service": "app1", "value": int64(1)},
			{"timestamp": time.Unix(20, 0).UTC(), "env": "prod", "service": "app2", "value": int64(1)},
			{"timestamp": time.Unix(20, 0).UTC(), "env": "dev", "service": "app1", "value": int64(1)},

			// time.Unix(25, 0)
			{"timestamp": time.Unix(25, 0).UTC(), "env": "dev", "service": "app1", "value": int64(1)},
			{"timestamp": time.Unix(25, 0).UTC(), "env": "dev", "service": "app2", "value": int64(1)},

			// time.Unix(30, 0)
			{"timestamp": time.Unix(30, 0).UTC(), "env": "dev", "service": "app2", "value": int64(2)},
			{"timestamp": time.Unix(30, 0).UTC(), "env": "dev", "service": "app1", "value": int64(1)},

			// time.Unix(35, 0)
			{"timestamp": time.Unix(35, 0).UTC(), "env": "dev", "service": "app2", "value": int64(1)},
			{"timestamp": time.Unix(35, 0).UTC(), "env": "dev", "service": "app1", "value": int64(1)},

			// time.Unix(40, 0)
			{"timestamp": time.Unix(40, 0).UTC(), "env": nil, "service": nil, "value": nil},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")

		require.Equal(t, len(expect), len(rows), "number of rows should match")
		// rows are expected to be sorted by timestamp.
		// for a given timestamp, no ordering is enforced based on labels.
		require.True(t, slices.IsSortedFunc(rows, func(a, b arrowtest.Row) int {
			return a["timestamp"].(time.Time).Compare(b["timestamp"].(time.Time))
		}))
		require.ElementsMatch(t, expect, rows)
	})

	t.Run("non-overlapping windows", func(t *testing.T) {
		opts := rangeAggregationOptions{
			partitionBy:   partitionBy,
			startTs:       time.Unix(10, 0),
			endTs:         time.Unix(40, 0),
			rangeInterval: 5 * time.Second,
			step:          10 * time.Second,
		}

		inputA := NewArrowtestPipeline(alloc, schema, rowsPipelineA...)
		inputB := NewArrowtestPipeline(alloc, schema, rowsPiplelineB...)
		pipeline, err := NewRangeAggregationPipeline([]Pipeline{inputA, inputB}, expressionEvaluator{}, opts)
		require.NoError(t, err)
		defer pipeline.Close()

		err = pipeline.Read(t.Context())
		require.NoError(t, err)
		record, err := pipeline.Value()
		require.NoError(t, err)
		defer record.Release()

		expect := arrowtest.Rows{
			// time.Unix(10, 0)
			{"timestamp": time.Unix(10, 0).UTC(), "env": "prod", "service": "app1", "value": int64(1)},
			{"timestamp": time.Unix(10, 0).UTC(), "env": "prod", "service": "app2", "value": int64(1)},
			{"timestamp": time.Unix(10, 0).UTC(), "env": "dev", "service": "app1", "value": int64(1)},

			// time.Unix(20, 0)
			{"timestamp": time.Unix(20, 0).UTC(), "env": "dev", "service": "app1", "value": int64(1)},

			// time.Unix(30, 0)
			{"timestamp": time.Unix(30, 0).UTC(), "env": "dev", "service": "app2", "value": int64(1)},
			{"timestamp": time.Unix(30, 0).UTC(), "env": "dev", "service": "app1", "value": int64(1)},

			// time.Unix(40, 0)
			{"timestamp": time.Unix(40, 0).UTC(), "env": nil, "service": nil, "value": nil},
		}

		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err, "should be able to convert record back to rows")

		require.Equal(t, len(expect), len(rows), "number of rows should match")
		// rows are expected to be sorted by timestamp.
		// for a given timestamp, no ordering is enforced based on labels.
		require.True(t, slices.IsSortedFunc(rows, func(a, b arrowtest.Row) int {
			return a["timestamp"].(time.Time).Compare(b["timestamp"].(time.Time))
		}))
		require.ElementsMatch(t, expect, rows)
	})
}
