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
	case *VectorAggregationExpr:
		if rangExpr, ok := e.Left.(*RangeAggregationExpr); ok && e.Operation == OpTypeSum {
			// if range expression is wrapped with a vector expression
			// we should send the vector expression for allowing reducing labels at the source.
			nextEv = SampleEvaluatorFunc(func(ctx context.Context, nextEvaluator SampleEvaluator, expr SampleExpr, p Params) (StepEvaluator, error) {
				it, err := ev.querier.SelectSamples(ctx, SelectSampleParams{
					&logproto.SampleQueryRequest{
						Start:    q.Start().Add(-rangExpr.Left.Interval).Add(-rangExpr.Left.Offset),
						End:      q.End().Add(-rangExpr.Left.Offset),
						Selector: e.String(), // intentionally send the the vector for reducing labels.
						Shards:   q.Shards(),
					},
				})
				if err != nil {
					return nil, err
				}
				return rangeAggEvaluator(iter.NewPeekingSampleIterator(it), rangExpr, q, rangExpr.Left.Offset)
			})
		}
		return vectorAggEvaluator(ctx, nextEv, e, q)
	case *RangeAggregationExpr:
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
		return rangeAggEvaluator(iter.NewPeekingSampleIterator(it), e, q, e.Left.Offset)
	case *BinOpExpr:
		return binOpStepEvaluator(ctx, nextEv, e, q)
	case *LabelReplaceExpr:
		return labelReplaceEvaluator(ctx, nextEv, e, q)
	default:
		return nil, EvaluatorUnsupportedType(e, ev)
	}
}

func vectorAggEvaluator(
	ctx context.Context,
	ev SampleEvaluator,
	expr *VectorAggregationExpr,
	q Params,
) (StepEvaluator, error) {
	nextEvaluator, err := ev.StepEvaluator(ctx, ev, expr.Left, q)
	if err != nil {
		return nil, err
	}
	lb := labels.NewBuilder(nil)
	buf := make([]byte, 0, 1024)
	sort.Strings(expr.Grouping.Groups)
	return newStepEvaluator(func() (bool, int64, promql.Vector) {
		next, ts, vec := nextEvaluator.Next()

		if !next {
			return false, 0, promql.Vector{}
		}
		result := map[uint64]*groupedAggregation{}
		if expr.Operation == OpTypeTopK || expr.Operation == OpTypeBottomK {
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
					value:      s.V,
					mean:       s.V,
					groupCount: 1,
				}

				inputVecLen := len(vec)
				resultSize := expr.Params
				if expr.Params > inputVecLen {
					resultSize = inputVecLen
				}
				if expr.Operation == OpTypeStdvar || expr.Operation == OpTypeStddev {
					result[groupingKey].value = 0.0
				} else if expr.Operation == OpTypeTopK {
					result[groupingKey].heap = make(vectorByValueHeap, 0, resultSize)
					heap.Push(&result[groupingKey].heap, &promql.Sample{
						Point:  promql.Point{V: s.V},
						Metric: s.Metric,
					})
				} else if expr.Operation == OpTypeBottomK {
					result[groupingKey].reverseHeap = make(vectorByReverseValueHeap, 0, resultSize)
					heap.Push(&result[groupingKey].reverseHeap, &promql.Sample{
						Point:  promql.Point{V: s.V},
						Metric: s.Metric,
					})
				}
				continue
			}
			switch expr.Operation {
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
				if len(group.heap) < expr.Params || group.heap[0].V < s.V || math.IsNaN(group.heap[0].V) {
					if len(group.heap) == expr.Params {
						heap.Pop(&group.heap)
					}
					heap.Push(&group.heap, &promql.Sample{
						Point:  promql.Point{V: s.V},
						Metric: s.Metric,
					})
				}

			case OpTypeBottomK:
				if len(group.reverseHeap) < expr.Params || group.reverseHeap[0].V > s.V || math.IsNaN(group.reverseHeap[0].V) {
					if len(group.reverseHeap) == expr.Params {
						heap.Pop(&group.reverseHeap)
					}
					heap.Push(&group.reverseHeap, &promql.Sample{
						Point:  promql.Point{V: s.V},
						Metric: s.Metric,
					})
				}
			default:
				panic(errors.Errorf("expected aggregation operator but got %q", expr.Operation))
			}
		}
		vec = vec[:0]
		for _, aggr := range result {
			switch expr.Operation {
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
	expr *RangeAggregationExpr,
	q Params,
	o time.Duration,
) (StepEvaluator, error) {
	agg, err := expr.aggregator()
	if err != nil {
		return nil, err
	}
	iter := newRangeVectorIterator(
		it,
		expr.Left.Interval.Nanoseconds(),
		q.Step().Nanoseconds(),
		q.Start().UnixNano(), q.End().UnixNano(), o.Nanoseconds(),
	)
	if expr.Operation == OpRangeTypeAbsent {
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
		if s.Metric.Has(logqlmodel.ErrorLabel) {
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
	ts, vec := r.iter.At(one)
	for _, s := range vec {
		// Errors are not allowed in metrics.
		if s.Metric.Has(logqlmodel.ErrorLabel) {
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
	expr *BinOpExpr,
	q Params,
) (StepEvaluator, error) {
	// first check if either side is a literal
	leftLit, lOk := expr.SampleExpr.(*LiteralExpr)
	rightLit, rOk := expr.RHS.(*LiteralExpr)

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

	// we have two non literal legs
	lse, err := ev.StepEvaluator(ctx, ev, expr.SampleExpr, q)
	if err != nil {
		return nil, err
	}
	rse, err := ev.StepEvaluator(ctx, ev, expr.RHS, q)
	if err != nil {
		return nil, err
	}

	return newStepEvaluator(func() (bool, int64, promql.Vector) {
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
		case OpTypeAnd:
			results = vectorAnd(lhs, rhs, lsigs, rsigs)
		case OpTypeOr:
			results = vectorOr(lhs, rhs, lsigs, rsigs)
		case OpTypeUnless:
			results = vectorUnless(lhs, rhs, lsigs, rsigs)
		default:
			results, err = vectorBinop(expr.Op, expr.Opts, lhs, rhs, lsigs, rsigs)
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
		if err != nil {
			errs = append(errs, err)
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

func matchingSignature(sample promql.Sample, opts *BinOpOptions) uint64 {
	if opts == nil || opts.VectorMatching == nil {
		return sample.Metric.Hash()
	} else if opts.VectorMatching.On {
		return sample.Metric.WithLabels(opts.VectorMatching.MatchingLabels...).Hash()
	} else {
		return sample.Metric.WithoutLabels(opts.VectorMatching.MatchingLabels...).Hash()
	}
}

func vectorBinop(op string, opts *BinOpOptions, lhs, rhs promql.Vector, lsigs, rsigs []uint64) (promql.Vector, error) {
	// handle one-to-one or many-to-one matching
	//for one-to-many, swap
	if opts != nil && opts.VectorMatching.Card == CardOneToMany {
		lhs, rhs = rhs, lhs
		lsigs, rsigs = rsigs, lsigs
	}
	rightSigs := make(map[uint64]*promql.Sample)
	matchedSigs := make(map[uint64]map[uint64]struct{})
	results := make(promql.Vector, 0)

	// Add all rhs samples to a map so we can easily find matches later.
	for i, sample := range rhs {
		sig := rsigs[i]
		if rightSigs[sig] != nil {
			side := "right"
			if opts.VectorMatching.Card == CardOneToMany {
				side = "left"
			}
			return nil, fmt.Errorf("found duplicate series on the %s hand-side"+
				";many-to-many matching not allowed: matching labels must be unique on one side", side)
		}
		rightSigs[sig] = &promql.Sample{
			Metric: sample.Metric,
			Point:  sample.Point,
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
			if opts.VectorMatching.Card == CardOneToOne {
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
			if opts.VectorMatching.Card == CardOneToMany {
				ls, rs = rs, ls
			}
		}

		if merged := mergeBinOp(op, ls, rs, filter, IsComparisonOperator(op)); merged != nil {
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
func resultMetric(lhs, rhs labels.Labels, opts *BinOpOptions) labels.Labels {
	lb := labels.NewBuilder(lhs)

	if opts != nil {
		matching := opts.VectorMatching
		if matching.Card == CardOneToOne {
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

func mergeBinOp(op string, left, right *promql.Sample, filter, isVectorComparison bool) *promql.Sample {
	var merger func(left, right *promql.Sample) *promql.Sample

	switch op {
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
				Metric: left.Metric,
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
	}
	return res
}

// literalStepEvaluator merges a literal with a StepEvaluator. Since order matters in
// non commutative operations, inverted should be true when the literalExpr is not the left argument.
func literalStepEvaluator(
	op string,
	lit *LiteralExpr,
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
	expr *LabelReplaceExpr,
	q Params,
) (StepEvaluator, error) {
	nextEvaluator, err := ev.StepEvaluator(ctx, ev, expr.Left, q)
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
