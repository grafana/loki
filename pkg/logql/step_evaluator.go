package logql

import (
	"github.com/prometheus/prometheus/promql"
)

type StepResult interface {
	PromVec() promql.Vector
}

type PromVec promql.Vector

var _ StepResult = PromVec{}

func (p PromVec) PromVec() promql.Vector {
	return promql.Vector(p)
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
