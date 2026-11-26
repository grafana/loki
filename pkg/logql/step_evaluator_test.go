package logql

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// testSketchVec is a StepResult used only to drive SketchMatrixStepEvaluator.
type testSketchVec struct {
	id string
}

func (testSketchVec) SampleVector() promql.Vector                    { return nil }
func (testSketchVec) QuantileSketchVec() ProbabilisticQuantileVector { return nil }
func (testSketchVec) CountMinSketchVec() CountMinSketchVector        { return CountMinSketchVector{} }
func (testSketchVec) CountDistinctSketchVec() CountDistinctSketchVector {
	return nil
}

var _ StepResult = testSketchVec{}

func TestSketchMatrixStepEvaluator(t *testing.T) {
	var (
		start = time.Unix(0, 0)
		end   = time.Unix(2, 0)
		step  = time.Second
	)
	params := mustSketchMatrixParams(t, start, end, step)

	v0 := testSketchVec{id: "0"}
	v1 := testSketchVec{id: "1"}
	v2 := testSketchVec{id: "2"}
	ev := newSketchMatrixStepEvaluator([]testSketchVec{v0, v1, v2}, params, "TestSketch")

	for i, want := range []testSketchVec{v0, v1, v2} {
		ok, ts, r := ev.Next()
		require.True(t, ok)
		require.Equal(t, start.Add(step*time.Duration(i)).UnixMilli(), ts)
		require.Equal(t, want, r)
	}

	ok, _, r := ev.Next()
	require.False(t, ok)
	require.Nil(t, r)
}

func TestSketchMatrixStepEvaluator_EmptyMatrix(t *testing.T) {
	params := mustSketchMatrixParams(t, time.Unix(0, 0), time.Unix(2, 0), time.Second)
	ev := newSketchMatrixStepEvaluator([]testSketchVec{}, params, "TestSketch")

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
	only := testSketchVec{id: "only"}
	ev := newSketchMatrixStepEvaluator([]testSketchVec{only}, params, "TestSketch")

	ok, ts, r := ev.Next()
	require.True(t, ok)
	require.Equal(t, start.UnixMilli(), ts)
	require.Equal(t, only, r)

	ok, _, r = ev.Next()
	require.False(t, ok)
	require.Nil(t, r)
}

func TestSketchMatrixStepEvaluator_StopsAtEnd(t *testing.T) {
	start := time.Unix(0, 0)
	params := mustSketchMatrixParams(t, start, start, time.Second)
	first := testSketchVec{id: "first"}
	unused := testSketchVec{id: "unused"}
	ev := newSketchMatrixStepEvaluator([]testSketchVec{first, unused}, params, "TestSketch")

	ok, ts, r := ev.Next()
	require.True(t, ok)
	require.Equal(t, start.UnixMilli(), ts)
	require.Equal(t, first, r)

	ok, _, r = ev.Next()
	require.False(t, ok)
	require.Nil(t, r)
}

func TestSketchMatrixStepEvaluator_CloseErrorExplain(t *testing.T) {
	params := mustSketchMatrixParams(t, time.Unix(0, 0), time.Unix(0, 0), time.Second)
	ev := newSketchMatrixStepEvaluator([]testSketchVec{}, params, "TestSketch")

	require.NoError(t, ev.Close())
	require.NoError(t, ev.Error())

	tree := NewTree()
	ev.Explain(tree)
	require.Equal(t, "TestSketch\n", tree.String())
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
