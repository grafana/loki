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

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/util"
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
	qs             string
	start, end     time.Time
	step, interval time.Duration
	direction      logproto.Direction
	limit          uint32
	shards         []string
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

// Sortable logql contain sort or sort_desc.
func Sortable(q Params) (bool, error) {
	var sortable bool
	expr, err := syntax.ParseSampleExpr(q.Query())
	if err != nil {
		return false, err
	}
	expr.Walk(func(e interface{}) {
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

// Evaluator is an interface for iterating over data at different nodes in the AST
type Evaluator interface {
	SampleEvaluator
	EntryEvaluator
}

type SampleEvaluator interface {
	// StepEvaluator returns a StepEvaluator for a given SampleExpr. It's explicitly passed another StepEvaluator// in order to enable arbitrary computation of embedded expressions. This allows more modular & extensible
	// StepEvaluator implementations which can be composed.
	StepEvaluator(ctx context.Context, nextEvaluator SampleEvaluator, expr syntax.SampleExpr, p Params) (StepEvaluator, error)
}

type SampleEvaluatorFunc func(ctx context.Context, nextEvaluator SampleEvaluator, expr syntax.SampleExpr, p Params) (StepEvaluator, error)

func (s SampleEvaluatorFunc) StepEvaluator(ctx context.Context, nextEvaluator SampleEvaluator, expr syntax.SampleExpr, p Params) (StepEvaluator, error) {
	return s(ctx, nextEvaluator, expr, p)
}

type EntryEvaluator interface {
	// Iterator returns the iter.EntryIterator for a given LogSelectorExpr
	Iterator(context.Context, syntax.LogSelectorExpr, Params) (iter.EntryIterator, error)
}

// EvaluatorUnsupportedType is a helper for signaling that an evaluator does not support an Expr type
func EvaluatorUnsupportedType(expr syntax.Expr, ev Evaluator) error {
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

func (ev *DefaultEvaluator) Iterator(ctx context.Context, expr syntax.LogSelectorExpr, q Params) (iter.EntryIterator, error) {
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
	expr syntax.SampleExpr,
	q Params,
) (StepEvaluator, error) {
	switch e := expr.(type) {
	case *syntax.VectorAggregationExpr:
		if rangExpr, ok := e.Left.(*syntax.RangeAggregationExpr); ok && e.Operation == syntax.OpTypeSum {
			// if range expression is wrapped with a vector expression
			// we should send the vector expression for allowing reducing labels at the source.
			nextEv = SampleEvaluatorFunc(func(ctx context.Context, _ SampleEvaluator, _ syntax.SampleExpr, _ Params) (StepEvaluator, error) {
				it, err := ev.querier.SelectSamples(ctx, SelectSampleParams{
					&logproto.SampleQueryRequest{
						Start:    q.Start().Add(-rangExpr.Left.Interval).Add(-rangExpr.Left.Offset),
						End:      q.End().Add(-rangExpr.Left.Offset),
						Selector: e.String(), // intentionally send the vector for reducing labels.
						Shards:   q.Shards(),
					},
				})
				if err != nil {
					return nil, err
				}
				return newRangeAggEvaluator(iter.NewPeekingSampleIterator(it), rangExpr, q, rangExpr.Left.Offset)
			})
		}
		return newVectorAggEvaluator(ctx, nextEv, e, q)
	case *syntax.RangeAggregationExpr:
		it, err := ev.querier.SelectSamples(ctx, SelectSampleParams{
			&logproto.SampleQueryRequest{
				Start:    q.Start().Add(-e.Left.Interval).Add(-e.Left.Offset),
				End:      q.End().Add(-e.Left.Offset),
				Selector: expr.String(),
				Shards:   q.Shards(),
			},
		})
		if err != nil {
			return nil, err
		}
		return newRangeAggEvaluator(iter.NewPeekingSampleIterator(it), e, q, e.Left.Offset)
	case *syntax.BinOpExpr:
		return binOpStepEvaluator(ctx, nextEv, e, q)
	case *syntax.LabelReplaceExpr:
		return labelReplaceEvaluator(ctx, nextEv, e, q)
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
	ev SampleEvaluator,
	expr *syntax.VectorAggregationExpr,
	q Params,
) (StepEvaluator, error) {
	if expr.Grouping == nil {
		return nil, errors.Errorf("aggregation operator '%q' without grouping", expr.Operation)
	}
	nextEvaluator, err := ev.StepEvaluator(ctx, ev, expr.Left, q)
	if err != nil {
		return nil, err
	}
	lb := labels.NewBuilder(nil)
	buf := make([]byte, 0, 1024)
	sort.Strings(expr.Grouping.Groups)
	return NewStepEvaluator(func() (bool, int64, promql.Vector) {
		next, ts, vec := nextEvaluator.Next()

		if !next {
			return false, 0, promql.Vector{}
		}
		result := map[uint64]*groupedAggregation{}
		if expr.Operation == syntax.OpTypeTopK || expr.Operation == syntax.OpTypeBottomK {
			if expr.Params < 1 {
				return next, ts, promql.Vector{}
			}
		}
		for _, s := range vec {
			metric := s.Metric

			var groupingKey uint64
			if expr.Grouping.Without {
				groupingKey, buf = metric.HashWithoutLabels(buf, expr.Grouping.Groups...)
			} else {
				groupingKey, buf = metric.HashForLabels(buf, expr.Grouping.Groups...)
			}
			group, ok := result[groupingKey]
			// Add a new group if it doesn't exist.
			if !ok {
				var m labels.Labels

				if expr.Grouping.Without {
					lb.Reset(metric)
					lb.Del(expr.Grouping.Groups...)
					lb.Del(labels.MetricName)
					m = lb.Labels()
				} else {
					m = make(labels.Labels, 0, len(expr.Grouping.Groups))
					for _, l := range metric {
						for _, n := range expr.Grouping.Groups {
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
				resultSize := expr.Params
				if expr.Params > inputVecLen {
					resultSize = inputVecLen
				}
				if expr.Operation == syntax.OpTypeStdvar || expr.Operation == syntax.OpTypeStddev {
					result[groupingKey].value = 0.0
				} else if expr.Operation == syntax.OpTypeTopK {
					result[groupingKey].heap = make(vectorByValueHeap, 0, resultSize)
					heap.Push(&result[groupingKey].heap, &promql.Sample{
						F:      s.F,
						Metric: s.Metric,
					})
				} else if expr.Operation == syntax.OpTypeBottomK {
					result[groupingKey].reverseHeap = make(vectorByReverseValueHeap, 0, resultSize)
					heap.Push(&result[groupingKey].reverseHeap, &promql.Sample{
						F:      s.F,
						Metric: s.Metric,
					})
				} else if expr.Operation == syntax.OpTypeSortDesc {
					result[groupingKey].heap = make(vectorByValueHeap, 0)
					heap.Push(&result[groupingKey].heap, &promql.Sample{
						F:      s.F,
						Metric: s.Metric,
					})
				} else if expr.Operation == syntax.OpTypeSort {
					result[groupingKey].reverseHeap = make(vectorByReverseValueHeap, 0)
					heap.Push(&result[groupingKey].reverseHeap, &promql.Sample{
						F:      s.F,
						Metric: s.Metric,
					})
				}
				continue
			}
			switch expr.Operation {
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
				if len(group.heap) < expr.Params || group.heap[0].F < s.F || math.IsNaN(group.heap[0].F) {
					if len(group.heap) == expr.Params {
						heap.Pop(&group.heap)
					}
					heap.Push(&group.heap, &promql.Sample{
						F:      s.F,
						Metric: s.Metric,
					})
				}

			case syntax.OpTypeBottomK:
				if len(group.reverseHeap) < expr.Params || group.reverseHeap[0].F > s.F || math.IsNaN(group.reverseHeap[0].F) {
					if len(group.reverseHeap) == expr.Params {
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
				panic(errors.Errorf("expected aggregation operator but got %q", expr.Operation))
			}
		}
		vec = vec[:0]
		for _, aggr := range result {
			switch expr.Operation {
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
		return next, ts, vec
	}, nextEvaluator.Close, nextEvaluator.Error)
}

func newRangeAggEvaluator(
	it iter.PeekingSampleIterator,
	expr *syntax.RangeAggregationExpr,
	q Params,
	o time.Duration,
) (StepEvaluator, error) {
	iter, err := newRangeVectorIterator(
		it, expr,
		expr.Left.Interval.Nanoseconds(),
		q.Step().Nanoseconds(),
		q.Start().UnixNano(), q.End().UnixNano(), o.Nanoseconds(),
	)
	if err != nil {
		return nil, err
	}
	if expr.Operation == syntax.OpRangeTypeAbsent {
		absentLabels, err := absentLabels(expr)
		if err != nil {
			return nil, err
		}
		return &absentRangeVectorEvaluator{
			iter: iter,
			lbs:  absentLabels,
		}, nil
	}
	return &rangeVectorEvaluator{
		iter: iter,
	}, nil
}

type rangeVectorEvaluator struct {
	iter RangeVectorIterator

	err error
}

func (r *rangeVectorEvaluator) Next() (bool, int64, promql.Vector) {
	next := r.iter.Next()
	if !next {
		return false, 0, promql.Vector{}
	}
	ts, vec := r.iter.At()
	for _, s := range vec {
		// Errors are not allowed in metrics unless they've been specifically requested.
		if s.Metric.Has(logqlmodel.ErrorLabel) && s.Metric.Get(logqlmodel.PreserveErrorLabel) != "true" {
			r.err = logqlmodel.NewPipelineErr(s.Metric)
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
	ts, vec := r.iter.At()
	for _, s := range vec {
		// Errors are not allowed in metrics unless they've been specifically requested.
		if s.Metric.Has(logqlmodel.ErrorLabel) && s.Metric.Get(logqlmodel.PreserveErrorLabel) != "true" {
			r.err = logqlmodel.NewPipelineErr(s.Metric)
			return false, 0, promql.Vector{}
		}
	}
	if len(vec) > 0 {
		return next, ts, promql.Vector{}
	}
	// values are missing.
	return next, ts, promql.Vector{
		promql.Sample{
			T:      ts,
			F:      1.,
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
	expr *syntax.BinOpExpr,
	q Params,
) (StepEvaluator, error) {
	// first check if either side is a literal
	leftLit, lOk := expr.SampleExpr.(*syntax.LiteralExpr)
	rightLit, rOk := expr.RHS.(*syntax.LiteralExpr)

	// match a literal expr with all labels in the other leg
	if lOk {
		rhs, err := ev.StepEvaluator(ctx, ev, expr.RHS, q)
		if err != nil {
			return nil, err
		}
		return literalStepEvaluator(
			expr.Op,
			leftLit,
			rhs,
			false,
			expr.Opts.ReturnBool,
		)
	}
	if rOk {
		lhs, err := ev.StepEvaluator(ctx, ev, expr.SampleExpr, q)
		if err != nil {
			return nil, err
		}
		return literalStepEvaluator(
			expr.Op,
			rightLit,
			lhs,
			true,
			expr.Opts.ReturnBool,
		)
	}

	var lse, rse StepEvaluator

	ctx, cancel := context.WithCancel(ctx)
	g := errgroup.Group{}

	// We have two non-literal legs,
	// load them in parallel
	g.Go(func() error {
		var err error
		lse, err = ev.StepEvaluator(ctx, ev, expr.SampleExpr, q)
		if err != nil {
			cancel()
		}
		return err
	})
	g.Go(func() error {
		var err error
		rse, err = ev.StepEvaluator(ctx, ev, expr.RHS, q)
		if err != nil {
			cancel()
		}
		return err
	})

	// ensure both sides are loaded before returning the combined evaluator
	if err := g.Wait(); err != nil {
		return nil, err
	}

	// keep a scoped reference to err as it's referenced in the Error()
	// implementation of this StepEvaluator
	var scopedErr error

	return NewStepEvaluator(func() (bool, int64, promql.Vector) {
		var (
			ts       int64
			next     bool
			lhs, rhs promql.Vector
		)
		next, ts, rhs = rse.Next()
		// These should _always_ happen at the same step on each evaluator.
		if !next {
			return next, ts, nil
		}
		// build matching signature for each sample in right vector
		rsigs := make([]uint64, len(rhs))
		for i, sample := range rhs {
			rsigs[i] = matchingSignature(sample, expr.Opts)
		}

		next, ts, lhs = lse.Next()
		if !next {
			return next, ts, nil
		}
		// build matching signature for each sample in left vector
		lsigs := make([]uint64, len(lhs))
		for i, sample := range lhs {
			lsigs[i] = matchingSignature(sample, expr.Opts)
		}

		var results promql.Vector
		switch expr.Op {
		case syntax.OpTypeAnd:
			results = vectorAnd(lhs, rhs, lsigs, rsigs)
		case syntax.OpTypeOr:
			results = vectorOr(lhs, rhs, lsigs, rsigs)
		case syntax.OpTypeUnless:
			results = vectorUnless(lhs, rhs, lsigs, rsigs)
		default:
			results, scopedErr = vectorBinop(expr.Op, expr.Opts, lhs, rhs, lsigs, rsigs)
		}
		return true, ts, results
	}, func() (lastError error) {
		for _, ev := range []StepEvaluator{lse, rse} {
			if err := ev.Close(); err != nil {
				lastError = err
			}
		}
		return lastError
	}, func() error {
		var errs []error
		if scopedErr != nil {
			errs = append(errs, scopedErr)
		}
		for _, ev := range []StepEvaluator{lse, rse} {
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
	})
}

func matchingSignature(sample promql.Sample, opts *syntax.BinOpOptions) uint64 {
	if opts == nil || opts.VectorMatching == nil {
		return sample.Metric.Hash()
	} else if opts.VectorMatching.On {
		return labels.NewBuilder(sample.Metric).Keep(opts.VectorMatching.MatchingLabels...).Labels().Hash()
	} else {
		return labels.NewBuilder(sample.Metric).Del(opts.VectorMatching.MatchingLabels...).Labels().Hash()
	}
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
		merged, err := syntax.MergeBinOp(op, ls, rs, filter, syntax.IsComparisonOperator(op))
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

// literalStepEvaluator merges a literal with a StepEvaluator. Since order matters in
// non-commutative operations, inverted should be true when the literalExpr is not the left argument.
func literalStepEvaluator(
	op string,
	lit *syntax.LiteralExpr,
	eval StepEvaluator,
	inverted bool,
	returnBool bool,
) (StepEvaluator, error) {
	val, err := lit.Value()
	if err != nil {
		return nil, err
	}
	var mergeErr error

	return NewStepEvaluator(
		func() (bool, int64, promql.Vector) {
			ok, ts, vec := eval.Next()
			results := make(promql.Vector, 0, len(vec))
			for _, sample := range vec {

				literalPoint := promql.Sample{
					Metric: sample.Metric,
					T:      ts,
					F:      val,
				}

				left, right := &literalPoint, &sample
				if inverted {
					left, right = right, left
				}
				merged, err := syntax.MergeBinOp(
					op,
					left,
					right,
					!returnBool,
					syntax.IsComparisonOperator(op),
				)
				if err != nil {
					mergeErr = err
					return false, 0, nil
				}
				if merged != nil {
					results = append(results, *merged)
				}
			}

			return ok, ts, results
		},
		eval.Close,
		func() error {
			if mergeErr != nil {
				return mergeErr
			}
			return eval.Error()
		},
	)
}

// vectorIterator return simple vector like (1).
type vectorIterator struct {
	stepMs, endMs, currentMs int64
	val                      float64
}

func newVectorIterator(val float64,
	stepMs, startMs, endMs int64) *vectorIterator {
	if stepMs == 0 {
		stepMs = 1
	}
	return &vectorIterator{
		val:       val,
		stepMs:    stepMs,
		endMs:     endMs,
		currentMs: startMs - stepMs,
	}
}

func (r *vectorIterator) Next() (bool, int64, promql.Vector) {
	r.currentMs = r.currentMs + r.stepMs
	if r.currentMs > r.endMs {
		return false, 0, nil
	}
	results := make(promql.Vector, 0)
	vectorPoint := promql.Sample{T: r.currentMs, F: r.val}
	results = append(results, vectorPoint)
	return true, r.currentMs, results
}

func (r *vectorIterator) Close() error {
	return nil
}

func (r *vectorIterator) Error() error {
	return nil
}

// labelReplaceEvaluator
func labelReplaceEvaluator(
	ctx context.Context,
	ev SampleEvaluator,
	expr *syntax.LabelReplaceExpr,
	q Params,
) (StepEvaluator, error) {
	nextEvaluator, err := ev.StepEvaluator(ctx, ev, expr.Left, q)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 0, 1024)
	var labelCache map[uint64]labels.Labels
	return NewStepEvaluator(func() (bool, int64, promql.Vector) {
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
			src := s.Metric.Get(expr.Src)
			indexes := expr.Re.FindStringSubmatchIndex(src)
			if indexes == nil {
				// If there is no match, no replacement should take place.
				labelCache[hash] = s.Metric
				continue
			}
			res := expr.Re.ExpandString([]byte{}, expr.Replacement, src, indexes)

			lb := labels.NewBuilder(s.Metric).Del(expr.Dst)
			if len(res) > 0 {
				lb.Set(expr.Dst, string(res))
			}
			outLbs := lb.Labels()
			labelCache[hash] = outLbs
			vec[i].Metric = outLbs
		}
		return next, ts, vec
	}, nextEvaluator.Close, nextEvaluator.Error)
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
