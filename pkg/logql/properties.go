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

// PropertyExpr is an expression which can determine certain properties of an expression
// and also impls ASTMapper
type PropertyExpr interface {
	Expr
	CanParallel() bool // Whether this expression can be parallelized

}

type propertyExpr struct {
	Expr
}

func (e propertyExpr) CanParallel() bool {
	switch expr := e.Expr.(type) {
	case *matchersExpr, *filterExpr:
		return true
	case *rangeAggregationExpr:
		return parallelOperations[expr.operation]
	case *vectorAggregationExpr:
		return parallelOperations[expr.operation] && propertyExpr{expr.left}.CanParallel()
	default:
		return false
	}
}
