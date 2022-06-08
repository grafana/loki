package logql

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
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
	{Timestamp: time.Unix(200, 0).UnixNano(), Hash: 12, Value: 0.001},
	{Timestamp: time.Unix(201, 0).UnixNano(), Hash: 13, Value: 0.003},
	{Timestamp: time.Unix(206, 0).UnixNano(), Hash: 14, Value: 0.01},
	{Timestamp: time.Unix(207, 0).UnixNano(), Hash: 15, Value: 0.02},
	{Timestamp: time.Unix(207, 1).UnixNano(), Hash: 16, Value: 0.005},
	{Timestamp: time.Unix(235, 1).UnixNano(), Hash: 17, Value: 0.002},
	{Timestamp: time.Unix(235, 2).UnixNano(), Hash: 18, Value: 0.001},
	{Timestamp: time.Unix(240, 0).UnixNano(), Hash: 19, Value: 0.023},
	{Timestamp: time.Unix(300, 0).UnixNano(), Hash: 20, Value: 0.021},
	{Timestamp: time.Unix(300, 1).UnixNano(), Hash: 21, Value: 0.005},
}

var (
	labelFoo, _      = syntax.ParseLabels("{app=\"foo\"}")
	labelBar, _      = syntax.ParseLabels("{app=\"bar\"}")
	labelFooLe005, _ = syntax.ParseLabels("{app=\"foo\", le=\"0.005\"}")
	labelFooLe025, _ = syntax.ParseLabels("{app=\"foo\", le=\"0.025\"}")
	labelBarLe005, _ = syntax.ParseLabels("{app=\"bar\", le=\"0.005\"}")
	labelBarLe025, _ = syntax.ParseLabels("{app=\"bar\", le=\"0.025\"}")
)

func newSampleIterator() iter.SampleIterator {
	return iter.NewSortSampleIterator([]iter.SampleIterator{
		iter.NewSeriesIterator(logproto.Series{
			Labels:     labelFoo.String(),
			Samples:    samples,
			StreamHash: labelFoo.Hash(),
		}),
		iter.NewSeriesIterator(logproto.Series{
			Labels:     labelBar.String(),
			Samples:    samples,
			StreamHash: labelBar.Hash(),
		}),
	})
}

func newfakePeekingSampleIterator() iter.PeekingSampleIterator {
	return iter.NewPeekingSampleIterator(newSampleIterator())
}

func newPoint(t time.Time, v float64) promql.Point {
	return promql.Point{T: t.UnixNano() / 1e+6, V: v}
}

func countOverTimeAgg() RangeVectorAggregator {
	return countOverTime
}

func Test_RangeVectorIterator(t *testing.T) {
	tests := []struct {
		selRange        int64
		step            int64
		offset          int64
		expectedVectors []promql.Vector
		expectedTs      []time.Time
		start, end      time.Time
	}{
		{
			(5 * time.Second).Nanoseconds(), // no overlap
			(30 * time.Second).Nanoseconds(),
			0,
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
			0,
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
			0,
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
			0,
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
		{
			(5 * time.Second).Nanoseconds(), // no overlap
			(30 * time.Second).Nanoseconds(),
			(10 * time.Second).Nanoseconds(),
			[]promql.Vector{
				[]promql.Sample{
					{Point: newPoint(time.Unix(20, 0), 2), Metric: labelBar},
					{Point: newPoint(time.Unix(20, 0), 2), Metric: labelFoo},
				},
				[]promql.Sample{
					{Point: newPoint(time.Unix(50, 0), 2), Metric: labelBar},
					{Point: newPoint(time.Unix(50, 0), 2), Metric: labelFoo},
				},
				{},
				[]promql.Sample{
					{Point: newPoint(time.Unix(110, 0), 1), Metric: labelBar},
					{Point: newPoint(time.Unix(110, 0), 1), Metric: labelFoo},
				},
			},
			[]time.Time{time.Unix(20, 0), time.Unix(50, 0), time.Unix(80, 0), time.Unix(110, 0)},
			time.Unix(20, 0), time.Unix(110, 0),
		},
	}

	for _, tt := range tests {
		t.Run(
			fmt.Sprintf("logs[%s] - step: %s - offset: %s", time.Duration(tt.selRange), time.Duration(tt.step), time.Duration(tt.offset)),
			func(t *testing.T) {
				it := newRangeVectorIterator(newfakePeekingSampleIterator(), tt.selRange,
					tt.step, tt.start.UnixNano(), tt.end.UnixNano(), tt.offset)

				i := 0
				for it.Next() {
					ts, v := it.At(countOverTimeAgg())
					require.ElementsMatch(t, tt.expectedVectors[i], v)
					require.Equal(t, tt.expectedTs[i].UnixNano()/1e+6, ts)
					i++
				}
				require.Equal(t, len(tt.expectedTs), i)
				require.Equal(t, len(tt.expectedVectors), i)
			})
	}
}

func Test_HistogramRangeVectorIterator(t *testing.T) {
	tests := []struct {
		selRange        int64
		step            int64
		offset          int64
		expectedVectors []promql.Vector
		expectedTs      []time.Time
		start, end      time.Time
	}{
		{
			(5 * time.Second).Nanoseconds(), // no overlap
			(30 * time.Second).Nanoseconds(),
			0,
			[]promql.Vector{
				[]promql.Sample{
					{Point: newPoint(time.Unix(210, 0), 1), Metric: labelBarLe005},
					{Point: newPoint(time.Unix(210, 0), 1), Metric: labelFooLe005},
					{Point: newPoint(time.Unix(210, 0), 2), Metric: labelBarLe025},
					{Point: newPoint(time.Unix(210, 0), 2), Metric: labelFooLe025},
				},
				[]promql.Sample{
					{Point: newPoint(time.Unix(240, 0), 2), Metric: labelBarLe005},
					{Point: newPoint(time.Unix(240, 0), 2), Metric: labelFooLe005},
					{Point: newPoint(time.Unix(240, 0), 1), Metric: labelBarLe025},
					{Point: newPoint(time.Unix(240, 0), 1), Metric: labelFooLe025},
				},
				{},
				[]promql.Sample{
					{Point: newPoint(time.Unix(300, 0), 1), Metric: labelBarLe025},
					{Point: newPoint(time.Unix(300, 0), 1), Metric: labelFooLe025},
				},
			},
			[]time.Time{time.Unix(210, 0), time.Unix(240, 0), time.Unix(270, 0), time.Unix(300, 0)},
			time.Unix(210, 0), time.Unix(300, 0),
		},
		{
			(35 * time.Second).Nanoseconds(), // will overlap by 5 sec
			(30 * time.Second).Nanoseconds(),
			0,
			[]promql.Vector{
				[]promql.Sample{
					{Point: newPoint(time.Unix(210, 0), 3), Metric: labelBarLe005},
					{Point: newPoint(time.Unix(210, 0), 3), Metric: labelFooLe005},
					{Point: newPoint(time.Unix(210, 0), 2), Metric: labelBarLe025},
					{Point: newPoint(time.Unix(210, 0), 2), Metric: labelFooLe025},
				},
				[]promql.Sample{
					{Point: newPoint(time.Unix(240, 0), 3), Metric: labelBarLe005},
					{Point: newPoint(time.Unix(240, 0), 3), Metric: labelFooLe005},
					{Point: newPoint(time.Unix(240, 0), 3), Metric: labelBarLe025},
					{Point: newPoint(time.Unix(240, 0), 3), Metric: labelFooLe025},
				},
				[]promql.Sample{
					{Point: newPoint(time.Unix(270, 0), 2), Metric: labelBarLe005},
					{Point: newPoint(time.Unix(270, 0), 2), Metric: labelFooLe005},
					{Point: newPoint(time.Unix(270, 0), 1), Metric: labelBarLe025},
					{Point: newPoint(time.Unix(270, 0), 1), Metric: labelFooLe025},
				},
				[]promql.Sample{
					{Point: newPoint(time.Unix(300, 0), 1), Metric: labelBarLe025},
					{Point: newPoint(time.Unix(300, 0), 1), Metric: labelFooLe025},
				},
			},
			[]time.Time{time.Unix(210, 0), time.Unix(240, 0), time.Unix(270, 0), time.Unix(300, 0)},
			time.Unix(210, 0), time.Unix(300, 0),
		},
		{
			(30 * time.Second).Nanoseconds(), // same range
			(30 * time.Second).Nanoseconds(),
			0,
			[]promql.Vector{
				[]promql.Sample{
					{Point: newPoint(time.Unix(210, 0), 3), Metric: labelBarLe005},
					{Point: newPoint(time.Unix(210, 0), 3), Metric: labelFooLe005},
					{Point: newPoint(time.Unix(210, 0), 2), Metric: labelBarLe025},
					{Point: newPoint(time.Unix(210, 0), 2), Metric: labelFooLe025},
				},
				[]promql.Sample{
					{Point: newPoint(time.Unix(240, 0), 2), Metric: labelBarLe005},
					{Point: newPoint(time.Unix(240, 0), 2), Metric: labelFooLe005},
					{Point: newPoint(time.Unix(240, 0), 1), Metric: labelBarLe025},
					{Point: newPoint(time.Unix(240, 0), 1), Metric: labelFooLe025},
				},
				[]promql.Sample{},
				[]promql.Sample{
					{Point: newPoint(time.Unix(300, 0), 1), Metric: labelBarLe025},
					{Point: newPoint(time.Unix(300, 0), 1), Metric: labelFooLe025},
				},
			},
			[]time.Time{time.Unix(210, 0), time.Unix(240, 0), time.Unix(270, 0), time.Unix(300, 0)},
			time.Unix(210, 0), time.Unix(300, 0),
		},
		{
			(50 * time.Second).Nanoseconds(), // all step are overlapping
			(10 * time.Second).Nanoseconds(),
			0,
			[]promql.Vector{
				[]promql.Sample{
					{Point: newPoint(time.Unix(310, 0), 1), Metric: labelBarLe005},
					{Point: newPoint(time.Unix(310, 0), 1), Metric: labelFooLe005},
					{Point: newPoint(time.Unix(310, 0), 1), Metric: labelBarLe025},
					{Point: newPoint(time.Unix(310, 0), 1), Metric: labelFooLe025},
				},
				[]promql.Sample{
					{Point: newPoint(time.Unix(320, 0), 1), Metric: labelBarLe005},
					{Point: newPoint(time.Unix(320, 0), 1), Metric: labelFooLe005},
					{Point: newPoint(time.Unix(320, 0), 1), Metric: labelBarLe025},
					{Point: newPoint(time.Unix(320, 0), 1), Metric: labelFooLe025},
				},
			},
			[]time.Time{time.Unix(310, 0), time.Unix(320, 0)},
			time.Unix(310, 0), time.Unix(320, 0),
		},
		{
			(5 * time.Second).Nanoseconds(), // no overlap
			(30 * time.Second).Nanoseconds(),
			(10 * time.Second).Nanoseconds(),
			[]promql.Vector{
				[]promql.Sample{
					{Point: newPoint(time.Unix(220, 0), 1), Metric: labelBarLe005},
					{Point: newPoint(time.Unix(220, 0), 1), Metric: labelFooLe005},
					{Point: newPoint(time.Unix(220, 0), 2), Metric: labelBarLe025},
					{Point: newPoint(time.Unix(220, 0), 2), Metric: labelFooLe025},
				},
				[]promql.Sample{
					{Point: newPoint(time.Unix(250, 0), 2), Metric: labelBarLe005},
					{Point: newPoint(time.Unix(250, 0), 2), Metric: labelFooLe005},
					{Point: newPoint(time.Unix(250, 0), 1), Metric: labelBarLe025},
					{Point: newPoint(time.Unix(250, 0), 1), Metric: labelFooLe025},
				},
				{},
				[]promql.Sample{
					{Point: newPoint(time.Unix(310, 0), 1), Metric: labelBarLe025},
					{Point: newPoint(time.Unix(310, 0), 1), Metric: labelFooLe025},
				},
			},
			[]time.Time{time.Unix(220, 0), time.Unix(250, 0), time.Unix(280, 0), time.Unix(310, 0)},
			time.Unix(220, 0), time.Unix(310, 0),
		},
	}

	for _, tt := range tests {
		t.Run(
			fmt.Sprintf("logs[%s] - step: %s - offset: %s", time.Duration(tt.selRange), time.Duration(tt.step), time.Duration(tt.offset)),
			func(t *testing.T) {
				it := newRangeVectorIterator(newfakePeekingSampleIterator(), tt.selRange,
					tt.step, tt.start.UnixNano(), tt.end.UnixNano(), tt.offset)

				i := 0
				for it.Next() {
					ts, v := it.At(bucketsOverTime([]float64{0.005, 0.025}))
					require.ElementsMatch(t, tt.expectedVectors[i], v)
					require.Equal(t, tt.expectedTs[i].UnixNano()/1e+6, ts)
					i++
				}
				require.Equal(t, len(tt.expectedTs), i)
				require.Equal(t, len(tt.expectedVectors), i)
			})
	}
}

func Test_RangeVectorIteratorBadLabels(t *testing.T) {
	badIterator := iter.NewPeekingSampleIterator(
		iter.NewSeriesIterator(logproto.Series{
			Labels:  "{badlabels=}",
			Samples: samples,
		}))
	it := newRangeVectorIterator(badIterator, (30 * time.Second).Nanoseconds(),
		(30 * time.Second).Nanoseconds(), time.Unix(10, 0).UnixNano(), time.Unix(100, 0).UnixNano(), 0)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer cancel()
		for it.Next() {
		}
	}()
	select {
	case <-time.After(1 * time.Second):
		require.Fail(t, "goroutine took too long")
	case <-ctx.Done():
	}
}
