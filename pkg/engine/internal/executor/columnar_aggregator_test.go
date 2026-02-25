package executor

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

var (
	groupByColumns = []arrow.Field{
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

func makeFloat64Array(mem memory.Allocator, values []float64, present []bool) *array.Float64 {
	builder := array.NewFloat64Builder(mem)
	defer builder.Release()
	for i, v := range values {
		if present != nil && !present[i] {
			builder.AppendNull()
		} else {
			builder.Append(v)
		}
	}
	return builder.NewFloat64Array()
}

func makeStringArray(mem memory.Allocator, values []string, present []bool) *array.String {
	builder := array.NewStringBuilder(mem)
	defer builder.Release()
	for i, v := range values {
		if present != nil && !present[i] {
			builder.AppendNull()
		} else {
			builder.Append(v)
		}
	}
	return builder.NewStringArray()
}

func TestColumnarAggregator(t *testing.T) {
	colTs := semconv.ColumnIdentTimestamp.FQN()
	colVal := semconv.ColumnIdentValue.FQN()
	colEnv := semconv.NewIdentifier("env", types.ColumnTypeLabel, types.Loki.String).FQN()
	colSvc := semconv.NewIdentifier("service", types.ColumnTypeLabel, types.Loki.String).FQN()

	mem := memory.NewGoAllocator()

	ts1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC).UnixNano()
	ts2 := time.Date(2024, 1, 1, 10, 0, 30, 0, time.UTC).UnixNano()
	ts3 := time.Date(2024, 1, 1, 10, 1, 0, 0, time.UTC).UnixNano()

	// Test data (two batches fed into every sub-test):
	//
	// Batch 1:
	//   Row | Timestamp | Value  | env   | service
	//   ----+-----------+--------+-------+--------
	//    0  | ts1       | 10     | prod  | app1
	//    1  | ts1       | <null> | prod  | app2
	//    2  | ts1       | 30     | <null>| app1
	//    3  | ts2       | 15     | prod  | app1
	//    4  | ts2       | <null> | prod  | app2
	//    5  | ts2       | 35     | dev   | <null>
	//
	// Batch 2:
	//   Row | Timestamp | Value  | env   | service
	//   ----+-----------+--------+-------+--------
	//    0  | ts1       | 5      | prod  | app1
	//    1  | ts2       | 10     | prod  | app1
	//    2  | ts3       | 8      | prod  | app1
	//    3  | ts3       | <null> | dev   | app1
	//    4  | ts3       | 12     | dev   | app2
	//    5  | ts1       | 7      | prod  | app2
	//
	// Timestamps:
	//   ts1 = 2024-01-01 10:00:00  [falls in window w1]
	//   ts2 = 2024-01-01 10:00:30  [falls in window w2]
	//   ts3 = 2024-01-01 10:01:00  [falls in window w2]

	// batch 1
	tsArr1 := makeTimestampArray(mem, []int64{ts1, ts1, ts1, ts2, ts2, ts2})
	valsArr1 := makeFloat64Array(mem, []float64{10, 0, 30, 15, 0, 35}, []bool{true, false, true, true, false, true})
	envArr1 := makeStringArray(mem, []string{"prod", "prod", "", "prod", "prod", "dev"}, []bool{true, true, false, true, true, true})
	svcArr1 := makeStringArray(mem, []string{"app1", "app2", "app1", "app1", "app2", ""}, []bool{true, true, true, true, true, false})
	defer tsArr1.Release()
	defer valsArr1.Release()
	defer envArr1.Release()
	defer svcArr1.Release()

	// batch 2
	tsArr2 := makeTimestampArray(mem, []int64{ts1, ts2, ts3, ts3, ts3, ts1})
	valsArr2 := makeFloat64Array(mem, []float64{5, 10, 8, 0, 12, 7}, []bool{true, true, true, false, true, true})
	envArr2 := makeStringArray(mem, []string{"prod", "prod", "prod", "dev", "dev", "prod"}, nil)
	svcArr2 := makeStringArray(mem, []string{"app1", "app1", "app1", "app1", "app2", "app2"}, nil)
	defer tsArr2.Release()
	defer valsArr2.Release()
	defer envArr2.Release()
	defer svcArr2.Release()

	groupByNone := []arrow.Field{}
	groupByEnv := []arrow.Field{groupByColumns[0]}
	groupByEnvSvc := groupByColumns

	// Windows: w0 (9:59, 10:00], w1 (10:00, 10:01], w2 (10:01, 10:02].
	w0 := window{start: time.Date(2024, 1, 1, 9, 59, 0, 0, time.UTC)}
	step := 60 * time.Second
	w0.end = w0.start.Add(step)

	w1 := window{start: w0.end}
	w1.end = w1.start.Add(step)

	w2 := window{start: w1.end}
	w2.end = w2.start.Add(step)

	matcherFactory := &matcherFactory{
		start:    w0.start,
		step:     step,
		interval: step,
		bounds:   window{start: w0.start, end: w2.end},
	}
	matchWindows := matcherFactory.createBatchMatcher([]window{
		w0, w1, w2,
	})

	toTime := func(ns int64) time.Time {
		return arrow.Timestamp(ns).ToTime(arrow.Nanosecond)
	}

	tests := []struct {
		name      string
		operation aggregationOperation
		groupBy   []arrow.Field
		matchWin  windowMatchFunc
		expect    arrowtest.Rows
	}{
		// Empty groupBy, no window
		{"op=sum/groupby=none/window=identity", aggregationOperationSum, groupByNone, nil, arrowtest.Rows{
			{colTs: toTime(ts1), colVal: float64(52)},
			{colTs: toTime(ts2), colVal: float64(60)},
			{colTs: toTime(ts3), colVal: float64(20)},
		}},
		{"op=avg/groupby=none/window=identity", aggregationOperationAvg, groupByNone, nil, arrowtest.Rows{
			{colTs: toTime(ts1), colVal: float64(13)},
			{colTs: toTime(ts2), colVal: float64(20)},
			{colTs: toTime(ts3), colVal: float64(10)},
		}},
		{"op=count/groupby=none/window=identity", aggregationOperationCount, groupByNone, nil, arrowtest.Rows{
			{colTs: toTime(ts1), colVal: float64(4)},
			{colTs: toTime(ts2), colVal: float64(3)},
			{colTs: toTime(ts3), colVal: float64(2)},
		}},
		{"op=max/groupby=none/window=identity", aggregationOperationMax, groupByNone, nil, arrowtest.Rows{
			{colTs: toTime(ts1), colVal: float64(30)},
			{colTs: toTime(ts2), colVal: float64(35)},
			{colTs: toTime(ts3), colVal: float64(12)},
		}},
		{"op=min/groupby=none/window=identity", aggregationOperationMin, groupByNone, nil, arrowtest.Rows{
			{colTs: toTime(ts1), colVal: float64(5)},
			{colTs: toTime(ts2), colVal: float64(10)},
			{colTs: toTime(ts3), colVal: float64(8)},
		}},
		// Empty groupBy, with aligned windows
		{"op=sum/groupby=none/window=aligned", aggregationOperationSum, groupByNone, matchWindows, arrowtest.Rows{
			{colTs: w1.end.UTC(), colVal: float64(52)},
			{colTs: w2.end.UTC(), colVal: float64(80)},
		}},
		{"op=avg/groupby=none/window=aligned", aggregationOperationAvg, groupByNone, matchWindows, arrowtest.Rows{
			{colTs: w1.end.UTC(), colVal: float64(13)},
			{colTs: w2.end.UTC(), colVal: float64(16)},
		}},
		{"op=count/groupby=none/window=aligned", aggregationOperationCount, groupByNone, matchWindows, arrowtest.Rows{
			{colTs: w1.end.UTC(), colVal: float64(4)},
			{colTs: w2.end.UTC(), colVal: float64(5)},
		}},
		{"op=max/groupby=none/window=aligned", aggregationOperationMax, groupByNone, matchWindows, arrowtest.Rows{
			{colTs: w1.end.UTC(), colVal: float64(30)},
			{colTs: w2.end.UTC(), colVal: float64(35)},
		}},
		{"op=min/groupby=none/window=aligned", aggregationOperationMin, groupByNone, matchWindows, arrowtest.Rows{
			{colTs: w1.end.UTC(), colVal: float64(5)},
			{colTs: w2.end.UTC(), colVal: float64(8)},
		}},
		// Single label (env), no window. ts1: prod (3 vals), env=null (1 val from row 2).
		{"op=sum/groupby=env/window=identity", aggregationOperationSum, groupByEnv, nil, arrowtest.Rows{
			{colTs: toTime(ts1), colVal: float64(22), colEnv: "prod"},
			{colTs: toTime(ts1), colVal: float64(30), colEnv: nil},
			{colTs: toTime(ts2), colVal: float64(25), colEnv: "prod"},
			{colTs: toTime(ts2), colVal: float64(35), colEnv: "dev"},
			{colTs: toTime(ts3), colVal: float64(8), colEnv: "prod"},
			{colTs: toTime(ts3), colVal: float64(12), colEnv: "dev"},
		}},
		{"op=avg/groupby=env/window=identity", aggregationOperationAvg, groupByEnv, nil, arrowtest.Rows{
			{colTs: toTime(ts1), colVal: 22.0 / 3, colEnv: "prod"},
			{colTs: toTime(ts1), colVal: float64(30), colEnv: nil},
			{colTs: toTime(ts2), colVal: float64(12.5), colEnv: "prod"},
			{colTs: toTime(ts2), colVal: float64(35), colEnv: "dev"},
			{colTs: toTime(ts3), colVal: float64(8), colEnv: "prod"},
			{colTs: toTime(ts3), colVal: float64(12), colEnv: "dev"},
		}},
		{"op=count/groupby=env/window=identity", aggregationOperationCount, groupByEnv, nil, arrowtest.Rows{
			{colTs: toTime(ts1), colVal: float64(3), colEnv: "prod"},
			{colTs: toTime(ts1), colVal: float64(1), colEnv: nil},
			{colTs: toTime(ts2), colVal: float64(2), colEnv: "prod"},
			{colTs: toTime(ts2), colVal: float64(1), colEnv: "dev"},
			{colTs: toTime(ts3), colVal: float64(1), colEnv: "prod"},
			{colTs: toTime(ts3), colVal: float64(1), colEnv: "dev"},
		}},
		{"op=max/groupby=env/window=identity", aggregationOperationMax, groupByEnv, nil, arrowtest.Rows{
			{colTs: toTime(ts1), colVal: float64(10), colEnv: "prod"},
			{colTs: toTime(ts1), colVal: float64(30), colEnv: nil},
			{colTs: toTime(ts2), colVal: float64(15), colEnv: "prod"},
			{colTs: toTime(ts2), colVal: float64(35), colEnv: "dev"},
			{colTs: toTime(ts3), colVal: float64(8), colEnv: "prod"},
			{colTs: toTime(ts3), colVal: float64(12), colEnv: "dev"},
		}},
		{"op=min/groupby=env/window=identity", aggregationOperationMin, groupByEnv, nil, arrowtest.Rows{
			{colTs: toTime(ts1), colVal: float64(5), colEnv: "prod"},
			{colTs: toTime(ts1), colVal: float64(30), colEnv: nil},
			{colTs: toTime(ts2), colVal: float64(10), colEnv: "prod"},
			{colTs: toTime(ts2), colVal: float64(35), colEnv: "dev"},
			{colTs: toTime(ts3), colVal: float64(8), colEnv: "prod"},
			{colTs: toTime(ts3), colVal: float64(12), colEnv: "dev"},
		}},
		// Single label (env), with window. w1 has ts1 (prod=3, env=null=1); w2 has ts2+ts3 (prod=3, dev=2).
		{"op=sum/groupby=env/window=aligned", aggregationOperationSum, groupByEnv, matchWindows, arrowtest.Rows{
			{colTs: w1.end.UTC(), colVal: float64(22), colEnv: "prod"},
			{colTs: w1.end.UTC(), colVal: float64(30), colEnv: nil},
			{colTs: w2.end.UTC(), colVal: float64(33), colEnv: "prod"},
			{colTs: w2.end.UTC(), colVal: float64(47), colEnv: "dev"},
		}},
		{"op=avg/groupby=env/window=aligned", aggregationOperationAvg, groupByEnv, matchWindows, arrowtest.Rows{
			{colTs: w1.end.UTC(), colVal: 22.0 / 3, colEnv: "prod"},
			{colTs: w1.end.UTC(), colVal: float64(30), colEnv: nil},
			{colTs: w2.end.UTC(), colVal: float64(11), colEnv: "prod"},
			{colTs: w2.end.UTC(), colVal: float64(23.5), colEnv: "dev"},
		}},
		{"op=count/groupby=env/window=aligned", aggregationOperationCount, groupByEnv, matchWindows, arrowtest.Rows{
			{colTs: w1.end.UTC(), colVal: float64(3), colEnv: "prod"},
			{colTs: w1.end.UTC(), colVal: float64(1), colEnv: nil},
			{colTs: w2.end.UTC(), colVal: float64(3), colEnv: "prod"},
			{colTs: w2.end.UTC(), colVal: float64(2), colEnv: "dev"},
		}},
		{"op=max/groupby=env/window=aligned", aggregationOperationMax, groupByEnv, matchWindows, arrowtest.Rows{
			{colTs: w1.end.UTC(), colVal: float64(10), colEnv: "prod"},
			{colTs: w1.end.UTC(), colVal: float64(30), colEnv: nil},
			{colTs: w2.end.UTC(), colVal: float64(15), colEnv: "prod"},
			{colTs: w2.end.UTC(), colVal: float64(35), colEnv: "dev"},
		}},
		{"op=min/groupby=env/window=aligned", aggregationOperationMin, groupByEnv, matchWindows, arrowtest.Rows{
			{colTs: w1.end.UTC(), colVal: float64(5), colEnv: "prod"},
			{colTs: w1.end.UTC(), colVal: float64(30), colEnv: nil},
			{colTs: w2.end.UTC(), colVal: float64(8), colEnv: "prod"},
			{colTs: w2.end.UTC(), colVal: float64(12), colEnv: "dev"},
		}},
		// Two labels (env, service), no window. Row 2: env=null,app1; row 5: dev,service=null.
		{"op=sum/groupby=two/window=identity", aggregationOperationSum, groupByEnvSvc, nil, arrowtest.Rows{
			{colTs: toTime(ts1), colVal: float64(15), colEnv: "prod", colSvc: "app1"},
			{colTs: toTime(ts1), colVal: float64(7), colEnv: "prod", colSvc: "app2"},
			{colTs: toTime(ts1), colVal: float64(30), colEnv: nil, colSvc: "app1"},
			{colTs: toTime(ts2), colVal: float64(25), colEnv: "prod", colSvc: "app1"},
			{colTs: toTime(ts2), colVal: float64(35), colEnv: "dev", colSvc: nil},
			{colTs: toTime(ts3), colVal: float64(8), colEnv: "prod", colSvc: "app1"},
			{colTs: toTime(ts3), colVal: float64(12), colEnv: "dev", colSvc: "app2"},
		}},
		{"op=avg/groupby=two/window=identity", aggregationOperationAvg, groupByEnvSvc, nil, arrowtest.Rows{
			{colTs: toTime(ts1), colVal: float64(7.5), colEnv: "prod", colSvc: "app1"},
			{colTs: toTime(ts1), colVal: float64(7), colEnv: "prod", colSvc: "app2"},
			{colTs: toTime(ts1), colVal: float64(30), colEnv: nil, colSvc: "app1"},
			{colTs: toTime(ts2), colVal: float64(12.5), colEnv: "prod", colSvc: "app1"},
			{colTs: toTime(ts2), colVal: float64(35), colEnv: "dev", colSvc: nil},
			{colTs: toTime(ts3), colVal: float64(8), colEnv: "prod", colSvc: "app1"},
			{colTs: toTime(ts3), colVal: float64(12), colEnv: "dev", colSvc: "app2"},
		}},
		{"op=count/groupby=two/window=identity", aggregationOperationCount, groupByEnvSvc, nil, arrowtest.Rows{
			{colTs: toTime(ts1), colVal: float64(2), colEnv: "prod", colSvc: "app1"},
			{colTs: toTime(ts1), colVal: float64(1), colEnv: "prod", colSvc: "app2"},
			{colTs: toTime(ts1), colVal: float64(1), colEnv: nil, colSvc: "app1"},
			{colTs: toTime(ts2), colVal: float64(2), colEnv: "prod", colSvc: "app1"},
			{colTs: toTime(ts2), colVal: float64(1), colEnv: "dev", colSvc: nil},
			{colTs: toTime(ts3), colVal: float64(1), colEnv: "prod", colSvc: "app1"},
			{colTs: toTime(ts3), colVal: float64(1), colEnv: "dev", colSvc: "app2"},
		}},
		{"op=max/groupby=two/window=identity", aggregationOperationMax, groupByEnvSvc, nil, arrowtest.Rows{
			{colTs: toTime(ts1), colVal: float64(10), colEnv: "prod", colSvc: "app1"},
			{colTs: toTime(ts1), colVal: float64(7), colEnv: "prod", colSvc: "app2"},
			{colTs: toTime(ts1), colVal: float64(30), colEnv: nil, colSvc: "app1"},
			{colTs: toTime(ts2), colVal: float64(15), colEnv: "prod", colSvc: "app1"},
			{colTs: toTime(ts2), colVal: float64(35), colEnv: "dev", colSvc: nil},
			{colTs: toTime(ts3), colVal: float64(8), colEnv: "prod", colSvc: "app1"},
			{colTs: toTime(ts3), colVal: float64(12), colEnv: "dev", colSvc: "app2"},
		}},
		{"op=min/groupby=two/window=identity", aggregationOperationMin, groupByEnvSvc, nil, arrowtest.Rows{
			{colTs: toTime(ts1), colVal: float64(5), colEnv: "prod", colSvc: "app1"},
			{colTs: toTime(ts1), colVal: float64(7), colEnv: "prod", colSvc: "app2"},
			{colTs: toTime(ts1), colVal: float64(30), colEnv: nil, colSvc: "app1"},
			{colTs: toTime(ts2), colVal: float64(10), colEnv: "prod", colSvc: "app1"},
			{colTs: toTime(ts2), colVal: float64(35), colEnv: "dev", colSvc: nil},
			{colTs: toTime(ts3), colVal: float64(8), colEnv: "prod", colSvc: "app1"},
			{colTs: toTime(ts3), colVal: float64(12), colEnv: "dev", colSvc: "app2"},
		}},
		// Two labels, with window. w1: prod/app1, prod/app2, null/app1; w2: prod/app1, dev/null, dev/app2.
		{"op=sum/groupby=two/window=aligned", aggregationOperationSum, groupByEnvSvc, matchWindows, arrowtest.Rows{
			{colTs: w1.end.UTC(), colVal: float64(15), colEnv: "prod", colSvc: "app1"},
			{colTs: w1.end.UTC(), colVal: float64(7), colEnv: "prod", colSvc: "app2"},
			{colTs: w1.end.UTC(), colVal: float64(30), colEnv: nil, colSvc: "app1"},
			{colTs: w2.end.UTC(), colVal: float64(33), colEnv: "prod", colSvc: "app1"},
			{colTs: w2.end.UTC(), colVal: float64(35), colEnv: "dev", colSvc: nil},
			{colTs: w2.end.UTC(), colVal: float64(12), colEnv: "dev", colSvc: "app2"},
		}},
		{"op=avg/groupby=two/window=aligned", aggregationOperationAvg, groupByEnvSvc, matchWindows, arrowtest.Rows{
			{colTs: w1.end.UTC(), colVal: float64(7.5), colEnv: "prod", colSvc: "app1"},
			{colTs: w1.end.UTC(), colVal: float64(7), colEnv: "prod", colSvc: "app2"},
			{colTs: w1.end.UTC(), colVal: float64(30), colEnv: nil, colSvc: "app1"},
			{colTs: w2.end.UTC(), colVal: float64(11), colEnv: "prod", colSvc: "app1"},
			{colTs: w2.end.UTC(), colVal: float64(35), colEnv: "dev", colSvc: nil},
			{colTs: w2.end.UTC(), colVal: float64(12), colEnv: "dev", colSvc: "app2"},
		}},
		{"op=count/groupby=two/window=aligned", aggregationOperationCount, groupByEnvSvc, matchWindows, arrowtest.Rows{
			{colTs: w1.end.UTC(), colVal: float64(2), colEnv: "prod", colSvc: "app1"},
			{colTs: w1.end.UTC(), colVal: float64(1), colEnv: "prod", colSvc: "app2"},
			{colTs: w1.end.UTC(), colVal: float64(1), colEnv: nil, colSvc: "app1"},
			{colTs: w2.end.UTC(), colVal: float64(3), colEnv: "prod", colSvc: "app1"},
			{colTs: w2.end.UTC(), colVal: float64(1), colEnv: "dev", colSvc: nil},
			{colTs: w2.end.UTC(), colVal: float64(1), colEnv: "dev", colSvc: "app2"},
		}},
		{"op=max/groupby=two/window=aligned", aggregationOperationMax, groupByEnvSvc, matchWindows, arrowtest.Rows{
			{colTs: w1.end.UTC(), colVal: float64(10), colEnv: "prod", colSvc: "app1"},
			{colTs: w1.end.UTC(), colVal: float64(7), colEnv: "prod", colSvc: "app2"},
			{colTs: w1.end.UTC(), colVal: float64(30), colEnv: nil, colSvc: "app1"},
			{colTs: w2.end.UTC(), colVal: float64(15), colEnv: "prod", colSvc: "app1"},
			{colTs: w2.end.UTC(), colVal: float64(35), colEnv: "dev", colSvc: nil},
			{colTs: w2.end.UTC(), colVal: float64(12), colEnv: "dev", colSvc: "app2"},
		}},
		{"op=min/groupby=two/window=aligned", aggregationOperationMin, groupByEnvSvc, matchWindows, arrowtest.Rows{
			{colTs: w1.end.UTC(), colVal: float64(5), colEnv: "prod", colSvc: "app1"},
			{colTs: w1.end.UTC(), colVal: float64(7), colEnv: "prod", colSvc: "app2"},
			{colTs: w1.end.UTC(), colVal: float64(30), colEnv: nil, colSvc: "app1"},
			{colTs: w2.end.UTC(), colVal: float64(8), colEnv: "prod", colSvc: "app1"},
			{colTs: w2.end.UTC(), colVal: float64(35), colEnv: "dev", colSvc: nil},
			{colTs: w2.end.UTC(), colVal: float64(12), colEnv: "dev", colSvc: "app2"},
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agg := newColumnarAggregator(20, columnarAggregatorOpts{
				operation:     tc.operation,
				groupByLabels: tc.groupBy,
				matchWindows:  tc.matchWin,
			})

			var labelCols1, labelCols2 []*array.String
			switch len(tc.groupBy) {
			case 0:
				labelCols1, labelCols2 = nil, nil
			case 1:
				labelCols1, labelCols2 = []*array.String{envArr1}, []*array.String{envArr2}
			default:
				labelCols1, labelCols2 = []*array.String{envArr1, svcArr1}, []*array.String{envArr2, svcArr2}
			}

			require.NoError(t, agg.AddBatch(tsArr1, valsArr1, labelCols1))
			require.NoError(t, agg.AddBatch(tsArr2, valsArr2, labelCols2))

			record, err := agg.BuildRecord()
			require.NoError(t, err)
			rows, err := arrowtest.RecordRows(record)
			require.NoError(t, err)
			require.ElementsMatch(t, tc.expect, rows)
		})
	}
}

func TestColumnarAggregatorSeriesLimit(t *testing.T) {
	mem := memory.NewGoAllocator()
	ts1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC).UnixNano()
	ts2 := time.Date(2024, 1, 1, 10, 1, 0, 0, time.UTC).UnixNano()

	agg := newColumnarAggregator(10, columnarAggregatorOpts{
		operation:     aggregationOperationSum,
		groupByLabels: groupByColumns,
		maxSeries:     2,
	})

	timestamps1 := makeTimestampArray(mem, []int64{ts1, ts1})
	defer timestamps1.Release()
	values1 := makeFloat64Array(mem, []float64{10, 20}, nil)
	defer values1.Release()
	envCol1 := makeStringArray(mem, []string{"prod", "prod"}, nil)
	defer envCol1.Release()
	svcCol1 := makeStringArray(mem, []string{"app1", "app2"}, nil)
	defer svcCol1.Release()
	err := agg.AddBatch(timestamps1, values1, []*array.String{envCol1, svcCol1})
	require.NoError(t, err)

	timestamps2 := makeTimestampArray(mem, []int64{ts2})
	defer timestamps2.Release()
	values2 := makeFloat64Array(mem, []float64{30}, nil)
	defer values2.Release()
	envCol2 := makeStringArray(mem, []string{"dev"}, nil)
	defer envCol2.Release()
	svcCol2 := makeStringArray(mem, []string{"app1"}, nil)
	defer svcCol2.Release()

	// add 3rd series
	err = agg.AddBatch(timestamps2, values2, []*array.String{envCol2, svcCol2})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrSeriesLimitExceeded))

	// add existing series with new ts should not error
	timestamps3 := makeTimestampArray(mem, []int64{ts2, ts2})
	err = agg.AddBatch(timestamps3, values1, []*array.String{envCol1, svcCol1})
	require.NoError(t, err)
}

type benchLabelConfig struct {
	name        string
	cardinality int
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

// buildBenchRecords builds proper arrow.RecordBatch objects with FQN column
// names so the expressionEvaluator can resolve timestamp, value, and label
// columns. The returned records are suitable for NewBufferedPipeline.
func buildBenchRecords(mem memory.Allocator, totalRows, batchSize, numDistinctTs int, labels []benchLabelConfig) []arrow.RecordBatch {
	const baseTs = int64(1704103200000000000)

	fields := make([]arrow.Field, 0, 2+len(labels))
	fields = append(fields, semconv.FieldFromIdent(semconv.ColumnIdentTimestamp, false))
	fields = append(fields, semconv.FieldFromIdent(semconv.ColumnIdentValue, false))
	for _, l := range labels {
		fields = append(fields, semconv.FieldFromIdent(
			semconv.NewIdentifier(l.name, types.ColumnTypeLabel, types.Loki.String), true,
		))
	}
	schema := arrow.NewSchema(fields, nil)

	tsType := types.Arrow.Timestamp.(*arrow.TimestampType)

	timestamps := make([]int64, totalRows)
	values := make([]float64, totalRows)
	for i := range totalRows {
		timestamps[i] = baseTs + int64(i%numDistinctTs)*1_000_000_000
		values[i] = float64(i)
	}

	labelCols := make([][]string, len(labels))
	for c, l := range labels {
		col := make([]string, totalRows)
		for i := range totalRows {
			col[i] = fmt.Sprintf("%s-%d", l.name, i%l.cardinality)
		}
		labelCols[c] = col
	}

	numBatches := (totalRows + batchSize - 1) / batchSize
	records := make([]arrow.RecordBatch, numBatches)
	for bi := range numBatches {
		start := bi * batchSize
		end := start + batchSize
		if end > totalRows {
			end = totalRows
		}
		n := end - start

		tsBuilder := array.NewTimestampBuilder(mem, tsType)
		for _, v := range timestamps[start:end] {
			tsBuilder.Append(arrow.Timestamp(v))
		}
		tsArr := tsBuilder.NewTimestampArray()
		tsBuilder.Release()

		valArr := makeFloat64Array(mem, values[start:end], nil)

		cols := make([]arrow.Array, 0, 2+len(labels))
		cols = append(cols, tsArr, valArr)
		for c := range labels {
			cols = append(cols, makeStringArray(mem, labelCols[c][start:end], nil))
		}

		records[bi] = array.NewRecordBatch(schema, cols, int64(n))
	}
	return records
}

func benchGroupByColumns(labels []benchLabelConfig) []physical.ColumnExpression {
	cols := make([]physical.ColumnExpression, len(labels))
	for i, l := range labels {
		cols[i] = &physical.ColumnExpr{
			Ref: types.ColumnRef{
				Column: l.name,
				Type:   types.ColumnTypeLabel,
			},
		}
	}
	return cols
}

// BenchmarkAggregations measures end-to-end Read() throughput for aggregation
// pipelines with varying input sizes, cardinality and aggregator types.
func BenchmarkAggregations(b *testing.B) {
	mem := memory.NewGoAllocator()

	type benchCase struct {
		name      string
		totalRows int
		batchSize int
		labels    []benchLabelConfig
	}

	cardLow := []benchLabelConfig{
		{"level", 5},
	}
	cardMedium := []benchLabelConfig{
		{"level", 5},
		{"namespace", 30},
	}
	cardHigh := []benchLabelConfig{
		{"level", 5},
		{"namespace", 30},
		{"service", 20},
	}

	rowCounts := []struct {
		name string
		n    int
	}{
		{"rows=10k", 10_000},
		{"rows=100k", 100_000},
		{"rows=1M", 1_000_000},
	}
	batchSizes := []int{100, 1_000, 8_000}
	cards := []struct {
		name   string
		labels []benchLabelConfig
	}{
		{"cardinality=low", cardLow},
		{"cardinality=medium", cardMedium},
		{"cardinality=high", cardHigh},
	}

	var cases []benchCase
	for _, rc := range rowCounts {
		for _, bs := range batchSizes {
			for _, cd := range cards {
				cases = append(cases, benchCase{
					name:      fmt.Sprintf("%s/batch=%d/%s", rc.name, bs, cd.name),
					totalRows: rc.n,
					batchSize: bs,
					labels:    cd.labels,
				})
			}
		}
	}

	const numDistinctTs = 1000

	// baseTs must match the constant inside buildBenchRecords.
	baseTs := time.Unix(0, 1704103200000000000)
	rangeStep := 5 * time.Second
	numWindows := numDistinctTs / int(rangeStep.Seconds())

	ctx := context.Background()

	for _, columnar := range []bool{true, false} {
		aggregator := "columnar"
		if !columnar {
			aggregator = "row_based"
		}

		b.Run(fmt.Sprintf("pipeline=range_aggregation/aggregator=%s", aggregator), func(b *testing.B) {
			for _, tc := range cases {
				b.Run(tc.name, func(b *testing.B) {
					records := buildBenchRecords(mem, tc.totalRows, tc.batchSize, numDistinctTs, tc.labels)

					groupBy := benchGroupByColumns(tc.labels)
					opts := rangeAggregationOptions{
						startTs:       baseTs.Add(rangeStep),
						endTs:         baseTs.Add(time.Duration(numDistinctTs) * time.Second),
						rangeInterval: rangeStep,
						step:          rangeStep,
						operation:     types.RangeAggregationTypeSum,
						columnar:      columnar,
						grouping: physical.Grouping{
							Columns: groupBy,
							Without: false,
						},
					}

					// Initial run to sanity check the stats on output record
					input := NewBufferedPipeline(records...)
					pipeline, err := newRangeAggregationPipeline([]Pipeline{input}, newExpressionEvaluator(), opts)
					if err != nil {
						b.Fatal(err)
					}

					rec, err := pipeline.Read(ctx)
					if err != nil {
						b.Fatal(err)
					}

					if rec.NumRows() == 0 {
						b.Fatal("expected non-zero output rows from range aggregation")
					}

					// num rows should be a numWindows * num series which is determiend by cardinality test config.
					b.Logf("input: %d rows, windows: %d, output: %d rows x %d cols", tc.totalRows, numWindows, rec.NumRows(), rec.NumCols())
					pipeline.Close()

					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						input := NewBufferedPipeline(records...)
						pipeline, err := newRangeAggregationPipeline([]Pipeline{input}, newExpressionEvaluator(), opts)
						if err != nil {
							b.Fatal(err)
						}
						if _, err := pipeline.Read(ctx); err != nil {
							b.Fatal(err)
						}
						pipeline.Close()
					}
					b.ReportMetric(float64(tc.totalRows)*float64(b.N)/b.Elapsed().Seconds(), "rows/s")
				})
			}
		})

		b.Run(fmt.Sprintf("pipeline=vector_aggregation/aggregator=%s", aggregator), func(b *testing.B) {
			for _, tc := range cases {
				b.Run(tc.name, func(b *testing.B) {
					records := buildBenchRecords(mem, tc.totalRows, tc.batchSize, numDistinctTs, tc.labels)

					groupBy := benchGroupByColumns(tc.labels)
					opts := vectorAggregationOptions{
						operation: types.VectorAggregationTypeSum,
						columnar:  columnar,
						grouping: physical.Grouping{
							Columns: groupBy,
							Without: false,
						},
					}

					// Initial run to sanity check the stats on output record
					input := NewBufferedPipeline(records...)
					pipeline, err := newVectorAggregationPipeline([]Pipeline{input}, newExpressionEvaluator(), opts)
					if err != nil {
						b.Fatal(err)
					}
					rec, err := pipeline.Read(ctx)
					if err != nil {
						b.Fatal(err)
					}
					if rec.NumRows() == 0 {
						b.Fatal("expected non-zero output rows from vector aggregation")
					}
					b.Logf("input: %d rows, output: %d rows x %d cols", tc.totalRows, rec.NumRows(), rec.NumCols())
					pipeline.Close()

					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						input := NewBufferedPipeline(records...)
						pipeline, err := newVectorAggregationPipeline([]Pipeline{input}, newExpressionEvaluator(), opts)
						if err != nil {
							b.Fatal(err)
						}
						if _, err := pipeline.Read(ctx); err != nil {
							b.Fatal(err)
						}
						pipeline.Close()
					}
					b.ReportMetric(float64(tc.totalRows)*float64(b.N)/b.Elapsed().Seconds(), "rows/s")
				})
			}
		})
	}
}
