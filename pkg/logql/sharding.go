package logql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/astmapper"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logql/stats"
)

/*
This includes a bunch of tooling for parallelization improvements based on backend shard factors. In schemas 10+ a shard factor (default 16) is introduced in the index store, calculated by hashing the label set of a log stream. This allows us to perform certain optimizations that fall under the umbrella of query remapping and querying shards individually. For instance, `{app="foo"} |= "bar"` can be executed on each shard independently, then reaggregated. There are also a class of optimizations that can be performed by altering a query into a functionally equivalent, but more parallelizable form. For instance, an average can be remapped into a sum/count, which can then take advantage of our sharded execution model.
*/

// ShardedEngine is an Engine implementation that can split queries into more parallelizable forms via
// querying the underlying backend shards individually and reaggregating them.
type ShardedEngine struct {
	timeout   time.Duration
	evaluator Evaluator
	metrics   *ShardingMetrics
}

// NewShardedEngine constructs a *ShardedEngine
func NewShardedEngine(opts EngineOpts, downstreamer Downstreamer, metrics *ShardingMetrics) *ShardedEngine {
	opts.applyDefault()
	return &ShardedEngine{
		timeout:   opts.Timeout,
		evaluator: NewDownstreamEvaluator(downstreamer),
		metrics:   metrics,
	}

}

// Query constructs a Query
func (ng *ShardedEngine) Query(p Params, shards int) Query {
	return &query{
		timeout:   ng.timeout,
		params:    p,
		evaluator: ng.evaluator,
		parse: func(ctx context.Context, query string) (Expr, error) {
			logger := spanlogger.FromContext(ctx)
			mapper, err := NewShardMapper(shards, ng.metrics)
			if err != nil {
				return nil, err
			}
			noop, parsed, err := mapper.Parse(query)
			if err != nil {
				level.Warn(logger).Log("msg", "failed mapping AST", "err", err.Error(), "query", query)
				return nil, err
			}

			level.Debug(logger).Log("no-op", noop, "mapped", parsed.String())
			return parsed, nil
		},
	}
}

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

type Shards []astmapper.ShardAnnotation

func (xs Shards) Encode() (encoded []string) {
	for _, shard := range xs {
		encoded = append(encoded, shard.String())
	}

	return encoded
}

// ParseShards parses a list of string encoded shards
func ParseShards(strs []string) (Shards, error) {
	if len(strs) == 0 {
		return nil, nil
	}
	shards := make([]astmapper.ShardAnnotation, 0, len(strs))

	for _, str := range strs {
		shard, err := astmapper.ParseShard(str)
		if err != nil {
			return nil, err
		}
		shards = append(shards, shard)
	}
	return shards, nil
}

// Downstreamer is an interface for deferring responsibility for query execution.
// It is decoupled from but consumed by a downStreamEvaluator to dispatch ASTs.
type Downstreamer interface {
	Downstream(Expr, Params, Shards) (Query, error)
}

// DownstreamEvaluator is an evaluator which handles shard aware AST nodes
type DownstreamEvaluator struct {
	Downstreamer
	defaultEvaluator *DefaultEvaluator
}

// Exec runs a query and collects stats from the embedded Downstreamer
func (ev DownstreamEvaluator) Exec(ctx context.Context, expr Expr, p Params, shards Shards) (Result, error) {
	qry, err := ev.Downstream(expr, p, shards)
	if err != nil {
		return Result{}, err
	}

	res, err := qry.Exec(ctx)
	if err != nil {
		return Result{}, err
	}

	err = stats.JoinResults(ctx, res.Statistics)
	if err != nil {
		level.Warn(util.Logger).Log("msg", "unable to merge downstream results", "err", err)
	}

	return res, nil

}

func NewDownstreamEvaluator(downstreamer Downstreamer) *DownstreamEvaluator {
	return &DownstreamEvaluator{
		Downstreamer: downstreamer,
		defaultEvaluator: NewDefaultEvaluator(
			QuerierFunc(func(_ context.Context, p SelectParams) (iter.EntryIterator, error) {
				// TODO(owen-d): add metric here, this should never happen.
				return nil, errors.New("Unimplemented")
			}),
			0,
		),
	}
}

// Evaluator returns a StepEvaluator for a given SampleExpr
func (ev *DownstreamEvaluator) StepEvaluator(
	ctx context.Context,
	nextEv Evaluator,
	expr SampleExpr,
	params Params,
) (StepEvaluator, error) {
	switch e := expr.(type) {

	case DownstreamSampleExpr:
		// downstream to a querier
		var shards []astmapper.ShardAnnotation
		if e.shard != nil {
			shards = append(shards, *e.shard)
		}
		res, err := ev.Exec(ctx, e.SampleExpr, params, shards)
		if err != nil {
			return nil, err
		}
		return ResultStepEvaluator(res, params)

	case *ConcatSampleExpr:
		type result struct {
			stepper StepEvaluator
			err     error
		}
		ctx, cancel := context.WithCancel(ctx)
		cur := e
		ch := make(chan result)
		done := make(chan struct{})
		count := 0

		for cur != nil {
			go func(expr SampleExpr) {
				eval, err := ev.StepEvaluator(ctx, nextEv, expr, params)
				if err != nil {
					level.Warn(util.Logger).Log("msg", "could not extract StepEvaluator", "err", err, "expr", expr.String())
				}
				select {
				case <-done:
				case ch <- result{eval, err}:
				}
			}(cur.SampleExpr)
			cur = cur.next
			count++
		}

		xs := make([]StepEvaluator, 0, count)
		cleanup := func() {
			cancel()           // cancel ctx
			done <- struct{}{} // send done signal to awaiting goroutines
			// Close previously opened StepEvaluators
			for _, x := range xs {
				x.Close() // close unused StepEvaluators
			}
		}

		for i := 0; i < count; i++ {
			select {
			case <-ctx.Done():
				defer cleanup()
				return nil, ctx.Err()
			case res := <-ch:
				if res.err != nil {
					defer cleanup()
					return nil, res.err
				}
				xs = append(xs, res.stepper)
			}
		}

		return ConcatEvaluator(xs)

	default:
		return ev.defaultEvaluator.StepEvaluator(ctx, nextEv, e, params)
	}
}

// Iterator returns the iter.EntryIterator for a given LogSelectorExpr
func (ev *DownstreamEvaluator) Iterator(
	ctx context.Context,
	expr LogSelectorExpr,
	params Params,
) (iter.EntryIterator, error) {
	switch e := expr.(type) {
	case DownstreamLogSelectorExpr:
		// downstream to a querier
		var shards Shards
		if e.shard != nil {
			shards = append(shards, *e.shard)
		}
		res, err := ev.Exec(ctx, e.LogSelectorExpr, params, shards)
		if err != nil {
			return nil, err
		}

		return ResultIterator(res, params)

	case *ConcatLogSelectorExpr:
		type result struct {
			iterator iter.EntryIterator
			err      error
		}
		ctx, cancel := context.WithCancel(ctx)
		cur := e
		ch := make(chan result)
		done := make(chan struct{})
		count := 0

		for cur != nil {
			go func(expr LogSelectorExpr) {
				iterator, err := ev.Iterator(ctx, expr, params)
				if err != nil {
					level.Warn(util.Logger).Log("msg", "could not extract Iterator", "err", err, "expr", expr.String())
				}
				select {
				case <-done:
				case ch <- result{iterator, err}:
				}
			}(cur.LogSelectorExpr)
			cur = cur.next
			count++
		}

		xs := make([]iter.EntryIterator, 0, count)

		cleanup := func() {
			cancel()           // cancel ctx
			done <- struct{}{} // send done signal to awaiting goroutines
			// Close previously opened Iterators
			for _, x := range xs {
				x.Close() // close unused Iterators
			}
		}

		for i := 0; i < count; i++ {
			select {
			case <-ctx.Done():
				defer cleanup()
				return nil, ctx.Err()
			case res := <-ch:
				if res.err != nil {
					defer cleanup()
					return nil, res.err
				}
				xs = append(xs, res.iterator)
			}
		}

		return iter.NewHeapIterator(ctx, xs, params.Direction()), nil

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
		start = params.Start()
		end   = params.End()
		step  = params.Step()
	)

	switch data := res.Data.(type) {
	case promql.Vector:
		var exhausted bool
		return newStepEvaluator(func() (bool, int64, promql.Vector) {
			if !exhausted {
				exhausted = true
				return true, start.UnixNano() / int64(time.Millisecond), data
			}
			return false, 0, nil
		}, nil)
	case promql.Matrix:
		return NewMatrixStepper(start, end, step, data), nil
	default:
		return nil, fmt.Errorf("unexpected type (%s) uncoercible to StepEvaluator", data.Type())
	}
}

// ResultIterator coerces a downstream streams result into an iter.EntryIterator
func ResultIterator(res Result, params Params) (iter.EntryIterator, error) {
	streams, ok := res.Data.(Streams)
	if !ok {
		return nil, fmt.Errorf("unexpected type (%s) for ResultIterator; expected %s", res.Data.Type(), ValueTypeStreams)
	}
	return iter.NewStreamsIterator(context.Background(), streams, params.Direction()), nil

}
