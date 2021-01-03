package logql

import (
	"container/heap"
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/log"
)

type QueryRangeType string

var (
	InstantType QueryRangeType = "instant"
	RangeType   QueryRangeType = "range"
)

// Params details the parameters associated with a loki request
type Params interface {
	Query() string
	Start() time.Time
	End() time.Time
	Step() time.Duration
	Interval() time.Duration
	Limit() uint32
	Direction() logproto.Direction
	Shards() []string
}

func NewLiteralParams(
	qs string,
	start, end time.Time,
	step, interval time.Duration,
	direction logproto.Direction,
	limit uint32,
	shards []string,
) LiteralParams {
	return LiteralParams{
		qs:        qs,
		start:     start,
		end:       end,
		step:      step,
		interval:  interval,
		direction: direction,
		limit:     limit,
		shards:    shards,
	}
}

// LiteralParams impls Params
type LiteralParams struct {
	qs         string
	start, end time.Time
	step       time.Duration
	interval   time.Duration
	direction  logproto.Direction
	limit      uint32
	shards     []string
}

func (p LiteralParams) Copy() LiteralParams { return p }

// String impls Params
func (p LiteralParams) Query() string { return p.qs }

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

// GetRangeType returns whether a query is an instant query or range query
func GetRangeType(q Params) QueryRangeType {
	if q.Start() == q.End() && q.Step() == 0 {
		return InstantType
	}
	return RangeType
}

// Evaluator is an interface for iterating over data at different nodes in the AST
type Evaluator interface {
	SampleEvaluator
	EntryEvaluator
}

type SampleEvaluator interface {
	// StepEvaluator returns a StepEvaluator for a given SampleExpr. It's explicitly passed another StepEvaluator// in order to enable arbitrary computation of embedded expressions. This allows more modular & extensible
	// StepEvaluator implementations which can be composed.
	StepEvaluator(ctx context.Context, nextEvaluator SampleEvaluator, expr SampleExpr, p Params) (StepEvaluator, error)
}

type SampleEvaluatorFunc func(ctx context.Context, nextEvaluator SampleEvaluator, expr SampleExpr, p Params) (StepEvaluator, error)

func (s SampleEvaluatorFunc) StepEvaluator(ctx context.Context, nextEvaluator SampleEvaluator, expr SampleExpr, p Params) (StepEvaluator, error) {
	return s(ctx, nextEvaluator, expr, p)
}

type EntryEvaluator interface {
	// Iterator returns the iter.EntryIterator for a given LogSelectorExpr
	Iterator(context.Context, LogSelectorExpr, Params) (iter.EntryIterator, error)
}

// EvaluatorUnsupportedType is a helper for signaling that an evaluator does not support an Expr type
func EvaluatorUnsupportedType(expr Expr, ev Evaluator) error {
	return errors.Errorf("unexpected expr type (%T) for Evaluator type (%T) ", expr, ev)
}

type DefaultEvaluator struct {
	maxLookBackPeriod time.Duration
	querier           Querier
}

// NewDefaultEvaluator constructs a DefaultEvaluator
func NewDefaultEvaluator(querier Querier, maxLookBackPeriod time.Duration) *DefaultEvaluator {
	return &DefaultEvaluator{
		querier:           querier,
		maxLookBackPeriod: maxLookBackPeriod,
	}

}

func (ev *DefaultEvaluator) Iterator(ctx context.Context, expr LogSelectorExpr, q Params) (iter.EntryIterator, error) {
	params := SelectLogParams{
		QueryRequest: &logproto.QueryRequest{
			Start:     q.Start(),
			End:       q.End(),
			Limit:     q.Limit(),
			Direction: q.Direction(),
			Selector:  expr.String(),
			Shards:    q.Shards(),
		},
	}

	if GetRangeType(q) == InstantType {
		params.Start = params.Start.Add(-ev.maxLookBackPeriod)
	}

	return ev.querier.SelectLogs(ctx, params)

}

func (ev *DefaultEvaluator) StepEvaluator(
	ctx context.Context,
	nextEv SampleEvaluator,
	expr SampleExpr,
	q Params,
) (StepEvaluator, error) {
	switch e := expr.(type) {
	case *vectorAggregationExpr:
		if rangExpr, ok := e.left.(*rangeAggregationExpr); ok && e.operation == OpTypeSum {
			// if range expression is wrapped with a vector expression
			// we should send the vector expression for allowing reducing labels at the source.
			nextEv = SampleEvaluatorFunc(func(ctx context.Context, nextEvaluator SampleEvaluator, expr SampleExpr, p Params) (StepEvaluator, error) {
				it, err := ev.querier.SelectSamples(ctx, SelectSampleParams{
					&logproto.SampleQueryRequest{
						Start:    q.Start().Add(-rangExpr.left.interval),
						End:      q.End(),
						Selector: e.String(), // intentionally send the the vector for reducing labels.
						Shards:   q.Shards(),
					},
				})
				if err != nil {
					return nil, err
				}
				return rangeAggEvaluator(iter.NewPeekingSampleIterator(it), rangExpr, q)
			})

		}
		return vectorAggEvaluator(ctx, nextEv, e, q)
	case *rangeAggregationExpr:
		it, err := ev.querier.SelectSamples(ctx, SelectSampleParams{
			&logproto.SampleQueryRequest{
				Start:    q.Start().Add(-e.left.interval),
				End:      q.End(),
				Selector: expr.String(),
				Shards:   q.Shards(),
			},
		})
		if err != nil {
			return nil, err
		}
		return rangeAggEvaluator(iter.NewPeekingSampleIterator(it), e, q)
	case *binOpExpr:
		return binOpStepEvaluator(ctx, nextEv, e, q)
	case *labelReplaceExpr:
		return labelReplaceEvaluator(ctx, nextEv, e, q)
	default:
		return nil, EvaluatorUnsupportedType(e, ev)
	}
}

func vectorAggEvaluator(
	ctx context.Context,
	ev SampleEvaluator,
	expr *vectorAggregationExpr,
	q Params,
) (StepEvaluator, error) {
	nextEvaluator, err := ev.StepEvaluator(ctx, ev, expr.left, q)
	if err != nil {
		return nil, err
	}
	lb := labels.NewBuilder(nil)
	buf := make([]byte, 0, 1024)
	sort.Strings(expr.grouping.groups)
	return newStepEvaluator(func() (bool, int64, promql.Vector) {
		next, ts, vec := nextEvaluator.Next()

		if !next {
			return false, 0, promql.Vector{}
		}
		result := map[uint64]*groupedAggregation{}
		if expr.operation == OpTypeTopK || expr.operation == OpTypeBottomK {
			if expr.params < 1 {
				return next, ts, promql.Vector{}
			}

		}
		for _, s := range vec {
			metric := s.Metric

			var (
				groupingKey uint64
			)
			if expr.grouping.without {
				groupingKey, buf = metric.HashWithoutLabels(buf, expr.grouping.groups...)
			} else {
				groupingKey, buf = metric.HashForLabels(buf, expr.grouping.groups...)
			}
			group, ok := result[groupingKey]
			// Add a new group if it doesn't exist.
			if !ok {
				var m labels.Labels

				if expr.grouping.without {
					lb.Reset(metric)
					lb.Del(expr.grouping.groups...)
					lb.Del(labels.MetricName)
					m = lb.Labels()
				} else {
					m = make(labels.Labels, 0, len(expr.grouping.groups))
					for _, l := range metric {
						for _, n := range expr.grouping.groups {
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
					value:      s.V,
					mean:       s.V,
					groupCount: 1,
				}

				inputVecLen := len(vec)
				resultSize := expr.params
				if expr.params > inputVecLen {
					resultSize = inputVecLen
				}
				if expr.operation == OpTypeStdvar || expr.operation == OpTypeStddev {
					result[groupingKey].value = 0.0
				} else if expr.operation == OpTypeTopK {
					result[groupingKey].heap = make(vectorByValueHeap, 0, resultSize)
					heap.Push(&result[groupingKey].heap, &promql.Sample{
						Point:  promql.Point{V: s.V},
						Metric: s.Metric,
					})
				} else if expr.operation == OpTypeBottomK {
					result[groupingKey].reverseHeap = make(vectorByReverseValueHeap, 0, resultSize)
					heap.Push(&result[groupingKey].reverseHeap, &promql.Sample{
						Point:  promql.Point{V: s.V},
						Metric: s.Metric,
					})
				}
				continue
			}
			switch expr.operation {
			case OpTypeSum:
				group.value += s.V

			case OpTypeAvg:
				group.groupCount++
				group.mean += (s.V - group.mean) / float64(group.groupCount)

			case OpTypeMax:
				if group.value < s.V || math.IsNaN(group.value) {
					group.value = s.V
				}

			case OpTypeMin:
				if group.value > s.V || math.IsNaN(group.value) {
					group.value = s.V
				}

			case OpTypeCount:
				group.groupCount++

			case OpTypeStddev, OpTypeStdvar:
				group.groupCount++
				delta := s.V - group.mean
				group.mean += delta / float64(group.groupCount)
				group.value += delta * (s.V - group.mean)

			case OpTypeTopK:
				if len(group.heap) < expr.params || group.heap[0].V < s.V || math.IsNaN(group.heap[0].V) {
					if len(group.heap) == expr.params {
						heap.Pop(&group.heap)
					}
					heap.Push(&group.heap, &promql.Sample{
						Point:  promql.Point{V: s.V},
						Metric: s.Metric,
					})
				}

			case OpTypeBottomK:
				if len(group.reverseHeap) < expr.params || group.reverseHeap[0].V > s.V || math.IsNaN(group.reverseHeap[0].V) {
					if len(group.reverseHeap) == expr.params {
						heap.Pop(&group.reverseHeap)
					}
					heap.Push(&group.reverseHeap, &promql.Sample{
						Point:  promql.Point{V: s.V},
						Metric: s.Metric,
					})
				}
			default:
				panic(errors.Errorf("expected aggregation operator but got %q", expr.operation))
			}
		}
		vec = vec[:0]
		for _, aggr := range result {
			switch expr.operation {
			case OpTypeAvg:
				aggr.value = aggr.mean

			case OpTypeCount:
				aggr.value = float64(aggr.groupCount)

			case OpTypeStddev:
				aggr.value = math.Sqrt(aggr.value / float64(aggr.groupCount))

			case OpTypeStdvar:
				aggr.value = aggr.value / float64(aggr.groupCount)

			case OpTypeTopK:
				// The heap keeps the lowest value on top, so reverse it.
				sort.Sort(sort.Reverse(aggr.heap))
				for _, v := range aggr.heap {
					vec = append(vec, promql.Sample{
						Metric: v.Metric,
						Point: promql.Point{
							T: ts,
							V: v.V,
						},
					})
				}
				continue // Bypass default append.

			case OpTypeBottomK:
				// The heap keeps the lowest value on top, so reverse it.
				sort.Sort(sort.Reverse(aggr.reverseHeap))
				for _, v := range aggr.reverseHeap {
					vec = append(vec, promql.Sample{
						Metric: v.Metric,
						Point: promql.Point{
							T: ts,
							V: v.V,
						},
					})
				}
				continue // Bypass default append.
			default:
			}
			vec = append(vec, promql.Sample{
				Metric: aggr.labels,
				Point: promql.Point{
					T: ts,
					V: aggr.value,
				},
			})
		}
		return next, ts, vec

	}, nextEvaluator.Close, nextEvaluator.Error)
}

func rangeAggEvaluator(
	it iter.PeekingSampleIterator,
	expr *rangeAggregationExpr,
	q Params,
) (StepEvaluator, error) {
	agg, err := expr.aggregator()
	if err != nil {
		return nil, err
	}
	iter := newRangeVectorIterator(
		it,
		expr.left.interval.Nanoseconds(),
		q.Step().Nanoseconds(),
		q.Start().UnixNano(), q.End().UnixNano(),
	)
	if expr.operation == OpRangeTypeAbsent {
		return &absentRangeVectorEvaluator{
			iter: iter,
			lbs:  absentLabels(expr),
		}, nil
	}
	return &rangeVectorEvaluator{
		iter: iter,
		agg:  agg,
	}, nil
}

type rangeVectorEvaluator struct {
	agg  RangeVectorAggregator
	iter RangeVectorIterator

	err error
}

func (r *rangeVectorEvaluator) Next() (bool, int64, promql.Vector) {
	next := r.iter.Next()
	if !next {
		return false, 0, promql.Vector{}
	}
	ts, vec := r.iter.At(r.agg)
	for _, s := range vec {
		// Errors are not allowed in metrics.
		if s.Metric.Has(log.ErrorLabel) {
			r.err = newPipelineErr(s.Metric)
			return false, 0, promql.Vector{}
		}
	}
	return true, ts, vec
}

func (r rangeVectorEvaluator) Close() error { return r.iter.Close() }

func (r rangeVectorEvaluator) Error() error {
	if r.err != nil {
		return r.err
	}
	return r.iter.Error()
}

type absentRangeVectorEvaluator struct {
	iter RangeVectorIterator
	lbs  labels.Labels

	err error
}

func (r *absentRangeVectorEvaluator) Next() (bool, int64, promql.Vector) {
	next := r.iter.Next()
	if !next {
		return false, 0, promql.Vector{}
	}
	ts, vec := r.iter.At(one)
	for _, s := range vec {
		// Errors are not allowed in metrics.
		if s.Metric.Has(log.ErrorLabel) {
			r.err = newPipelineErr(s.Metric)
			return false, 0, promql.Vector{}
		}
	}
	if len(vec) > 0 {
		return next, ts, promql.Vector{}
	}
	// values are missing.
	return next, ts, promql.Vector{
		promql.Sample{
			Point: promql.Point{
				T: ts,
				V: 1.,
			},
			Metric: r.lbs,
		},
	}
}

func (r absentRangeVectorEvaluator) Close() error { return r.iter.Close() }

func (r absentRangeVectorEvaluator) Error() error {
	if r.err != nil {
		return r.err
	}
	return r.iter.Error()
}

// binOpExpr explicitly does not handle when both legs are literals as
// it makes the type system simpler and these are reduced in mustNewBinOpExpr
func binOpStepEvaluator(
	ctx context.Context,
	ev SampleEvaluator,
	expr *binOpExpr,
	q Params,
) (StepEvaluator, error) {
	// first check if either side is a literal
	leftLit, lOk := expr.SampleExpr.(*literalExpr)
	rightLit, rOk := expr.RHS.(*literalExpr)

	// match a literal expr with all labels in the other leg
	if lOk {
		rhs, err := ev.StepEvaluator(ctx, ev, expr.RHS, q)
		if err != nil {
			return nil, err
		}
		return literalStepEvaluator(
			expr.op,
			leftLit,
			rhs,
			false,
			expr.opts.ReturnBool,
		)
	}
	if rOk {
		lhs, err := ev.StepEvaluator(ctx, ev, expr.SampleExpr, q)
		if err != nil {
			return nil, err
		}
		return literalStepEvaluator(
			expr.op,
			rightLit,
			lhs,
			true,
			expr.opts.ReturnBool,
		)
	}

	// we have two non literal legs
	lhs, err := ev.StepEvaluator(ctx, ev, expr.SampleExpr, q)
	if err != nil {
		return nil, err
	}
	rhs, err := ev.StepEvaluator(ctx, ev, expr.RHS, q)
	if err != nil {
		return nil, err
	}

	return newStepEvaluator(func() (bool, int64, promql.Vector) {
		pairs := map[uint64][2]*promql.Sample{}
		var ts int64

		// populate pairs
		for i, eval := range []StepEvaluator{lhs, rhs} {
			next, timestamp, vec := eval.Next()

			ts = timestamp

			// These should _always_ happen at the same step on each evaluator.
			if !next {
				return next, ts, nil
			}

			for _, sample := range vec {
				// TODO(owen-d): this seems wildly inefficient: we're calculating
				// the hash on each sample & step per evaluator.
				// We seem limited to this approach due to using the StepEvaluator ifc.
				hash := sample.Metric.Hash()
				pair := pairs[hash]
				pair[i] = &promql.Sample{
					Metric: sample.Metric,
					Point:  sample.Point,
				}
				pairs[hash] = pair
			}
		}

		results := make(promql.Vector, 0, len(pairs))
		for _, pair := range pairs {

			// merge
			if merged := mergeBinOp(expr.op, pair[0], pair[1], !expr.opts.ReturnBool, IsComparisonOperator(expr.op)); merged != nil {
				results = append(results, *merged)
			}
		}

		return true, ts, results
	}, func() (lastError error) {
		for _, ev := range []StepEvaluator{lhs, rhs} {
			if err := ev.Close(); err != nil {
				lastError = err
			}
		}
		return lastError
	}, func() error {
		var errs []error
		for _, ev := range []StepEvaluator{lhs, rhs} {
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
			return fmt.Errorf("Multiple errors: %+v", errs)
		}
	})
}

func mergeBinOp(op string, left, right *promql.Sample, filter, isVectorComparison bool) *promql.Sample {
	var merger func(left, right *promql.Sample) *promql.Sample

	switch op {
	case OpTypeOr:
		merger = func(left, right *promql.Sample) *promql.Sample {
			// return the left entry found (prefers left hand side)
			if left != nil {
				return left
			}
			return right
		}

	case OpTypeAnd:
		merger = func(left, right *promql.Sample) *promql.Sample {
			// return left sample if there's a second sample for that label set
			if left != nil && right != nil {
				return left
			}
			return nil
		}

	case OpTypeUnless:
		merger = func(left, right *promql.Sample) *promql.Sample {
			// return left sample if there's not a second sample for that label set
			if right == nil {
				return left
			}
			return nil
		}

	case OpTypeAdd:
		merger = func(left, right *promql.Sample) *promql.Sample {
			if left == nil || right == nil {
				return nil
			}
			res := promql.Sample{
				Metric: left.Metric,
				Point:  left.Point,
			}
			res.Point.V += right.Point.V
			return &res
		}

	case OpTypeSub:
		merger = func(left, right *promql.Sample) *promql.Sample {
			if left == nil || right == nil {
				return nil
			}
			res := promql.Sample{
				Metric: left.Metric,
				Point:  left.Point,
			}
			res.Point.V -= right.Point.V
			return &res
		}

	case OpTypeMul:
		merger = func(left, right *promql.Sample) *promql.Sample {
			if left == nil || right == nil {
				return nil
			}
			res := promql.Sample{
				Metric: left.Metric,
				Point:  left.Point,
			}
			res.Point.V *= right.Point.V
			return &res
		}

	case OpTypeDiv:
		merger = func(left, right *promql.Sample) *promql.Sample {
			if left == nil || right == nil {
				return nil
			}
			res := promql.Sample{
				Metric: left.Metric.Copy(),
				Point:  left.Point,
			}

			// guard against divide by zero
			if right.Point.V == 0 {
				res.Point.V = math.NaN()
			} else {
				res.Point.V /= right.Point.V
			}
			return &res
		}

	case OpTypeMod:
		merger = func(left, right *promql.Sample) *promql.Sample {
			if left == nil || right == nil {
				return nil
			}
			res := promql.Sample{
				Metric: left.Metric,
				Point:  left.Point,
			}
			// guard against divide by zero
			if right.Point.V == 0 {
				res.Point.V = math.NaN()
			} else {
				res.Point.V = math.Mod(res.Point.V, right.Point.V)
			}
			return &res
		}

	case OpTypePow:
		merger = func(left, right *promql.Sample) *promql.Sample {
			if left == nil || right == nil {
				return nil
			}

			res := promql.Sample{
				Metric: left.Metric,
				Point:  left.Point,
			}
			res.Point.V = math.Pow(left.Point.V, right.Point.V)
			return &res
		}

	case OpTypeCmpEQ:
		merger = func(left, right *promql.Sample) *promql.Sample {
			if left == nil || right == nil {
				return nil
			}

			res := &promql.Sample{
				Metric: left.Metric,
				Point:  left.Point,
			}

			val := 0.
			if left.Point.V == right.Point.V {
				val = 1.
			} else if filter {
				return nil
			}
			res.Point.V = val
			return res
		}

	case OpTypeNEQ:
		merger = func(left, right *promql.Sample) *promql.Sample {
			if left == nil || right == nil {
				return nil
			}

			res := &promql.Sample{
				Metric: left.Metric,
				Point:  left.Point,
			}

			val := 0.
			if left.Point.V != right.Point.V {
				val = 1.
			} else if filter {
				return nil
			}
			res.Point.V = val
			return res
		}

	case OpTypeGT:
		merger = func(left, right *promql.Sample) *promql.Sample {
			if left == nil || right == nil {
				return nil
			}

			res := &promql.Sample{
				Metric: left.Metric,
				Point:  left.Point,
			}

			val := 0.
			if left.Point.V > right.Point.V {
				val = 1.
			} else if filter {
				return nil
			}
			res.Point.V = val
			return res
		}

	case OpTypeGTE:
		merger = func(left, right *promql.Sample) *promql.Sample {
			if left == nil || right == nil {
				return nil
			}

			res := &promql.Sample{
				Metric: left.Metric,
				Point:  left.Point,
			}

			val := 0.
			if left.Point.V >= right.Point.V {
				val = 1.
			} else if filter {
				return nil
			}
			res.Point.V = val
			return res
		}

	case OpTypeLT:
		merger = func(left, right *promql.Sample) *promql.Sample {
			if left == nil || right == nil {
				return nil
			}

			res := &promql.Sample{
				Metric: left.Metric,
				Point:  left.Point,
			}

			val := 0.
			if left.Point.V < right.Point.V {
				val = 1.
			} else if filter {
				return nil
			}
			res.Point.V = val
			return res
		}

	case OpTypeLTE:
		merger = func(left, right *promql.Sample) *promql.Sample {
			if left == nil || right == nil {
				return nil
			}

			res := &promql.Sample{
				Metric: left.Metric,
				Point:  left.Point,
			}

			val := 0.
			if left.Point.V <= right.Point.V {
				val = 1.
			} else if filter {
				return nil
			}
			res.Point.V = val
			return res
		}

	default:
		panic(errors.Errorf("should never happen: unexpected operation: (%s)", op))
	}

	res := merger(left, right)
	if !isVectorComparison {
		return res
	}

	if filter {
		// if a filter-enabled vector-wise comparison has returned non-nil,
		// ensure we return the left hand side's value (2) instead of the
		// comparison operator's result (1: the truthy answer)
		if res != nil {
			return left
		}

		// otherwise it's been filtered out
		return res
	}

	// This only leaves vector comparisons which are not filters.
	// If we could not find a match but we have a left node to compare, create an entry with a 0 value.
	// This can occur when we don't find a matching label set in the vectors.
	if res == nil && left != nil && right == nil {
		res = &promql.Sample{
			Metric: left.Metric,
			Point:  left.Point,
		}
		res.Point.V = 0
	}
	return res

}

// literalStepEvaluator merges a literal with a StepEvaluator. Since order matters in
// non commutative operations, inverted should be true when the literalExpr is not the left argument.
func literalStepEvaluator(
	op string,
	lit *literalExpr,
	eval StepEvaluator,
	inverted bool,
	returnBool bool,
) (StepEvaluator, error) {
	return newStepEvaluator(
		func() (bool, int64, promql.Vector) {
			ok, ts, vec := eval.Next()

			results := make(promql.Vector, 0, len(vec))
			for _, sample := range vec {
				literalPoint := promql.Sample{
					Metric: sample.Metric,
					Point:  promql.Point{T: ts, V: lit.value},
				}

				left, right := &literalPoint, &sample
				if inverted {
					left, right = right, left
				}

				if merged := mergeBinOp(
					op,
					left,
					right,
					!returnBool,
					IsComparisonOperator(op),
				); merged != nil {
					results = append(results, *merged)
				}
			}

			return ok, ts, results
		},
		eval.Close,
		eval.Error,
	)
}

func labelReplaceEvaluator(
	ctx context.Context,
	ev SampleEvaluator,
	expr *labelReplaceExpr,
	q Params,
) (StepEvaluator, error) {
	nextEvaluator, err := ev.StepEvaluator(ctx, ev, expr.left, q)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 0, 1024)
	var labelCache map[uint64]labels.Labels
	return newStepEvaluator(func() (bool, int64, promql.Vector) {
		next, ts, vec := nextEvaluator.Next()
		if !next {
			return false, 0, promql.Vector{}
		}
		if labelCache == nil {
			labelCache = make(map[uint64]labels.Labels, len(vec))
		}
		var hash uint64
		for i, s := range vec {
			hash, buf = s.Metric.HashWithoutLabels(buf)
			if labels, ok := labelCache[hash]; ok {
				vec[i].Metric = labels
				continue
			}
			src := s.Metric.Get(expr.src)
			indexes := expr.re.FindStringSubmatchIndex(src)
			if indexes == nil {
				// If there is no match, no replacement should take place.
				labelCache[hash] = s.Metric
				continue
			}
			res := expr.re.ExpandString([]byte{}, expr.replacement, src, indexes)

			lb := labels.NewBuilder(s.Metric).Del(expr.dst)
			if len(res) > 0 {
				lb.Set(expr.dst, string(res))
			}
			outLbs := lb.Labels()
			labelCache[hash] = outLbs
			vec[i].Metric = outLbs
		}
		return next, ts, vec
	}, nextEvaluator.Close, nextEvaluator.Error)
}

// This is to replace missing timeseries during absent_over_time aggregation.
func absentLabels(expr SampleExpr) labels.Labels {
	m := labels.Labels{}

	lm := expr.Selector().Matchers()
	if len(lm) == 0 {
		return m
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
	return m
}
