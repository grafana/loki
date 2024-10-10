package logql

import (
	"container/heap"
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
	"github.com/grafana/loki/v3/pkg/util"
)

type QueryRangeType string

const trueString = "true"

var (
	InstantType QueryRangeType = "instant"
	RangeType   QueryRangeType = "range"
)

// Params details the parameters associated with a loki request
type Params interface {
	QueryString() string
	Start() time.Time
	End() time.Time
	Step() time.Duration
	Interval() time.Duration
	Limit() uint32
	Direction() logproto.Direction
	Shards() []string
	GetExpression() syntax.Expr
	GetStoreChunks() *logproto.ChunkRefGroup
	CachingOptions() resultscache.CachingOptions
}

func NewLiteralParams(
	qs string,
	start, end time.Time,
	step, interval time.Duration,
	direction logproto.Direction,
	limit uint32,
	shards []string,
	storeChunks *logproto.ChunkRefGroup,
) (LiteralParams, error) {
	return newLiteralParams(
		qs,
		start,
		end,
		step,
		interval,
		direction,
		limit,
		shards,
		storeChunks,
		resultscache.CachingOptions{},
	)
}

func NewLiteralParamsWithCaching(
	qs string,
	start, end time.Time,
	step, interval time.Duration,
	direction logproto.Direction,
	limit uint32,
	shards []string,
	storeChunks *logproto.ChunkRefGroup,
	cachingOptions resultscache.CachingOptions,
) (LiteralParams, error) {
	return newLiteralParams(
		qs,
		start,
		end,
		step,
		interval,
		direction,
		limit,
		shards,
		storeChunks,
		cachingOptions,
	)
}

func newLiteralParams(
	qs string,
	start, end time.Time,
	step, interval time.Duration,
	direction logproto.Direction,
	limit uint32,
	shards []string,
	storeChunks *logproto.ChunkRefGroup,
	cachingOptions resultscache.CachingOptions,
) (LiteralParams, error) {
	p := LiteralParams{
		queryString:    qs,
		start:          start,
		end:            end,
		step:           step,
		interval:       interval,
		direction:      direction,
		limit:          limit,
		shards:         shards,
		storeChunks:    storeChunks,
		cachingOptions: cachingOptions,
	}
	var err error
	p.queryExpr, err = syntax.ParseExpr(qs)
	return p, err
}

// LiteralParams impls Params
type LiteralParams struct {
	queryString    string
	start, end     time.Time
	step, interval time.Duration
	direction      logproto.Direction
	limit          uint32
	shards         []string
	queryExpr      syntax.Expr
	storeChunks    *logproto.ChunkRefGroup
	cachingOptions resultscache.CachingOptions
}

func (p LiteralParams) Copy() LiteralParams { return p }

// String impls Params
func (p LiteralParams) QueryString() string { return p.queryString }

// GetExpression impls Params
func (p LiteralParams) GetExpression() syntax.Expr { return p.queryExpr }

// Start impls Params
func (p LiteralParams) Start() time.Time { return p.start }

// End impls Params
func (p LiteralParams) End() time.Time { return p.end }

// Step impls Params
func (p LiteralParams) Step() time.Duration { return p.step }

// Interval impls Params
func (p LiteralParams) Interval() time.Duration { return p.interval }

// Limit impls Params
func (p LiteralParams) Limit() uint32 { return p.limit }

// Direction impls Params
func (p LiteralParams) Direction() logproto.Direction { return p.direction }

// Shards impls Params
func (p LiteralParams) Shards() []string { return p.shards }

// StoreChunks impls Params
func (p LiteralParams) GetStoreChunks() *logproto.ChunkRefGroup { return p.storeChunks }

// CachingOptions returns whether Loki query created from this params should be cached.
func (p LiteralParams) CachingOptions() resultscache.CachingOptions {
	return p.cachingOptions
}

// GetRangeType returns whether a query is an instant query or range query
func GetRangeType(q Params) QueryRangeType {
	if q.Start() == q.End() && q.Step() == 0 {
		return InstantType
	}
	return RangeType
}

// ParamsWithExpressionOverride overrides the query expression so that the query
// string and the expression can differ. This is useful for for query planning
// when plan my not match externally available logql syntax
type ParamsWithExpressionOverride struct {
	Params
	ExpressionOverride syntax.Expr
}

// GetExpression returns the parsed expression of the query.
func (p ParamsWithExpressionOverride) GetExpression() syntax.Expr {
	return p.ExpressionOverride
}

// ParamsWithExpressionOverride overrides the shards. Since the backing
// implementation of the Params interface is unknown they are embedded and the
// original shards are shadowed.
type ParamsWithShardsOverride struct {
	Params
	ShardsOverride []string
}

// Shards returns this overwriting shards.
func (p ParamsWithShardsOverride) Shards() []string {
	return p.ShardsOverride
}

type ParamsWithChunkOverrides struct {
	Params
	StoreChunksOverride *logproto.ChunkRefGroup
}

func (p ParamsWithChunkOverrides) GetStoreChunks() *logproto.ChunkRefGroup {
	return p.StoreChunksOverride
}

func ParamOverridesFromShard(base Params, shard *ShardWithChunkRefs) (result Params) {
	if shard == nil {
		return base
	}

	result = ParamsWithShardsOverride{
		Params:         base,
		ShardsOverride: Shards{shard.Shard}.Encode(),
	}

	if shard.chunks != nil {
		result = ParamsWithChunkOverrides{
			Params:              result,
			StoreChunksOverride: shard.chunks,
		}
	}

	return result
}

// Sortable logql contain sort or sort_desc.
func Sortable(q Params) (bool, error) {
	var sortable bool
	expr, ok := q.GetExpression().(syntax.SampleExpr)
	if !ok {
		return false, errors.New("only sample expression supported")
	}
	expr.Walk(func(e syntax.Expr) {
		rangeExpr, ok := e.(*syntax.VectorAggregationExpr)
		if !ok {
			return
		}
		if rangeExpr.Operation == syntax.OpTypeSort || rangeExpr.Operation == syntax.OpTypeSortDesc {
			sortable = true
			return
		}
	})
	return sortable, nil
}

// EvaluatorFactory is an interface for iterating over data at different nodes in the AST
type EvaluatorFactory interface {
	SampleEvaluatorFactory
	EntryEvaluatorFactory
}

type SampleEvaluatorFactory interface {
	// NewStepEvaluator returns a NewStepEvaluator for a given SampleExpr. It's explicitly passed another NewStepEvaluator// in order to enable arbitrary computation of embedded expressions. This allows more modular & extensible
	// NewStepEvaluator implementations which can be composed.
	NewStepEvaluator(ctx context.Context, nextEvaluatorFactory SampleEvaluatorFactory, expr syntax.SampleExpr, p Params) (StepEvaluator, error)
}

type SampleEvaluatorFunc func(ctx context.Context, nextEvaluatorFactory SampleEvaluatorFactory, expr syntax.SampleExpr, p Params) (StepEvaluator, error)

func (s SampleEvaluatorFunc) NewStepEvaluator(ctx context.Context, nextEvaluatorFactory SampleEvaluatorFactory, expr syntax.SampleExpr, p Params) (StepEvaluator, error) {
	return s(ctx, nextEvaluatorFactory, expr, p)
}

type EntryEvaluatorFactory interface {
	// NewIterator returns the iter.EntryIterator for a given LogSelectorExpr
	NewIterator(context.Context, syntax.LogSelectorExpr, Params) (iter.EntryIterator, error)
}

// EvaluatorUnsupportedType is a helper for signaling that an evaluator does not support an Expr type
func EvaluatorUnsupportedType(expr syntax.Expr, ev EvaluatorFactory) error {
	return errors.Errorf("unexpected expr type (%T) for Evaluator type (%T) ", expr, ev)
}

type DefaultEvaluator struct {
	maxLookBackPeriod         time.Duration
	maxCountMinSketchHeapSize int
	querier                   Querier
}

// NewDefaultEvaluator constructs a DefaultEvaluator
func NewDefaultEvaluator(querier Querier, maxLookBackPeriod time.Duration, maxCountMinSketchHeapSize int) *DefaultEvaluator {
	return &DefaultEvaluator{
		querier:                   querier,
		maxLookBackPeriod:         maxLookBackPeriod,
		maxCountMinSketchHeapSize: maxCountMinSketchHeapSize,
	}
}

func (ev *DefaultEvaluator) NewIterator(ctx context.Context, expr syntax.LogSelectorExpr, q Params) (iter.EntryIterator, error) {
	params := SelectLogParams{
		QueryRequest: &logproto.QueryRequest{
			Start:     q.Start(),
			End:       q.End(),
			Limit:     q.Limit(),
			Direction: q.Direction(),
			Selector:  expr.String(),
			Shards:    q.Shards(),
			Plan: &plan.QueryPlan{
				AST: expr,
			},
			StoreChunks: q.GetStoreChunks(),
		},
	}

	if GetRangeType(q) == InstantType {
		params.Start = params.Start.Add(-ev.maxLookBackPeriod)
	}

	return ev.querier.SelectLogs(ctx, params)
}

func (ev *DefaultEvaluator) NewStepEvaluator(
	ctx context.Context,
	nextEvFactory SampleEvaluatorFactory,
	expr syntax.SampleExpr,
	q Params,
) (StepEvaluator, error) {
	switch e := expr.(type) {
	case *syntax.VectorAggregationExpr:
		if rangExpr, ok := e.Left.(*syntax.RangeAggregationExpr); ok && e.Operation == syntax.OpTypeSum {
			// if range expression is wrapped with a vector expression
			// we should send the vector expression for allowing reducing labels at the source.
			nextEvFactory = SampleEvaluatorFunc(func(ctx context.Context, _ SampleEvaluatorFactory, _ syntax.SampleExpr, _ Params) (StepEvaluator, error) {
				it, err := ev.querier.SelectSamples(ctx, SelectSampleParams{
					&logproto.SampleQueryRequest{
						// extend startTs backwards by step
						Start: q.Start().Add(-rangExpr.Left.Interval).Add(-rangExpr.Left.Offset),
						// add leap nanosecond to endTs to include lines exactly at endTs. range iterators work on start exclusive, end inclusive ranges
						End: q.End().Add(-rangExpr.Left.Offset).Add(time.Nanosecond),
						// intentionally send the vector for reducing labels.
						Selector: e.String(),
						Shards:   q.Shards(),
						Plan: &plan.QueryPlan{
							AST: expr,
						},
						StoreChunks: q.GetStoreChunks(),
					},
				})
				if err != nil {
					return nil, err
				}
				return newRangeAggEvaluator(iter.NewPeekingSampleIterator(it), rangExpr, q, rangExpr.Left.Offset)
			})
		}
		return newVectorAggEvaluator(ctx, nextEvFactory, e, q, ev.maxCountMinSketchHeapSize)
	case *syntax.RangeAggregationExpr:
		it, err := ev.querier.SelectSamples(ctx, SelectSampleParams{
			&logproto.SampleQueryRequest{
				// extend startTs backwards by step
				Start: q.Start().Add(-e.Left.Interval).Add(-e.Left.Offset),
				// add leap nanosecond to endTs to include lines exactly at endTs. range iterators work on start exclusive, end inclusive ranges
				End: q.End().Add(-e.Left.Offset).Add(time.Nanosecond),
				// intentionally send the vector for reducing labels.
				Selector: e.String(),
				Shards:   q.Shards(),
				Plan: &plan.QueryPlan{
					AST: expr,
				},
				StoreChunks: q.GetStoreChunks(),
			},
		})
		if err != nil {
			return nil, err
		}
		return newRangeAggEvaluator(iter.NewPeekingSampleIterator(it), e, q, e.Left.Offset)
	case *syntax.BinOpExpr:
		return newBinOpStepEvaluator(ctx, nextEvFactory, e, q)
	case *syntax.LabelReplaceExpr:
		return newLabelReplaceEvaluator(ctx, nextEvFactory, e, q)
	case *syntax.VectorExpr:
		val, err := e.Value()
		if err != nil {
			return nil, err
		}
		return newVectorIterator(val, q.Step().Milliseconds(), q.Start().UnixMilli(), q.End().UnixMilli()), nil
	default:
		return nil, EvaluatorUnsupportedType(e, ev)
	}
}

func newVectorAggEvaluator(
	ctx context.Context,
	evFactory SampleEvaluatorFactory,
	expr *syntax.VectorAggregationExpr,
	q Params,
	maxCountMinSketchHeapSize int,
) (StepEvaluator, error) {
	if expr.Grouping == nil {
		return nil, errors.Errorf("aggregation operator '%q' without grouping", expr.Operation)
	}
	nextEvaluator, err := evFactory.NewStepEvaluator(ctx, evFactory, expr.Left, q)
	if err != nil {
		return nil, err
	}
	sort.Strings(expr.Grouping.Groups)

	if expr.Operation == syntax.OpTypeCountMinSketch {
		return newCountMinSketchVectorAggEvaluator(nextEvaluator, expr, maxCountMinSketchHeapSize)
	}

	return &VectorAggEvaluator{
		nextEvaluator: nextEvaluator,
		expr:          expr,
		buf:           make([]byte, 0, 1024),
		lb:            labels.NewBuilder(nil),
	}, nil
}

type VectorAggEvaluator struct {
	nextEvaluator StepEvaluator
	expr          *syntax.VectorAggregationExpr
	buf           []byte
	lb            *labels.Builder
}

func (e *VectorAggEvaluator) Next() (bool, int64, StepResult) {
	next, ts, r := e.nextEvaluator.Next()

	if !next {
		return false, 0, SampleVector{}
	}
	vec := r.SampleVector()
	result := map[uint64]*groupedAggregation{}
	if e.expr.Operation == syntax.OpTypeTopK || e.expr.Operation == syntax.OpTypeBottomK {
		if e.expr.Params < 1 {
			return next, ts, SampleVector{}
		}
	}
	for _, s := range vec {
		metric := s.Metric

		var groupingKey uint64
		if e.expr.Grouping.Without {
			groupingKey, e.buf = metric.HashWithoutLabels(e.buf, e.expr.Grouping.Groups...)
		} else {
			groupingKey, e.buf = metric.HashForLabels(e.buf, e.expr.Grouping.Groups...)
		}
		group, ok := result[groupingKey]
		// Add a new group if it doesn't exist.
		if !ok {
			var m labels.Labels

			if e.expr.Grouping.Without {
				e.lb.Reset(metric)
				e.lb.Del(e.expr.Grouping.Groups...)
				e.lb.Del(labels.MetricName)
				m = e.lb.Labels()
			} else {
				m = make(labels.Labels, 0, len(e.expr.Grouping.Groups))
				for _, l := range metric {
					for _, n := range e.expr.Grouping.Groups {
						if l.Name == n {
							m = append(m, l)
							break
						}
					}
				}
				sort.Sort(m)
			}
			result[groupingKey] = &groupedAggregation{
				labels:     m,
				value:      s.F,
				mean:       s.F,
				groupCount: 1,
			}

			inputVecLen := len(vec)
			resultSize := e.expr.Params
			if e.expr.Params > inputVecLen {
				resultSize = inputVecLen
			}
			if e.expr.Operation == syntax.OpTypeStdvar || e.expr.Operation == syntax.OpTypeStddev {
				result[groupingKey].value = 0.0
			} else if e.expr.Operation == syntax.OpTypeTopK {
				result[groupingKey].heap = make(vectorByValueHeap, 0, resultSize)
				heap.Push(&result[groupingKey].heap, &promql.Sample{
					F:      s.F,
					Metric: s.Metric,
				})
			} else if e.expr.Operation == syntax.OpTypeBottomK {
				result[groupingKey].reverseHeap = make(vectorByReverseValueHeap, 0, resultSize)
				heap.Push(&result[groupingKey].reverseHeap, &promql.Sample{
					F:      s.F,
					Metric: s.Metric,
				})
			} else if e.expr.Operation == syntax.OpTypeSortDesc {
				result[groupingKey].heap = make(vectorByValueHeap, 0)
				heap.Push(&result[groupingKey].heap, &promql.Sample{
					F:      s.F,
					Metric: s.Metric,
				})
			} else if e.expr.Operation == syntax.OpTypeSort {
				result[groupingKey].reverseHeap = make(vectorByReverseValueHeap, 0)
				heap.Push(&result[groupingKey].reverseHeap, &promql.Sample{
					F:      s.F,
					Metric: s.Metric,
				})
			}
			continue
		}
		switch e.expr.Operation {
		case syntax.OpTypeSum:
			group.value += s.F

		case syntax.OpTypeAvg:
			group.groupCount++
			group.mean += (s.F - group.mean) / float64(group.groupCount)

		case syntax.OpTypeMax:
			if group.value < s.F || math.IsNaN(group.value) {
				group.value = s.F
			}

		case syntax.OpTypeMin:
			if group.value > s.F || math.IsNaN(group.value) {
				group.value = s.F
			}

		case syntax.OpTypeCount:
			group.groupCount++

		case syntax.OpTypeStddev, syntax.OpTypeStdvar:
			group.groupCount++
			delta := s.F - group.mean
			group.mean += delta / float64(group.groupCount)
			group.value += delta * (s.F - group.mean)

		case syntax.OpTypeTopK:
			if len(group.heap) < e.expr.Params || group.heap[0].F < s.F || math.IsNaN(group.heap[0].F) {
				if len(group.heap) == e.expr.Params {
					heap.Pop(&group.heap)
				}
				heap.Push(&group.heap, &promql.Sample{
					F:      s.F,
					Metric: s.Metric,
				})
			}

		case syntax.OpTypeBottomK:
			if len(group.reverseHeap) < e.expr.Params || group.reverseHeap[0].F > s.F || math.IsNaN(group.reverseHeap[0].F) {
				if len(group.reverseHeap) == e.expr.Params {
					heap.Pop(&group.reverseHeap)
				}
				heap.Push(&group.reverseHeap, &promql.Sample{
					F:      s.F,
					Metric: s.Metric,
				})
			}
		case syntax.OpTypeSortDesc:
			heap.Push(&group.heap, &promql.Sample{
				F:      s.F,
				Metric: s.Metric,
			})
		case syntax.OpTypeSort:
			heap.Push(&group.reverseHeap, &promql.Sample{
				F:      s.F,
				Metric: s.Metric,
			})
		default:
			panic(errors.Errorf("expected aggregation operator but got %q", e.expr.Operation))
		}
	}
	vec = vec[:0]
	for _, aggr := range result {
		switch e.expr.Operation {
		case syntax.OpTypeAvg:
			aggr.value = aggr.mean

		case syntax.OpTypeCount:
			aggr.value = float64(aggr.groupCount)

		case syntax.OpTypeStddev:
			aggr.value = math.Sqrt(aggr.value / float64(aggr.groupCount))

		case syntax.OpTypeStdvar:
			aggr.value = aggr.value / float64(aggr.groupCount)

		case syntax.OpTypeTopK, syntax.OpTypeSortDesc:
			// The heap keeps the lowest value on top, so reverse it.
			sort.Sort(sort.Reverse(aggr.heap))
			for _, v := range aggr.heap {
				vec = append(vec, promql.Sample{
					Metric: v.Metric,
					T:      ts,
					F:      v.F,
				})
			}
			continue // Bypass default append.

		case syntax.OpTypeBottomK, syntax.OpTypeSort:
			// The heap keeps the lowest value on top, so reverse it.
			sort.Sort(sort.Reverse(aggr.reverseHeap))
			for _, v := range aggr.reverseHeap {
				vec = append(vec, promql.Sample{
					Metric: v.Metric,
					T:      ts,
					F:      v.F,
				})
			}
			continue // Bypass default append.
		default:
		}
		vec = append(vec, promql.Sample{
			Metric: aggr.labels,
			T:      ts,
			F:      aggr.value,
		})
	}
	return next, ts, SampleVector(vec)
}

func (e *VectorAggEvaluator) Close() error {
	return e.nextEvaluator.Close()
}

func (e *VectorAggEvaluator) Error() error {
	return e.nextEvaluator.Error()
}

func newRangeAggEvaluator(
	it iter.PeekingSampleIterator,
	expr *syntax.RangeAggregationExpr,
	q Params,
	o time.Duration,
) (StepEvaluator, error) {
	switch expr.Operation {
	case syntax.OpRangeTypeAbsent:
		iter, err := newRangeVectorIterator(
			it, expr,
			expr.Left.Interval.Nanoseconds(),
			q.Step().Nanoseconds(),
			q.Start().UnixNano(), q.End().UnixNano(), o.Nanoseconds(),
		)
		if err != nil {
			return nil, err
		}

		absentLabels, err := absentLabels(expr)
		if err != nil {
			return nil, err
		}
		return &AbsentRangeVectorEvaluator{
			iter: iter,
			lbs:  absentLabels,
		}, nil
	case syntax.OpRangeTypeQuantileSketch:
		iter := newQuantileSketchIterator(
			it,
			expr.Left.Interval.Nanoseconds(),
			q.Step().Nanoseconds(),
			q.Start().UnixNano(), q.End().UnixNano(), o.Nanoseconds(),
		)

		return &QuantileSketchStepEvaluator{
			iter: iter,
		}, nil
	case syntax.OpRangeTypeFirstWithTimestamp:
		iter := newFirstWithTimestampIterator(
			it,
			expr.Left.Interval.Nanoseconds(),
			q.Step().Nanoseconds(),
			q.Start().UnixNano(), q.End().UnixNano(), o.Nanoseconds(),
		)

		return &RangeVectorEvaluator{
			iter: iter,
		}, nil
	case syntax.OpRangeTypeLastWithTimestamp:
		iter := newLastWithTimestampIterator(
			it,
			expr.Left.Interval.Nanoseconds(),
			q.Step().Nanoseconds(),
			q.Start().UnixNano(), q.End().UnixNano(), o.Nanoseconds(),
		)

		return &RangeVectorEvaluator{
			iter: iter,
		}, nil
	default:
		iter, err := newRangeVectorIterator(
			it, expr,
			expr.Left.Interval.Nanoseconds(),
			q.Step().Nanoseconds(),
			q.Start().UnixNano(), q.End().UnixNano(), o.Nanoseconds(),
		)
		if err != nil {
			return nil, err
		}

		return &RangeVectorEvaluator{
			iter: iter,
		}, nil
	}
}

type RangeVectorEvaluator struct {
	iter RangeVectorIterator

	err error
}

func (r *RangeVectorEvaluator) Next() (bool, int64, StepResult) {
	next := r.iter.Next()
	if !next {
		return false, 0, SampleVector{}
	}
	ts, vec := r.iter.At()
	for _, s := range vec.SampleVector() {
		// Errors are not allowed in metrics unless they've been specifically requested.
		if s.Metric.Has(logqlmodel.ErrorLabel) && s.Metric.Get(logqlmodel.PreserveErrorLabel) != trueString {
			r.err = logqlmodel.NewPipelineErr(s.Metric)
			return false, 0, SampleVector{}
		}
	}
	return true, ts, vec
}

func (r *RangeVectorEvaluator) Close() error { return r.iter.Close() }

func (r *RangeVectorEvaluator) Error() error {
	if r.err != nil {
		return r.err
	}
	return r.iter.Error()
}

type AbsentRangeVectorEvaluator struct {
	iter RangeVectorIterator
	lbs  labels.Labels

	err error
}

func (r *AbsentRangeVectorEvaluator) Next() (bool, int64, StepResult) {
	next := r.iter.Next()
	if !next {
		return false, 0, SampleVector{}
	}
	ts, vec := r.iter.At()
	for _, s := range vec.SampleVector() {
		// Errors are not allowed in metrics unless they've been specifically requested.
		if s.Metric.Has(logqlmodel.ErrorLabel) && s.Metric.Get(logqlmodel.PreserveErrorLabel) != trueString {
			r.err = logqlmodel.NewPipelineErr(s.Metric)
			return false, 0, SampleVector{}
		}
	}
	if len(vec.SampleVector()) > 0 {
		return next, ts, SampleVector{}
	}
	// values are missing.
	return next, ts, SampleVector{
		promql.Sample{
			T:      ts,
			F:      1.,
			Metric: r.lbs,
		},
	}
}

func (r AbsentRangeVectorEvaluator) Close() error { return r.iter.Close() }

func (r AbsentRangeVectorEvaluator) Error() error {
	if r.err != nil {
		return r.err
	}
	return r.iter.Error()
}

// newBinOpStepEvaluator explicitly does not handle when both legs are literals as
// it makes the type system simpler and these are reduced in mustNewBinOpExpr
func newBinOpStepEvaluator(
	ctx context.Context,
	evFactory SampleEvaluatorFactory,
	expr *syntax.BinOpExpr,
	q Params,
) (StepEvaluator, error) {
	// first check if either side is a literal
	leftLit, lOk := expr.SampleExpr.(*syntax.LiteralExpr)
	rightLit, rOk := expr.RHS.(*syntax.LiteralExpr)

	// match a literal expr with all labels in the other leg
	if lOk {
		rhs, err := evFactory.NewStepEvaluator(ctx, evFactory, expr.RHS, q)
		if err != nil {
			return nil, err
		}
		return newLiteralStepEvaluator(
			expr.Op,
			leftLit,
			rhs,
			false,
			expr.Opts.ReturnBool,
		)
	}
	if rOk {
		lhs, err := evFactory.NewStepEvaluator(ctx, evFactory, expr.SampleExpr, q)
		if err != nil {
			return nil, err
		}
		return newLiteralStepEvaluator(
			expr.Op,
			rightLit,
			lhs,
			true,
			expr.Opts.ReturnBool,
		)
	}

	var lse, rse StepEvaluator

	ctx, cancel := context.WithCancelCause(ctx)
	g := errgroup.Group{}

	// We have two non-literal legs,
	// load them in parallel
	g.Go(func() error {
		var err error
		lse, err = evFactory.NewStepEvaluator(ctx, evFactory, expr.SampleExpr, q)
		if err != nil {
			cancel(fmt.Errorf("new step evaluator for left leg errored: %w", err))
		}
		return err
	})
	g.Go(func() error {
		var err error
		rse, err = evFactory.NewStepEvaluator(ctx, evFactory, expr.RHS, q)
		if err != nil {
			cancel(fmt.Errorf("new step evaluator for right leg errored: %w", err))
		}
		return err
	})

	// ensure both sides are loaded before returning the combined evaluator
	if err := g.Wait(); err != nil {
		return nil, err
	}

	return &BinOpStepEvaluator{
		rse:  rse,
		lse:  lse,
		expr: expr,
	}, nil
}

type BinOpStepEvaluator struct {
	rse     StepEvaluator
	lse     StepEvaluator
	expr    *syntax.BinOpExpr
	lastErr error
}

func (e *BinOpStepEvaluator) Next() (bool, int64, StepResult) {
	var (
		ts       int64
		next     bool
		lhs, rhs promql.Vector
		r        StepResult
	)
	next, ts, r = e.rse.Next()
	// These should _always_ happen at the same step on each evaluator.
	if !next {
		return next, ts, nil
	}
	rhs = r.SampleVector()
	// build matching signature for each sample in right vector
	rsigs := make([]uint64, len(rhs))
	for i, sample := range rhs {
		rsigs[i] = matchingSignature(sample, e.expr.Opts)
	}

	next, ts, r = e.lse.Next()
	if !next {
		return next, ts, nil
	}
	lhs = r.SampleVector()
	// build matching signature for each sample in left vector
	lsigs := make([]uint64, len(lhs))
	for i, sample := range lhs {
		lsigs[i] = matchingSignature(sample, e.expr.Opts)
	}

	var results promql.Vector
	switch e.expr.Op {
	case syntax.OpTypeAnd:
		results = vectorAnd(lhs, rhs, lsigs, rsigs)
	case syntax.OpTypeOr:
		results = vectorOr(lhs, rhs, lsigs, rsigs)
	case syntax.OpTypeUnless:
		results = vectorUnless(lhs, rhs, lsigs, rsigs)
	default:
		results, e.lastErr = vectorBinop(e.expr.Op, e.expr.Opts, lhs, rhs, lsigs, rsigs)
	}
	return true, ts, SampleVector(results)
}

func (e *BinOpStepEvaluator) Close() (lastError error) {
	for _, ev := range []StepEvaluator{e.lse, e.rse} {
		if err := ev.Close(); err != nil {
			lastError = err
		}
	}
	return lastError
}

func (e *BinOpStepEvaluator) Error() error {
	var errs []error
	if e.lastErr != nil {
		errs = append(errs, e.lastErr)
	}
	for _, ev := range []StepEvaluator{e.lse, e.rse} {
		if err := ev.Error(); err != nil {
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

func matchingSignature(sample promql.Sample, opts *syntax.BinOpOptions) uint64 {
	if opts == nil || opts.VectorMatching == nil {
		return sample.Metric.Hash()
	} else if opts.VectorMatching.On {
		return labels.NewBuilder(sample.Metric).Keep(opts.VectorMatching.MatchingLabels...).Labels().Hash()
	}

	return labels.NewBuilder(sample.Metric).Del(opts.VectorMatching.MatchingLabels...).Labels().Hash()
}

func vectorBinop(op string, opts *syntax.BinOpOptions, lhs, rhs promql.Vector, lsigs, rsigs []uint64) (promql.Vector, error) {
	// handle one-to-one or many-to-one matching
	// for one-to-many, swap
	if opts != nil && opts.VectorMatching.Card == syntax.CardOneToMany {
		lhs, rhs = rhs, lhs
		lsigs, rsigs = rsigs, lsigs
	}
	rightSigs := make(map[uint64]*promql.Sample)
	matchedSigs := make(map[uint64]map[uint64]struct{})
	results := make(promql.Vector, 0)

	// Add all rhs samples to a map, so we can easily find matches later.
	for i, sample := range rhs {
		sig := rsigs[i]
		if rightSigs[sig] != nil {
			side := "right"
			if opts.VectorMatching.Card == syntax.CardOneToMany {
				side = "left"
			}
			return nil, fmt.Errorf("found duplicate series on the %s hand-side"+
				";many-to-many matching not allowed: matching labels must be unique on one side", side)
		}
		rightSigs[sig] = &promql.Sample{
			Metric: sample.Metric,
			T:      sample.T,
			F:      sample.F,
		}
	}

	for i, sample := range lhs {
		ls := &sample
		sig := lsigs[i]
		rs, found := rightSigs[sig] // Look for a match in the rhs Vector.
		if !found {
			continue
		}

		metric := resultMetric(ls.Metric, rs.Metric, opts)
		insertedSigs, exists := matchedSigs[sig]
		filter := true
		if opts != nil {
			if opts.VectorMatching.Card == syntax.CardOneToOne {
				if exists {
					return nil, errors.New("multiple matches for labels: many-to-one matching must be explicit (group_left/group_right)")
				}
				matchedSigs[sig] = nil
			} else {
				insertSig := metric.Hash()
				if !exists {
					insertedSigs = map[uint64]struct{}{}
					matchedSigs[sig] = insertedSigs
				} else if _, duplicate := insertedSigs[insertSig]; duplicate {
					return nil, errors.New("multiple matches for labels: grouping labels must ensure unique matches")
				}
				insertedSigs[insertSig] = struct{}{}
			}
			// merge
			if opts.ReturnBool {
				filter = false
			}
			// swap back before apply binary operator
			if opts.VectorMatching.Card == syntax.CardOneToMany {
				ls, rs = rs, ls
			}
		}
		merged, err := syntax.MergeBinOp(op, ls, rs, false, filter, syntax.IsComparisonOperator(op))
		if err != nil {
			return nil, err
		}
		if merged != nil {
			// replace with labels specified by expr
			merged.Metric = metric
			results = append(results, *merged)
		}
	}
	return results, nil
}

func vectorAnd(lhs, rhs promql.Vector, lsigs, rsigs []uint64) promql.Vector {
	if len(lhs) == 0 || len(rhs) == 0 {
		return nil // Short-circuit: AND with nothing is nothing.
	}

	rightSigs := make(map[uint64]struct{})
	results := make(promql.Vector, 0)

	for _, sig := range rsigs {
		rightSigs[sig] = struct{}{}
	}
	for i, ls := range lhs {
		if _, ok := rightSigs[lsigs[i]]; ok {
			results = append(results, ls)
		}
	}
	return results
}

func vectorOr(lhs, rhs promql.Vector, lsigs, rsigs []uint64) promql.Vector {
	if len(lhs) == 0 {
		return rhs
	} else if len(rhs) == 0 {
		return lhs
	}

	leftSigs := make(map[uint64]struct{})
	results := make(promql.Vector, 0)

	for i, ls := range lhs {
		leftSigs[lsigs[i]] = struct{}{}
		results = append(results, ls)
	}
	for i, rs := range rhs {
		if _, ok := leftSigs[rsigs[i]]; !ok {
			results = append(results, rs)
		}
	}
	return results
}

func vectorUnless(lhs, rhs promql.Vector, lsigs, rsigs []uint64) promql.Vector {
	if len(lhs) == 0 || len(rhs) == 0 {
		return lhs
	}

	rightSigs := make(map[uint64]struct{})
	results := make(promql.Vector, 0)

	for _, sig := range rsigs {
		rightSigs[sig] = struct{}{}
	}

	for i, ls := range lhs {
		if _, ok := rightSigs[lsigs[i]]; !ok {
			results = append(results, ls)
		}
	}
	return results
}

// resultMetric returns the metric for the given sample(s) based on the Vector
// binary operation and the matching options.
func resultMetric(lhs, rhs labels.Labels, opts *syntax.BinOpOptions) labels.Labels {
	lb := labels.NewBuilder(lhs)

	if opts != nil {
		matching := opts.VectorMatching
		if matching.Card == syntax.CardOneToOne {
			if matching.On {
			Outer:
				for _, l := range lhs {
					for _, n := range matching.MatchingLabels {
						if l.Name == n {
							continue Outer
						}
					}
					lb.Del(l.Name)
				}
			} else {
				lb.Del(matching.MatchingLabels...)
			}
		}
		for _, ln := range matching.Include {
			// Included labels from the `group_x` modifier are taken from the "one"-side.
			if v := rhs.Get(ln); v != "" {
				lb.Set(ln, v)
			} else {
				lb.Del(ln)
			}
		}
	}

	return lb.Labels()
}

// newLiteralStepEvaluator merges a literal with a StepEvaluator. Since order matters in
// non-commutative operations, inverted should be true when the literalExpr is not the left argument.
func newLiteralStepEvaluator(
	op string,
	lit *syntax.LiteralExpr,
	nextEv StepEvaluator,
	inverted bool,
	returnBool bool,
) (*LiteralStepEvaluator, error) {
	val, err := lit.Value()
	if err != nil {
		return nil, err
	}

	return &LiteralStepEvaluator{
		nextEv:     nextEv,
		val:        val,
		inverted:   inverted,
		op:         op,
		returnBool: returnBool,
	}, nil
}

type LiteralStepEvaluator struct {
	nextEv     StepEvaluator
	mergeErr   error
	val        float64
	inverted   bool
	op         string
	returnBool bool
}

func (e *LiteralStepEvaluator) Next() (bool, int64, StepResult) {
	ok, ts, r := e.nextEv.Next()
	if !ok {
		return ok, ts, r
	}
	vec := r.SampleVector()
	results := make(promql.Vector, 0, len(vec))
	for _, sample := range vec {

		literalPoint := promql.Sample{
			Metric: sample.Metric,
			T:      ts,
			F:      e.val,
		}

		left, right := &literalPoint, &sample
		if e.inverted {
			left, right = right, left
		}
		merged, err := syntax.MergeBinOp(
			e.op,
			left,
			right,
			!e.inverted,
			!e.returnBool,
			syntax.IsComparisonOperator(e.op),
		)
		if err != nil {
			e.mergeErr = err
			return false, 0, nil
		}
		if merged != nil {
			results = append(results, *merged)
		}
	}

	return ok, ts, SampleVector(results)
}

func (e *LiteralStepEvaluator) Close() error {
	return e.nextEv.Close()
}

func (e *LiteralStepEvaluator) Error() error {
	if e.mergeErr != nil {
		return e.mergeErr
	}
	return e.nextEv.Error()
}

// VectorIterator return simple vector like (1).
type VectorIterator struct {
	stepMs, endMs, currentMs int64
	val                      float64
}

func newVectorIterator(val float64,
	stepMs, startMs, endMs int64,
) *VectorIterator {
	if stepMs == 0 {
		stepMs = 1
	}
	return &VectorIterator{
		val:       val,
		stepMs:    stepMs,
		endMs:     endMs,
		currentMs: startMs - stepMs,
	}
}

func (r *VectorIterator) Next() (bool, int64, StepResult) {
	r.currentMs = r.currentMs + r.stepMs
	if r.currentMs > r.endMs {
		return false, 0, nil
	}
	results := make(promql.Vector, 0)
	vectorPoint := promql.Sample{T: r.currentMs, F: r.val}
	results = append(results, vectorPoint)
	return true, r.currentMs, SampleVector(results)
}

func (r *VectorIterator) Close() error {
	return nil
}

func (r *VectorIterator) Error() error {
	return nil
}

// newLabelReplaceEvaluator
func newLabelReplaceEvaluator(
	ctx context.Context,
	evFactory SampleEvaluatorFactory,
	expr *syntax.LabelReplaceExpr,
	q Params,
) (*LabelReplaceEvaluator, error) {
	nextEvaluator, err := evFactory.NewStepEvaluator(ctx, evFactory, expr.Left, q)
	if err != nil {
		return nil, err
	}

	return &LabelReplaceEvaluator{
		nextEvaluator: nextEvaluator,
		expr:          expr,
		buf:           make([]byte, 0, 1024),
	}, nil
}

type LabelReplaceEvaluator struct {
	nextEvaluator StepEvaluator
	labelCache    map[uint64]labels.Labels
	expr          *syntax.LabelReplaceExpr
	buf           []byte
}

func (e *LabelReplaceEvaluator) Next() (bool, int64, StepResult) {
	next, ts, r := e.nextEvaluator.Next()
	if !next {
		return false, 0, SampleVector{}
	}
	vec := r.SampleVector()
	if e.labelCache == nil {
		e.labelCache = make(map[uint64]labels.Labels, len(vec))
	}
	var hash uint64
	for i, s := range vec {
		hash, e.buf = s.Metric.HashWithoutLabels(e.buf)
		if labels, ok := e.labelCache[hash]; ok {
			vec[i].Metric = labels
			continue
		}
		src := s.Metric.Get(e.expr.Src)
		indexes := e.expr.Re.FindStringSubmatchIndex(src)
		if indexes == nil {
			// If there is no match, no replacement should take place.
			e.labelCache[hash] = s.Metric
			continue
		}
		res := e.expr.Re.ExpandString([]byte{}, e.expr.Replacement, src, indexes)

		lb := labels.NewBuilder(s.Metric).Del(e.expr.Dst)
		if len(res) > 0 {
			lb.Set(e.expr.Dst, string(res))
		}
		outLbs := lb.Labels()
		e.labelCache[hash] = outLbs
		vec[i].Metric = outLbs
	}
	return next, ts, SampleVector(vec)
}

func (e *LabelReplaceEvaluator) Close() error {
	return e.nextEvaluator.Close()
}

func (e *LabelReplaceEvaluator) Error() error {
	return e.nextEvaluator.Error()
}

// This is to replace missing timeseries during absent_over_time aggregation.
func absentLabels(expr syntax.SampleExpr) (labels.Labels, error) {
	m := labels.Labels{}

	selector, err := expr.Selector()
	if err != nil {
		return nil, err
	}
	lm := selector.Matchers()
	if len(lm) == 0 {
		return m, nil
	}

	empty := []string{}
	for _, ma := range lm {
		if ma.Name == labels.MetricName {
			continue
		}
		if ma.Type == labels.MatchEqual && !m.Has(ma.Name) {
			m = labels.NewBuilder(m).Set(ma.Name, ma.Value).Labels()
		} else {
			empty = append(empty, ma.Name)
		}
	}

	for _, v := range empty {
		m = labels.NewBuilder(m).Del(v).Labels()
	}
	return m, nil
}
