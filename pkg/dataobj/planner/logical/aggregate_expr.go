// Package logical implements logical query plan operations and expressions
package logical

import (
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
)

// AggregateOp represents the type of aggregation operation to perform
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

// AggregateExpr represents an aggregation operation on an expression
type AggregateExpr struct {
	// name is the identifier for this aggregation
	name string
	// op specifies which aggregation operation to perform
	op AggregateOp
	// expr is the expression to aggregate
	expr Expr
}

// newAggregateExprConstructor creates a constructor function for a specific aggregate operation
func newAggregateExprConstructor(op AggregateOp) func(name string, expr Expr) AggregateExpr {
	return func(name string, expr Expr) AggregateExpr {
		return AggregateExpr{
			name: name,
			op:   op,
			expr: expr,
		}
	}
}

// Type implements the Expr interface
func (a AggregateExpr) Type() ExprType {
	return ExprTypeAggregate
}

func (a AggregateExpr) Name() string {
	return a.name
}

func (a AggregateExpr) Op() AggregateOp {
	return a.op
}

func (a AggregateExpr) SubExpr() Expr {
	return a.expr
}

// ToField converts the aggregation expression to a column schema
func (a AggregateExpr) ToField(p Plan) schema.ColumnSchema {
	// Get the input field schema

	return schema.ColumnSchema{
		Name: a.name,
		// Aggregations typically result in numeric types
		Type: determineAggregationTypeFromFieldType(a.expr.ToField(p).Type, a.op),
	}
}

func determineAggregationTypeFromFieldType(_ datasetmd.ValueType, _ AggregateOp) datasetmd.ValueType {
	// TODO: implement
	return datasetmd.VALUE_TYPE_INT64
}
