package logql

import (
	"context"
	"errors"

	"github.com/grafana/loki/pkg/iter"
	"github.com/prometheus/prometheus/pkg/labels"
)

type ShardMapper struct {
	shards int
}

func (m ShardMapper) Map(expr Expr) (Expr, error) {
	cloned, err := CloneExpr(expr)
	if err != nil {
		return nil, err
	}

	if CanParallel(cloned) {
		return m.parallelize(cloned)
	}

	switch e := cloned.(type) {
	case *rangeAggregationExpr:
		mapped, err := m.Map(e.left.left)
		if err != nil {
			return nil, err
		}
		e.left.left = mapped.(LogSelectorExpr)
		return e, nil
	case *vectorAggregationExpr:
		mapped, err := m.Map(e.left)
		if err != nil {
			return nil, err
		}
		e.left = mapped.(SampleExpr)
		return e, nil
	default:
		return nil, errors.Errorf("unexpected expr marked as not parallelizable: %+v", expr)
	}
}

// func (m ShardMapper) parallelize(expr Expr) (Expr, error) {
// 	switch e := expr.(type) {
// 	case *matchersExpr:
// 	case *filterExpr:
// 	case *rangeAggregationExpr:
// 	case *vectorAggregationExpr:
// 	default:
// 		return nil, errors.Errorf("unexpected expr: %+v", expr)
// 	}
// }

// DownstreamExpr impls both LogSelectorExpr and SampleExpr in order to transparently
// wrap an expr and signal that it should be executed on a downstream querier.
type DownstreamExpr struct {
	shard *int
	Expr
}

func (e DownstreamExpr) Selector() LogSelectorExpr {
	return e.Expr.(SampleExpr).Selector()
}

func (e DownstreamExpr) Filter() (Filter, error) {
	return e.Expr.(LogSelectorExpr).Filter()
}

func (e DownstreamExpr) Matchers() []*labels.Matcher {
	return e.Expr.(LogSelectorExpr).Matchers()
}

// ConcatSampleExpr is a sample expr which is used to signal a list of
// SampleExprs which should be joined
type ConcatSampleExpr struct {
	SampleExpr
	next *ConcatSampleExpr
}

// ConcatLogSelectorExpr is a sample expr which is used to signal a list of
// LogSelectorExprs which should be joined
type ConcatLogSelectorExpr struct {
	LogSelectorExpr
	next *ConcatLogSelectorExpr
}

// shardedEvaluator is an evaluator which handles shard aware AST nodes
// and embeds a default evaluator otherwise
type shardedEvaluator struct {
	shards    int
	evaluator *defaultEvaluator
}

// Evaluator returns a StepEvaluator for a given SampleExpr
func (ev *shardedEvaluator) Evaluator(
	ctx context.Context,
	expr SampleExpr,
	params Params,
) (StepEvaluator, error) {
	switch e := expr.(type) {
	case DownstreamExpr:
		// TODO(owen): downstream this and present as StepEvaluator
		return nil, errors.New("unimplemented")
	case ConcatSampleExpr:
		var evaluators []StepEvaluator
		for {
			eval, err := ev.Evaluator(ctx, e.SampleExpr, params)
			if err != nil {
				return nil, err
			}
			evaluators = append(evaluators, eval)
			if e.next != nil {
				break
			}
			e = *e.next
		}
		return ConcatEvaluator(evaluators)
	default:
		return ev.evaluator.Evaluator(ctx, expr, params)
	}
}

// Iterator returns the iter.EntryIterator for a given LogSelectorExpr
func (ev *shardedEvaluator) Iterator(
	ctx context.Context,
	expr LogSelectorExpr,
	params Params,
) (iter.EntryIterator, error) {
	switch e := expr.(type) {
	case DownstreamExpr:
		// TODO(owen): downstream this and present as iter.EntryIterator
		return nil, errors.New("unimplemented")
	case ConcatLogSelectorExpr:
		var iters []iter.EntryIterator

		for {
			iter, err := ev.Iterator(ctx, e.LogSelectorExpr, params)
			// TODO(owen): close these iters?
			if err != nil {
				return nil, err
			}
			iters = append(iters, iter)
			if e.next == nil {
				break
			}
			e = *e.next
		}
		return iter.NewHeapIterator(ctx, iters, params.Direction())
	default:
		return nil, errors.Errorf("unexpected type (%T): %v", e, e)
	}
}

/*
map :: AST -> AST


*/
