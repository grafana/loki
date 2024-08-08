package logql

import (
	"fmt"
	"math"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var splittableVectorOp = map[string]struct{}{
	syntax.OpTypeSum:      {},
	syntax.OpTypeCount:    {},
	syntax.OpTypeMax:      {},
	syntax.OpTypeMin:      {},
	syntax.OpTypeAvg:      {},
	syntax.OpTypeTopK:     {},
	syntax.OpTypeSort:     {},
	syntax.OpTypeSortDesc: {},
}

var splittableRangeVectorOp = map[string]struct{}{
	syntax.OpRangeTypeRate:      {},
	syntax.OpRangeTypeBytesRate: {},
	syntax.OpRangeTypeBytes:     {},
	syntax.OpRangeTypeCount:     {},
	syntax.OpRangeTypeSum:       {},
	syntax.OpRangeTypeMax:       {},
	syntax.OpRangeTypeMin:       {},
}

// RangeMapper is used to rewrite LogQL sample expressions into multiple
// downstream sample expressions with a smaller time range that can be executed
// using the downstream engine.
//
// A rewrite is performed using the following rules:
//  1. Check if query is splittable based on the range.
//  2. Check if the query is splittable based on the query AST
//  3. Range aggregations are split into multiple downstream range aggregation expressions
//     that are concatenated with an appropriate vector aggregator with a grouping operator.
//     If the range aggregation has a grouping, the grouping is also applied to
//     the resultant vector aggregator expression.
//     If the range aggregation has no grouping, a grouping operator using "without" is applied
//     to the resultant vector aggregator expression to preserve the stream labels.
//  4. Vector aggregations are split into multiple downstream vector aggregations
//     that are merged with vector aggregation using "without" and then aggregated
//     using the vector aggregation with the same operator,
//     either with or without grouping.
//  5. Left and right-hand side of binary operations are split individually
//     using the same rules as above.
type RangeMapper struct {
	splitByInterval time.Duration
	metrics         *MapperMetrics
	stats           *MapperStats

	splitAlignTs time.Time
}

// NewRangeMapperWithSplitAlign is similar to `NewRangeMapper` except it accepts additional `splitAlign` argument and used to
// align the subqueries generated according to that. Look at `rangeSplitAlign` method for more information.
func NewRangeMapperWithSplitAlign(interval time.Duration, splitAlign time.Time, metrics *MapperMetrics, stats *MapperStats) (RangeMapper, error) {
	rm, err := NewRangeMapper(interval, metrics, stats)
	if err != nil {
		return RangeMapper{}, err
	}
	rm.splitAlignTs = splitAlign

	return rm, nil
}

// NewRangeMapper creates a new RangeMapper instance with the given duration as
// split interval. The interval must be greater than 0.
func NewRangeMapper(interval time.Duration, metrics *MapperMetrics, stats *MapperStats) (RangeMapper, error) {
	if interval <= 0 {
		return RangeMapper{}, fmt.Errorf("cannot create RangeMapper with splitByInterval <= 0; got %s", interval)
	}
	return RangeMapper{
		splitByInterval: interval,
		metrics:         metrics,
		stats:           stats,
	}, nil
}

func NewRangeMapperMetrics(registerer prometheus.Registerer) *MapperMetrics {
	return newMapperMetrics(registerer, "range")
}

// Parse parses the given LogQL query string into a sample expression and
// applies the rewrite rules for splitting it into a sample expression that can
// be executed by the downstream engine.
// It returns a boolean indicating whether a rewrite was possible, the
// rewritten sample expression, and an error in case the rewrite failed.
func (m RangeMapper) Parse(expr syntax.Expr) (bool, syntax.Expr, error) {
	origExpr, ok := expr.(syntax.SampleExpr)
	if !ok {
		return true, nil, errors.New("only sample expression supported")
	}

	recorder := m.metrics.downstreamRecorder()

	if !isSplittableByRange(origExpr) {
		m.metrics.ParsedQueries.WithLabelValues(NoopKey).Inc()
		return true, origExpr, nil
	}

	modExpr, err := m.Map(origExpr, nil, recorder)
	if err != nil {
		m.metrics.ParsedQueries.WithLabelValues(FailureKey).Inc()
		return true, nil, err
	}

	noop := origExpr.String() == modExpr.String()
	if noop {
		// reset split queries counter if the query is a noop
		m.stats.resetSplitQueries()
		m.metrics.ParsedQueries.WithLabelValues(NoopKey).Inc()
	} else {
		m.metrics.ParsedQueries.WithLabelValues(SuccessKey).Inc()
	}

	recorder.Finish() // only record metrics for successful mappings

	return noop, modExpr, err
}

// Map rewrites sample expression expr and returns the resultant sample expression to be executed by the downstream engine
// It is called recursively on the expression tree.
// The function takes an optional vector aggregation as second argument, that
// is pushed down to the downstream expression.
func (m RangeMapper) Map(expr syntax.SampleExpr, vectorAggrPushdown *syntax.VectorAggregationExpr, recorder *downstreamRecorder) (syntax.SampleExpr, error) {
	// immediately clone the passed expr to avoid mutating the original
	expr = syntax.MustClone(expr)
	switch e := expr.(type) {
	case *syntax.VectorAggregationExpr:
		return m.mapVectorAggregationExpr(e, recorder)
	case *syntax.RangeAggregationExpr:
		return m.mapRangeAggregationExpr(e, vectorAggrPushdown, recorder), nil
	case *syntax.BinOpExpr:
		lhsMapped, err := m.Map(e.SampleExpr, vectorAggrPushdown, recorder)
		if err != nil {
			return nil, err
		}
		// if left-hand side is a noop, we need to return the original expression
		// so the whole expression is a noop and thus not executed using the
		// downstream engine.
		// Note: literal expressions are identical to their mapped expression,
		// map binary expression if left-hand size is a literal
		if _, ok := e.SampleExpr.(*syntax.LiteralExpr); e.SampleExpr.String() == lhsMapped.String() && !ok {
			return e, nil
		}
		rhsMapped, err := m.Map(e.RHS, vectorAggrPushdown, recorder)
		if err != nil {
			return nil, err
		}
		// if right-hand side is a noop, we need to return the original expression
		// so the whole expression is a noop and thus not executed using the
		// downstream engine
		// Note: literal expressions are identical to their mapped expression,
		// map binary expression if right-hand size is a literal
		if _, ok := e.RHS.(*syntax.LiteralExpr); e.RHS.String() == rhsMapped.String() && !ok {
			return e, nil
		}
		e.SampleExpr = lhsMapped
		e.RHS = rhsMapped
		return e, nil
	case *syntax.LabelReplaceExpr:
		lhsMapped, err := m.Map(e.Left, vectorAggrPushdown, recorder)
		if err != nil {
			return nil, err
		}
		e.Left = lhsMapped
		return e, nil
	case *syntax.LiteralExpr:
		return e, nil
	case *syntax.VectorExpr:
		return e, nil
	default:
		// ConcatSampleExpr and DownstreamSampleExpr are not supported input expression types
		return nil, errors.Errorf("unexpected expr type (%T) for ASTMapper type (%T) ", expr, m)
	}
}

// getRangeInterval returns the interval in the range vector
// Note that this function must not be called with a BinOpExpr as argument
// as it returns only the range of the RHS.
// Example: expression `count_over_time({app="foo"}[10m])` returns 10m
func getRangeInterval(expr syntax.SampleExpr) time.Duration {
	var rangeInterval time.Duration
	expr.Walk(func(e syntax.Expr) {
		switch concrete := e.(type) {
		case *syntax.RangeAggregationExpr:
			rangeInterval = concrete.Left.Interval
		}
	})
	return rangeInterval
}

// hasLabelExtractionStage returns true if an expression contains a stage for label extraction,
// such as `| json` or `| logfmt`, that would result in an exploding amount of series in downstream queries.
func hasLabelExtractionStage(expr syntax.SampleExpr) bool {
	found := false
	expr.Walk(func(e syntax.Expr) {
		switch concrete := e.(type) {
		case *syntax.LogfmtParserExpr:
			found = true
		case *syntax.LabelParserExpr:
			// It will **not** return true for `regexp`, `unpack` and `pattern`, since these label extraction
			// stages can control how many labels, and therefore the resulting amount of series, are extracted.
			if concrete.Op == syntax.OpParserTypeJSON {
				found = true
			}
		}
	})
	return found
}

// sumOverFullRange returns an expression that sums up individual downstream queries (with preserving labels)
// and dividing it by the full range in seconds to calculate a rate value.
// The operation defines the range aggregation operation of the downstream queries.
// Examples:
// rate({app="foo"}[2m])
// => (sum without (count_over_time({app="foo"}[1m]) ++ count_over_time({app="foo"}[1m]) offset 1m) / 120)
// rate({app="foo"} | unwrap bar [2m])
// => (sum without (sum_over_time({app="foo"}[1m]) ++ sum_over_time({app="foo"}[1m]) offset 1m) / 120)
func (m RangeMapper) sumOverFullRange(expr *syntax.RangeAggregationExpr, overrideDownstream *syntax.VectorAggregationExpr, operation string, rangeInterval time.Duration, recorder *downstreamRecorder) syntax.SampleExpr {
	var downstreamExpr syntax.SampleExpr = &syntax.RangeAggregationExpr{
		Left:      expr.Left,
		Operation: operation,
	}
	// Optimization: in case overrideDownstream exists, the downstream expression can be optimized with the grouping
	// and operation of the overrideDownstream expression in order to reduce the returned streams' label set.
	if overrideDownstream != nil {
		downstreamExpr = &syntax.VectorAggregationExpr{
			Left:      downstreamExpr,
			Grouping:  overrideDownstream.Grouping,
			Operation: overrideDownstream.Operation,
		}
		// Ensure our modified expression is still valid.
		if downstreamExpr.(*syntax.VectorAggregationExpr).Left.(*syntax.RangeAggregationExpr).Validate() != nil {
			return expr
		}
	}

	return &syntax.BinOpExpr{
		SampleExpr: &syntax.VectorAggregationExpr{
			Left: m.mapConcatSampleExpr(downstreamExpr, rangeInterval, recorder),
			Grouping: &syntax.Grouping{
				Without: true,
				Groups:  []string{},
			},
			Operation: syntax.OpTypeSum,
		},
		RHS:  &syntax.LiteralExpr{Val: rangeInterval.Seconds()},
		Op:   syntax.OpTypeDiv,
		Opts: &syntax.BinOpOptions{},
	}
}

// vectorAggrWithRangeDownstreams returns an expression that aggregates a concat sample expression of multiple range
// aggregations. If a vector aggregation is pushed down, the downstream queries of the concat sample expression are
// wrapped in the vector aggregation of the parent node.
// Example:
// min(bytes_over_time({job="bar"} [2m])
// => min without (bytes_over_time({job="bar"} [1m]) ++ bytes_over_time({job="bar"} [1m] offset 1m))
// min by (app) (bytes_over_time({job="bar"} [2m])
// => min without (min by (app) (bytes_over_time({job="bar"} [1m])) ++ min by (app) (bytes_over_time({job="bar"} [1m] offset 1m)))
func (m RangeMapper) vectorAggrWithRangeDownstreams(expr *syntax.RangeAggregationExpr, vectorAggrPushdown *syntax.VectorAggregationExpr, op string, rangeInterval time.Duration, recorder *downstreamRecorder) syntax.SampleExpr {
	grouping := expr.Grouping
	if expr.Grouping == nil {
		grouping = &syntax.Grouping{
			Without: true,
			Groups:  []string{},
		}
	}
	var downstream syntax.SampleExpr = expr
	if vectorAggrPushdown != nil {
		downstream = vectorAggrPushdown
	}
	return &syntax.VectorAggregationExpr{
		Left:      m.mapConcatSampleExpr(downstream, rangeInterval, recorder),
		Grouping:  grouping,
		Operation: op,
	}
}

// appendDownstream adds expression expr with a range interval 'interval' and offset 'offset' to the downstreams list.
// Returns the updated downstream ConcatSampleExpr.
func appendDownstream(downstreams *ConcatSampleExpr, expr syntax.SampleExpr, interval time.Duration, offset time.Duration) *ConcatSampleExpr {
	sampleExpr := syntax.MustClone(expr)
	sampleExpr.Walk(func(e syntax.Expr) {
		switch concrete := e.(type) {
		case *syntax.RangeAggregationExpr:
			concrete.Left.Interval = interval
			if offset != 0 {
				concrete.Left.Offset = offset
			}
		}
	})
	downstreams = &ConcatSampleExpr{
		DownstreamSampleExpr: DownstreamSampleExpr{
			SampleExpr: sampleExpr,
		},
		next: downstreams,
	}
	return downstreams
}

func getOffsets(expr syntax.SampleExpr) []time.Duration {
	// Expect to always find at most 1 offset, so preallocate it accordingly
	offsets := make([]time.Duration, 0, 1)

	expr.Walk(func(e syntax.Expr) {
		switch concrete := e.(type) {
		case *syntax.RangeAggregationExpr:
			offsets = append(offsets, concrete.Left.Offset)
		}
	})
	return offsets
}

// getOriginalOffset returns the offset specified in the input expr
// Note that the returned offset can be zero or negative
func (m RangeMapper) getOriginalOffset(expr syntax.SampleExpr) (offset time.Duration, err error) {
	offsets := getOffsets(expr)
	if len(offsets) == 0 {
		return time.Duration(0), nil
	}
	if len(offsets) > 1 {
		return time.Duration(0), fmt.Errorf("found %d offsets while expecting at most 1", len(offsets))
	}

	return offsets[0], nil
}

// mapConcatSampleExpr transform expr in multiple downstream subexpressions split by offset range interval
// rangeInterval should be greater than m.splitByInterval, otherwise the resultant expression
// will have an unnecessary aggregation operation
func (m RangeMapper) mapConcatSampleExpr(expr syntax.SampleExpr, rangeInterval time.Duration, recorder *downstreamRecorder) syntax.SampleExpr {
	if m.splitAlignTs.IsZero() {
		return m.rangeSplit(expr, rangeInterval, recorder)
	}
	return m.rangeSplitAlign(expr, rangeInterval, recorder)
}

// rangeSplitAlign try to split given `rangeInterval` into units of `m.splitByInterval` by making sure `rangeInterval` is aligned with `m.splitByInterval` for as much as the units as possible.
// Consider following example with real use case.
// Instant Query: `sum(rate({foo="bar"}[3h])`
// execTs: 12:34:00
// splitBy: 1h
// Given above parameters, queries will be split into following
// 1. sum(rate({foo="bar"}[34m]))
// 2. sum(rate({foo="bar"}[1h] offset 34m))
// 3. sum(rate({foo="bar"}[1h] offset 1h34m))
// 4. sum(rate({foo="bar"}[26m] offset 2h34m))
func (m RangeMapper) rangeSplitAlign(
	expr syntax.SampleExpr, rangeInterval time.Duration, recorder *downstreamRecorder,
) syntax.SampleExpr {
	if rangeInterval <= m.splitByInterval {
		return expr
	}

	originalOffset, err := m.getOriginalOffset(expr)
	if err != nil {
		return expr
	}

	align := m.splitAlignTs.Sub(m.splitAlignTs.Truncate(m.splitByInterval)) // say, 12:34:00 - 12:00:00(truncated) = 34m

	if align == 0 {
		return m.rangeSplit(expr, rangeInterval, recorder) // Don't have to align
	}

	var (
		newRng = align

		// TODO(kavi): If the originalOffset is non-zero, there may be a edge case, where subqueries generated won't be aligned correctly. Handle this edge case in separate PR.
		newOffset            = originalOffset
		downstreams          *ConcatSampleExpr
		pendingRangeInterval = rangeInterval
		splits               = 0
	)

	// first subquery
	downstreams = appendDownstream(downstreams, expr, newRng, newOffset)
	splits++

	newOffset += align // e.g: offset 34m
	pendingRangeInterval -= newRng
	newRng = m.splitByInterval // [1h]

	// Rest of the subqueries.
	for pendingRangeInterval > 0 {
		if pendingRangeInterval < m.splitByInterval {
			newRng = pendingRangeInterval // last subquery
		}
		downstreams = appendDownstream(downstreams, expr, newRng, newOffset)
		newOffset += m.splitByInterval
		pendingRangeInterval -= newRng
		splits++
	}

	// update stats and metrics
	m.stats.AddSplitQueries(splits)
	recorder.Add(splits, MetricsKey)

	return downstreams
}

func (m RangeMapper) rangeSplit(expr syntax.SampleExpr, rangeInterval time.Duration, recorder *downstreamRecorder) syntax.SampleExpr {
	splitCount := int(math.Ceil(float64(rangeInterval) / float64(m.splitByInterval)))
	if splitCount <= 1 {
		return expr
	}

	originalOffset, err := m.getOriginalOffset(expr)
	if err != nil {
		return expr
	}

	var downstreams *ConcatSampleExpr
	for split := 0; split < splitCount; split++ {
		splitOffset := time.Duration(split) * m.splitByInterval
		// The range interval of the last downstream query can be smaller than the split interval
		splitRangeInterval := m.splitByInterval
		if splitOffset+splitRangeInterval > rangeInterval {
			splitRangeInterval = rangeInterval - splitOffset
		}
		// The offset of downstream queries is always the original offset + a multiple of the split interval
		splitOffset += originalOffset
		downstreams = appendDownstream(downstreams, expr, splitRangeInterval, splitOffset)
	}

	// Update stats and metrics
	m.stats.AddSplitQueries(splitCount)
	recorder.Add(splitCount, MetricsKey)

	return downstreams
}

func (m RangeMapper) mapVectorAggregationExpr(expr *syntax.VectorAggregationExpr, recorder *downstreamRecorder) (syntax.SampleExpr, error) {
	rangeInterval := getRangeInterval(expr)

	// in case the interval is smaller than the configured split interval,
	// don't split it.
	if rangeInterval <= m.splitByInterval {
		return expr, nil
	}

	// In order to minimize the amount of streams on the downstream query,
	// we can push down the outer vector aggregation to the downstream query.
	// This does not work for `count()` and `topk()`, though.
	// We also do not want to push down, if the inner expression is a binary operation.
	var vectorAggrPushdown *syntax.VectorAggregationExpr
	if _, ok := expr.Left.(*syntax.BinOpExpr); !ok && expr.Operation != syntax.OpTypeCount && expr.Operation != syntax.OpTypeTopK && expr.Operation != syntax.OpTypeSort && expr.Operation != syntax.OpTypeSortDesc {
		vectorAggrPushdown = expr
	}

	// Split the vector aggregation's inner expression
	lhsMapped, err := m.Map(expr.Left, vectorAggrPushdown, recorder)
	if err != nil {
		return nil, err
	}

	return &syntax.VectorAggregationExpr{
		Left:      lhsMapped,
		Grouping:  expr.Grouping,
		Params:    expr.Params,
		Operation: expr.Operation,
	}, nil
}

// mapRangeAggregationExpr maps expr into a new SampleExpr with multiple downstream subqueries split by range interval
// Optimization: in order to reduce the returned stream from the inner downstream functions, in case a range aggregation
// expression is aggregated by a vector aggregation expression with a label grouping, the downstream expression can be
// exactly the same as the initial query concatenated by a `sum` operation. If this is the case, overrideDownstream
// contains the initial query which will be the downstream expression with a split range interval.
// Example: `sum by (a) (bytes_over_time)`
// Is mapped to `sum by (a) (sum without downstream<sum by (a) (bytes_over_time)>++downstream<sum by (a) (bytes_over_time)>++...)`
func (m RangeMapper) mapRangeAggregationExpr(expr *syntax.RangeAggregationExpr, vectorAggrPushdown *syntax.VectorAggregationExpr, recorder *downstreamRecorder) syntax.SampleExpr {
	rangeInterval := getRangeInterval(expr)

	// in case the interval is smaller than the configured split interval,
	// don't split it.
	if rangeInterval <= m.splitByInterval {
		return expr
	}

	labelExtractor := hasLabelExtractionStage(expr)

	// Downstream queries with label extractors can potentially produce a huge amount of series
	// which can impact the queries and consequently fail.
	// Note: vector aggregation expressions aggregate the result in a single empty label set,
	// so these expressions can be pushed downstream
	if expr.Grouping == nil && vectorAggrPushdown == nil && labelExtractor {
		return expr
	}
	switch expr.Operation {
	case syntax.OpRangeTypeSum:
		return m.vectorAggrWithRangeDownstreams(expr, vectorAggrPushdown, syntax.OpTypeSum, rangeInterval, recorder)
	case syntax.OpRangeTypeBytes, syntax.OpRangeTypeCount:
		// Downstream queries with label extractors use concat as aggregation operator instead of sum
		// in order to merge the resultant label sets
		if labelExtractor {
			var downstream syntax.SampleExpr = expr
			if vectorAggrPushdown != nil {
				downstream = vectorAggrPushdown
			}
			return m.mapConcatSampleExpr(downstream, rangeInterval, recorder)
		}
		return m.vectorAggrWithRangeDownstreams(expr, vectorAggrPushdown, syntax.OpTypeSum, rangeInterval, recorder)
	case syntax.OpRangeTypeMax:
		return m.vectorAggrWithRangeDownstreams(expr, vectorAggrPushdown, syntax.OpTypeMax, rangeInterval, recorder)
	case syntax.OpRangeTypeMin:
		return m.vectorAggrWithRangeDownstreams(expr, vectorAggrPushdown, syntax.OpTypeMin, rangeInterval, recorder)
	case syntax.OpRangeTypeRate:
		if labelExtractor && vectorAggrPushdown.Operation != syntax.OpTypeSum {
			return expr
		}
		// rate({app="foo"}[2m]) =>
		// => (sum without (count_over_time({app="foo"}[1m]) ++ count_over_time({app="foo"}[1m]) offset 1m) / 120)
		op := syntax.OpRangeTypeCount
		if expr.Left.Unwrap != nil {
			// rate({app="foo"} | unwrap bar [2m])
			// => (sum without (sum_over_time({app="foo"}[1m]) ++ sum_over_time({app="foo"}[1m]) offset 1m) / 120)
			op = syntax.OpRangeTypeSum
		}
		return m.sumOverFullRange(expr, vectorAggrPushdown, op, rangeInterval, recorder)
	case syntax.OpRangeTypeBytesRate:
		if labelExtractor && vectorAggrPushdown.Operation != syntax.OpTypeSum {
			return expr
		}
		return m.sumOverFullRange(expr, vectorAggrPushdown, syntax.OpRangeTypeBytes, rangeInterval, recorder)
	default:
		// this should not be reachable.
		// If an operation is splittable it should have an optimization listed.
		level.Warn(util_log.Logger).Log(
			"msg", "unexpected range aggregation expression",
			"operation", expr.Operation,
		)
		return expr
	}
}

// isSplittableByRange returns whether it is possible to optimize the given
// sample expression.
// A vector aggregation is splittable, if the aggregation operation is
// supported and the inner expression is also splittable.
// A range aggregation is splittable, if the aggregation operation is
// supported.
// A binary expression is splittable, if both the left and the right-hand side
// are splittable.
func isSplittableByRange(expr syntax.SampleExpr) bool {
	switch e := expr.(type) {
	case *syntax.VectorAggregationExpr:
		_, ok := splittableVectorOp[e.Operation]
		return ok && isSplittableByRange(e.Left)
	case *syntax.RangeAggregationExpr:
		_, ok := splittableRangeVectorOp[e.Operation]
		return ok
	case *syntax.BinOpExpr:
		_, literalLHS := e.SampleExpr.(*syntax.LiteralExpr)
		_, literalRHS := e.RHS.(*syntax.LiteralExpr)
		// Note: if both left-hand side and right-hand side are literal expressions,
		// the syntax.ParseSampleExpr returns a literal expression
		return isSplittableByRange(e.SampleExpr) || literalLHS && isSplittableByRange(e.RHS) || literalRHS
	case *syntax.LabelReplaceExpr:
		return isSplittableByRange(e.Left)
	case *syntax.VectorExpr:
		return false
	default:
		return false
	}
}
