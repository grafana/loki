package logql

import (
	"fmt"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/logql/syntax"
	util_log "github.com/grafana/loki/pkg/util/log"
)

var splittableVectorOp = map[string]struct{}{
	syntax.OpTypeSum:   {},
	syntax.OpTypeCount: {},
	syntax.OpTypeMax:   {},
	syntax.OpTypeMin:   {},
	syntax.OpTypeAvg:   {},
	syntax.OpTypeTopK:  {},
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
// 1) Check if query is splittable based on the range.
// 2) Check if the query is splittable based on the query AST
// 3) Range aggregations are split into multiple downstream range aggregations
//    that are merged with a vector aggregation with an appropriate operator.
//    If the range aggregation has a grouping, the grouping is also applied on
//    the merge aggregation.
//    If the range aggregation has no grouping, the grouping is using "without"
//    to preserve the stream labels.
// 4) Vector aggregations are split into multiple downstream vector aggregations
//    that are merged with vector aggregation using "without" and then aggregated
//    using the vector aggregation with the same operator,
//    either with or without grouping.
// 5) Left and right hand side of binary operations are split individually
//    using the same rules as above.
type RangeMapper struct {
	splitByInterval time.Duration
}

// NewRangeMapper creates a new RangeMapper instance with the given duration as
// split interval. The interval must be greater than 0.
func NewRangeMapper(interval time.Duration) (RangeMapper, error) {
	if interval <= 0 {
		return RangeMapper{}, fmt.Errorf("cannot create RangeMapper with splitByInterval <= 0; got %s", interval)
	}
	return RangeMapper{
		splitByInterval: interval,
	}, nil
}

// Parse parses the given LogQL query string into a sample expression and
// applies the rewrite rules for splitting it into a sample expression that can
// be executed by the downstream engine.
// It returns a boolean indicating whether a rewrite was possible, the
// rewritten sample expression, and an error in case the rewrite failed.
func (m RangeMapper) Parse(query string) (bool, syntax.Expr, error) {
	origExpr, err := syntax.ParseSampleExpr(query)
	if err != nil {
		return true, nil, err
	}

	if !isSplittableByRange(origExpr) {
		return true, origExpr, nil
	}

	modExpr, err := m.Map(origExpr, nil)
	if err != nil {
		return true, nil, err
	}

	return origExpr.String() == modExpr.String(), modExpr, err
}

// Map rewrites an existing sample expression and returns the modified
// expression. It is called recursively on the expression tree.
// The function takes an optional vector aggregation as second argument, that
// is pushed down to the downstream expression.
func (m RangeMapper) Map(expr syntax.SampleExpr, vectorAggrPushdown *syntax.VectorAggregationExpr) (syntax.SampleExpr, error) {
	// immediately clone the passed expr to avoid mutating the original
	expr, err := clone(expr)
	if err != nil {
		return nil, err
	}

	switch e := expr.(type) {
	case *syntax.VectorAggregationExpr:
		return m.mapVectorAggregationExpr(e)
	case *syntax.RangeAggregationExpr:
		return m.mapRangeAggregationExpr(e, vectorAggrPushdown), nil
	case *syntax.BinOpExpr:
		lhsMapped, err := m.Map(e.SampleExpr, vectorAggrPushdown)
		if err != nil {
			return nil, err
		}
		rhsMapped, err := m.Map(e.RHS, vectorAggrPushdown)
		if err != nil {
			return nil, err
		}
		e.SampleExpr = lhsMapped
		e.RHS = rhsMapped
		return e, nil
	default:
		return nil, errors.Errorf("unexpected expr type (%T) for ASTMapper type (%T) ", expr, m)
	}
}

// getRangeInterval returns the interval in the range vector
// Note that this function must not be called with a BinOpExpr as argument
// as it returns only the range of the RHS.
// Example: expression `count_over_time({app="foo"}[10m])` returns 10m
func getRangeInterval(expr syntax.SampleExpr) time.Duration {
	var rangeInterval time.Duration
	expr.Walk(func(e interface{}) {
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
	expr.Walk(func(e interface{}) {
		switch concrete := e.(type) {
		case *syntax.LabelParserExpr:
			// It will **not** return true for `regexp`, `unpack` and `pattern`, since these label extraction
			// stages can control how many labels, and therefore the resulting amount of series, are extracted.
			if concrete.Op == syntax.OpParserTypeJSON || concrete.Op == syntax.OpParserTypeLogfmt {
				found = true
			}
		}
	})
	return found
}

// sumOverFullRange returns an expression that sums up individual downstream queries (with preserving labels)
// and dividing it by the full range in seconds to calculate a rate value.
// The operation defines the range aggregation operation of the downstream queries.
// Example:
// rate({app="foo"}[2m])
// => (sum without (count_over_time({app="foo"}[1m]) ++ count_over_time({app="foo"}[1m]) offset 1m) / 120)
func (m RangeMapper) sumOverFullRange(expr *syntax.RangeAggregationExpr, overrideDownstream *syntax.VectorAggregationExpr, operation string, rangeInterval time.Duration) syntax.SampleExpr {
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
	}
	return &syntax.BinOpExpr{
		SampleExpr: &syntax.VectorAggregationExpr{
			Left: m.mapConcatSampleExpr(downstreamExpr, rangeInterval),
			Grouping: &syntax.Grouping{
				Without: true,
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
func (m RangeMapper) vectorAggrWithRangeDownstreams(expr *syntax.RangeAggregationExpr, vectorAggrPushdown *syntax.VectorAggregationExpr, op string, rangeInterval time.Duration) syntax.SampleExpr {
	grouping := expr.Grouping
	if expr.Grouping == nil {
		grouping = &syntax.Grouping{
			Without: true,
		}
	}
	var downstream syntax.SampleExpr = expr
	if vectorAggrPushdown != nil {
		downstream = vectorAggrPushdown
	}
	return &syntax.VectorAggregationExpr{
		Left:      m.mapConcatSampleExpr(downstream, rangeInterval),
		Grouping:  grouping,
		Operation: op,
	}
}

// appendDownstream adds expression expr with a range interval 'interval' and offset 'offset'  to the downstreams list.
// Returns the updated downstream ConcatSampleExpr.
func appendDownstream(downstreams *ConcatSampleExpr, expr syntax.SampleExpr, interval time.Duration, offset time.Duration) *ConcatSampleExpr {
	sampleExpr, _ := clone(expr)
	sampleExpr.Walk(func(e interface{}) {
		switch concrete := e.(type) {
		case *syntax.RangeAggregationExpr:
			concrete.Left.Interval = interval
			if offset != 0 {
				concrete.Left.Offset += offset
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

// mapConcatSampleExpr transform expr in multiple downstream subexpressions split by offset range interval
// rangeInterval should be greater than m.splitByInterval, otherwise the resultant expression
// will have an unnecessary aggregation operation
func (m RangeMapper) mapConcatSampleExpr(expr syntax.SampleExpr, rangeInterval time.Duration) syntax.SampleExpr {
	splitCount := int(rangeInterval / m.splitByInterval)

	if splitCount == 0 {
		return expr
	}

	var split int
	var downstreams *ConcatSampleExpr
	for split = 0; split < splitCount; split++ {
		downstreams = appendDownstream(downstreams, expr, m.splitByInterval, time.Duration(split)*m.splitByInterval)
	}
	// Add the remainder offset interval
	if rangeInterval%m.splitByInterval != 0 {
		offset := time.Duration(split) * m.splitByInterval
		downstreams = appendDownstream(downstreams, expr, rangeInterval-offset, offset)
	}

	return downstreams
}

func (m RangeMapper) mapVectorAggregationExpr(expr *syntax.VectorAggregationExpr) (syntax.SampleExpr, error) {
	rangeInterval := getRangeInterval(expr)

	// in case the interval is smaller than the configured split interval,
	// don't split it.
	// TODO: what if there is another internal expr with an interval that can be split?
	if rangeInterval <= m.splitByInterval {
		return expr, nil
	}

	// In order to minimize the amount of streams on the downstream query,
	// we can push down the outer vector aggregation to the downstream query.
	// This does not work for `count()` and `topk()`, though.
	// We also do not want to push down, if the inner expression is a binary operation.
	// TODO: Currently, it is sending the last inner expression grouping dowstream.
	//  Which grouping should be sent downstream?
	var vectorAggrPushdown *syntax.VectorAggregationExpr
	if _, ok := expr.Left.(*syntax.BinOpExpr); !ok && expr.Operation != syntax.OpTypeCount && expr.Operation != syntax.OpTypeTopK {
		vectorAggrPushdown = expr
	}

	// Split the vector aggregation's inner expression
	lhsMapped, err := m.Map(expr.Left, vectorAggrPushdown)
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
func (m RangeMapper) mapRangeAggregationExpr(expr *syntax.RangeAggregationExpr, vectorAggrPushdown *syntax.VectorAggregationExpr) syntax.SampleExpr {
	rangeInterval := getRangeInterval(expr)

	// in case the interval is smaller than the configured split interval,
	// don't split it.
	if rangeInterval <= m.splitByInterval {
		return expr
	}

	// We cannot execute downstream queries that would potentially produce a huge amount of series
	// and therefore would very likely fail.
	if expr.Grouping == nil && hasLabelExtractionStage(expr) {
		return expr
	}
	switch expr.Operation {
	case syntax.OpRangeTypeBytes, syntax.OpRangeTypeCount, syntax.OpRangeTypeSum:
		return m.vectorAggrWithRangeDownstreams(expr, vectorAggrPushdown, syntax.OpTypeSum, rangeInterval)
	case syntax.OpRangeTypeMax:
		return m.vectorAggrWithRangeDownstreams(expr, vectorAggrPushdown, syntax.OpTypeMax, rangeInterval)
	case syntax.OpRangeTypeMin:
		return m.vectorAggrWithRangeDownstreams(expr, vectorAggrPushdown, syntax.OpTypeMin, rangeInterval)
	case syntax.OpRangeTypeRate:
		return m.sumOverFullRange(expr, vectorAggrPushdown, syntax.OpRangeTypeCount, rangeInterval)
	case syntax.OpRangeTypeBytesRate:
		return m.sumOverFullRange(expr, vectorAggrPushdown, syntax.OpRangeTypeBytes, rangeInterval)
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
// A binary expression is splittable, if both the left and the right hand side
// are splittable.
func isSplittableByRange(expr syntax.SampleExpr) bool {
	switch e := expr.(type) {
	case *syntax.VectorAggregationExpr:
		_, ok := splittableVectorOp[e.Operation]
		return ok && isSplittableByRange(e.Left)
	case *syntax.BinOpExpr:
		return isSplittableByRange(e.SampleExpr) && isSplittableByRange(e.RHS)
	case *syntax.LabelReplaceExpr:
		return isSplittableByRange(e.Left)
	case *syntax.RangeAggregationExpr:
		_, ok := splittableRangeVectorOp[e.Operation]
		return ok
	default:
		return false
	}
}

// clone returns a copy of the given sample expression
// This is needed whenever we want to modify the existing query tree.
// clone is identical as syntax.Expr.Clone() but with the additional type
// casting for syntax.SampleExpr.
func clone(expr syntax.SampleExpr) (syntax.SampleExpr, error) {
	return syntax.ParseSampleExpr(expr.String())
}
