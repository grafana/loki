package executor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

const arrowTimestampFormat = "2006-01-02T15:04:05.000000000Z"

func TestRangeAggregationPipeline(t *testing.T) {
	// input schema with timestamp, partition-by columns and non-partition columns
	fields := []arrow.Field{
		{Name: types.ColumnNameBuiltinTimestamp, Type: arrow.FixedWidthTypes.Timestamp_ns, Metadata: datatype.ColumnMetadataBuiltinTimestamp},
		{Name: "env", Type: arrow.BinaryTypes.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeLabel, datatype.String)},
		{Name: "service", Type: arrow.BinaryTypes.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeLabel, datatype.String)},
		{Name: "severity", Type: arrow.BinaryTypes.String, Metadata: datatype.ColumnMetadata(types.ColumnTypeMetadata, datatype.String)}, // extra column not included in partition_by
	}

	// test data for first input
	now := time.Now().UTC()
	input1CSV := strings.Join([]string{
		fmt.Sprintf("%s,prod,app1,error", now.Format(arrowTimestampFormat)), // excluded, falls on the open interval
		fmt.Sprintf("%s,prod,app1,info", now.Add(-5*time.Minute).Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,prod,app1,error", now.Add(-5*time.Minute).Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,prod,app2,error", now.Add(-10*time.Minute).Format(arrowTimestampFormat)), // included, falls on closed interval
		fmt.Sprintf("%s,dev,,error", now.Add(-10*time.Minute).Format(arrowTimestampFormat)),
	}, "\n")

	// test data for second input
	input2R1CSV := strings.Join([]string{
		fmt.Sprintf("%s,prod,app2,info", now.Add(-5*time.Minute).Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,prod,app2,error", now.Add(-10*time.Minute).Format(arrowTimestampFormat)),
	}, "\n") // record 1
	input2R2CSV := strings.Join([]string{
		fmt.Sprintf("%s,prod,app3,info", now.Add(-5*time.Minute).Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,prod,app3,error", now.Add(-10*time.Minute).Format(arrowTimestampFormat)),
		fmt.Sprintf("%s,dev,app2,error", now.Add(-15*time.Minute).Format(arrowTimestampFormat)), // excluded, out of range
	}, "\n") // record 2

	input1Record, err := CSVToArrow(fields, input1CSV)
	require.NoError(t, err)
	defer input1Record.Release()

	input2Record1, err := CSVToArrow(fields, input2R1CSV)
	require.NoError(t, err)
	defer input2Record1.Release()

	input2Record2, err := CSVToArrow(fields, input2R2CSV)
	require.NoError(t, err)
	defer input2Record2.Release()

	// Create input pipelines
	input1 := NewBufferedPipeline(input1Record)
	input2 := NewBufferedPipeline(input2Record1, input2Record2)

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
		startTs:       now,
		endTs:         now,
		rangeInterval: 10 * time.Minute,
	}

	pipeline, err := NewRangeAggregationPipeline([]Pipeline{input1, input2}, expressionEvaluator{}, opts)
	require.NoError(t, err)
	defer pipeline.Close()

	// Read the pipeline output
	err = pipeline.Read()
	require.NoError(t, err)
	record, err := pipeline.Value()
	require.NoError(t, err)
	defer record.Release()

	// Define expected results
	expected := map[string]int64{
		"prod,app1": 2,
		"prod,app2": 3,
		"prod,app3": 2,
		"dev,":      1,
	}

	require.Equal(t, int64(len(expected)), record.NumRows(), "number of records should match")

	actual := make(map[string]int64)
	for i := range int(record.NumRows()) {
		require.Equal(t, record.Column(0).(*array.Timestamp).Value(i).ToTime(arrow.Nanosecond), now)

		value := record.Column(1).(*array.Int64).Value(i)
		env := record.Column(2).(*array.String).Value(i)
		service := record.Column(3).(*array.String).Value(i)
		key := fmt.Sprintf("%s,%s", env, service)
		actual[key] = value
	}

	require.EqualValues(t, expected, actual, "aggregation results should match")
}
