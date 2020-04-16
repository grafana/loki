package logql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/astmapper"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/iter"
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
		qry, err := ev.Downstream(e.SampleExpr, params, shards)
		if err != nil {
			return nil, err
		}

		res, err := qry.Exec(ctx)
		if err != nil {
			return nil, err
		}
		return ResultStepEvaluator(res, params)

	case *ConcatSampleExpr:
		// ensure they all impl the same (SampleExpr, LogSelectorExpr) & concat
		var xs []StepEvaluator
		cur := e

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

	case *vectorAggregationExpr, *binOpExpr:
		return ev.defaultEvaluator.StepEvaluator(ctx, nextEv, e, params)

	default:
		return nil, EvaluatorUnsupportedType(expr, ev)
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
		qry, err := ev.Downstream(e.LogSelectorExpr, params, shards)
		if err != nil {
			return nil, err
		}

		res, err := qry.Exec(ctx)
		if err != nil {
			return nil, err
		}
		return ResultIterator(res, params)

	case *ConcatLogSelectorExpr:
		var iters []iter.EntryIterator
		cur := e
		for cur != nil {
			iterator, err := ev.Iterator(ctx, cur.LogSelectorExpr, params)
			if err != nil {
				// Close previously opened StepEvaluators
				for _, x := range iters {
					x.Close()
				}
				return nil, err
			}
			iters = append(iters, iterator)
			cur = cur.next
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
		return nil, fmt.Errorf("Unexpected type (%s) for ResultIterator; expected %s", res.Data.Type(), ValueTypeStreams)
	}
	return iter.NewStreamsIterator(context.Background(), streams, params.Direction()), nil

}

type shardedEngine struct {
	timeout   time.Duration
	mapper    ShardMapper
	evaluator Evaluator
	metrics   *ShardingMetrics
}

func NewShardedEngine(opts EngineOpts, shards int, downstreamer Downstreamer, metrics *ShardingMetrics) (Engine, error) {
	opts.applyDefault()
	mapper, err := NewShardMapper(shards, metrics)
	if err != nil {
		return nil, err
	}

	return &shardedEngine{
		timeout:   opts.Timeout,
		mapper:    mapper,
		evaluator: NewDownstreamEvaluator(downstreamer),
		metrics:   metrics,
	}, nil

}

func (ng *shardedEngine) query(p Params) Query {
	return &query{
		timeout:   ng.timeout,
		params:    p,
		evaluator: ng.evaluator,
		parse:     ng.mapper.Parse,
	}
}

func (ng *shardedEngine) NewRangeQuery(p Params) Query {
	return ng.query(p)
}

func (ng *shardedEngine) NewInstantQuery(p Params) Query {
	return ng.query(p)
}
