// Package logical implements logical query plan operations and expressions
package logical

// aggregateOp represents the type of aggregation operation to perform
type aggregateOp string

const (
	// aggregateOpSum represents a sum aggregation
	aggregateOpSum aggregateOp = "sum"
	// aggregateOpAvg represents an average aggregation
	aggregateOpAvg aggregateOp = "avg"
	// aggregateOpMin represents a minimum value aggregation
	aggregateOpMin aggregateOp = "min"
	// aggregateOpMax represents a maximum value aggregation
	aggregateOpMax aggregateOp = "max"
	// aggregateOpCount represents a count aggregation
	aggregateOpCount aggregateOp = "count"
)

// AggregateExpr represents an aggregation operation on an expression
type AggregateExpr struct {
	// name is the identifier for this aggregation
	name string
	// op specifies which aggregation operation to perform
	op aggregateOp
	// expr is the expression to aggregate
	expr Expr
}
