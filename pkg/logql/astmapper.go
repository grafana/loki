package logql

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

func (m ShardMapper) Map(expr Expr) (Expr, error) { return nil, nil }

// technically, std{dev,var} are also parallelizable if there is no cross-shard merging
// in descendent nodes in the AST. This optimization is currently avoided for simplicity.
var parallelOperations = map[string]bool{
	OpTypeSum:           true,
	OpTypeAvg:           true,
	OpTypeMax:           true,
	OpTypeMin:           true,
	OpTypeCount:         true,
	OpTypeBottomK:       true,
	OpTypeTopK:          true,
	OpTypeCountOverTime: true,
	OpTypeRate:          true,
}

func CanParallel(e Expr) bool {
	switch expr := e.(type) {
	case *matchersExpr, *filterExpr:
		return true
	case *rangeAggregationExpr:
		return parallelOperations[expr.operation]
	case *vectorAggregationExpr:
		return parallelOperations[expr.operation] && CanParallel(expr.left)
	default:
		return false
	}
}
