package logql

import (
	"github.com/prometheus/prometheus/promql"
)

type StepResult interface {
	promql.Vector
}

// StepEvaluator evaluate a single step of a query.
type StepEvaluator[T StepResult] interface {
	// while Next returns a promql.Value, the only acceptable types are Scalar and Vector.
	Next() (ok bool, ts int64, r T)
	// Close all resources used.
	Close() error
	// Reports any error
	Error() error
	// Explain returns a print of the step evaluation tree
	Explain(Node)
}
