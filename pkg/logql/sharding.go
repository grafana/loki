package logql

import (
	"context"
	"fmt"

	"github.com/cortexproject/cortex/pkg/querier/astmapper"
	"github.com/grafana/loki/pkg/iter"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/promql"
)

// DownstreamSampleExpr is a SampleExpr which signals downstream computation
type DownstreamSampleExpr struct {
	shard *astmapper.ShardAnnotation
	SampleExpr
}

func (d DownstreamSampleExpr) String() string {
	return fmt.Sprintf("downstream<%s, shard=%s>", d.SampleExpr.String(), d.shard)
}

// DownstreamLogSelectorExpr is a LogSelectorExpr which signals downstream computation
type DownstreamLogSelectorExpr struct {
	shard *astmapper.ShardAnnotation
	LogSelectorExpr
}

func (d DownstreamLogSelectorExpr) String() string {
	return fmt.Sprintf("downstream<%s, shard=%s>", d.LogSelectorExpr.String(), d.shard)
}

// ConcatSampleExpr is an expr for concatenating multiple SampleExpr
// Contract: The embedded SampleExprs within a linked list of ConcatSampleExprs must be of the
// same structure. This makes special implementations of SampleExpr.Associative() unnecessary.
type ConcatSampleExpr struct {
	SampleExpr
	next *ConcatSampleExpr
}

func (c ConcatSampleExpr) String() string {
	if c.next == nil {
		return c.SampleExpr.String()
	}

	return fmt.Sprintf("%s ++ %s", c.SampleExpr.String(), c.next.String())
}

// ConcatLogSelectorExpr is an expr for concatenating multiple LogSelectorExpr
type ConcatLogSelectorExpr struct {
	LogSelectorExpr
	next *ConcatLogSelectorExpr
}

func (c ConcatLogSelectorExpr) String() string {
	if c.next == nil {
		return c.LogSelectorExpr.String()
	}

	return fmt.Sprintf("%s ++ %s", c.LogSelectorExpr.String(), c.next.String())
}

// downstreamEvaluator is an evaluator which handles shard aware AST nodes
type downstreamEvaluator struct{ Downstreamer }

// Evaluator returns a StepEvaluator for a given SampleExpr
func (ev *downstreamEvaluator) StepEvaluator(
	ctx context.Context,
	nextEv Evaluator,
	expr SampleExpr,
	params Params,
) (StepEvaluator, error) {
	switch e := expr.(type) {
	case DownstreamSampleExpr:
		// downstream to a querier
		return nil, errors.New("unimplemented")

	case ConcatSampleExpr:
		// ensure they all impl the same (SampleExpr, LogSelectorExpr) & concat
		var xs []StepEvaluator
		cur := &e

		for cur != nil {
			eval, err := ev.StepEvaluator(ctx, nextEv, cur.SampleExpr, params)
			if err != nil {
				// Close previously opened StepEvaluators
				for _, x := range xs {
					x.Close()
				}
				return nil, err
			}
			xs = append(xs, eval)
			cur = cur.next
		}

		return ConcatEvaluator(xs)

	default:
		return nil, EvaluatorUnsupportedType(expr, ev)
	}
}

// Iterator returns the iter.EntryIterator for a given LogSelectorExpr
func (ev *downstreamEvaluator) Iterator(
	ctx context.Context,
	expr LogSelectorExpr,
	params Params,
) (iter.EntryIterator, error) {
	switch e := expr.(type) {
	case DownstreamLogSelectorExpr:
		// downstream to a querier
		return nil, errors.New("unimplemented")
	case ConcatLogSelectorExpr:
		var iters []iter.EntryIterator
		cur := &e
		for cur != nil {
			iterator, err := ev.Iterator(ctx, e, params)
			if err != nil {
				// Close previously opened StepEvaluators
				for _, x := range iters {
					x.Close()
				}
				return nil, err
			}
			iters = append(iters, iterator)
		}
		return iter.NewHeapIterator(ctx, iters, params.Direction()), nil
	}
	return nil, errors.New("unimplemented")
}

// ConcatEvaluator joins multiple StepEvaluators.
// Contract: They must be of identical start, end, and step values.
func ConcatEvaluator(evaluators []StepEvaluator) (StepEvaluator, error) {
	return newStepEvaluator(
		func() (done bool, ts int64, vec promql.Vector) {
			var cur promql.Vector
			for _, eval := range evaluators {
				done, ts, cur = eval.Next()
				vec = append(vec, cur...)
			}
			return done, ts, vec

		},
		func() (lastErr error) {
			for _, eval := range evaluators {
				if err := eval.Close(); err != nil {
					lastErr = err
				}
			}
			return lastErr
		},
	)
}
