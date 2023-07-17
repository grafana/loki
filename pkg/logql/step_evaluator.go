package logql

import (
	"errors"

	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/logql/sketch"
)

type T int64

const (
	VecType T = iota
	TopKVecType
)

type StepResult interface {
	Type() T

	SampleVector() promql.Vector
	TopkVector() []sketch.Topk
}

type SampleVector promql.Vector

func (SampleVector) Type() T {
	return VecType
}

func (s SampleVector) SampleVector() promql.Vector {
	return promql.Vector(s)
}

func (s SampleVector) TopkVector() []sketch.Topk {
	return nil
}

type TopKVector []sketch.Topk

func (TopKVector) Type() T {
	return TopKVecType
}

func (v TopKVector) SampleVector() promql.Vector {
	return nil
}

func (v TopKVector) TopkVector() []sketch.Topk {
	return v
}

// StepEvaluator evaluate a single step of a query.
type StepEvaluator interface {
	// while Next returns a promql.Value, the only acceptable types are Scalar and Vector.
	Next() (ok bool, ts int64, r StepResult)
	// Close all resources used.
	Close() error
	// Reports any error
	Error() error

	Type() T
}

type stepEvaluator struct {
	fn    func() (bool, int64, StepResult)
	close func() error
	err   func() error
	t     T
}

func NewStepEvaluator(fn func() (bool, int64, StepResult), closeFn func() error, err func() error) (StepEvaluator, error) {
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

func (e *stepEvaluator) Next() (bool, int64, StepResult) {
	return e.fn()
}

func (e *stepEvaluator) Close() error {
	return e.close()
}

func (e *stepEvaluator) Error() error {
	return e.err()
}
