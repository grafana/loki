package logql

import (
	"errors"

	"github.com/prometheus/prometheus/promql"
)

// StepEvaluator evaluate a single step of a query.
type StepEvaluator interface {
	// while Next returns a promql.Value, the only acceptable types are Scalar and Vector.
	Next() (bool, int64, promql.Vector)
	// Close all resources used.
	Close() error
}

type stepEvaluator struct {
	fn    func() (bool, int64, promql.Vector)
	close func() error
}

func newStepEvaluator(fn func() (bool, int64, promql.Vector), close func() error) (StepEvaluator, error) {
	if fn == nil {
		return nil, errors.New("nil step evaluator fn")
	}

	if close == nil {
		close = func() error { return nil }
	}

	return &stepEvaluator{
		fn:    fn,
		close: close,
	}, nil
}

func (e *stepEvaluator) Next() (bool, int64, promql.Vector) {
	return e.fn()
}

func (e *stepEvaluator) Close() error {
	return e.close()
}
