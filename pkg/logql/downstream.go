package logql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logql/sketch"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/logqlmodel/metadata"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/astmapper"
	"github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

/*
The downstream engine is responsible for executing multiple downstream computations in parallel.
Each downstream computation includes an Expr and an optional sharding representation based on backend shard factors.

In schemas 10+ a shard factor (default 16) is introduced in the index store,
calculated by hashing the label set of a log stream. This allows us to perform certain optimizations
that fall under the umbrella of query remapping and querying shards individually.
For instance, `{app="foo"} |= "bar"` can be executed on each shard independently, then re-aggregated:
	downstream<{app="foo"} |= "bar", shard=0_of_n>
	...
	++ downstream<{app="foo"} |= "bar", shard=n-1_of_n>

There are also a class of optimizations that can be performed by altering a query into a functionally equivalent,
but more parallelizable form. For instance, an average can be remapped into a sum/count expression where each
operand expression can take advantage of the parallel execution model:
	downstream<SUM_EXPR, shard=<nil>>
	/
	downstream<COUNT_EXPR, shard=<nil>>
*/

// DownstreamEngine is an Engine implementation that can split queries into more parallelizable forms via
// querying the underlying backend shards individually and re-aggregating them.
type DownstreamEngine struct {
	logger         log.Logger
	opts           EngineOpts
	downstreamable Downstreamable
	limits         Limits
}

// NewDownstreamEngine constructs a *DownstreamEngine
func NewDownstreamEngine(opts EngineOpts, downstreamable Downstreamable, limits Limits, logger log.Logger) *DownstreamEngine {
	opts.applyDefault()
	return &DownstreamEngine{
		logger:         logger,
		opts:           opts,
		downstreamable: downstreamable,
		limits:         limits,
	}
}

func (ng *DownstreamEngine) Opts() EngineOpts { return ng.opts }

// Query constructs a Query
func (ng *DownstreamEngine) Query(ctx context.Context, p Params, mapped syntax.Expr) Query {
	return &query{
		logger:    ng.logger,
		params:    p,
		evaluator: NewDownstreamEvaluator(ng.downstreamable.Downstreamer(ctx)),
		parse: func(_ context.Context, _ string) (syntax.Expr, error) {
			return mapped, nil
		},
		limits: ng.limits,
	}
}

// DownstreamSampleExpr is a SampleExpr which signals downstream computation
type DownstreamSampleExpr struct {
	shard *astmapper.ShardAnnotation
	syntax.SampleExpr
}

func (d DownstreamSampleExpr) String() string {
	return fmt.Sprintf("downstream<%s, shard=%s>", d.SampleExpr.String(), d.shard)
}

// DownstreamLogSelectorExpr is a LogSelectorExpr which signals downstream computation
type DownstreamLogSelectorExpr struct {
	shard *astmapper.ShardAnnotation
	syntax.LogSelectorExpr
}

func (d DownstreamLogSelectorExpr) String() string {
	return fmt.Sprintf("downstream<%s, shard=%s>", d.LogSelectorExpr.String(), d.shard)
}

func (d DownstreamSampleExpr) Walk(f syntax.WalkFn) { f(d) }

// DownstreamSampleExpr is a SampleExpr which signals downstream computation
type DownstreamTopkSampleExpr struct {
	shard *astmapper.ShardAnnotation
	syntax.TopkSampleExpr
}

func (d DownstreamTopkSampleExpr) String() string {
	return fmt.Sprintf("downstream_topk<%s, shard=%s>", d.TopkSampleExpr.String(), d.shard)
}

func (d DownstreamTopkSampleExpr) Walk(f syntax.WalkFn) { f(d) }

var defaultMaxDepth = 4

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

	return fmt.Sprintf("%s ++ %s", c.DownstreamSampleExpr.String(), c.next.string(defaultMaxDepth-1))
}

// in order to not display huge queries with thousands of shards,
// we can limit the number of stringified subqueries.
func (c ConcatSampleExpr) string(maxDepth int) string {
	if c.next == nil {
		return c.DownstreamSampleExpr.String()
	}
	if maxDepth <= 1 {
		return fmt.Sprintf("%s ++ ...", c.DownstreamSampleExpr.String())
	}
	return fmt.Sprintf("%s ++ %s", c.DownstreamSampleExpr.String(), c.next.string(maxDepth-1))
}

func (c ConcatSampleExpr) Walk(f syntax.WalkFn) {
	f(c)
	f(c.next)
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

	return fmt.Sprintf("%s ++ %s", c.DownstreamLogSelectorExpr.String(), c.next.string(defaultMaxDepth-1))
}

// in order to not display huge queries with thousands of shards,
// we can limit the number of stringified subqueries.
func (c ConcatLogSelectorExpr) string(maxDepth int) string {
	if c.next == nil {
		return c.DownstreamLogSelectorExpr.String()
	}
	if maxDepth <= 1 {
		return fmt.Sprintf("%s ++ ...", c.DownstreamLogSelectorExpr.String())
	}
	return fmt.Sprintf("%s ++ %s", c.DownstreamLogSelectorExpr.String(), c.next.string(maxDepth-1))
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

// TopkConcatSampleExpr is an expr for concatenating multiple TopkSampleExpr
// Contract: The embedded TopkSampleExprs within a linked list of TopkConcatSampleExprs must be of the
// same structure. This makes special implementations of SampleExpr.Associative() unnecessary.
type TopkMergeSampleExpr struct {
	DownstreamTopkSampleExpr
	next *TopkMergeSampleExpr
}

func (c TopkMergeSampleExpr) String() string {
	if c.next == nil {
		return c.DownstreamTopkSampleExpr.String()
	}

	return fmt.Sprintf("merge_topk(%s, %s)", c.DownstreamTopkSampleExpr.String(), c.next.string(defaultMaxDepth-1))
}

// in order to not display huge queries with thousands of shards,
// we can limit the number of stringified subqueries.
func (c TopkMergeSampleExpr) string(maxDepth int) string {
	if c.next == nil {
		return c.DownstreamTopkSampleExpr.String()
	}
	if maxDepth <= 1 {
		return fmt.Sprintf("%s, ...", c.DownstreamTopkSampleExpr.String())
	}
	return fmt.Sprintf("%s, %s", c.DownstreamTopkSampleExpr.String(), c.next.string(maxDepth-1))
}

func (c TopkMergeSampleExpr) Walk(f syntax.WalkFn) {
	f(c)
	f(c.next)
}

type Downstreamable interface {
	Downstreamer(ctx context.Context) Downstreamer
}

type DownstreamQuery struct {
	Expr   syntax.Expr
	Params Params
	Shards Shards
}

// Downstreamer is an interface for deferring responsibility for query execution.
// It is decoupled from but consumed by a downStreamEvaluator to dispatch ASTs.
type Downstreamer interface {
	Downstream(ctx context.Context, queries []DownstreamQuery) ([]logqlmodel.Result, error)
}

// DownstreamEvaluator is an evaluator which handles shard aware AST nodes
type DownstreamEvaluator struct {
	Downstreamer
	defaultEvaluator Evaluator
	probabilistic    bool
}

// Downstream runs queries and collects stats from the embedded Downstreamer
func (ev DownstreamEvaluator) Downstream(ctx context.Context, queries []DownstreamQuery) ([]logqlmodel.Result, error) {
	results, err := ev.Downstreamer.Downstream(ctx, queries)
	if err != nil {
		return nil, err
	}

	for _, res := range results {
		// TODO(owen-d/ewelch): Shard counts should be set by the querier
		// so we don't have to do it in tricky ways in multiple places.
		// See pkg/queryrange/downstreamer.go:*accumulatedStreams.Accumulate
		// for another example
		if res.Statistics.Summary.Shards == 0 {
			res.Statistics.Summary.Shards = 1
		}

		stats.JoinResults(ctx, res.Statistics)
	}

	for _, res := range results {
		if err := metadata.JoinHeaders(ctx, res.Headers); err != nil {
			level.Warn(util_log.Logger).Log("msg", "unable to add headers to results context", "error", err)
			break
		}
	}

	return results, nil
}

type errorQuerier struct{}

func (errorQuerier) SelectLogs(_ context.Context, _ SelectLogParams) (iter.EntryIterator, error) {
	return nil, errors.New("unimplemented")
}

func (errorQuerier) SelectSamples(_ context.Context, _ SelectSampleParams) (iter.SampleIterator, error) {
	return nil, errors.New("unimplemented")
}

func NewDownstreamEvaluator(downstreamer Downstreamer) *DownstreamEvaluator {
	return &DownstreamEvaluator{
		Downstreamer:     downstreamer,
		defaultEvaluator: NewDefaultEvaluator(&errorQuerier{}, 0),
	}
}

// StepEvaluator returns a StepEvaluator for a given SampleExpr
func (ev *DownstreamEvaluator) StepEvaluator(
	ctx context.Context,
	nextEv SampleEvaluator,
	expr syntax.SampleExpr,
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
				level.Warn(util_log.Logger).Log(
					"msg", "could not extract StepEvaluator",
					"err", err,
					"expr", queries[i].Expr.String(),
				)
				return nil, err
			}
			xs = append(xs, stepper)
		}

		return ConcatEvaluator(xs)

	case *TopkMergeSampleExpr:
		cur := e
		var queries []DownstreamQuery
		for cur != nil {
			qry := DownstreamQuery{
				Expr:   cur.DownstreamTopkSampleExpr,
				Params: params,
			}
			if shard := cur.DownstreamTopkSampleExpr.shard; shard != nil {
				qry.Shards = Shards{*shard}
			}
			queries = append(queries, qry)
			cur = cur.next
		}

		results, err := ev.Downstream(ctx, queries)
		if err != nil {
			return nil, err
		}

		xs := make([]ProbabilisticStepEvaluator, 0, len(queries))
		for i, res := range results {
			stepper, err := ProbabilisticResultStepEvaluator(res, params)
			if err != nil {
				level.Warn(util_log.Logger).Log(
					"msg", "could not extract StepEvaluator",
					"err", err,
					"expr", queries[i].Expr.String(),
				)
				return nil, err
			}
			xs = append(xs, stepper)
		}

		return TopkMergeEvalator(xs)

	default:
		return ev.defaultEvaluator.StepEvaluator(ctx, nextEv, e, params)
	}
}

// Iterator returns the iter.EntryIterator for a given LogSelectorExpr
func (ev *DownstreamEvaluator) Iterator(
	ctx context.Context,
	expr syntax.LogSelectorExpr,
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

		xs := make([]iter.EntryIterator, 0, len(results))
		for i, res := range results {
			iter, err := ResultIterator(res, params)
			if err != nil {
				level.Warn(util_log.Logger).Log(
					"msg", "could not extract Iterator",
					"err", err,
					"expr", queries[i].Expr.String(),
				)
			}
			xs = append(xs, iter)
		}

		return iter.NewSortEntryIterator(xs, params.Direction()), nil

	default:
		return nil, EvaluatorUnsupportedType(expr, ev)
	}
}

// ConcatEvaluator joins multiple StepEvaluators.
// Contract: They must be of identical start, end, and step values.
func ConcatEvaluator(evaluators []StepEvaluator) (StepEvaluator, error) {
	return NewStepEvaluator(
		func() (ok bool, ts int64, vec promql.Vector) {
			var cur promql.Vector
			for _, eval := range evaluators {
				ok, ts, cur = eval.Next()
				vec = append(vec, cur...)
			}
			return ok, ts, vec
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
				return util.MultiError(errs)
			}
		},
	)
}

func TopkMergeEvalator(evaluators []ProbabilisticStepEvaluator) (StepEvaluator, error) {
	var err error
	return NewStepEvaluator(
		func() (ok bool, ts int64, vec promql.Vector) {
			var cur StepResult
			var acc *sketch.Topk
			for _, eval := range evaluators {
				ok, ts, cur = eval.Next()
				if !ok {
					continue
				}
				if acc == nil {
					acc = cur.TopkVector().Topk
				} else {
					err = acc.Merge(cur.TopkVector().Topk)
					if err != nil {
						return false, 0, promql.Vector{}
					}
				}
			}

			if acc == nil {
				return false, 0, promql.Vector{}
			}

			for _, r := range acc.Topk() {
				for _, e := range r.Result {
					ls, err := syntax.ParseLabels(e.Event)
					if err != nil {
						return false, 0, promql.Vector{}
					}
					vec = append(vec, promql.Sample{
						T:      ts,
						F:      float64(e.Count),
						Metric: ls,
					})
				}
			}

			return ok, ts, vec
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
			if err != nil {
				errs = append(errs, err)
			}
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
				return util.MultiError(errs)
			}
		},
	)
}

// ResultStepEvaluator coerces a downstream vector or matrix into a StepEvaluator
func ResultStepEvaluator(res logqlmodel.Result, params Params) (StepEvaluator, error) {
	var (
		start = params.Start()
		end   = params.End()
		step  = params.Step()
	)

	switch data := res.Data.(type) {
	case promql.Vector:
		var exhausted bool
		return NewStepEvaluator(func() (bool, int64, promql.Vector) {
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

// ProbabilisticResultStepEvaluator coerces a downstream StepResult into a
// ProbabilisticStepEvaluator
func ProbabilisticResultStepEvaluator(res logqlmodel.Result, params Params) (ProbabilisticStepEvaluator, error) {
	var (
		start = params.Start()
	)

	switch data := res.Data.(type) {
	case promql.Vector:
		var exhausted bool
		ev, err := NewStepEvaluator(func() (bool, int64, promql.Vector) {
			if !exhausted {
				exhausted = true
				return true, start.UnixNano() / int64(time.Millisecond), data
			}
			return false, 0, nil
		}, nil, nil)
		return stepEvaluatorAdapter{ev}, err
	case sketch.TopKMatrix:
		return NewTopKMatrixStepper(data), nil
	default:
		return nil, fmt.Errorf("unexpected type (%s) uncoercible to StepEvaluator", data.Type())
	}
}

// ResultIterator coerces a downstream streams result into an iter.EntryIterator
func ResultIterator(res logqlmodel.Result, params Params) (iter.EntryIterator, error) {
	streams, ok := res.Data.(logqlmodel.Streams)
	if !ok {
		return nil, fmt.Errorf("unexpected type (%s) for ResultIterator; expected %s", res.Data.Type(), logqlmodel.ValueTypeStreams)
	}
	return iter.NewStreamsIterator(streams, params.Direction()), nil
}
