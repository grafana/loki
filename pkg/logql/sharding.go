package logql

import (
	"context"
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/astmapper"
	"github.com/grafana/loki/pkg/iter"
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

// Downstreamer is an interface for deferring responsibility for query execution.
// It is decoupled from but consumed by a downStreamEvaluator to dispatch ASTs.
type Downstreamer interface {
	Downstream(Expr, Params, *astmapper.ShardAnnotation) Query
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
		qry := ev.Downstream(e.SampleExpr, params, e.shard)
		res, err := qry.Exec(ctx)
		if err != nil {
			return nil, err
		}
		return ResultStepEvaluator(res, params)

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
		qry := ev.Downstream(e.LogSelectorExpr, params, e.shard)
		res, err := qry.Exec(ctx)
		if err != nil {
			return nil, err
		}
		return ResultIterator(res, params)

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

	default:
		return nil, EvaluatorUnsupportedType(expr, ev)
	}
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

// ResultStepEvaluator coerces a downstream vector or matrix into a StepEvaluator
func ResultStepEvaluator(res Result, params Params) (StepEvaluator, error) {
	var (
		end       = params.End()
		step      = params.Step()
		ts        = params.Start()
		increment = func() {
			ts = ts.Add(step)
		}
	)

	switch data := res.Data.(type) {
	case promql.Vector:
		var exhausted bool
		return newStepEvaluator(func() (bool, int64, promql.Vector) {
			if !exhausted {
				exhausted = true
				return true, ts.UnixNano() / int64(time.Millisecond), data
			}
			return false, 0, nil
		}, nil)
	case promql.Matrix:
		var i int
		var maxLn int
		if len(data) > 0 {
			maxLn = len(data[0].Points)
		}
		return newStepEvaluator(func() (bool, int64, promql.Vector) {
			defer increment()
			if ts.After(end) {
				return false, 0, nil
			}

			tsInt := ts.UnixNano() / int64(time.Millisecond)

			// Ensure that the resulting StepEvaluator maintains
			// the same shape that the parameters expect. For example,
			// it's possible that a downstream query returns matches no
			// log streams and thus returns an empty matrix.
			// However, we still need to ensure that it can be merged effectively
			// with another leg that may match series.
			// Therefore, we determine our steps from the parameters
			// and not the underlying Matrix.
			if i >= maxLn {
				return true, tsInt, nil
			}

			vec := make(promql.Vector, 0, len(data))
			for j := 0; j < len(data); j++ {
				series := data[j]
				vec = append(vec, promql.Sample{
					Point:  series.Points[i],
					Metric: series.Metric,
				})
			}
			i++
			return true, tsInt, vec
		}, nil)
	default:
		return nil, fmt.Errorf("unexpected type (%s) uncoercible to StepEvaluator", data.Type())
	}
}

// ResultIterator coerces a downstream streams result into an iter.EntryIterator
func ResultIterator(res Result, params Params) (iter.EntryIterator, error) {
	streams, ok := res.Data.(Streams)
	if !ok {
		return nil, fmt.Errorf("Unexpected type (%s) for ResultIterator; expected %s", res.Data.Type(), ValueTypeStreams)
	}
	return iter.NewStreamsIterator(context.Background(), streams, params.Direction()), nil

}
