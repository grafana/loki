package ruler

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/grafana/loki/pkg/logqlmodel"
)

// EvaluatorWithJitter wraps a given Evaluator. It applies a randomly-generated jitter (sleep) before each evaluation to
// protect against thundering-herd scenarios where multiple rules are evaluated at the same time.
type EvaluatorWithJitter struct {
	inner     Evaluator
	maxJitter time.Duration
	rng       *rand.Rand
}

func NewEvaluatorWithJitter(inner Evaluator, maxJitter time.Duration, rngSource rand.Source) *EvaluatorWithJitter {
	return &EvaluatorWithJitter{
		inner:     inner,
		maxJitter: maxJitter,
		rng:       rand.New(rngSource),
	}
}

func (e *EvaluatorWithJitter) Eval(ctx context.Context, qs string, now time.Time) (*logqlmodel.Result, error) {
	if e.inner == nil {
		return nil, fmt.Errorf("no inner evaluator defined")
	}

	if e.maxJitter > 0 {
		jitter := time.Duration(e.rng.Int63n(e.maxJitter.Nanoseconds()))
		time.Sleep(jitter)
	}

	return e.inner.Eval(ctx, qs, now)
}
