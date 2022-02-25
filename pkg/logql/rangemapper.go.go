package logql

import (
	"fmt"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"

	util_log "github.com/grafana/loki/pkg/util/log"
)

func NewRangeVectorMapper(interval time.Duration) (RangeVectorMapper, error) {
	if interval <= 0 {
		return RangeVectorMapper{}, fmt.Errorf("Cannot create RangeVectorMapper with <=0 interval. Received %s", interval)
	}
	return RangeVectorMapper{
		interval: interval,
	}, nil
}

type RangeVectorMapper struct {
	interval time.Duration
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
	// case *LabelReplaceExpr:
	// 	return m.mapLabelReplaceExpr(e, r)
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

func (m RangeVectorMapper) mapSampleExpr(expr SampleExpr) SampleExpr {
	var head *ConcatSampleExpr
	var rangeInterval time.Duration
	expr.Walk(func(e interface{}) {
		switch concrete := e.(type) {
		case *RangeAggregationExpr:
			rangeInterval = concrete.Left.Interval
		}
	})
	// handle case were we don't have it should never happen.
	// interval = 1m
	// test = [5m]
	splitCount := int(rangeInterval / m.interval)
	for i := 0; i < splitCount; i++ {
		subExpr, _ := Clone(expr)
		subSampleExpr := subExpr.(SampleExpr)
		offset := time.Duration(i) * m.interval
		subSampleExpr.Walk(func(e interface{}) {
			switch concrete := e.(type) {
			case *RangeAggregationExpr:
				concrete.Left.Interval = m.interval
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

	switch expr.Operation {
	case OpTypeSum:
		// sum(x) -> sum(sum(x offset interval) ++ sum(x, shard=2)...)
		return &VectorAggregationExpr{
			Left:      m.mapSampleExpr(expr),
			Grouping:  expr.Grouping,
			Params:    expr.Params,
			Operation: expr.Operation,
		}, nil

	case OpTypeCount:
		// count(x) -> sum(count(x, shard=1) ++ count(x, shard=2)...)
		sharded := m.mapSampleExpr(expr)
		return &VectorAggregationExpr{
			Left:      sharded,
			Grouping:  expr.Grouping,
			Operation: OpTypeSum,
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

func (m RangeVectorMapper) mapLabelReplaceExpr(expr *LabelReplaceExpr) (SampleExpr, error) {
	subMapped, err := m.Map(expr.Left)
	if err != nil {
		return nil, err
	}
	cpy := *expr
	cpy.Left = subMapped.(SampleExpr)
	return &cpy, nil
}

func (m RangeVectorMapper) mapRangeAggregationExpr(expr *RangeAggregationExpr) SampleExpr {
	if isSplittableByRange(expr) {
		switch expr.Operation {
		case OpRangeTypeBytes, OpRangeTypeBytesRate, OpRangeTypeCount, OpRangeTypeRate:
			return &VectorAggregationExpr{
				Left:      m.mapSampleExpr(expr),
				Grouping:  expr.Grouping,
				Operation: OpTypeSum,
			}
		case OpRangeTypeMin:
			return &VectorAggregationExpr{
				Left:      m.mapSampleExpr(expr),
				Grouping:  expr.Grouping,
				Operation: OpTypeMin,
			}
		case OpRangeTypeMax:
			return &VectorAggregationExpr{
				Left:      m.mapSampleExpr(expr),
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
