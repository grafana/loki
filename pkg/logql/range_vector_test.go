package logql

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
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
}

var (
	labelFoo, _ = syntax.ParseLabels("{app=\"foo\"}")
	labelBar, _ = syntax.ParseLabels("{app=\"bar\"}")
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

func Benchmark_RangeVectorIteratorCompare(b *testing.B) {

	// no overlap test case.
	buildStreamingIt := func() (RangeVectorIterator, error) {
		tt := struct {
			selRange   int64
			step       int64
			offset     int64
			start, end time.Time
		}{
			(5 * time.Second).Nanoseconds(),
			(30 * time.Second).Nanoseconds(),
			0,
			time.Unix(10, 0),
			time.Unix(100, 0),
		}

		iter := newfakePeekingSampleIterator()
		expr := &syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeCount}
		selRange := tt.selRange
		step := tt.step
		start := tt.start.UnixNano()
		end := tt.end.UnixNano()
		offset := tt.offset
		if step == 0 {
			step = 1
		}
		if offset != 0 {
			start = start - offset
			end = end - offset
		}

		it := &streamRangeVectorIterator{
			iter:     iter,
			step:     step,
			end:      end,
			selRange: selRange,
			metrics:  map[string]labels.Labels{},
			r:        expr,
			current:  start - step, // first loop iteration will set it to start
			offset:   offset,
		}
		return it, nil
	}

	buildBatchIt := func() (RangeVectorIterator, error) {
		tt := struct {
			selRange   int64
			step       int64
			offset     int64
			start, end time.Time
		}{
			(5 * time.Second).Nanoseconds(), // no overlap
			(30 * time.Second).Nanoseconds(),
			0,
			time.Unix(10, 0),
			time.Unix(100, 0),
		}

		iter := newfakePeekingSampleIterator()
		expr := &syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeCount}
		selRange := tt.selRange
		step := tt.step
		start := tt.start.UnixNano()
		end := tt.end.UnixNano()
		offset := tt.offset
		if step == 0 {
			step = 1
		}
		if offset != 0 {
			start = start - offset
			end = end - offset
		}

		vectorAggregator, err := aggregator(expr)
		if err != nil {
			return nil, err
		}

		return &batchRangeVectorIterator{
			iter:     iter,
			step:     step,
			end:      end,
			selRange: selRange,
			metrics:  map[string]labels.Labels{},
			window:   map[string]*promql.Series{},
			agg:      vectorAggregator,
			current:  start - step, // first loop iteration will set it to start
			offset:   offset,
		}, nil
	}

	b.Run("streaming agg", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			it, err := buildStreamingIt()
			if err != nil {
				b.Fatal(err)
			}
			for it.Next() {
				_, _ = it.At()
			}
		}
	})

	b.Run("batch agg", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			it, err := buildBatchIt()
			if err != nil {
				b.Fatal(err)
			}
			for it.Next() {
				_, _ = it.At()
			}
		}
	})

}

func Benchmark_RangeVectorIterator(b *testing.B) {
	b.ReportAllocs()
	tt := struct {
		selRange   int64
		step       int64
		offset     int64
		start, end time.Time
	}{
		(5 * time.Second).Nanoseconds(), // no overlap
		(30 * time.Second).Nanoseconds(),
		0,
		time.Unix(10, 0),
		time.Unix(100, 0),
	}

	for i := 0; i < b.N; i++ {
		i := 0
		it, err := newRangeVectorIterator(newfakePeekingSampleIterator(),
			&syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeCount}, tt.selRange,
			tt.step, tt.start.UnixNano(), tt.end.UnixNano(), tt.offset)
		if err != nil {
			panic(err)
		}
		for it.Next() {
			_, _ = it.At()
			i++
		}
	}

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
				it, err := newRangeVectorIterator(newfakePeekingSampleIterator(),
					&syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeCount}, tt.selRange,
					tt.step, tt.start.UnixNano(), tt.end.UnixNano(), tt.offset)
				require.NoError(t, err)

				i := 0
				for it.Next() {
					ts, v := it.At()
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
	it, err := newRangeVectorIterator(badIterator,
		&syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeCount}, (30 * time.Second).Nanoseconds(),
		(30 * time.Second).Nanoseconds(), time.Unix(10, 0).UnixNano(), time.Unix(100, 0).UnixNano(), 0)
	require.NoError(t, err)

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
