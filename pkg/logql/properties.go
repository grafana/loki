package logql

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
		return parallelOperations[expr.operation] && CanParallel(expr.Left)
	default:
		return false
	}
}
