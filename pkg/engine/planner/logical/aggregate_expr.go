package logical

import (
	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// AggregateOp represents the type of aggregation operation to perform.
// It is a string-based enum that identifies different aggregation functions
// that can be applied to expressions.
type AggregateOp string

const (
	// AggregateOpSum represents a sum aggregation
	AggregateOpSum AggregateOp = "sum"
	// AggregateOpAvg represents an average aggregation
	AggregateOpAvg AggregateOp = "avg"
	// AggregateOpMin represents a minimum value aggregation
	AggregateOpMin AggregateOp = "min"
	// AggregateOpMax represents a maximum value aggregation
	AggregateOpMax AggregateOp = "max"
	// AggregateOpCount represents a count aggregation
	AggregateOpCount AggregateOp = "count"
)

// Convenience constructors for each aggregate operation
var (
	// Sum creates a sum aggregation expression
	Sum = newAggregateExprConstructor(AggregateOpSum)
	// Avg creates an average aggregation expression
	Avg = newAggregateExprConstructor(AggregateOpAvg)
	// Min creates a minimum value aggregation expression
	Min = newAggregateExprConstructor(AggregateOpMin)
	// Max creates a maximum value aggregation expression
	Max = newAggregateExprConstructor(AggregateOpMax)
	// Count creates a count aggregation expression
	Count = newAggregateExprConstructor(AggregateOpCount)
)

// AggregateExpr represents an aggregation operation on an expression.
// It encapsulates the operation to perform (sum, avg, etc.), the expression
// to aggregate, and a name for the result.
type AggregateExpr struct {
	// name is the identifier for this aggregation
	name string
	// op specifies which aggregation operation to perform
	op AggregateOp
	// expr is the expression to aggregate
	expr Expr
}

// newAggregateExprConstructor creates a constructor function for a specific aggregate operation.
// This is a higher-order function that returns a function for creating aggregate expressions
// with a specific operation type.
func newAggregateExprConstructor(op AggregateOp) func(name string, expr Expr) AggregateExpr {
	return func(name string, expr Expr) AggregateExpr {
		return AggregateExpr{
			name: name,
			op:   op,
			expr: expr,
		}
	}
}

// Type returns the type of the expression.
// For aggregate expressions, this is always ExprTypeAggregate.
func (a AggregateExpr) Type() ExprType {
	return ExprTypeAggregate
}

// Name returns the name of the aggregation.
// This is used as the column name in the output schema.
func (a AggregateExpr) Name() string {
	return a.name
}

// Op returns the aggregation operation.
// This identifies which aggregation function to apply.
func (a AggregateExpr) Op() AggregateOp {
	return a.op
}

// SubExpr returns the expression being aggregated.
// This is the input to the aggregation function.
func (a AggregateExpr) SubExpr() Expr {
	return a.expr
}

// ToField converts the aggregation expression to a column schema.
// It determines the output type based on the input expression and
// the aggregation operation.
func (a AggregateExpr) ToField(p Plan) schema.ColumnSchema {
	// Get the input field schema

	return schema.ColumnSchema{
		Name: a.name,
		// Aggregations typically result in numeric types
		Type: determineAggregationTypeFromFieldType(a.expr.ToField(p).Type, a.op),
	}
}

// determineAggregationTypeFromFieldType calculates the output type of an aggregation
// based on the input field type and the aggregation operation.
// Currently, this is a placeholder that always returns int64, but it should be
// implemented to handle different input types and operations correctly.
func determineAggregationTypeFromFieldType(_ schema.ValueType, _ AggregateOp) schema.ValueType {
	// TODO: implement
	return schema.ValueTypeInt64
}
