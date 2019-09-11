package logql

import (
	"fmt"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
)

var entries = []logproto.Entry{
	{Timestamp: time.Unix(2, 0)},
	{Timestamp: time.Unix(5, 0)},
	{Timestamp: time.Unix(6, 0)},
	{Timestamp: time.Unix(10, 0)},
	{Timestamp: time.Unix(10, 1)},
	{Timestamp: time.Unix(11, 0)},
	{Timestamp: time.Unix(35, 0)},
	{Timestamp: time.Unix(35, 1)},
	{Timestamp: time.Unix(40, 0)},
	{Timestamp: time.Unix(100, 0)},
	{Timestamp: time.Unix(100, 1)},
}

var labelFoo, _ = promql.ParseMetric("{app=\"foo\"}")
var labelBar, _ = promql.ParseMetric("{app=\"bar\"}")

func newEntryIterator() iter.EntryIterator {
	return iter.NewHeapIterator([]iter.EntryIterator{
		iter.NewStreamIterator(&logproto.Stream{
			Labels:  labelFoo.String(),
			Entries: entries,
		}),
		iter.NewStreamIterator(&logproto.Stream{
			Labels:  labelBar.String(),
			Entries: entries,
		}),
	}, logproto.FORWARD)
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
		},
	}

	for _, tt := range tests {
		t.Run(
			fmt.Sprintf("logs[%s] - step: %s", time.Duration(tt.selRange), time.Duration(tt.step)),
			func(t *testing.T) {
				it := newRangeVectorIterator(newEntryIterator(), tt.selRange,
					tt.step, time.Unix(10, 0).UnixNano(), time.Unix(100, 0).UnixNano())

				i := 0
				for it.Next() {
					ts, v := it.At(count)
					require.ElementsMatch(t, tt.expectedVectors[i], v)
					require.Equal(t, tt.expectedTs[i].UnixNano()/1e+6, ts)
					i++
				}
				require.Equal(t, len(tt.expectedTs), i)
				require.Equal(t, len(tt.expectedVectors), i)
			})
	}
}
