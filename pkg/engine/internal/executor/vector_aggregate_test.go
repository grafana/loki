package executor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/assertions"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func init() {
	assertions.Enabled = true
}

func TestVectorAggregationPipeline(t *testing.T) {
	// input schema with timestamp, value and group by columns
	fields := []arrow.Field{
		semconv.FieldFromIdent(semconv.ColumnIdentTimestamp, false),
		semconv.FieldFromIdent(semconv.ColumnIdentValue, false),
		semconv.FieldFromFQN("utf8.label.env", true),
		semconv.FieldFromFQN("utf8.label.service", true),
	}

	now := time.Now().UTC()
	t1 := now.Add(-10 * time.Minute)
	t2 := now.Add(-5 * time.Minute)
	t3 := now

	input1CSV := strings.Join([]string{
		// t1 data
		fmt.Sprintf("%s,10,prod,app1", t1.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,20,prod,app2", t1.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,30,dev,app1", t1.Format(arrowTimestampFormat)),
		// t2 data
		fmt.Sprintf("%s,15,prod,app1", t2.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,25,prod,app2", t2.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,35,dev,app2", t2.Format(arrowTimestampFormat)),
		// t3 data
		fmt.Sprintf("%s,40,prod,app1", t3.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,50,prod,app2", t3.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,60,dev,app1", t3.Format(arrowTimestampFormat)),
	}, "\n")

	input2CSV := strings.Join([]string{
		// t1 data
		fmt.Sprintf("%s,5,prod,app1", t1.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,15,dev,app2", t1.Format(arrowTimestampFormat)),
		// t2 data
		fmt.Sprintf("%s,10,prod,app1", t2.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,20,dev,app1", t2.Format(arrowTimestampFormat)),
		// t3 data
		fmt.Sprintf("%s,30,prod,app2", t3.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,40,dev,app2", t3.Format(arrowTimestampFormat)),
	}, "\n")

	input1Record, err := CSVToArrow(fields, input1CSV)
	require.NoError(t, err)

	input2Record, err := CSVToArrow(fields, input2CSV)
	require.NoError(t, err)

	// Create input pipelines
	input1 := NewBufferedPipeline(input1Record)
	input2 := NewBufferedPipeline(input2Record)

	// Create group by expressions
	grouping := physical.Grouping{
		Columns: []physical.ColumnExpression{
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
		Without: false,
	}

	pipeline, err := newVectorAggregationPipeline([]Pipeline{input1, input2}, newExpressionEvaluator(), vectorAggregationOptions{
		grouping:       grouping,
		operation:      types.VectorAggregationTypeSum,
		maxQuerySeries: 0, // no limit for test
	})
	require.NoError(t, err)
	defer pipeline.Close()

	// Read the pipeline output
	record, err := pipeline.Read(t.Context())
	require.NoError(t, err)

	// Define expected results - sum of values for each group at each timestamp
	expected := map[time.Time]map[string]float64{
		t1: {
			"prod,app1": 15, // 10 + 5
			"prod,app2": 20, // 20
			"dev,app1":  30, // 30
			"dev,app2":  15, // 15
		},
		t2: {
			"prod,app1": 25, // 15 + 10
			"prod,app2": 25, // 25
			"dev,app1":  20, // 20
			"dev,app2":  35, // 35
		},
		t3: {
			"prod,app1": 40, // 40
			"prod,app2": 80, // 50 + 30
			"dev,app1":  60, // 60
			"dev,app2":  40, // 40
		},
	}

	// Verify results
	actual := make(map[time.Time]map[string]float64)
	for i := range int(record.NumRows()) {
		ts := record.Column(0).(*array.Timestamp).Value(i).ToTime(arrow.Nanosecond)
		value := record.Column(1).(*array.Float64).Value(i)
		env := record.Column(2).(*array.String).Value(i)
		service := record.Column(3).(*array.String).Value(i)
		key := fmt.Sprintf("%s,%s", env, service)

		if _, ok := actual[ts]; !ok {
			actual[ts] = make(map[string]float64)
		}
		actual[ts][key] = value
	}

	// Verify each timestamp's values for each group
	for ts, groups := range expected {
		require.Contains(t, actual, ts, "timestamp %v should exist", ts)
		for group, expectedValue := range groups {
			require.Contains(t, actual[ts], group, "group %s should exist at timestamp %v", group, ts)
			require.Equal(t, expectedValue, actual[ts][group],
				"value mismatch for group %s at timestamp %v", group, ts)
		}
	}
}

// TestVectorAggregationPipeline_MissingGroupingColumn verifies that a "by" grouping column
// absent from the Arrow schema is eliminated from the series.
//
// Before the fix, an absent column fell back to a scalar "" (non-null), which was included
// in the aggregation label set with empty value causing a mismatch in response compared to chunks engine.
func TestVectorAggregationPipeline_MissingGroupingColumn(t *testing.T) {
	const (
		fqnDetectedLevel = "utf8.metadata.detected_level"
		fqnLevel         = "utf8.label.level"
	)

	schemaWithLevel := arrow.NewSchema([]arrow.Field{
		semconv.FieldFromFQN(colTs, false),
		semconv.FieldFromFQN(colVal, false),
		semconv.FieldFromFQN(fqnDetectedLevel, true),
		semconv.FieldFromFQN(fqnLevel, true),
	}, nil)

	schemaWithoutLevel := arrow.NewSchema([]arrow.Field{
		semconv.FieldFromFQN(colTs, false),
		semconv.FieldFromFQN(colVal, false),
		semconv.FieldFromFQN(fqnDetectedLevel, true),
	}, nil)

	ts := time.Unix(20, 0).UTC()

	rowsWithLevel := arrowtest.Rows{
		{colTs: ts, colVal: float64(1), fqnDetectedLevel: "info", fqnLevel: nil},
		{colTs: ts, colVal: float64(2), fqnDetectedLevel: "info", fqnLevel: nil},
	}

	rowsWithoutLevel := arrowtest.Rows{
		{colTs: ts, colVal: float64(3), fqnDetectedLevel: "info"},
		{colTs: ts, colVal: float64(4), fqnDetectedLevel: "info"},
	}

	opts := vectorAggregationOptions{
		grouping: physical.Grouping{
			Columns: []physical.ColumnExpression{
				&physical.ColumnExpr{Ref: types.ColumnRef{Column: "level", Type: types.ColumnTypeAmbiguous}},
				&physical.ColumnExpr{Ref: types.ColumnRef{Column: "detected_level", Type: types.ColumnTypeAmbiguous}},
			},
		},
		operation:      types.VectorAggregationTypeSum,
		maxQuerySeries: 0,
	}

	inputA := NewArrowtestPipeline(schemaWithLevel, rowsWithLevel)
	inputB := NewArrowtestPipeline(schemaWithoutLevel, rowsWithoutLevel)

	pipeline, err := newVectorAggregationPipeline([]Pipeline{inputA, inputB}, newExpressionEvaluator(), opts)
	require.NoError(t, err)
	defer pipeline.Close()

	record, err := pipeline.Read(t.Context())
	require.NoError(t, err)

	result, err := arrowtest.RecordRows(record)
	require.NoError(t, err)

	// All four rows share the same logical series — detected_level="info" with level absent.
	// They must merge into a single bucket (sum=10), not split by whether level was in the schema.
	expect := arrowtest.Rows{
		{
			colTs:                           ts,
			colVal:                          float64(10), // 1+2+3+4
			"utf8.ambiguous.detected_level": "info",
			"utf8.ambiguous.level":          nil,
		},
	}
	require.Equal(t, len(expect), len(result), "absent grouping column must not split series into separate streams")
	require.ElementsMatch(t, expect, result)
}

// TestVectorAggregationPipeline_WithoutGroupsByShortName verifies that "without"
// grouping works correctly when columns across records or multiple columns within
// a record have the same short name but different FQNs.
//
// Take the following records with the same short name "status" column:
//
//	Record 1: `utf8.label.status`
//	Record 2: `utf8.metadata.status`
//	Record 3: both `utf8.label.status` and `utf8.metadata.status`
//
// These should be considered as `utf8.ambiguous.status` when grouping.
// For record 3, [NewCoalesce] is used to resolve precedence between the two columns.
func TestVectorAggregationPipeline_WithoutGroupsByShortName(t *testing.T) {
	const (
		fqnEnv            = "utf8.label.env"
		fqnService        = "utf8.label.service"
		fqnLabelStatus    = "utf8.label.status"
		fqnMetadataStatus = "utf8.metadata.status"
	)

	ts := time.Unix(20, 0).UTC()

	schemaA := arrow.NewSchema([]arrow.Field{
		semconv.FieldFromFQN(colTs, false),
		semconv.FieldFromFQN(colVal, false),
		semconv.FieldFromFQN(fqnService, true),
		semconv.FieldFromFQN(fqnLabelStatus, true),
		semconv.FieldFromFQN(fqnEnv, true),
	}, nil)

	schemaB := arrow.NewSchema([]arrow.Field{
		semconv.FieldFromFQN(colTs, false),
		semconv.FieldFromFQN(colVal, false),
		semconv.FieldFromFQN(fqnEnv, true),
		semconv.FieldFromFQN(fqnService, true),
		semconv.FieldFromFQN(fqnMetadataStatus, true),
	}, nil)

	schemaC := arrow.NewSchema([]arrow.Field{
		semconv.FieldFromFQN(colTs, false),
		semconv.FieldFromFQN(colVal, false),
		semconv.FieldFromFQN(fqnMetadataStatus, true),
		semconv.FieldFromFQN(fqnService, true),
		semconv.FieldFromFQN(fqnLabelStatus, true),
		semconv.FieldFromFQN(fqnEnv, true),
	}, nil)

	rowsA := arrowtest.Rows{
		{colTs: ts, colVal: float64(1), fqnService: nil, fqnLabelStatus: "200", fqnEnv: "prod"},
		{colTs: ts, colVal: float64(3), fqnService: "api", fqnLabelStatus: "500", fqnEnv: "prod"},
	}

	rowsB := arrowtest.Rows{
		{colTs: ts, colVal: float64(2), fqnEnv: "prod", fqnService: nil, fqnMetadataStatus: "200"},
		{colTs: ts, colVal: float64(4), fqnEnv: "prod", fqnService: "api", fqnMetadataStatus: "500"},
	}

	rowsC := arrowtest.Rows{
		{colTs: ts, colVal: float64(5), fqnMetadataStatus: "200", fqnService: nil, fqnLabelStatus: nil, fqnEnv: "prod"},
		{colTs: ts, colVal: float64(6), fqnMetadataStatus: nil, fqnService: "api", fqnLabelStatus: "500", fqnEnv: "prod"},
	}

	opts := vectorAggregationOptions{
		grouping: physical.Grouping{
			Columns: []physical.ColumnExpression{
				&physical.ColumnExpr{Ref: types.ColumnRef{Column: "env", Type: types.ColumnTypeAmbiguous}},
			},
			Without: true,
		},
		operation:      types.VectorAggregationTypeSum,
		maxQuerySeries: 0,
	}

	inputA := NewArrowtestPipeline(schemaA, rowsA)
	inputB := NewArrowtestPipeline(schemaB, rowsB)
	inputC := NewArrowtestPipeline(schemaC, rowsC)

	pipeline, err := newVectorAggregationPipeline([]Pipeline{inputA, inputB, inputC}, newExpressionEvaluator(), opts)
	require.NoError(t, err)
	defer pipeline.Close()

	record, err := pipeline.Read(t.Context())
	require.NoError(t, err)

	result, err := arrowtest.RecordRows(record)
	require.NoError(t, err)

	expect := arrowtest.Rows{
		{
			colTs:                    ts,
			colVal:                   float64(8), // 1 + 2 + 5
			"utf8.ambiguous.service": nil,
			"utf8.ambiguous.status":  "200",
		},
		{
			colTs:                    ts,
			colVal:                   float64(13), // 3 + 4 + 6
			"utf8.ambiguous.service": "api",
			"utf8.ambiguous.status":  "500",
		},
	}

	require.Equal(t, len(expect), len(result), "same logical label values must aggregate together across records and physical types in without() grouping")
	require.ElementsMatch(t, expect, result)
}

// TestVectorAggregationPipeline_WithoutSortsColumns verifies that "without" grouping
// works correctly when columns are in different order across records.
// They should be sorted by short name before calling the aggregator.
func TestVectorAggregationPipeline_WithoutSortsColumns(t *testing.T) {
	const (
		fqnEnv         = "utf8.label.env"
		fqnService     = "utf8.label.service"
		fqnLabelStatus = "utf8.label.status"
	)

	ts := time.Unix(20, 0).UTC()

	schemaA := arrow.NewSchema([]arrow.Field{
		semconv.FieldFromFQN(colTs, false),
		semconv.FieldFromFQN(colVal, false),
		semconv.FieldFromFQN(fqnService, true),
		semconv.FieldFromFQN(fqnLabelStatus, true),
		semconv.FieldFromFQN(fqnEnv, true),
	}, nil)

	schemaB := arrow.NewSchema([]arrow.Field{
		semconv.FieldFromFQN(colTs, false),
		semconv.FieldFromFQN(colVal, false),
		semconv.FieldFromFQN(fqnEnv, true),
		semconv.FieldFromFQN(fqnLabelStatus, true),
		semconv.FieldFromFQN(fqnService, true),
	}, nil)

	schemaC := arrow.NewSchema([]arrow.Field{
		semconv.FieldFromFQN(colTs, false),
		semconv.FieldFromFQN(colVal, false),
		semconv.FieldFromFQN(fqnLabelStatus, true),
		semconv.FieldFromFQN(fqnEnv, true),
	}, nil)

	rowsA := arrowtest.Rows{
		{colTs: ts, colVal: float64(1), fqnService: nil, fqnLabelStatus: "200", fqnEnv: "prod"},
		{colTs: ts, colVal: float64(10), fqnService: "api", fqnLabelStatus: "500", fqnEnv: "prod"},
	}

	rowsB := arrowtest.Rows{
		{colTs: ts, colVal: float64(2), fqnEnv: "prod", fqnLabelStatus: "200", fqnService: nil},
		{colTs: ts, colVal: float64(20), fqnEnv: "prod", fqnLabelStatus: "500", fqnService: "api"},
	}

	rowsC := arrowtest.Rows{
		{colTs: ts, colVal: float64(3), fqnLabelStatus: "200", fqnEnv: "prod"},
		{colTs: ts, colVal: float64(30), fqnLabelStatus: "503", fqnEnv: "prod"},
	}

	opts := vectorAggregationOptions{
		grouping: physical.Grouping{
			Columns: []physical.ColumnExpression{
				&physical.ColumnExpr{Ref: types.ColumnRef{Column: "env", Type: types.ColumnTypeAmbiguous}},
			},
			Without: true,
		},
		operation:      types.VectorAggregationTypeSum,
		maxQuerySeries: 0,
	}

	inputA := NewArrowtestPipeline(schemaA, rowsA)
	inputB := NewArrowtestPipeline(schemaB, rowsB)
	inputC := NewArrowtestPipeline(schemaC, rowsC)

	pipeline, err := newVectorAggregationPipeline([]Pipeline{inputA, inputB, inputC}, newExpressionEvaluator(), opts)
	require.NoError(t, err)
	defer pipeline.Close()

	record, err := pipeline.Read(t.Context())
	require.NoError(t, err)

	result, err := arrowtest.RecordRows(record)
	require.NoError(t, err)

	expect := arrowtest.Rows{
		{
			colTs:                    ts,
			colVal:                   float64(6), // 1 + 2 + 3
			"utf8.ambiguous.service": nil,
			"utf8.ambiguous.status":  "200",
		},
		{
			colTs:                    ts,
			colVal:                   float64(30), // 10 + 20
			"utf8.ambiguous.service": "api",
			"utf8.ambiguous.status":  "500",
		},
		{
			colTs:                    ts,
			colVal:                   float64(30),
			"utf8.ambiguous.service": nil,
			"utf8.ambiguous.status":  "503",
		},
	}

	require.Equal(t, len(expect), len(result), "retained logical label order must be canonical and missing columns must merge with null values in without() grouping")
	require.ElementsMatch(t, expect, result)
}
