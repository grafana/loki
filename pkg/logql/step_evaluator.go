package logql

import (
	"errors"

	"github.com/prometheus/prometheus/promql"
)

// StepEvaluator evaluate a single step of a query.
type StepEvaluator interface {
	// while Next returns a promql.Value, the only acceptable types are Scalar and Vector.
	Next() (ok bool, ts int64, vec promql.Vector)
	// Close all resources used.
	Close() error
	// Reports any error
	Error() error

	Type() T
}

type stepEvaluator struct {
	fn    func() (bool, int64, promql.Vector)
	close func() error
	err   func() error
	t     T
}

func NewStepEvaluator(fn func() (bool, int64, promql.Vector), closeFn func() error, err func() error) (StepEvaluator, error) {
	if fn == nil {
		return nil, errors.New("nil step evaluator fn")
	}

	if closeFn == nil {
		closeFn = func() error { return nil }
	}

	if err == nil {
		err = func() error { return nil }
	}
	return &stepEvaluator{
		fn:    fn,
		close: closeFn,
		err:   err,
	}, nil
}

func (e *stepEvaluator) Type() T {
	return e.t
}

func (e *stepEvaluator) Next() (bool, int64, promql.Vector) {
	return e.fn()
}

func (e *stepEvaluator) Close() error {
	return e.close()
}

func (e *stepEvaluator) Error() error {
	return e.err()
}
