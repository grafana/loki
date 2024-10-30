package logql

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/metadata"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
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
func (ng *DownstreamEngine) Query(ctx context.Context, p Params) Query {
	return &query{
		logger:    ng.logger,
		params:    p,
		evaluator: NewDownstreamEvaluator(ng.downstreamable.Downstreamer(ctx)),
		limits:    ng.limits,
	}
}

// DownstreamSampleExpr is a SampleExpr which signals downstream computation
type DownstreamSampleExpr struct {
	shard *ShardWithChunkRefs
	syntax.SampleExpr
}

func (d DownstreamSampleExpr) String() string {
	return fmt.Sprintf("downstream<%s, shard=%s>", d.SampleExpr.String(), d.shard)
}

// The DownstreamSampleExpr is not part of LogQL. In the prettified version it's
// represented as e.g. `downstream<count_over_time({foo="bar"} |= "error"), shard=1_of_3>`
func (d DownstreamSampleExpr) Pretty(level int) string {
	s := syntax.Indent(level)
	if !syntax.NeedSplit(d) {
		return s + d.String()
	}

	s += "downstream<\n"

	s += d.SampleExpr.Pretty(level + 1)
	s += ",\n"
	s += syntax.Indent(level+1) + "shard="
	if d.shard != nil {
		s += d.shard.String() + "\n"
	} else {
		s += "nil\n"
	}

	s += syntax.Indent(level) + ">"
	return s
}

// DownstreamLogSelectorExpr is a LogSelectorExpr which signals downstream computation
type DownstreamLogSelectorExpr struct {
	shard *ShardWithChunkRefs
	syntax.LogSelectorExpr
}

func (d DownstreamLogSelectorExpr) String() string {
	return fmt.Sprintf("downstream<%s, shard=%s>", d.LogSelectorExpr.String(), d.shard)
}

// The DownstreamLogSelectorExpr is not part of LogQL. In the prettified version it's
// represented as e.g. `downstream<{foo="bar"} |= "error", shard=1_of_3>`
func (d DownstreamLogSelectorExpr) Pretty(level int) string {
	s := syntax.Indent(level)
	if !syntax.NeedSplit(d) {
		return s + d.String()
	}

	s += "downstream<\n"

	s += d.LogSelectorExpr.Pretty(level + 1)
	s += ",\n"
	s += syntax.Indent(level+1) + "shard="
	if d.shard != nil {
		s += d.shard.String() + "\n"
	} else {
		s += "nil\n"
	}

	s += syntax.Indent(level) + ">"
	return s
}

func (d DownstreamSampleExpr) Walk(f syntax.WalkFn) { f(d) }

var defaultMaxDepth = 4

// ConcatSampleExpr is an expr for concatenating multiple SampleExpr
// Contract: The embedded SampleExprs within a linked list of ConcatSampleExprs must be of the
// same structure. This makes special implementations of SampleExpr.Associative() unnecessary.
type ConcatSampleExpr struct {
	DownstreamSampleExpr
	next *ConcatSampleExpr
}

func (c *ConcatSampleExpr) String() string {
	if c.next == nil {
		return c.DownstreamSampleExpr.String()
	}

	return fmt.Sprintf("%s ++ %s", c.DownstreamSampleExpr.String(), c.next.string(defaultMaxDepth-1))
}

// in order to not display huge queries with thousands of shards,
// we can limit the number of stringified subqueries.
func (c *ConcatSampleExpr) string(maxDepth int) string {
	if c.next == nil {
		return c.DownstreamSampleExpr.String()
	}
	if maxDepth <= 1 {
		return fmt.Sprintf("%s ++ ...", c.DownstreamSampleExpr.String())
	}
	return fmt.Sprintf("%s ++ %s", c.DownstreamSampleExpr.String(), c.next.string(maxDepth-1))
}

func (c *ConcatSampleExpr) Walk(f syntax.WalkFn) {
	f(c)
	f(c.next)
}

// ConcatSampleExpr has no LogQL repretenstation. It is expressed in in the
// prettified version as e.g. `concat(downstream<count_over_time({foo="bar"}), shard=...> ++ )`
func (c *ConcatSampleExpr) Pretty(level int) string {
	s := syntax.Indent(level)
	if !syntax.NeedSplit(c) {
		return s + c.String()
	}

	s += "concat(\n"

	head := c
	for i := 0; i < defaultMaxDepth && head != nil; i++ {
		if i > 0 {
			s += syntax.Indent(level+1) + "++\n"
		}
		s += head.DownstreamSampleExpr.Pretty(level + 1)
		s += "\n"
		head = head.next
	}
	// There are more downstream samples...
	if head != nil {
		s += syntax.Indent(level+1) + "++ ...\n"
	}
	s += syntax.Indent(level) + ")"

	return s
}

// ConcatLogSelectorExpr is an expr for concatenating multiple LogSelectorExpr
type ConcatLogSelectorExpr struct {
	DownstreamLogSelectorExpr
	next *ConcatLogSelectorExpr
}

func (c *ConcatLogSelectorExpr) String() string {
	if c.next == nil {
		return c.DownstreamLogSelectorExpr.String()
	}

	return fmt.Sprintf("%s ++ %s", c.DownstreamLogSelectorExpr.String(), c.next.string(defaultMaxDepth-1))
}

// in order to not display huge queries with thousands of shards,
// we can limit the number of stringified subqueries.
func (c *ConcatLogSelectorExpr) string(maxDepth int) string {
	if c.next == nil {
		return c.DownstreamLogSelectorExpr.String()
	}
	if maxDepth <= 1 {
		return fmt.Sprintf("%s ++ ...", c.DownstreamLogSelectorExpr.String())
	}
	return fmt.Sprintf("%s ++ %s", c.DownstreamLogSelectorExpr.String(), c.next.string(maxDepth-1))
}

// ConcatLogSelectorExpr has no representation in LogQL. Its prettified version
// is e.g. `concat(downstream<{foo="bar"} |= "error", shard=1_of_3>)`
func (c *ConcatLogSelectorExpr) Pretty(level int) string {
	s := syntax.Indent(level)
	if !syntax.NeedSplit(c) {
		return s + c.String()
	}

	s += "concat(\n"

	head := c
	for i := 0; i < defaultMaxDepth && head != nil; i++ {
		if i > 0 {
			s += syntax.Indent(level+1) + "++\n"
		}
		s += head.DownstreamLogSelectorExpr.Pretty(level + 1)
		s += "\n"
		head = head.next
	}
	// There are more downstream samples...
	if head != nil {
		s += syntax.Indent(level+1) + "++ ...\n"
	}
	s += ")"

	return s
}

// QuantileSketchEvalExpr evaluates a quantile sketch to the actual quantile.
type QuantileSketchEvalExpr struct {
	syntax.SampleExpr
	quantileMergeExpr *QuantileSketchMergeExpr
	quantile          *float64
}

func (e QuantileSketchEvalExpr) String() string {
	return fmt.Sprintf("quantileSketchEval<%s>", e.quantileMergeExpr.String())
}

func (e *QuantileSketchEvalExpr) Walk(f syntax.WalkFn) {
	f(e)
	e.quantileMergeExpr.Walk(f)
}

type QuantileSketchMergeExpr struct {
	syntax.SampleExpr
	downstreams []DownstreamSampleExpr
}

func (e QuantileSketchMergeExpr) String() string {
	var sb strings.Builder
	for i, d := range e.downstreams {
		if i >= defaultMaxDepth {
			break
		}

		if i > 0 {
			sb.WriteString(" ++ ")
		}

		sb.WriteString(d.String())
	}
	return fmt.Sprintf("quantileSketchMerge<%s>", sb.String())
}

func (e *QuantileSketchMergeExpr) Walk(f syntax.WalkFn) {
	f(e)
	for _, d := range e.downstreams {
		d.Walk(f)
	}
}

type MergeFirstOverTimeExpr struct {
	syntax.SampleExpr
	downstreams []DownstreamSampleExpr
}

func (e MergeFirstOverTimeExpr) String() string {
	var sb strings.Builder
	for i, d := range e.downstreams {
		if i >= defaultMaxDepth {
			break
		}

		if i > 0 {
			sb.WriteString(" ++ ")
		}

		sb.WriteString(d.String())
	}
	return fmt.Sprintf("MergeFirstOverTime<%s>", sb.String())
}

func (e *MergeFirstOverTimeExpr) Walk(f syntax.WalkFn) {
	f(e)
	for _, d := range e.downstreams {
		d.Walk(f)
	}
}

type MergeLastOverTimeExpr struct {
	syntax.SampleExpr
	downstreams []DownstreamSampleExpr
}

func (e MergeLastOverTimeExpr) String() string {
	var sb strings.Builder
	for i, d := range e.downstreams {
		if i >= defaultMaxDepth {
			break
		}

		if i > 0 {
			sb.WriteString(" ++ ")
		}

		sb.WriteString(d.String())
	}
	return fmt.Sprintf("MergeLastOverTime<%s>", sb.String())
}

func (e *MergeLastOverTimeExpr) Walk(f syntax.WalkFn) {
	f(e)
	for _, d := range e.downstreams {
		d.Walk(f)
	}
}

type Downstreamable interface {
	Downstreamer(context.Context) Downstreamer
}

type DownstreamQuery struct {
	Params Params
}

type Resp struct {
	I   int
	Res logqlmodel.Result
	Err error
}

// Downstreamer is an interface for deferring responsibility for query execution.
// It is decoupled from but consumed by a downStreamEvaluator to dispatch ASTs.
type Downstreamer interface {
	Downstream(context.Context, []DownstreamQuery, Accumulator) ([]logqlmodel.Result, error)
}

// Accumulator is an interface for accumulating query results.
type Accumulator interface {
	Accumulate(context.Context, logqlmodel.Result, int) error
	Result() []logqlmodel.Result
}

// DownstreamEvaluator is an evaluator which handles shard aware AST nodes
type DownstreamEvaluator struct {
	Downstreamer
	defaultEvaluator EvaluatorFactory
}

// Downstream runs queries and collects stats from the embedded Downstreamer
func (ev DownstreamEvaluator) Downstream(ctx context.Context, queries []DownstreamQuery, acc Accumulator) ([]logqlmodel.Result, error) {
	results, err := ev.Downstreamer.Downstream(ctx, queries, acc)
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
		if err := metadata.AddWarnings(ctx, res.Warnings...); err != nil {
			level.Warn(util_log.Logger).Log("msg", "unable to add headers to results context", "error", err)
		}

		if err := metadata.JoinHeaders(ctx, res.Headers); err != nil {
			level.Warn(util_log.Logger).Log("msg", "unable to add headers to results context", "error", err)
			break
		}
	}

	return results, nil
}

type errorQuerier struct{}

func (errorQuerier) SelectLogs(_ context.Context, _ SelectLogParams) (iter.EntryIterator, error) {
	return nil, errors.New("SelectLogs unimplemented: the query-frontend cannot evaluate an expression that selects logs. this is likely a bug in the query engine. please contact your system operator")
}

func (errorQuerier) SelectSamples(_ context.Context, _ SelectSampleParams) (iter.SampleIterator, error) {
	return nil, errors.New("SelectSamples unimplemented: the query-frontend cannot evaluate an expression that selects samples. this is likely a bug in the query engine. please contact your system operator")
}

func NewDownstreamEvaluator(downstreamer Downstreamer) *DownstreamEvaluator {
	return &DownstreamEvaluator{
		Downstreamer:     downstreamer,
		defaultEvaluator: NewDefaultEvaluator(&errorQuerier{}, 0),
	}
}

// NewStepEvaluator returns a NewStepEvaluator for a given SampleExpr
func (ev *DownstreamEvaluator) NewStepEvaluator(
	ctx context.Context,
	nextEvFactory SampleEvaluatorFactory,
	expr syntax.SampleExpr,
	params Params,
) (StepEvaluator, error) {
	switch e := expr.(type) {

	case DownstreamSampleExpr:
		// downstream to a querier
		acc := NewBufferedAccumulator(1)
		results, err := ev.Downstream(ctx, []DownstreamQuery{{
			Params: ParamsWithExpressionOverride{
				Params:             ParamOverridesFromShard(params, e.shard),
				ExpressionOverride: e.SampleExpr,
			},
		}}, acc)
		if err != nil {
			return nil, err
		}
		return NewResultStepEvaluator(results[0], params)

	case *ConcatSampleExpr:
		cur := e
		var queries []DownstreamQuery
		for cur != nil {
			qry := DownstreamQuery{
				Params: ParamsWithExpressionOverride{
					Params:             ParamOverridesFromShard(params, cur.DownstreamSampleExpr.shard),
					ExpressionOverride: cur.DownstreamSampleExpr.SampleExpr,
				},
			}
			queries = append(queries, qry)
			cur = cur.next
		}

		acc := NewBufferedAccumulator(len(queries))
		results, err := ev.Downstream(ctx, queries, acc)
		if err != nil {
			return nil, err
		}

		xs := make([]StepEvaluator, 0, len(queries))
		for i, res := range results {
			stepper, err := NewResultStepEvaluator(res, params)
			if err != nil {
				level.Warn(util_log.Logger).Log(
					"msg", "could not extract StepEvaluator",
					"err", err,
					"expr", queries[i].Params.GetExpression().String(),
				)
				return nil, err
			}
			xs = append(xs, stepper)
		}

		return NewConcatStepEvaluator(xs), nil
	case *QuantileSketchEvalExpr:
		var queries []DownstreamQuery
		if e.quantileMergeExpr != nil {
			for _, d := range e.quantileMergeExpr.downstreams {
				qry := DownstreamQuery{
					Params: ParamsWithExpressionOverride{
						Params:             ParamOverridesFromShard(params, d.shard),
						ExpressionOverride: d.SampleExpr,
					},
				}
				queries = append(queries, qry)
			}
		}

		acc := newQuantileSketchAccumulator()
		results, err := ev.Downstream(ctx, queries, acc)
		if err != nil {
			return nil, err
		}

		if len(results) != 1 {
			return nil, fmt.Errorf("unexpected results length for sharded quantile: got (%d), want (1)", len(results))
		}

		matrix, ok := results[0].Data.(ProbabilisticQuantileMatrix)
		if !ok {
			return nil, fmt.Errorf("unexpected matrix type: got (%T), want (ProbabilisticQuantileMatrix)", results[0].Data)
		}
		inner := NewQuantileSketchMatrixStepEvaluator(matrix, params)
		return NewQuantileSketchVectorStepEvaluator(inner, *e.quantile), nil
	case *MergeFirstOverTimeExpr:
		queries := make([]DownstreamQuery, len(e.downstreams))

		for i, d := range e.downstreams {
			qry := DownstreamQuery{
				Params: ParamsWithExpressionOverride{
					Params:             params,
					ExpressionOverride: d.SampleExpr,
				},
			}
			if shard := d.shard; shard != nil {
				qry.Params = ParamsWithShardsOverride{
					Params:         qry.Params,
					ShardsOverride: Shards{shard.Shard}.Encode(),
				}
			}
			queries[i] = qry
		}

		acc := NewBufferedAccumulator(len(queries))
		results, err := ev.Downstream(ctx, queries, acc)
		if err != nil {
			return nil, err
		}

		xs := make([]promql.Matrix, 0, len(queries))
		for _, res := range results {

			switch data := res.Data.(type) {
			case promql.Matrix:
				xs = append(xs, data)
			default:
				return nil, fmt.Errorf("unexpected type (%s) uncoercible to StepEvaluator", data.Type())
			}
		}

		return NewMergeFirstOverTimeStepEvaluator(params, xs), nil
	case *MergeLastOverTimeExpr:
		queries := make([]DownstreamQuery, len(e.downstreams))

		for i, d := range e.downstreams {
			qry := DownstreamQuery{
				Params: ParamsWithExpressionOverride{
					Params:             params,
					ExpressionOverride: d.SampleExpr,
				},
			}
			if shard := d.shard; shard != nil {
				qry.Params = ParamsWithShardsOverride{
					Params:         qry.Params,
					ShardsOverride: Shards{shard.Shard}.Encode(),
				}
			}
			queries[i] = qry
		}

		acc := NewBufferedAccumulator(len(queries))
		results, err := ev.Downstream(ctx, queries, acc)
		if err != nil {
			return nil, err
		}

		xs := make([]promql.Matrix, 0, len(queries))
		for _, res := range results {

			switch data := res.Data.(type) {
			case promql.Matrix:
				xs = append(xs, data)
			default:
				return nil, fmt.Errorf("unexpected type (%s) uncoercible to StepEvaluator", data.Type())
			}
		}
		return NewMergeLastOverTimeStepEvaluator(params, xs), nil
	default:
		return ev.defaultEvaluator.NewStepEvaluator(ctx, nextEvFactory, e, params)
	}
}

// NewIterator returns the iter.EntryIterator for a given LogSelectorExpr
func (ev *DownstreamEvaluator) NewIterator(
	ctx context.Context,
	expr syntax.LogSelectorExpr,
	params Params,
) (iter.EntryIterator, error) {
	switch e := expr.(type) {
	case DownstreamLogSelectorExpr:
		// downstream to a querier
		acc := NewStreamAccumulator(params)
		results, err := ev.Downstream(ctx, []DownstreamQuery{{
			Params: ParamsWithExpressionOverride{
				Params:             ParamOverridesFromShard(params, e.shard),
				ExpressionOverride: e.LogSelectorExpr,
			},
		}}, acc)
		if err != nil {
			return nil, err
		}
		return ResultIterator(results[0], params.Direction())

	case *ConcatLogSelectorExpr:
		cur := e
		var queries []DownstreamQuery
		for cur != nil {
			qry := DownstreamQuery{
				Params: ParamsWithExpressionOverride{
					Params:             ParamOverridesFromShard(params, cur.DownstreamLogSelectorExpr.shard),
					ExpressionOverride: cur.DownstreamLogSelectorExpr.LogSelectorExpr,
				},
			}
			queries = append(queries, qry)
			cur = cur.next
		}

		acc := NewStreamAccumulator(params)
		results, err := ev.Downstream(ctx, queries, acc)
		if err != nil {
			return nil, err
		}

		xs := make([]iter.EntryIterator, 0, len(results))
		for i, res := range results {
			iter, err := ResultIterator(res, params.Direction())
			if err != nil {
				level.Warn(util_log.Logger).Log(
					"msg", "could not extract Iterator",
					"err", err,
					"expr", queries[i].Params.GetExpression().String(),
				)
			}
			xs = append(xs, iter)
		}

		return iter.NewSortEntryIterator(xs, params.Direction()), nil

	default:
		return nil, EvaluatorUnsupportedType(expr, ev)
	}
}

type ConcatStepEvaluator struct {
	evaluators []StepEvaluator
}

// NewConcatStepEvaluator joins multiple StepEvaluators.
// Contract: They must be of identical start, end, and step values.
func NewConcatStepEvaluator(evaluators []StepEvaluator) *ConcatStepEvaluator {
	return &ConcatStepEvaluator{evaluators}
}

func (e *ConcatStepEvaluator) Next() (bool, int64, StepResult) {
	var (
		cur StepResult
		ok  bool
		ts  int64
	)
	vec := SampleVector{}
	for _, eval := range e.evaluators {
		ok, ts, cur = eval.Next()
		if ok {
			vec = append(vec, cur.SampleVector()...)
		}
	}
	return ok, ts, vec
}

func (e *ConcatStepEvaluator) Close() (lastErr error) {
	for _, eval := range e.evaluators {
		if err := eval.Close(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func (e *ConcatStepEvaluator) Error() error {
	var errs []error
	for _, eval := range e.evaluators {
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
}

// NewResultStepEvaluator coerces a downstream vector or matrix into a StepEvaluator
func NewResultStepEvaluator(res logqlmodel.Result, params Params) (StepEvaluator, error) {
	var (
		start = params.Start()
		end   = params.End()
		step  = params.Step()
	)

	if res.Data == nil {
		return nil, fmt.Errorf("data in the passed result is nil (res.Data), cannot be processed by stepevaluator")
	}

	switch data := res.Data.(type) {
	case promql.Vector:
		return NewVectorStepEvaluator(start, data), nil
	case promql.Matrix:
		return NewMatrixStepEvaluator(start, end, step, data), nil
	default:
		return nil, fmt.Errorf("unexpected type (%s) uncoercible to StepEvaluator", data.Type())
	}
}

// ResultIterator coerces a downstream streams result into an iter.EntryIterator
func ResultIterator(res logqlmodel.Result, direction logproto.Direction) (iter.EntryIterator, error) {
	streams, ok := res.Data.(logqlmodel.Streams)
	if !ok {
		return nil, fmt.Errorf("unexpected type (%s) for ResultIterator; expected %s", res.Data.Type(), logqlmodel.ValueTypeStreams)
	}
	return iter.NewStreamsIterator(streams, direction), nil
}
