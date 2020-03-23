package logql

import (
	"fmt"

	"github.com/cortexproject/cortex/pkg/querier/astmapper"
	"github.com/pkg/errors"
)

// ASTMapper is the exported interface for mapping between multiple AST representations
type ASTMapper interface {
	Map(Expr) (Expr, error)
}

// CloneExpr is a helper function to clone a node.
func CloneExpr(expr Expr) (Expr, error) {
	return ParseExpr(expr.String())
}

type ShardMapper struct {
	shards int
}

func (m ShardMapper) Map(expr Expr) (Expr, error) {
	switch e := expr.(type) {
	case *literalExpr:
		return e, nil
	case *matchersExpr, *filterExpr:
		return m.mapLogSelectorExpr(e.(LogSelectorExpr)), nil
	case *vectorAggregationExpr:
		return m.mapVectorAggregationExpr(e)
	case *rangeAggregationExpr:
		return m.mapRangeAggregationExpr(e)
	case *binOpExpr:
		lhsMapped, err := m.Map(e.SampleExpr)
		if err != nil {
			return nil, err
		}
		rhsMapped, err := m.Map(e.SampleExpr)
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
		return nil, MapperUnsupportedType(expr, m)
	}
}

func (m ShardMapper) mapLogSelectorExpr(expr LogSelectorExpr) LogSelectorExpr {
	var head *ConcatLogSelectorExpr
	for i := m.shards - 1; i >= 0; i-- {
		head = &ConcatLogSelectorExpr{
			LogSelectorExpr: DownstreamLogSelectorExpr{
				LogSelectorExpr: expr,
				shard:           &astmapper.ShardAnnotation{Shard: i, Of: m.shards},
			},
			next: head,
		}
	}

	return head
}

// technically, std{dev,var} are also parallelizable if there is no cross-shard merging
// in descendent nodes in the AST. This optimization is currently avoided for simplicity.
func (m ShardMapper) mapVectorAggregationExpr(expr *vectorAggregationExpr) (Expr, error) {
	switch expr.operation {
	case OpTypeSum:
	case OpTypeAvg:
	case OpTypeMax:
	case OpTypeMin:
	case OpTypeCount:
	case OpTypeBottomK:
	case OpTypeTopK:
	default:
		return expr, nil
	}
}

func (m ShardMapper) mapRangeAggregationExpr(expr *rangeAggregationExpr) (Expr, error) {
	switch expr.operation {
	case OpTypeCountOverTime:
	case OpTypeRate:
	default:
		return expr, nil
	}
}

func badASTMapping(expected string, got Expr) error {
	return fmt.Errorf("Bad AST mapping: expected one type (%s), but got (%T)", expected, got)
}

// MapperUnsuportedType is a helper for signaling that an evaluator does not support an Expr type
func MapperUnsupportedType(expr Expr, m ASTMapper) error {
	return errors.Errorf("unexpected expr type (%T) for ASTMapper type (%T) ", expr, m)
}
