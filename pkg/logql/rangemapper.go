package logql

import (
	"fmt"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/logql/syntax"
	util_log "github.com/grafana/loki/pkg/util/log"
)

type RangeVectorMapper struct {
	splitByInterval time.Duration
}

func NewRangeVectorMapper(interval time.Duration) (RangeVectorMapper, error) {
	if interval <= 0 {
		return RangeVectorMapper{}, fmt.Errorf("cannot create RangeVectorMapper with <=0 splitByInterval. Received %s", interval)
	}
	return RangeVectorMapper{
		splitByInterval: interval,
	}, nil
}

// Parse returns (noop, parsed expression, error)
func (m RangeVectorMapper) Parse(query string) (bool, syntax.Expr, error) {
	origExpr, err := syntax.ParseExpr(query)
	if err != nil {
		return true, nil, err
	}

	modExpr, err := m.Map(origExpr)
	if err != nil {
		return true, nil, err
	}

	return origExpr.String() == modExpr.String(), modExpr, err
}

func (m RangeVectorMapper) Map(expr syntax.Expr) (syntax.Expr, error) {
	// immediately clone the passed expr to avoid mutating the original
	expr, err := syntax.Clone(expr)
	if err != nil {
		return nil, err
	}

	switch e := expr.(type) {
	case *syntax.VectorAggregationExpr:
		return m.mapVectorAggregationExpr(e)
	case *syntax.RangeAggregationExpr:
		// noop - TODO: the aggregation of range aggregation expressions needs to preserve the labels
		return m.mapRangeAggregationExpr(e), nil
	case *syntax.BinOpExpr:
		lhsMapped, err := m.Map(e.SampleExpr)
		if err != nil {
			return nil, err
		}
		rhsMapped, err := m.Map(e.RHS)
		if err != nil {
			return nil, err
		}
		lhsSampleExpr, ok := lhsMapped.(syntax.SampleExpr)
		if !ok {
			return nil, badASTMapping(lhsMapped)
		}
		rhsSampleExpr, ok := rhsMapped.(syntax.SampleExpr)
		if !ok {
			return nil, badASTMapping(rhsMapped)
		}
		e.SampleExpr = lhsSampleExpr
		e.RHS = rhsSampleExpr
		return e, nil
	default:
		return nil, errors.Errorf("unexpected expr type (%T) for ASTMapper type (%T) ", expr, m)
	}
}

// getRangeInterval returns the interval in the range vector
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
// such as `| json` or `| logfmt`
func hasLabelExtractionStage(expr syntax.SampleExpr) bool {
	found := false
	expr.Walk(func(e interface{}) {
		switch concrete := e.(type) {
		case *syntax.LabelParserExpr:
			if concrete.Op == syntax.OpParserTypeJSON || concrete.Op == syntax.OpParserTypeLogfmt {
				found = true
			}
		}
	})
	return found
}

// sumOverFullRange returns an expression that sum up individual downstream queries by preserving labels
// and dividing it by the full range in seconds to calculate a rate value
// The operation defines the range aggregation operation of the downstream queries.
// Example:
// rate({app="foo"}[2m])
// => (sum without (count_over_time({app="foo"}[1m]) ++ count_over_time({app="foo"}[1m]) offset 1m) / 120)
func (m RangeVectorMapper) sumOverFullRange(expr *syntax.RangeAggregationExpr, operation string, rangeInterval time.Duration) syntax.SampleExpr {
	without := &syntax.Grouping{
		Without: true,
	}
	downstreamExpr := &syntax.RangeAggregationExpr{
		Left:      expr.Left,
		Operation: operation,
	}
	return &syntax.BinOpExpr{
		SampleExpr: &syntax.VectorAggregationExpr{
			Left:      m.mapConcatSampleExpr(downstreamExpr, rangeInterval),
			Grouping:  without,
			Operation: syntax.OpTypeSum,
		},
		RHS:  &syntax.LiteralExpr{Val: rangeInterval.Seconds()},
		Op:   syntax.OpTypeDiv,
		Opts: &syntax.BinOpOptions{},
	}
}

// splitDownstreams adds expression expr with a range interval 'interval' and offset 'offset'  to the downstreams list.
// Returns the updated downstream ConcatSampleExpr.
func (m RangeVectorMapper) splitDownstreams(downstreams *ConcatSampleExpr, expr syntax.SampleExpr, interval time.Duration, offset time.Duration) *ConcatSampleExpr {
	subExpr, _ := syntax.Clone(expr)
	subSampleExpr := subExpr.(syntax.SampleExpr)
	subSampleExpr.Walk(func(e interface{}) {
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
			SampleExpr: subSampleExpr,
		},
		next: downstreams,
	}
	return downstreams
}

// mapConcatSampleExpr transform expr in multiple downstream subexpressions split by offset range interval
// rangeInterval should be greater than m.splitByInterval, otherwise the resultant expression
// will have an unnecessary aggregation operation
func (m RangeVectorMapper) mapConcatSampleExpr(expr syntax.SampleExpr, rangeInterval time.Duration) syntax.SampleExpr {
	var downstreams *ConcatSampleExpr
	interval := 0

	splitCount := int(rangeInterval / m.splitByInterval)
	for interval = 0; interval < splitCount; interval++ {
		downstreams = m.splitDownstreams(downstreams, expr, m.splitByInterval, time.Duration(interval)*m.splitByInterval)
	}
	// Add the remainder offset interval
	if rangeInterval%m.splitByInterval != 0 {
		offset := time.Duration(interval) * m.splitByInterval
		downstreams = m.splitDownstreams(downstreams, expr, rangeInterval-offset, offset)
	}

	if downstreams == nil {
		return expr
	}
	return downstreams
}

func (m RangeVectorMapper) mapVectorAggregationExpr(expr *syntax.VectorAggregationExpr) (syntax.SampleExpr, error) {
	rangeInterval := getRangeInterval(expr)

	// in case the interval is smaller than the configured split interval,
	// don't split it.
	// TODO: what if there is another internal expr with an interval that can be split?
	if rangeInterval <= m.splitByInterval {
		return expr, nil
	}

	// Split the vector aggregation child node
	subMapped, err := m.Map(expr.Left)
	if err != nil {
		return nil, err
	}
	sampleExpr, ok := subMapped.(syntax.SampleExpr)
	if !ok {
		return nil, badASTMapping(subMapped)
	}

	return &syntax.VectorAggregationExpr{
		Left:      sampleExpr,
		Grouping:  expr.Grouping,
		Params:    expr.Params,
		Operation: expr.Operation,
	}, nil
}

func (m RangeVectorMapper) mapRangeAggregationExpr(expr *syntax.RangeAggregationExpr) syntax.SampleExpr {
	// TODO: In case expr is non-splittable, can we attempt to shard a child node?

	rangeInterval := getRangeInterval(expr)

	// in case the interval is smaller than the configured split interval,
	// don't split it.
	if rangeInterval <= m.splitByInterval {
		return expr
	}

	if isSplittableByRange(expr) {
		if expr.Grouping == nil && hasLabelExtractionStage(expr) {
			return expr
		}
		switch expr.Operation {
		case syntax.OpRangeTypeBytes, syntax.OpRangeTypeCount, syntax.OpRangeTypeSum:
			return &syntax.VectorAggregationExpr{
				Left: m.mapConcatSampleExpr(expr, rangeInterval),
				Grouping: &syntax.Grouping{
					Without: true,
				},
				Operation: syntax.OpTypeSum,
			}
		case syntax.OpRangeTypeMax:
			grouping := expr.Grouping
			if expr.Grouping == nil {
				grouping = &syntax.Grouping{
					Without: true,
				}
			}
			return &syntax.VectorAggregationExpr{
				Left:      m.mapConcatSampleExpr(expr, rangeInterval),
				Grouping:  grouping,
				Operation: syntax.OpTypeMax,
			}
		case syntax.OpRangeTypeMin:
			grouping := expr.Grouping
			if expr.Grouping == nil {
				grouping = &syntax.Grouping{
					Without: true,
				}
			}
			return &syntax.VectorAggregationExpr{
				Left:      m.mapConcatSampleExpr(expr, rangeInterval),
				Grouping:  grouping,
				Operation: syntax.OpTypeMin,
			}
		case syntax.OpRangeTypeRate:
			return m.sumOverFullRange(expr, syntax.OpRangeTypeCount, rangeInterval)
		case syntax.OpRangeTypeBytesRate:
			return m.sumOverFullRange(expr, syntax.OpRangeTypeBytes, rangeInterval)
		default:
			// this should not be reachable. If an operation is splittable it should
			// have an optimization listed
			level.Warn(util_log.Logger).Log(
				"msg", "unexpected range aggregation expression which appears shardable, ignoring",
				"operation", expr.Operation,
			)
			return expr
		}
	}

	return expr
}

func isSplittableByRange(expr syntax.SampleExpr) bool {
	switch e := expr.(type) {
	case *syntax.VectorAggregationExpr:
		_, ok := SplittableVectorOp[e.Operation]
		return ok && isSplittableByRange(e.Left)
	case *syntax.BinOpExpr:
		return true
	case *syntax.LabelReplaceExpr:
		return true
	case *syntax.RangeAggregationExpr:
		_, ok := SplittableRangeVectorOp[e.Operation]
		return ok
	default:
		return false
	}
}

var SplittableVectorOp = map[string]struct{}{
	syntax.OpTypeSum:   {},
	syntax.OpTypeCount: {},
	syntax.OpTypeMax:   {},
	syntax.OpTypeMin:   {},
}

var SplittableRangeVectorOp = map[string]struct{}{
	syntax.OpRangeTypeRate:      {},
	syntax.OpRangeTypeBytesRate: {},
	syntax.OpRangeTypeBytes:     {},
	syntax.OpRangeTypeCount:     {},
	syntax.OpRangeTypeSum:       {},
	syntax.OpRangeTypeMax:       {},
	syntax.OpRangeTypeMin:       {},
}