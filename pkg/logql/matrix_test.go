package logql

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
)

func TestMatrixStepper(t *testing.T) {
	var (
		start = time.Unix(0, 0)
		end   = time.Unix(6, 0)
		step  = time.Second
	)

	m := promql.Matrix{
		promql.Series{
			Metric: labels.Labels{{Name: "foo", Value: "bar"}},
			Points: []promql.Point{
				{T: start.UnixNano() / int64(step), V: 0},
				{T: start.Add(step).UnixNano() / int64(time.Millisecond), V: 1},
				{T: start.Add(2*step).UnixNano() / int64(time.Millisecond), V: 2},
				{T: start.Add(3*step).UnixNano() / int64(time.Millisecond), V: 3},
				{T: start.Add(4*step).UnixNano() / int64(time.Millisecond), V: 4},
				{T: start.Add(5*step).UnixNano() / int64(time.Millisecond), V: 5},
			},
		},
		promql.Series{
			Metric: labels.Labels{{Name: "bazz", Value: "buzz"}},
			Points: []promql.Point{
				{T: start.Add(2*step).UnixNano() / int64(time.Millisecond), V: 2},
				{T: start.Add(4*step).UnixNano() / int64(time.Millisecond), V: 4},
			},
		},
	}

	s := NewMatrixStepper(start, end, step, m)

	expected := []promql.Vector{
		{
			promql.Sample{
				Point:  promql.Point{T: start.UnixNano() / int64(step), V: 0},
				Metric: labels.Labels{{Name: "foo", Value: "bar"}},
			},
			promql.Sample{
				Point:  promql.Point{T: start.UnixNano() / int64(step), V: 0},
				Metric: labels.Labels{{Name: "bazz", Value: "buzz"}},
			},
		},
		{
			promql.Sample{
				Point:  promql.Point{T: start.Add(step).UnixNano() / int64(time.Millisecond), V: 1},
				Metric: labels.Labels{{Name: "foo", Value: "bar"}},
			},
			promql.Sample{
				Point:  promql.Point{T: start.Add(step).UnixNano() / int64(time.Millisecond), V: 0},
				Metric: labels.Labels{{Name: "bazz", Value: "buzz"}},
			},
		},
		{
			promql.Sample{
				Point:  promql.Point{T: start.Add(2*step).UnixNano() / int64(time.Millisecond), V: 2},
				Metric: labels.Labels{{Name: "foo", Value: "bar"}},
			},
			promql.Sample{
				Point:  promql.Point{T: start.Add(2*step).UnixNano() / int64(time.Millisecond), V: 2},
				Metric: labels.Labels{{Name: "bazz", Value: "buzz"}},
			},
		},
		{
			promql.Sample{
				Point:  promql.Point{T: start.Add(3*step).UnixNano() / int64(time.Millisecond), V: 3},
				Metric: labels.Labels{{Name: "foo", Value: "bar"}},
			},
			promql.Sample{
				Point:  promql.Point{T: start.Add(3*step).UnixNano() / int64(time.Millisecond), V: 0},
				Metric: labels.Labels{{Name: "bazz", Value: "buzz"}},
			},
		},
		{
			promql.Sample{
				Point:  promql.Point{T: start.Add(4*step).UnixNano() / int64(time.Millisecond), V: 4},
				Metric: labels.Labels{{Name: "foo", Value: "bar"}},
			},
			promql.Sample{
				Point:  promql.Point{T: start.Add(4*step).UnixNano() / int64(time.Millisecond), V: 4},
				Metric: labels.Labels{{Name: "bazz", Value: "buzz"}},
			},
		},
		{
			promql.Sample{
				Point:  promql.Point{T: start.Add(5*step).UnixNano() / int64(time.Millisecond), V: 5},
				Metric: labels.Labels{{Name: "foo", Value: "bar"}},
			},
			promql.Sample{
				Point:  promql.Point{T: start.Add(5*step).UnixNano() / int64(time.Millisecond), V: 0},
				Metric: labels.Labels{{Name: "bazz", Value: "buzz"}},
			},
		},
	}

	for i := 0; i < int(end.Sub(start)/step); i++ {
		ok, ts, vec := s.Next()
		require.Equal(t, ok, true)
		require.Equal(t, start.Add(step*time.Duration(i)).UnixNano()/int64(time.Millisecond), ts)
		require.Equal(t, expected[i], vec)
	}

	ok, _, _ := s.Next()

	require.Equal(t, ok, false)
}
