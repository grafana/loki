package logql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/astmapper"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logql/stats"
)

/*
This includes a bunch of tooling for parallelization improvements based on backend shard factors.
In schemas 10+ a shard factor (default 16) is introduced in the index store,
calculated by hashing the label set of a log stream. This allows us to perform certain optimizations
that fall under the umbrella of query remapping and querying shards individually.
For instance, `{app="foo"} |= "bar"` can be executed on each shard independently, then reaggregated.
There are also a class of optimizations that can be performed by altering a query into a functionally equivalent,
but more parallelizable form. For instance, an average can be remapped into a sum/count,
which can then take advantage of our sharded execution model.
*/

// ShardedEngine is an Engine implementation that can split queries into more parallelizable forms via
// querying the underlying backend shards individually and reaggregating them.
type ShardedEngine struct {
	timeout        time.Duration
	downstreamable Downstreamable
	limits         Limits
	metrics        *ShardingMetrics
}

// NewShardedEngine constructs a *ShardedEngine
func NewShardedEngine(opts EngineOpts, downstreamable Downstreamable, metrics *ShardingMetrics, limits Limits) *ShardedEngine {
	opts.applyDefault()
	return &ShardedEngine{
		timeout:        opts.Timeout,
		downstreamable: downstreamable,
		metrics:        metrics,
		limits:         limits,
	}

}

// Query constructs a Query
func (ng *ShardedEngine) Query(p Params, mapped Expr) Query {
	return &query{
		timeout:   ng.timeout,
		params:    p,
		evaluator: NewDownstreamEvaluator(ng.downstreamable.Downstreamer()),
		parse: func(_ context.Context, _ string) (Expr, error) {
			return mapped, nil
		},
		limits: ng.limits,
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
	DownstreamSampleExpr
	next *ConcatSampleExpr
}

func (c ConcatSampleExpr) String() string {
	if c.next == nil {
		return c.DownstreamSampleExpr.String()
	}

	return fmt.Sprintf("%s ++ %s", c.DownstreamSampleExpr.String(), c.next.String())
}

// ConcatLogSelectorExpr is an expr for concatenating multiple LogSelectorExpr
type ConcatLogSelectorExpr struct {
	DownstreamLogSelectorExpr
	next *ConcatLogSelectorExpr
}

func (c ConcatLogSelectorExpr) String() string {
	if c.next == nil {
		return c.DownstreamLogSelectorExpr.String()
	}

	return fmt.Sprintf("%s ++ %s", c.DownstreamLogSelectorExpr.String(), c.next.String())
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

type Downstreamable interface {
	Downstreamer() Downstreamer
}

type DownstreamQuery struct {
	Expr   Expr
	Params Params
	Shards Shards
}

// Downstreamer is an interface for deferring responsibility for query execution.
// It is decoupled from but consumed by a downStreamEvaluator to dispatch ASTs.
type Downstreamer interface {
	Downstream(context.Context, []DownstreamQuery) ([]Result, error)
}

// DownstreamEvaluator is an evaluator which handles shard aware AST nodes
type DownstreamEvaluator struct {
	Downstreamer
	defaultEvaluator Evaluator
}

// Downstream runs queries and collects stats from the embedded Downstreamer
func (ev DownstreamEvaluator) Downstream(ctx context.Context, queries []DownstreamQuery) ([]Result, error) {
	results, err := ev.Downstreamer.Downstream(ctx, queries)
	if err != nil {
		return nil, err
	}

	for _, res := range results {
		if err := stats.JoinResults(ctx, res.Statistics); err != nil {
			level.Warn(util.Logger).Log("msg", "unable to merge downstream results", "err", err)
		}
	}

	return results, nil

}

type errorQuerier struct{}

func (errorQuerier) SelectLogs(ctx context.Context, p SelectLogParams) (iter.EntryIterator, error) {
	return nil, errors.New("Unimplemented")
}
func (errorQuerier) SelectSamples(ctx context.Context, p SelectSampleParams) (iter.SampleIterator, error) {
	return nil, errors.New("Unimplemented")
}

func NewDownstreamEvaluator(downstreamer Downstreamer) *DownstreamEvaluator {
	return &DownstreamEvaluator{
		Downstreamer:     downstreamer,
		defaultEvaluator: NewDefaultEvaluator(&errorQuerier{}, 0),
	}
}

// Evaluator returns a StepEvaluator for a given SampleExpr
func (ev *DownstreamEvaluator) StepEvaluator(
	ctx context.Context,
	nextEv SampleEvaluator,
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
		results, err := ev.Downstream(ctx, []DownstreamQuery{{
			Expr:   e.SampleExpr,
			Params: params,
			Shards: shards,
		}})
		if err != nil {
			return nil, err
		}
		return ResultStepEvaluator(results[0], params)

	case *ConcatSampleExpr:
		cur := e
		var queries []DownstreamQuery
		for cur != nil {
			qry := DownstreamQuery{
				Expr:   cur.DownstreamSampleExpr.SampleExpr,
				Params: params,
			}
			if shard := cur.DownstreamSampleExpr.shard; shard != nil {
				qry.Shards = Shards{*shard}
			}
			queries = append(queries, qry)
			cur = cur.next
		}

		results, err := ev.Downstream(ctx, queries)
		if err != nil {
			return nil, err
		}

		xs := make([]StepEvaluator, 0, len(queries))
		for i, res := range results {
			stepper, err := ResultStepEvaluator(res, params)
			if err != nil {
				level.Warn(util.Logger).Log(
					"msg", "could not extract StepEvaluator",
					"err", err,
					"expr", queries[i].Expr.String(),
				)
				return nil, err
			}
			xs = append(xs, stepper)
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
		results, err := ev.Downstream(ctx, []DownstreamQuery{{
			Expr:   e.LogSelectorExpr,
			Params: params,
			Shards: shards,
		}})

		if err != nil {
			return nil, err
		}
		return ResultIterator(results[0], params)

	case *ConcatLogSelectorExpr:
		cur := e
		var queries []DownstreamQuery
		for cur != nil {
			qry := DownstreamQuery{
				Expr:   cur.DownstreamLogSelectorExpr.LogSelectorExpr,
				Params: params,
			}
			if shard := cur.DownstreamLogSelectorExpr.shard; shard != nil {
				qry.Shards = Shards{*shard}
			}
			queries = append(queries, qry)
			cur = cur.next
		}

		results, err := ev.Downstream(ctx, queries)
		if err != nil {
			return nil, err
		}

		xs := make([]iter.EntryIterator, 0, len(queries))
		for i, res := range results {
			iter, err := ResultIterator(res, params)
			if err != nil {
				level.Warn(util.Logger).Log(
					"msg", "could not extract Iterator",
					"err", err,
					"expr", queries[i].Expr.String(),
				)
			}
			xs = append(xs, iter)
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
		func() error {
			var errs []error
			for _, eval := range evaluators {
				if err := eval.Error(); err != nil {
					errs = append(errs, err)
				}
			}
			switch len(errs) {
			case 0:
				return nil
			case 1:
				return errs[0]
			default:
				return fmt.Errorf("Multiple errors: %+v", errs)
			}
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
		}, nil, nil)
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
