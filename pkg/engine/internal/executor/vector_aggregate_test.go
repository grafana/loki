package executor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

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

	pipeline, err := newVectorAggregationPipeline([]Pipeline{input1, input2}, grouping, newExpressionEvaluator(), types.VectorAggregationTypeSum, nil)
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
