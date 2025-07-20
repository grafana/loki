package logql

import (
	"github.com/prometheus/prometheus/promql"
)

type StepResult interface {
	SampleVector() promql.Vector
	QuantileSketchVec() ProbabilisticQuantileVector
	CountMinSketchVec() CountMinSketchVector
}

type SampleVector promql.Vector

var _ StepResult = SampleVector{}

func (p SampleVector) SampleVector() promql.Vector {
	return promql.Vector(p)
}

func (p SampleVector) QuantileSketchVec() ProbabilisticQuantileVector {
	return ProbabilisticQuantileVector{}
}

func (SampleVector) CountMinSketchVec() CountMinSketchVector {
	return CountMinSketchVector{}
}

// StepEvaluator evaluate a single step of a query.
type StepEvaluator interface {
	// while Next returns a promql.Value, the only acceptable types are Scalar and Vector.
	Next() (ok bool, ts int64, r StepResult)
	// Close all resources used.
	Close() error
	// Reports any error
	Error() error
	// Explain returns a print of the step evaluation tree
	Explain(Node)
}

type EmptyEvaluator[R StepResult] struct {
	value R
}

var _ StepEvaluator = EmptyEvaluator[SampleVector]{}

// Close implements StepEvaluator.
func (EmptyEvaluator[_]) Close() error { return nil }

// Error implements StepEvaluator.
func (EmptyEvaluator[_]) Error() error { return nil }

// Next implements StepEvaluator.
func (e EmptyEvaluator[_]) Next() (ok bool, ts int64, r StepResult) {
	return false, 0, e.value
}
