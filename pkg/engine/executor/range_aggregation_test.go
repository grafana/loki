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
	pipeline, err := newRangeAggregationPipeline([]Pipeline{inputA, inputB}, expressionEvaluator{}, opts)
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

func TestArithmeticMatchers(t *testing.T) {
	// Test that our arithmetic matchers work correctly by testing the full pipeline
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	fields := []arrow.Field{
		{Name: types.ColumnNameBuiltinTimestamp, Type: datatype.Arrow.Timestamp, Metadata: datatype.ColumnMetadataBuiltinTimestamp},
		{Name: "env", Type: datatype.Arrow.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeMetadata, datatype.Loki.String)},
	}
	schema := arrow.NewSchema(fields, nil)

	rows := []arrowtest.Rows{{
		{"timestamp": time.Unix(10, 0).UTC(), "env": "prod"},
		{"timestamp": time.Unix(15, 0).UTC(), "env": "prod"},
		{"timestamp": time.Unix(20, 0).UTC(), "env": "prod"},
	}}

	partitionBy := []physical.ColumnExpression{
		&physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: "env",
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
			step:          10 * time.Second, // step == range for aligned
		}

		input := NewArrowtestPipeline(alloc, schema, rows...)
		pipeline, err := newRangeAggregationPipeline([]Pipeline{input}, expressionEvaluator{}, opts)
		require.NoError(t, err)
		defer pipeline.Close()

		err = pipeline.Read(t.Context())
		require.NoError(t, err)
		record, err := pipeline.Value()
		require.NoError(t, err)
		defer record.Release()

		// Should have results for aligned windows
		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err)
		require.Greater(t, len(rows), 0)
	})

	t.Run("gapped windows", func(t *testing.T) {
		opts := rangeAggregationOptions{
			partitionBy:   partitionBy,
			startTs:       time.Unix(10, 0),
			endTs:         time.Unix(40, 0),
			rangeInterval: 5 * time.Second,
			step:          10 * time.Second, // step > range for gapped
		}

		input := NewArrowtestPipeline(alloc, schema, rows...)
		pipeline, err := newRangeAggregationPipeline([]Pipeline{input}, expressionEvaluator{}, opts)
		require.NoError(t, err)
		defer pipeline.Close()

		err = pipeline.Read(t.Context())
		require.NoError(t, err)
		record, err := pipeline.Value()
		require.NoError(t, err)
		defer record.Release()

		// Should have results for gapped windows
		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err)
		require.Greater(t, len(rows), 0)
	})

	t.Run("overlapping windows", func(t *testing.T) {
		opts := rangeAggregationOptions{
			partitionBy:   partitionBy,
			startTs:       time.Unix(10, 0),
			endTs:         time.Unix(40, 0),
			rangeInterval: 10 * time.Second,
			step:          5 * time.Second, // range > step for overlapping
		}

		input := NewArrowtestPipeline(alloc, schema, rows...)
		pipeline, err := newRangeAggregationPipeline([]Pipeline{input}, expressionEvaluator{}, opts)
		require.NoError(t, err)
		defer pipeline.Close()

		err = pipeline.Read(t.Context())
		require.NoError(t, err)
		record, err := pipeline.Value()
		require.NoError(t, err)
		defer record.Release()

		// Should have results for overlapping windows
		rows, err := arrowtest.RecordRows(record)
		require.NoError(t, err)
		require.Greater(t, len(rows), 0)
	})
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
		pipeline, err := newRangeAggregationPipeline([]Pipeline{inputA, inputB}, expressionEvaluator{}, opts)
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
		pipeline, err := newRangeAggregationPipeline([]Pipeline{inputA, inputB}, expressionEvaluator{}, opts)
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
		pipeline, err := newRangeAggregationPipeline([]Pipeline{inputA, inputB}, expressionEvaluator{}, opts)
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

func TestCreateAlignedMatcher(t *testing.T) {
	var (
		startTS       = time.Unix(1000, 0)
		endTS         = time.Unix(2000, 0)
		step          = 100 * time.Second
		rangeInterval = 100 * time.Second // step = range
		opts          = rangeAggregationOptions{
			startTs:       startTS,
			endTs:         endTS,
			rangeInterval: rangeInterval,
			step:          step,
		}
	)

	pipeline := &rangeAggregationPipeline{
		opts: opts,
	}
	pipeline.init()

	tests := []struct {
		name       string
		timestamps []time.Time
		expected   []time.Time
	}{
		{
			name: "ts outside matching range",
			timestamps: []time.Time{
				// ts <= startTS - step
				time.Unix(890, 0),
				time.Unix(900, 0),
				// ts > endTS
				time.Unix(2000, 1),
				time.Unix(2050, 0),
			},
		},
		{
			name: "ts on inclusive boundary",
			timestamps: []time.Time{
				time.Unix(1000, 0),
				time.Unix(1100, 0),
				time.Unix(1200, 0),
				time.Unix(2000, 0),
			},
			expected: []time.Time{
				time.Unix(1000, 0),
				time.Unix(1100, 0),
				time.Unix(1200, 0),
				time.Unix(2000, 0),
			},
		},
		{
			name: "ts inside windows",
			timestamps: []time.Time{
				time.Unix(999, 0),
				time.Unix(1050, 0),
				time.Unix(1099, 999999999),
				time.Unix(1150, 0),
				time.Unix(1250, 0),
				time.Unix(1950, 0),
				time.Unix(1950, 1),
			},
			expected: []time.Time{
				time.Unix(1000, 0),
				time.Unix(1100, 0),
				time.Unix(1100, 0),
				time.Unix(1200, 0),
				time.Unix(1300, 0),
				time.Unix(2000, 0),
				time.Unix(2000, 0),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i, ts := range tt.timestamps {
				got := pipeline.matchingTimeWindows(ts)

				if tt.expected != nil {
					require.Len(t, got, 1, "timestamp %v should match exactly one window", ts)
					require.Equal(t, tt.expected[i], got[0])
				} else {
					require.Nil(t, got, "timestamp %v should not match any window", ts)
				}
			}
		})
	}
}

func TestCreateGappedMatcher(t *testing.T) {
	var (
		startTS       = time.Unix(1000, 0)
		endTS         = time.Unix(2000, 0)
		step          = 100 * time.Second
		rangeInterval = 50 * time.Second

		opts = rangeAggregationOptions{
			startTs:       startTS,
			endTs:         endTS,
			rangeInterval: rangeInterval,
			step:          step,
		}
	)

	pipeline := &rangeAggregationPipeline{
		opts: opts,
	}
	pipeline.init()

	tests := []struct {
		name       string
		timestamps []time.Time
		expected   []time.Time
	}{
		{
			name: "ts outside matching range",
			timestamps: []time.Time{
				// ts <= startTS
				time.Unix(900, 0),
				time.Unix(910, 0),
				time.Unix(950, 0),
				// ts > endTS
				time.Unix(2000, 1),
				time.Unix(2050, 0),
			},
		},
		{
			name: "ts on inclusive boundary",
			timestamps: []time.Time{
				time.Unix(1000, 0),
				time.Unix(1500, 0),
				time.Unix(2000, 0),
			},
			expected: []time.Time{
				time.Unix(1000, 0),
				time.Unix(1500, 0),
				time.Unix(2000, 0),
			},
		},
		{
			name: "ts inside windows",
			timestamps: []time.Time{
				time.Unix(950, 1), // right after open interval
				time.Unix(1080, 0),
				time.Unix(1570, 0),
				time.Unix(1999, 999999999),
			},
			expected: []time.Time{
				time.Unix(1000, 0),
				time.Unix(1100, 0),
				time.Unix(1600, 0),
				time.Unix(2000, 0),
			},
		},
		{
			name: "ts falling in gaps",
			timestamps: []time.Time{
				time.Unix(1040, 0),
				time.Unix(1820, 0),
				time.Unix(1949, 999999999),
			},
		},
		{
			name: "ts falling on exclusive boundary",
			timestamps: []time.Time{
				time.Unix(1050, 0),
				time.Unix(1550, 0),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i, ts := range tt.timestamps {
				got := pipeline.matchingTimeWindows(ts)

				if tt.expected != nil {
					require.Len(t, got, 1, "timestamp %v should match exactly one window", ts)
					require.Equal(t, tt.expected[i], got[0])
				} else {
					require.Nil(t, got, "timestamp %v should not match any window", ts)
				}
			}
		})
	}
}

func TestCreateOverlappingMatcher(t *testing.T) {
	var (
		startTs       = time.Unix(10, 0)
		endTs         = time.Unix(40, 0)
		rangeInterval = 10 * time.Second
		step          = 5 * time.Second

		// This creates overlapping windows:
		// Window 0: (0, 10]
		// Window 1: (5, 15]
		// Window 2: (10, 20]
		// Window 3: (15, 25]
		// Window 4: (20, 30]
		// Window 5: (25, 35]
		// Window 6: (30, 40]

		opts = rangeAggregationOptions{
			startTs:       startTs,
			endTs:         endTs,
			rangeInterval: rangeInterval,
			step:          step,
		}
	)

	pipeline := &rangeAggregationPipeline{
		opts: opts,
	}
	pipeline.init()

	testCases := []struct {
		name      string
		timestamp time.Time
		expected  []time.Time // expected window end times
	}{
		{
			name:      "timestamp in single window only",
			timestamp: time.Unix(1, 0), // only in window 0
			expected:  []time.Time{time.Unix(10, 0)},
		},
		{
			name:      "timestamp in overlap between windows 0 and 1",
			timestamp: time.Unix(8, 0), // in both window 0 and 1
			expected:  []time.Time{time.Unix(10, 0), time.Unix(15, 0)},
		},
		{
			name:      "timestamp in overlap between windows 1 and 2",
			timestamp: time.Unix(12, 0), // in both window 1 and 2
			expected:  []time.Time{time.Unix(15, 0), time.Unix(20, 0)},
		},
		{
			name:      "timestamp in overlap between windows 2 and 3",
			timestamp: time.Unix(18, 0), // in both window 2 and 3
			expected:  []time.Time{time.Unix(20, 0), time.Unix(25, 0)},
		},
		{
			name:      "timestamp in overlap between windows 3 and 4",
			timestamp: time.Unix(22, 0), // in both window 3 and 4
			expected:  []time.Time{time.Unix(25, 0), time.Unix(30, 0)},
		},
		{
			name:      "timestamp in overlap between windows 4 and 5",
			timestamp: time.Unix(28, 0), // in both window 4 and 5
			expected:  []time.Time{time.Unix(30, 0), time.Unix(35, 0)},
		},
		{
			name:      "timestamp in overlap between windows 5 and 6",
			timestamp: time.Unix(32, 0), // in both window 5 and 6
			expected:  []time.Time{time.Unix(35, 0), time.Unix(40, 0)},
		},
		{
			name:      "timestamp in single window at end",
			timestamp: time.Unix(39, 0), // only in window 6
			expected:  []time.Time{time.Unix(40, 0)},
		},
		{
			name:      "timestamp at window boundary (inclusive)",
			timestamp: time.Unix(10, 0), // at end of window 0, start of window 1
			expected:  []time.Time{time.Unix(10, 0), time.Unix(15, 0)},
		},
		{
			name:      "timestamp at window boundary (inclusive) 2",
			timestamp: time.Unix(15, 0), // at end of window 1, start of window 2
			expected:  []time.Time{time.Unix(15, 0), time.Unix(20, 0)},
		},
		{
			name:      "timestamp at window boundary (inclusive) 3",
			timestamp: time.Unix(20, 0), // at end of window 2, start of window 3
			expected:  []time.Time{time.Unix(20, 0), time.Unix(25, 0)},
		},
		{
			name:      "timestamp at window boundary (inclusive) 4",
			timestamp: time.Unix(25, 0), // at end of window 3, start of window 4
			expected:  []time.Time{time.Unix(25, 0), time.Unix(30, 0)},
		},
		{
			name:      "timestamp at window boundary (inclusive) 5",
			timestamp: time.Unix(30, 0), // at end of window 4, start of window 5
			expected:  []time.Time{time.Unix(30, 0), time.Unix(35, 0)},
		},
		{
			name:      "timestamp at window boundary (inclusive) 6",
			timestamp: time.Unix(35, 0), // at end of window 5, start of window 6
			expected:  []time.Time{time.Unix(35, 0), time.Unix(40, 0)},
		},
		{
			name:      "timestamp at final window boundary",
			timestamp: time.Unix(40, 0), // at end of window 6
			expected:  []time.Time{time.Unix(40, 0)},
		},
		{
			name:      "timestamp before first window",
			timestamp: time.Unix(0, 0), // before any window
			expected:  nil,
		},
		{
			name:      "timestamp after last window",
			timestamp: time.Unix(41, 0), // after all windows
			expected:  nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := pipeline.matchingTimeWindows(tc.timestamp)

			if tc.expected == nil {
				require.Nil(t, result, "timestamp %v should not match any window", tc.timestamp)
			} else {
				require.ElementsMatch(t, tc.expected, result,
					"timestamp %v should match windows ending at %v, got %v",
					tc.timestamp, tc.expected, result)
			}
		})
	}

}
