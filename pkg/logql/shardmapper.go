package logql

import (
	"fmt"

	"github.com/cortexproject/cortex/pkg/querier/astmapper"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
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

	// if this AST contains unshardable operations, don't shard this at this level,
	// but attempt to shard a child node.
	if shardable := isShardable(expr.Operations()); !shardable {
		subMapped, err := m.Map(expr.left)
		if err != nil {
			return nil, err
		}
		sampleExpr, ok := subMapped.(SampleExpr)
		if !ok {
			return nil, badASTMapping("SampleExpr", subMapped)
		}

		return &vectorAggregationExpr{
			left:      sampleExpr,
			grouping:  expr.grouping,
			params:    expr.params,
			operation: expr.operation,
		}, nil

	}

	switch expr.operation {
	case OpTypeSum:
		// sum(x) -> sum(sum(x, shard=1) ++ sum(x, shard=2)...)
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
		// this should not be reachable. If an operation is shardable it should
		// have an optimization listed.
		level.Warn(util.Logger).Log(
			"msg", "unexpected operation which appears shardable, ignoring",
			"operation", expr.operation,
		)
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

// isShardable returns false if any of the listed operation types are not shardable and true otherwise
func isShardable(ops []string) bool {
	for _, op := range ops {
		if shardable := shardableOps[op]; !shardable {
			return false
		}
	}
	return true
}

// shardableOps lists the operations which may be sharded.
// topk, botk, max, & min all must be concatenated and then evaluated in order to avoid
// potential data loss due to series distribution across shards.
// For example, grouping by `cluster` for a `max` operation may yield
// 2 results on the first shard and 10 results on the second. If we prematurely
// calculated `max`s on each shard, the shard/label combination with `2` may be
// discarded and some other combination with `11` may be reported falsely as the max.
//
// Explanation: this is my (owen-d) best understanding.
//
// For an operation to be shardable, first the sample-operation itself must be associative like (+, *) but not (%, /, ^).
// Secondly, if the operation is part of a vector aggregation expression or utilizes logical/set binary ops,
// the vector operation must be distributive over the sample-operation.
// This ensures that the vector merging operation can be applied repeatedly to data in different shards.
// references:
// https://en.wikipedia.org/wiki/Associative_property
// https://en.wikipedia.org/wiki/Distributive_property
var shardableOps = map[string]bool{
	// vector ops
	OpTypeSum: true,
	// avg is only marked as shardable because we remap it into sum/count.
	OpTypeAvg:   true,
	OpTypeCount: true,

	// range vector ops
	OpTypeCountOverTime: true,
	OpTypeRate:          true,

	// binops - arith
	OpTypeAdd: true,
	OpTypeMul: true,
}
