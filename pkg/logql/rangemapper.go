package logql

import (
	"fmt"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"

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

func (m RangeVectorMapper) Parse(query string) (bool, Expr, error) {
	parsed, err := ParseExpr(query)
	if err != nil {
		return false, nil, err
	}

	mapped, err := m.Map(parsed)
	if err != nil {
		return false, nil, err
	}

	return parsed.String() == mapped.String(), mapped, err
}

func (m RangeVectorMapper) Map(expr Expr) (Expr, error) {
	// immediately clone the passed expr to avoid mutating the original
	expr, err := Clone(expr)
	if err != nil {
		return nil, err
	}

	switch e := expr.(type) {
	case *VectorAggregationExpr:
		return m.mapVectorAggregationExpr(e)
	case *RangeAggregationExpr:
		return m.mapRangeAggregationExpr(e), nil
	case *BinOpExpr:
		lhsMapped, err := m.Map(e.SampleExpr)
		if err != nil {
			return nil, err
		}
		rhsMapped, err := m.Map(e.RHS)
		if err != nil {
			return nil, err
		}
		lhsSampleExpr, ok := lhsMapped.(SampleExpr)
		if !ok {
			return nil, badASTMapping("SampleExpr", lhsMapped)
		}
		rhsSampleExpr, ok := rhsMapped.(SampleExpr)
		if !ok {
			return nil, badASTMapping("SampleExpr", rhsMapped)
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
func getRangeInterval(expr SampleExpr) time.Duration {
	var rangeInterval time.Duration
	expr.Walk(func(e interface{}) {
		switch concrete := e.(type) {
		case *RangeAggregationExpr:
			rangeInterval = concrete.Left.Interval
		}
	})
	return rangeInterval
}

// mapSampleExpr transform expr in multiple subexpressions split by offset range interval
// rangeInterval should be greater than m.splitByInterval, otherwise the resultant expression
// will have an unnecessary aggregation operation
func (m RangeVectorMapper) mapSampleExpr(expr SampleExpr, rangeInterval time.Duration) SampleExpr {
	var head *ConcatSampleExpr

	splitCount := int(rangeInterval / m.splitByInterval)
	for i := 0; i < splitCount; i++ {
		subExpr, _ := Clone(expr)
		subSampleExpr := subExpr.(SampleExpr)
		offset := time.Duration(i) * m.splitByInterval
		subSampleExpr.Walk(func(e interface{}) {
			switch concrete := e.(type) {
			case *RangeAggregationExpr:
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

// technically, std{dev,var} are also parallelizable if there is no cross-shard merging
// in descendent nodes in the AST. This optimization is currently avoided for simplicity.
func (m RangeVectorMapper) mapVectorAggregationExpr(expr *VectorAggregationExpr) (SampleExpr, error) {
	// if this AST contains unshardable operations, don't shard this at this level,
	// but attempt to shard a child node.
	if !isSplittableByRange(expr) {
		subMapped, err := m.Map(expr.Left)
		if err != nil {
			return nil, err
		}
		sampleExpr, ok := subMapped.(SampleExpr)
		if !ok {
			return nil, badASTMapping("SampleExpr", subMapped)
		}

		return &VectorAggregationExpr{
			Left:      sampleExpr,
			Grouping:  expr.Grouping,
			Params:    expr.Params,
			Operation: expr.Operation,
		}, nil
	}

	rangeInterval := getRangeInterval(expr)

	// in case the interval is smaller than the configured split interval,
	// don't split it.
	// TODO: what if there is another internal expr with an interval that can be split?
	if rangeInterval <= m.splitByInterval {
		return expr, nil
	}

	switch expr.Operation {
	case OpTypeSum:
		// sum(x) -> sum(sum(x offset splitByInterval1) ++ sum(x offset splitByInterval2)...)
		return &VectorAggregationExpr{
			Left:      m.mapSampleExpr(expr, rangeInterval),
			Grouping:  expr.Grouping,
			Params:    expr.Params,
			Operation: expr.Operation,
		}, nil

	case OpTypeCount:
		// count(x) -> sum(count(x offset splitByInterval1) ++ count(x offset splitByInterval2)...)
		return &VectorAggregationExpr{
			Left:      m.mapSampleExpr(expr, rangeInterval),
			Grouping:  expr.Grouping,
			Operation: OpTypeSum,
		}, nil
	case OpTypeMax:
		// max(x) -> max(max(x offset splitByInterval1) ++ max(x offset splitByInterval2)...)
		return &VectorAggregationExpr{
			Left:      m.mapSampleExpr(expr, rangeInterval),
			Grouping:  expr.Grouping,
			Operation: expr.Operation,
		}, nil
	case OpTypeMin:
		// min(x) -> min(min(x offset splitByInterval1) ++ min(x offset splitByInterval2)...)
		return &VectorAggregationExpr{
			Left:      m.mapSampleExpr(expr, rangeInterval),
			Grouping:  expr.Grouping,
			Operation: expr.Operation,
		}, nil
	default:
		// this should not be reachable. If an operation is shardable it should
		// have an optimization listed.
		level.Warn(util_log.Logger).Log(
			"msg", "unexpected operation which appears shardable, ignoring",
			"operation", expr.Operation,
		)
		return expr, nil
	}
}

func (m RangeVectorMapper) mapRangeAggregationExpr(expr *RangeAggregationExpr) SampleExpr {
	// TODO: In case expr is non-splittable, can we attempt to shard a child node?

	rangeInterval := getRangeInterval(expr)

	// in case the interval is smaller than the configured split interval,
	// don't split it.
	// TODO: what if there is another internal expr with an interval that can be split?
	if rangeInterval <= m.splitByInterval {
		return expr
	}

	if isSplittableByRange(expr) {
		switch expr.Operation {
		case OpRangeTypeBytes, OpRangeTypeBytesRate, OpRangeTypeCount, OpRangeTypeRate, OpRangeTypeSum:
			return &VectorAggregationExpr{
				Left:      m.mapSampleExpr(expr, rangeInterval),
				Grouping:  expr.Grouping,
				Operation: OpTypeSum,
			}
		case OpRangeTypeMin:
			return &VectorAggregationExpr{
				Left:      m.mapSampleExpr(expr, rangeInterval),
				Grouping:  expr.Grouping,
				Operation: OpTypeMin,
			}
		case OpRangeTypeMax:
			return &VectorAggregationExpr{
				Left:      m.mapSampleExpr(expr, rangeInterval),
				Grouping:  expr.Grouping,
				Operation: OpTypeMax,
			}
		}
	}

	return expr
}

func isSplittableByRange(expr SampleExpr) bool {
	switch e := expr.(type) {
	case *VectorAggregationExpr:
		_, ok := SplittableVectorOp[e.Operation]
		return ok && isSplittableByRange(e.Left)
	case *BinOpExpr:
		return true
	case *LabelReplaceExpr:
		return true
	case *RangeAggregationExpr:
		_, ok := SplittableRangeVectorOp[e.Operation]
		return ok
	default:
		return false
	}
}

var SplittableVectorOp = map[string]struct{}{
	OpTypeSum:   {},
	OpTypeCount: {},
	OpTypeMax:   {},
	OpTypeMin:   {},
}

var SplittableRangeVectorOp = map[string]struct{}{
	OpRangeTypeBytes:     {},
	OpRangeTypeBytesRate: {},
	OpRangeTypeCount:     {},
	OpRangeTypeRate:      {},
	OpRangeTypeSum:       {},
	OpRangeTypeMin:       {},
	OpRangeTypeMax:       {},
}
