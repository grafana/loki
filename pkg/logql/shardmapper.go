package logql

import (
	"fmt"

	"github.com/cortexproject/cortex/pkg/querier/astmapper"
)

func NewShardMapper(shards int) (ShardMapper, error) {
	if shards < 2 {
		return ShardMapper{}, fmt.Errorf("Cannot create ShardMapper with <2 shards. Received %d", shards)
	}
	return ShardMapper{shards}, nil
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
		return m.mapRangeAggregationExpr(e), nil
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
				shard: &astmapper.ShardAnnotation{
					Shard: i,
					Of:    m.shards,
				},
				LogSelectorExpr: expr,
			},
			next: head,
		}
	}

	return head
}

func (m ShardMapper) mapSampleExpr(expr SampleExpr) SampleExpr {
	var head *ConcatSampleExpr
	for i := m.shards - 1; i >= 0; i-- {
		head = &ConcatSampleExpr{
			SampleExpr: DownstreamSampleExpr{
				shard: &astmapper.ShardAnnotation{
					Shard: i,
					Of:    m.shards,
				},
				SampleExpr: expr,
			},
			next: head,
		}
	}

	return head
}

// technically, std{dev,var} are also parallelizable if there is no cross-shard merging
// in descendent nodes in the AST. This optimization is currently avoided for simplicity.
func (m ShardMapper) mapVectorAggregationExpr(expr *vectorAggregationExpr) (SampleExpr, error) {
	switch expr.operation {
	// sum(x) -> sum(sum(x, shard=1) ++ sum(x, shard=2)...)
	// max(x) -> max(max(x, shard=1) ++ max(x, shard=2)...)
	// min(x) -> min(min(x, shard=1) ++ min(x, shard=2)...)
	// topk(x) -> topk(topk(x, shard=1) ++ topk(x, shard=2)...)
	// botk(x) -> botk(botk(x, shard=1) ++ botk(x, shard=2)...)
	case OpTypeSum, OpTypeMax, OpTypeMin, OpTypeTopK, OpTypeBottomK:
		return &vectorAggregationExpr{
			left:      m.mapSampleExpr(expr),
			grouping:  expr.grouping,
			params:    expr.params,
			operation: expr.operation,
		}, nil

	case OpTypeAvg:
		// avg(x) -> sum(x)/count(x)
		lhs, err := m.mapVectorAggregationExpr(&vectorAggregationExpr{
			left:      expr.left,
			grouping:  expr.grouping,
			operation: OpTypeSum,
		})
		if err != nil {
			return nil, err
		}
		rhs, err := m.mapVectorAggregationExpr(&vectorAggregationExpr{
			left:      expr.left,
			grouping:  expr.grouping,
			operation: OpTypeCount,
		})
		if err != nil {
			return nil, err
		}

		return &binOpExpr{
			SampleExpr: lhs,
			RHS:        rhs,
			op:         OpTypeDiv,
		}, nil

	case OpTypeCount:
		// count(x) -> sum(count(x, shard=1) ++ count(x, shard=2)...)
		sharded := m.mapSampleExpr(expr)
		return &vectorAggregationExpr{
			left:      sharded,
			grouping:  expr.grouping,
			operation: OpTypeSum,
		}, nil
	default:
		return expr, nil
	}
}

func (m ShardMapper) mapRangeAggregationExpr(expr *rangeAggregationExpr) SampleExpr {
	switch expr.operation {
	case OpTypeCountOverTime, OpTypeRate:
		// count_over_time(x) -> count_over_time(x, shard=1) ++ count_over_time(x, shard=2)...
		// rate(x) -> rate(x, shard=1) ++ rate(x, shard=2)...
		return m.mapSampleExpr(expr)
	default:
		return expr
	}
}
