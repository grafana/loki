package astmapper

import (
	"fmt"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/prometheus/promql"

	"github.com/cortexproject/cortex/pkg/util"
)

var summableAggregates = map[promql.ItemType]struct{}{
	promql.SUM:     {},
	promql.MIN:     {},
	promql.MAX:     {},
	promql.TOPK:    {},
	promql.BOTTOMK: {},
	promql.COUNT:   {},
}

var nonParallelFuncs = []string{
	"histogram_quantile",
	"quantile_over_time",
	"absent",
}

// CanParallelize tests if a subtree is parallelizable.
// A subtree is parallelizable if all of its components are parallelizable.
func CanParallelize(node promql.Node) bool {
	switch n := node.(type) {
	case nil:
		// nil handles cases where we check optional fields that are not set
		return true

	case promql.Expressions:
		for _, e := range n {
			if !CanParallelize(e) {
				return false
			}
		}
		return true

	case *promql.AggregateExpr:
		_, ok := summableAggregates[n.Op]
		return ok && CanParallelize(n.Expr)

	case *promql.BinaryExpr:
		// since binary exprs use each side for merging, they cannot be parallelized
		return false

	case *promql.Call:
		if n.Func == nil {
			return false
		}
		if !ParallelizableFunc(*n.Func) {
			return false
		}

		for _, e := range n.Args {
			if !CanParallelize(e) {
				return false
			}
		}
		return true

	case *promql.SubqueryExpr:
		return CanParallelize(n.Expr)

	case *promql.ParenExpr:
		return CanParallelize(n.Expr)

	case *promql.UnaryExpr:
		// Since these are only currently supported for Scalars, should be parallel-compatible
		return true

	case *promql.EvalStmt:
		return CanParallelize(n.Expr)

	case *promql.MatrixSelector, *promql.NumberLiteral, *promql.StringLiteral, *promql.VectorSelector:
		return true

	default:
		level.Error(util.Logger).Log("err", fmt.Sprintf("CanParallel: unhandled node type %T", node))
		return false
	}

}

// ParallelizableFunc ensures that a promql function can be part of a parallel query.
func ParallelizableFunc(f promql.Function) bool {

	for _, v := range nonParallelFuncs {
		if v == f.Name {
			return false
		}
	}
	return true
}
