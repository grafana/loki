package ruler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logqlmodel"
)

const fixedRandNum int64 = 987654321

type mockEval struct{}

func (m mockEval) Eval(context.Context, string, time.Time) (*logqlmodel.Result, error) {
	return nil, nil
}

type fakeSource struct{}

func (f fakeSource) Int63() int64 { return fixedRandNum }
func (f fakeSource) Seed(int64)   {}

func TestEvaluationWithJitter(t *testing.T) {
	const jitter = 2 * time.Second

	eval := NewEvaluatorWithJitter(mockEval{}, jitter, fakeSource{})

	then := time.Now()
	_, _ = eval.Eval(context.Background(), "some logql query...", time.Now())
	since := time.Since(then)

	require.GreaterOrEqual(t, since.Nanoseconds(), fixedRandNum)
}

func TestEvaluationWithNoJitter(t *testing.T) {
	const jitter = 0

	inner := mockEval{}
	eval := NewEvaluatorWithJitter(inner, jitter, fakeSource{})

	// return the inner evaluator if jitter is disabled
	require.Exactly(t, inner, eval)
}
