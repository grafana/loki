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
)

func init() {
	assertions.Enabled = true
}

func TestVectorAggregationPipeline(t *testing.T) {
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
		fmt.Sprintf("%s,10,prod,app1", t1.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,20,prod,app2", t1.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,30,dev,app1", t1.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,15,prod,app1", t2.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,25,prod,app2", t2.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,35,dev,app2", t2.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,40,prod,app1", t3.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,50,prod,app2", t3.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,60,dev,app1", t3.Format(arrowTimestampFormat)),
	}, "\n")

	input2CSV := strings.Join([]string{
		fmt.Sprintf("%s,5,prod,app1", t1.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,15,dev,app2", t1.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,10,prod,app1", t2.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,20,dev,app1", t2.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,30,prod,app2", t3.Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,40,dev,app2", t3.Format(arrowTimestampFormat)),
	}, "\n")

	grouping := physical.Grouping{
		Columns: []physical.ColumnExpression{
			&physical.ColumnExpr{Ref: types.ColumnRef{Column: "env", Type: types.ColumnTypeAmbiguous}},
			&physical.ColumnExpr{Ref: types.ColumnRef{Column: "service", Type: types.ColumnTypeAmbiguous}},
		},
		Without: false,
	}

	verifyResults := func(t *testing.T, record arrow.RecordBatch, expected map[time.Time]map[string]float64) {
		t.Helper()
		actual := make(map[time.Time]map[string]float64)
		numLabelCols := int(record.NumCols()) - 2 // first two are timestamp and value

		for i := range int(record.NumRows()) {
			ts := record.Column(0).(*array.Timestamp).Value(i).ToTime(arrow.Nanosecond)
			value := record.Column(1).(*array.Float64).Value(i)

			var parts []string
			for col := 2; col < 2+numLabelCols; col++ {
				parts = append(parts, record.Column(col).(*array.String).Value(i))
			}
			key := strings.Join(parts, ",")

			if _, ok := actual[ts]; !ok {
				actual[ts] = make(map[string]float64)
			}
			actual[ts][key] = value
		}

		for ts, groups := range expected {
			require.Contains(t, actual, ts, "timestamp %v should exist", ts)
			for group, expectedValue := range groups {
				require.Contains(t, actual[ts], group, "group %s should exist at timestamp %v", group, ts)
				require.Equal(t, expectedValue, actual[ts][group],
					"value mismatch for group %s at timestamp %v", group, ts)
			}
		}
	}

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
	for name, columnar := range map[string]bool{
		"row-based": false,
		"columnar":  true,
	} {
		t.Run(name, func(t *testing.T) {
			input1Record, err := CSVToArrow(fields, input1CSV)
			require.NoError(t, err)
			input2Record, err := CSVToArrow(fields, input2CSV)
			require.NoError(t, err)

			pipeline, err := newVectorAggregationPipeline(
				[]Pipeline{NewBufferedPipeline(input1Record), NewBufferedPipeline(input2Record)},
				newExpressionEvaluator(),
				vectorAggregationOptions{
					grouping:       grouping,
					operation:      types.VectorAggregationTypeSum,
					maxQuerySeries: 0,
					columnar:       columnar,
				},
			)
			require.NoError(t, err)
			defer pipeline.Close()

			record, err := pipeline.Read(t.Context())
			require.NoError(t, err)
			verifyResults(t, record, expected)
		})
	}

	expectedNoGroup := map[time.Time]map[string]float64{
		t1: {"": 80},  // 10+20+30+5+15
		t2: {"": 105}, // 15+25+35+10+20
		t3: {"": 220}, // 40+50+60+30+40
	}
	for name, columnar := range map[string]bool{
		"no groupby labels/row-based": false,
		"no groupby labels/columnar":  true,
	} {
		t.Run(name, func(t *testing.T) {
			input1Record, err := CSVToArrow(fields, input1CSV)
			require.NoError(t, err)
			input2Record, err := CSVToArrow(fields, input2CSV)
			require.NoError(t, err)

			pipeline, err := newVectorAggregationPipeline(
				[]Pipeline{NewBufferedPipeline(input1Record), NewBufferedPipeline(input2Record)},
				newExpressionEvaluator(),
				vectorAggregationOptions{
					grouping:       physical.Grouping{},
					operation:      types.VectorAggregationTypeSum,
					maxQuerySeries: 0,
					columnar:       columnar,
				},
			)
			require.NoError(t, err)
			defer pipeline.Close()

			record, err := pipeline.Read(t.Context())
			require.NoError(t, err)
			verifyResults(t, record, expectedNoGroup)
		})
	}
}
