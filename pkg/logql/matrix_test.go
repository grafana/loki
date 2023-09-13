package logql

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
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
			Metric: labels.FromStrings("foo", "bar"),
			Floats: []promql.FPoint{
				{T: start.UnixNano(), F: 0},
				{T: start.Add(step).UnixNano() / int64(time.Millisecond), F: 1},
				{T: start.Add(2*step).UnixNano() / int64(time.Millisecond), F: 2},
				{T: start.Add(3*step).UnixNano() / int64(time.Millisecond), F: 3},
				{T: start.Add(4*step).UnixNano() / int64(time.Millisecond), F: 4},
				{T: start.Add(5*step).UnixNano() / int64(time.Millisecond), F: 5},
			},
		},
		promql.Series{
			Metric: labels.FromStrings("bazz", "buzz"),
			Floats: []promql.FPoint{
				{T: start.Add(2*step).UnixNano() / int64(time.Millisecond), F: 2},
				{T: start.Add(4*step).UnixNano() / int64(time.Millisecond), F: 4},
			},
		},
	}

	s := NewMatrixStepEvaluator(start, end, step, m)

	expected := []promql.Vector{
		{
			promql.Sample{
				T: start.UnixNano(), F: 0,
				Metric: labels.FromStrings("foo", "bar"),
			},
		},
		{
			promql.Sample{
				T: start.Add(step).UnixNano() / int64(time.Millisecond), F: 1,
				Metric: labels.FromStrings("foo", "bar"),
			},
		},
		{
			promql.Sample{
				T: start.Add(2*step).UnixNano() / int64(time.Millisecond), F: 2,
				Metric: labels.FromStrings("foo", "bar"),
			},
			promql.Sample{
				T: start.Add(2*step).UnixNano() / int64(time.Millisecond), F: 2,
				Metric: labels.FromStrings("bazz", "buzz"),
			},
		},
		{
			promql.Sample{
				T: start.Add(3*step).UnixNano() / int64(time.Millisecond), F: 3,
				Metric: labels.FromStrings("foo", "bar"),
			},
		},
		{
			promql.Sample{
				T: start.Add(4*step).UnixNano() / int64(time.Millisecond), F: 4,
				Metric: labels.FromStrings("foo", "bar"),
			},
			promql.Sample{
				T: start.Add(4*step).UnixNano() / int64(time.Millisecond), F: 4,
				Metric: labels.FromStrings("bazz", "buzz"),
			},
		},
		{
			promql.Sample{
				T: start.Add(5*step).UnixNano() / int64(time.Millisecond), F: 5,
				Metric: labels.FromStrings("foo", "bar"),
			},
		},
		{},
	}

	for i := 0; i <= int(end.Sub(start)/step); i++ {
		ok, ts, vec := s.Next()
		require.Equal(t, ok, true)
		require.Equal(t, start.Add(step*time.Duration(i)).UnixNano()/int64(time.Millisecond), ts)
		require.Equal(t, expected[i], vec.PromVec())
	}

	ok, _, _ := s.Next()

	require.Equal(t, ok, false)
}

func Test_SingleStepMatrix(t *testing.T) {
	var (
		start = time.Unix(0, 0)
		end   = time.Unix(0, 0)
		step  = time.Second
	)

	m := promql.Matrix{
		promql.Series{
			Metric: labels.EmptyLabels(),
			Floats: []promql.FPoint{
				{T: start.UnixNano(), F: 10},
			},
		},
	}

	s := NewMatrixStepEvaluator(start, end, step, m)

	ok, ts, vec := s.Next()
	require.True(t, ok)
	require.Equal(t, start.UnixNano(), ts)
	require.Equal(t, promql.Vector{promql.Sample{
		T: start.UnixNano(), F: 10,
		Metric: labels.EmptyLabels(),
	}}, vec.PromVec())

	ok, _, _ = s.Next()
	require.False(t, ok)
}
