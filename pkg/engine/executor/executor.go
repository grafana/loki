// Package executor holds logic to execte physical plans.
package executor

import (
	"context"
	"iter"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

// Options control the behavior of a call to [Run], including with information
// for how to read from storage.
type Options struct {
	// TODO(rfratto): storage client
	// TODO(rfratto): trace
}

// Run executes a physical plan from a specified [physical.Node]. Run returns
// an iterator of [Result] instances, which hold either an [arrow.Record] or an
// error.
//
// Execution only begins once the iterator is being read, and iteration will
// continue until all results are generated, the provided context is cancelled,
// or an error occurs.
func Run(ctx context.Context, opts Options, plan *physical.Plan, root physical.Node) iter.Seq[Result] {
	eval := newEvaluator(opts, plan)
	return eval.processNode(ctx, root)
}

// Result denotes a single result from the executor. A result can either be an
// [arrow.Record], or an error.
type Result struct {
	val arrow.Record
	err error
}

func recordResult(r arrow.Record) Result { return Result{val: r} }
func errorResult(err error) Result       { return Result{err: err} }

// Value returns the record of the Result, or an error if the Result represents
// an error.
func (r Result) Value() (arrow.Record, error) { return r.val, r.err }
