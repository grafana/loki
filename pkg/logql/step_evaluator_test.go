package logql

import (
	"testing"
	"time"

	"github.com/axiomhq/hyperloglog"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestSketchMatrixStepEvaluator(t *testing.T) {
	var (
		start = time.Unix(0, 0)
		end   = time.Unix(2, 0)
		step  = time.Second
	)
	params := mustSketchMatrixParams(t, start, end, step)

	v0 := countDistinctStepVec("0")
	v1 := countDistinctStepVec("1")
	v2 := countDistinctStepVec("2")
	ev := NewCountDistinctSketchMatrixStepEvaluator(CountDistinctSketchMatrix{v0, v1, v2}, params)

	for i, want := range []CountDistinctSketchVector{v0, v1, v2} {
		ok, ts, r := ev.Next()
		require.True(t, ok)
		require.Equal(t, start.Add(step*time.Duration(i)).UnixMilli(), ts)
		require.Equal(t, want, r.CountDistinctSketchVec())
	}

	ok, _, r := ev.Next()
	require.False(t, ok)
	require.Nil(t, r)
}

func TestSketchMatrixStepEvaluator_EmptyMatrix(t *testing.T) {
	params := mustSketchMatrixParams(t, time.Unix(0, 0), time.Unix(2, 0), time.Second)
	ev := NewCountDistinctSketchMatrixStepEvaluator(CountDistinctSketchMatrix{}, params)

	ok, _, r := ev.Next()
	require.False(t, ok)
	require.Nil(t, r)
}

func TestSketchMatrixStepEvaluator_FewerVectorsThanSteps(t *testing.T) {
	var (
		start = time.Unix(0, 0)
		end   = time.Unix(2, 0)
		step  = time.Second
	)
	params := mustSketchMatrixParams(t, start, end, step)
	only := countDistinctStepVec("only")
	ev := NewCountDistinctSketchMatrixStepEvaluator(CountDistinctSketchMatrix{only}, params)

	ok, ts, r := ev.Next()
	require.True(t, ok)
	require.Equal(t, start.UnixMilli(), ts)
	require.Equal(t, only, r.CountDistinctSketchVec())

	ok, _, r = ev.Next()
	require.False(t, ok)
	require.Nil(t, r)
}

func TestSketchMatrixStepEvaluator_StopsAtEnd(t *testing.T) {
	start := time.Unix(0, 0)
	params := mustSketchMatrixParams(t, start, start, time.Second)
	first := countDistinctStepVec("first")
	unused := countDistinctStepVec("unused")
	ev := NewCountDistinctSketchMatrixStepEvaluator(CountDistinctSketchMatrix{first, unused}, params)

	ok, ts, r := ev.Next()
	require.True(t, ok)
	require.Equal(t, start.UnixMilli(), ts)
	require.Equal(t, first, r.CountDistinctSketchVec())

	ok, _, r = ev.Next()
	require.False(t, ok)
	require.Nil(t, r)
}

func TestSketchMatrixStepEvaluator_QuantileAlias(t *testing.T) {
	var (
		start = time.Unix(0, 0)
		end   = time.Unix(1, 0)
		step  = time.Second
	)
	params := mustSketchMatrixParams(t, start, end, step)
	v0 := ProbabilisticQuantileVector{{T: 0, Metric: labels.FromStrings("step", "0")}}
	v1 := ProbabilisticQuantileVector{{T: 1, Metric: labels.FromStrings("step", "1")}}
	ev := NewQuantileSketchMatrixStepEvaluator(ProbabilisticQuantileMatrix{v0, v1}, params)

	ok, ts, r := ev.Next()
	require.True(t, ok)
	require.Equal(t, start.UnixMilli(), ts)
	require.Equal(t, v0, r.QuantileSketchVec())

	ok, ts, r = ev.Next()
	require.True(t, ok)
	require.Equal(t, start.Add(step).UnixMilli(), ts)
	require.Equal(t, v1, r.QuantileSketchVec())

	ok, _, r = ev.Next()
	require.False(t, ok)
	require.Nil(t, r)
}

func TestSketchMatrixStepEvaluator_CloseErrorExplain(t *testing.T) {
	params := mustSketchMatrixParams(t, time.Unix(0, 0), time.Unix(0, 0), time.Second)

	t.Run("count-distinct", func(t *testing.T) {
		ev := NewCountDistinctSketchMatrixStepEvaluator(CountDistinctSketchMatrix{}, params)
		require.NoError(t, ev.Close())
		require.NoError(t, ev.Error())

		tree := NewTree()
		ev.Explain(tree)
		require.Equal(t, "CountDistinctSketchMatrix\n", tree.String())
	})

	t.Run("quantile", func(t *testing.T) {
		ev := NewQuantileSketchMatrixStepEvaluator(ProbabilisticQuantileMatrix{}, params)
		require.NoError(t, ev.Close())
		require.NoError(t, ev.Error())

		tree := NewTree()
		ev.Explain(tree)
		require.Equal(t, "QuantileSketchMatrix\n", tree.String())
	})
}

func mustSketchMatrixParams(t *testing.T, start, end time.Time, step time.Duration) LiteralParams {
	t.Helper()
	params, err := NewLiteralParams(
		`count_over_time({app="a"}[1m])`,
		start,
		end,
		step,
		0,
		logproto.FORWARD,
		1000,
		nil,
		nil,
	)
	require.NoError(t, err)
	return params
}

func countDistinctStepVec(step string) CountDistinctSketchVector {
	return CountDistinctSketchVector{{
		F:      hyperloglog.New14(),
		Metric: labels.FromStrings("step", step),
	}}
}
