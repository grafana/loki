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

// mapSampleExpr transform expr in multiple subexpressions split by offset range interval
// rangeInterval should be greater than m.splitByInterval, otherwise the resultant expression
// will have an unnecessary aggregation operation
func (m RangeVectorMapper) mapSampleExpr(expr syntax.SampleExpr, rangeInterval time.Duration) syntax.SampleExpr {
	var head *ConcatSampleExpr

	splitCount := int(rangeInterval / m.splitByInterval)
	for i := 0; i < splitCount; i++ {
		subExpr, _ := syntax.Clone(expr)
		subSampleExpr := subExpr.(syntax.SampleExpr)
		offset := time.Duration(i) * m.splitByInterval
		subSampleExpr.Walk(func(e interface{}) {
			switch concrete := e.(type) {
			case *syntax.RangeAggregationExpr:
				concrete.Left.Interval = m.splitByInterval
				if offset != 0 {
					concrete.Left.Offset = offset
				}
			}

		})
		head = &ConcatSampleExpr{
			DownstreamSampleExpr: DownstreamSampleExpr{
				SampleExpr: subSampleExpr,
			},
			next: head,
		}
	}

	if head == nil {
		return expr
	}
	return head
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
		switch expr.Operation {
		case syntax.OpRangeTypeBytes, syntax.OpRangeTypeCount, syntax.OpRangeTypeSum:
			return &syntax.VectorAggregationExpr{
				Left: m.mapSampleExpr(expr, rangeInterval),
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
				Left:      m.mapSampleExpr(expr, rangeInterval),
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
				Left:      m.mapSampleExpr(expr, rangeInterval),
				Grouping:  grouping,
				Operation: syntax.OpTypeMin,
			}
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
	syntax.OpRangeTypeBytes: {},
	syntax.OpRangeTypeCount: {},
	syntax.OpRangeTypeSum:   {},
	syntax.OpRangeTypeMax:   {},
	syntax.OpRangeTypeMin:   {},
}
