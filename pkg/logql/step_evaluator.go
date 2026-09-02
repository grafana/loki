package logql

import (
	"time"

	"github.com/prometheus/prometheus/promql"
)

type StepResult interface {
	SampleVector() promql.Vector
	QuantileSketchVec() ProbabilisticQuantileVector
	CountMinSketchVec() CountMinSketchVector
	CountDistinctSketchVec() CountDistinctSketchVector
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

func (SampleVector) CountDistinctSketchVec() CountDistinctSketchVector {
	return CountDistinctSketchVector{}
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

// SketchMatrixStepEvaluator steps through a matrix of sketch vectors, one
// evaluation timestamp at a time. Quantile and count-distinct sketches share
// this stepper and differ only in the vector type stored in the matrix.
type SketchMatrixStepEvaluator[V StepResult] struct {
	end, ts time.Time
	step    time.Duration
	m       []V
	name    string
}

func newSketchMatrixStepEvaluator[V StepResult](m []V, params Params, name string) *SketchMatrixStepEvaluator[V] {
	return &SketchMatrixStepEvaluator[V]{
		end:  params.End(),
		ts:   params.Start().Add(-params.Step()), // corrected on first Next()
		step: params.Step(),
		m:    m,
		name: name,
	}
}

func (m *SketchMatrixStepEvaluator[V]) Next() (bool, int64, StepResult) {
	m.ts = m.ts.Add(m.step)
	if m.ts.After(m.end) {
		return false, 0, nil
	}

	ts := m.ts.UnixNano() / int64(time.Millisecond)
	if len(m.m) == 0 {
		return false, 0, nil
	}

	vec := m.m[0]
	m.m = m.m[1:]
	return true, ts, vec
}

func (*SketchMatrixStepEvaluator[_]) Close() error { return nil }

func (*SketchMatrixStepEvaluator[_]) Error() error { return nil }

func (m *SketchMatrixStepEvaluator[_]) Explain(parent Node) {
	parent.Child(m.name)
}
