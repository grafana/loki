package executor

import (
	"context"
	"errors"
	"fmt"
	"iter"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

// TODO(rfratto): document constraints on how to properly write process
// methods; particularly around guaranteeing that no Record leaks:
//
// * Each process method iterator must yield an error OR generate and yield at
//   most one Record. Iterators may not generate multiple Records in advance.
//
// * Each iterator consumer must Release() the Record after it is done with it.
//
// It is safe for iterators to stop iterating early if yield returns false, as
// future Records won't have been generated yet.

type evaluator struct {
	opts Options
	plan *physical.Plan
	mem  memory.Allocator
}

func newEvaluator(opts Options, plan *physical.Plan) *evaluator {
	return &evaluator{opts: opts, plan: plan}
}

func (e *evaluator) processNode(ctx context.Context, n physical.Node) iter.Seq[Result] {
	children := e.plan.Children(n)

	iters := make([]iter.Seq[Result], len(children))
	for i, child := range children {
		iters[i] = e.processNode(ctx, child)
	}

	return func(yield func(Result) bool) {
		switch n := n.(type) {
		case *physical.DataObjScan:
			e.processDataObjScan(n, iters)(yield)
		case *physical.Limit:
			e.processLimit(n, iters)(yield)
		default:
			yield(Result{err: fmt.Errorf("unsupported node type %T", n)})
		}
	}
}

func (e *evaluator) processDataObjScan(n *physical.DataObjScan, input []iter.Seq[Result]) iter.Seq[Result] {
	return func(yield func(Result) bool) {
		if len(input) > 0 {
			yield(Result{err: errors.New("DataObjScan should not have any inputs")})
			return
		}

		// TODO(rfratto): read from storage
		_ = n
	}
}

func (e *evaluator) processLimit(n *physical.Limit, input []iter.Seq[Result]) iter.Seq[Result] {
	return func(yield func(Result) bool) {
		if len(input) != 1 {
			yield(errorResult(errors.New("limit nodes must have exactly one input")))
			return
		}

		// We gradually reduce offset and limit as we process more records, as the
		// offset and limit may cross record boundaries.
		var (
			offset = int64(n.Offset)
			limit  = int64(n.Limit)
		)

		for r := range input[0] {
			rec, err := r.Value()
			if err != nil {
				yield(errorResult(fmt.Errorf("error reading record: %w", err)))
				return
			}

			// We want to slice rec so it only contains the rows we're looking for
			// accounting for both the limit and offset.
			//
			// We constrain the start and end to be within the bounds of the record.
			var (
				start = min(offset, rec.NumRows())
				end   = min(start+limit, rec.NumRows())

				length = end - start
			)

			offset -= start
			limit -= length

			// Short circuit: if we have no rows to yield from this record, we can
			// avoid yielding anything.
			stop := length > 0 && !yield(recordResult(rec.NewSlice(start, end)))
			rec.Release()

			if stop || limit <= 0 {
				return
			}
		}
	}
}
