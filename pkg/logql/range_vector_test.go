package logql

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
)

var samples = []logproto.Sample{
	{Timestamp: time.Unix(2, 0).UnixNano(), Hash: 1, Value: 1.},
	{Timestamp: time.Unix(5, 0).UnixNano(), Hash: 2, Value: 1.},
	{Timestamp: time.Unix(6, 0).UnixNano(), Hash: 3, Value: 1.},
	{Timestamp: time.Unix(10, 0).UnixNano(), Hash: 4, Value: 1.},
	{Timestamp: time.Unix(10, 1).UnixNano(), Hash: 5, Value: 1.},
	{Timestamp: time.Unix(11, 0).UnixNano(), Hash: 6, Value: 1.},
	{Timestamp: time.Unix(35, 0).UnixNano(), Hash: 7, Value: 1.},
	{Timestamp: time.Unix(35, 1).UnixNano(), Hash: 8, Value: 1.},
	{Timestamp: time.Unix(40, 0).UnixNano(), Hash: 9, Value: 1.},
	{Timestamp: time.Unix(100, 0).UnixNano(), Hash: 10, Value: 1.},
	{Timestamp: time.Unix(100, 1).UnixNano(), Hash: 11, Value: 1.},
}

var labelFoo, _ = promql_parser.ParseMetric("{app=\"foo\"}")
var labelBar, _ = promql_parser.ParseMetric("{app=\"bar\"}")

func newSampleIterator() iter.SampleIterator {
	return iter.NewHeapSampleIterator(context.Background(), []iter.SampleIterator{
		iter.NewSeriesIterator(logproto.Series{
			Labels:  labelFoo.String(),
			Samples: samples,
		}),
		iter.NewSeriesIterator(logproto.Series{
			Labels:  labelBar.String(),
			Samples: samples,
		}),
	})
}

func newfakePeekingSampleIterator() iter.PeekingSampleIterator {
	return iter.NewPeekingSampleIterator(newSampleIterator())
}

func newPoint(t time.Time, v float64) promql.Point {
	return promql.Point{T: t.UnixNano() / 1e+6, V: v}
}

func Test_RangeVectorIterator(t *testing.T) {
	tests := []struct {
		selRange        int64
		step            int64
		expectedVectors []promql.Vector
		expectedTs      []time.Time
		start, end      time.Time
	}{
		{
			(5 * time.Second).Nanoseconds(), // no overlap
			(30 * time.Second).Nanoseconds(),
			[]promql.Vector{
				[]promql.Sample{
					{Point: newPoint(time.Unix(10, 0), 2), Metric: labelBar},
					{Point: newPoint(time.Unix(10, 0), 2), Metric: labelFoo},
				},
				[]promql.Sample{
					{Point: newPoint(time.Unix(40, 0), 2), Metric: labelBar},
					{Point: newPoint(time.Unix(40, 0), 2), Metric: labelFoo},
				},
				{},
				[]promql.Sample{
					{Point: newPoint(time.Unix(100, 0), 1), Metric: labelBar},
					{Point: newPoint(time.Unix(100, 0), 1), Metric: labelFoo},
				},
			},
			[]time.Time{time.Unix(10, 0), time.Unix(40, 0), time.Unix(70, 0), time.Unix(100, 0)},
			time.Unix(10, 0), time.Unix(100, 0),
		},
		{
			(35 * time.Second).Nanoseconds(), // will overlap by 5 sec
			(30 * time.Second).Nanoseconds(),
			[]promql.Vector{
				[]promql.Sample{
					{Point: newPoint(time.Unix(10, 0), 4), Metric: labelBar},
					{Point: newPoint(time.Unix(10, 0), 4), Metric: labelFoo},
				},
				[]promql.Sample{
					{Point: newPoint(time.Unix(40, 0), 7), Metric: labelBar},
					{Point: newPoint(time.Unix(40, 0), 7), Metric: labelFoo},
				},
				[]promql.Sample{
					{Point: newPoint(time.Unix(70, 0), 2), Metric: labelBar},
					{Point: newPoint(time.Unix(70, 0), 2), Metric: labelFoo},
				},
				[]promql.Sample{
					{Point: newPoint(time.Unix(100, 0), 1), Metric: labelBar},
					{Point: newPoint(time.Unix(100, 0), 1), Metric: labelFoo},
				},
			},
			[]time.Time{time.Unix(10, 0), time.Unix(40, 0), time.Unix(70, 0), time.Unix(100, 0)},
			time.Unix(10, 0), time.Unix(100, 0),
		},
		{
			(30 * time.Second).Nanoseconds(), // same range
			(30 * time.Second).Nanoseconds(),
			[]promql.Vector{
				[]promql.Sample{
					{Point: newPoint(time.Unix(10, 0), 4), Metric: labelBar},
					{Point: newPoint(time.Unix(10, 0), 4), Metric: labelFoo},
				},
				[]promql.Sample{
					{Point: newPoint(time.Unix(40, 0), 5), Metric: labelBar},
					{Point: newPoint(time.Unix(40, 0), 5), Metric: labelFoo},
				},
				[]promql.Sample{},
				[]promql.Sample{
					{Point: newPoint(time.Unix(100, 0), 1), Metric: labelBar},
					{Point: newPoint(time.Unix(100, 0), 1), Metric: labelFoo},
				},
			},
			[]time.Time{time.Unix(10, 0), time.Unix(40, 0), time.Unix(70, 0), time.Unix(100, 0)},
			time.Unix(10, 0), time.Unix(100, 0),
		},
		{
			(50 * time.Second).Nanoseconds(), // all step are overlapping
			(10 * time.Second).Nanoseconds(),
			[]promql.Vector{
				[]promql.Sample{
					{Point: newPoint(time.Unix(110, 0), 2), Metric: labelBar},
					{Point: newPoint(time.Unix(110, 0), 2), Metric: labelFoo},
				},
				[]promql.Sample{
					{Point: newPoint(time.Unix(120, 0), 2), Metric: labelBar},
					{Point: newPoint(time.Unix(120, 0), 2), Metric: labelFoo},
				},
			},
			[]time.Time{time.Unix(110, 0), time.Unix(120, 0)},
			time.Unix(110, 0), time.Unix(120, 0),
		},
	}

	for _, tt := range tests {
		t.Run(
			fmt.Sprintf("logs[%s] - step: %s", time.Duration(tt.selRange), time.Duration(tt.step)),
			func(t *testing.T) {
				it := newRangeVectorIterator(newfakePeekingSampleIterator(), tt.selRange,
					tt.step, tt.start.UnixNano(), tt.end.UnixNano())

				i := 0
				for it.Next() {
					ts, v := it.At(countOverTime)
					require.ElementsMatch(t, tt.expectedVectors[i], v)
					require.Equal(t, tt.expectedTs[i].UnixNano()/1e+6, ts)
					i++
				}
				require.Equal(t, len(tt.expectedTs), i)
				require.Equal(t, len(tt.expectedVectors), i)
			})
	}
}
