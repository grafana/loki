package logql

import (
	"errors"

)

// StepEvaluator evaluate a single step of a query.
type StepEvaluator[V any] interface {
	// while Next returns a promql.Value, the only acceptable types are Scalar and Vector.
	Next() (ok bool, ts int64, vec V)
	// Close all resources used.
	Close() error
	// Reports any error
	Error() error
}

type stepEvaluator[V any] struct {
	fn    func() (bool, int64, V)
	close func() error
	err   func() error
}

func NewStepEvaluator[V any](fn func() (bool, int64, V), closeFn func() error, err func() error) (StepEvaluator[V], error) {
	if fn == nil {
		return nil, errors.New("nil step evaluator fn")
	}

	if closeFn == nil {
		closeFn = func() error { return nil }
	}

	if err == nil {
		err = func() error { return nil }
	}
	return &stepEvaluator[V]{
		fn:    fn,
		close: closeFn,
		err:   err,
	}, nil
}

func (e *stepEvaluator[V]) Next() (bool, int64, V) {
	return e.fn()
}

func (e *stepEvaluator[V]) Close() error {
	return e.close()
}

func (e *stepEvaluator[V]) Error() error {
	return e.err()
}
