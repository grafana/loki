// Package logical implements logical query plan operations and expressions
package logical

import (
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
)

// Expr represents an expression that can be evaluated to produce a column
type Expr interface {
	// ToField converts the expression to a column schema
	ToField(Plan) schema.ColumnSchema
	// Type returns the type of the expression
	Type() ExprType
}

type ExprType int

const (
	ExprTypeInvalid ExprType = iota
	ExprTypeColumn
	ExprTypeLiteral
	ExprTypeBinaryOp
	ExprTypeAggregate
)

func (t ExprType) String() string {
	switch t {
	case ExprTypeColumn:
		return "Column"
	case ExprTypeLiteral:
		return "Literal"
	case ExprTypeBinaryOp:
		return "BinaryOp"
	case ExprTypeAggregate:
		return "Aggregate"
	default:
		return "Unknown"
	}
}

type columnExpr interface {
	Expr
	ColumnName() string
}

type literalExpr interface {
	Expr
	Literal() string
	ValueType() datasetmd.ValueType
}

type binaryOpExpr interface {
	Expr
	Name() string
	Op() string
	Left() Expr
	Right() Expr
}

type aggregateExpr interface {
	Expr
	Name() string
	Op() AggregateOp
	Expr() Expr
}

// Plan represents a logical query plan node that can provide its schema and children
type Plan interface {
	// Type returns the type of the plan node
	Type() PlanType
	// Schema returns the schema of the data produced by this plan node
	Schema() schema.Schema
}

type PlanType int

const (
	PlanTypeInvalid PlanType = iota
	PlanTypeTable
	PlanTypeFilter
	PlanTypeProjection
	PlanTypeAggregate
)

func (t PlanType) String() string {
	switch t {
	case PlanTypeTable:
		return "Table"
	case PlanTypeFilter:
		return "Filter"
	case PlanTypeProjection:
		return "Projection"
	case PlanTypeAggregate:
		return "Aggregate"
	default:
		return "Unknown"
	}
}

// compile-time checks to ensure types implement the ast interface
var (
	_ Plan = (tableNode)(nil)
	_ Plan = (filterNode)(nil)
	_ Plan = (projectionNode)(nil)
	_ Plan = (aggregateNode)(nil)
)

type tableNode interface {
	Plan
	TableSchema() schema.Schema
	TableName() string
}

type filterNode interface {
	Plan
	Child() Plan // convenience method because filter nodes have a single child Plan
	FilterExpr() Expr
}

type projectionNode interface {
	Plan
	Child() Plan // convenience method because projection nodes have a single child Plan
	ProjectExprs() []Expr
}

type aggregateNode interface {
	Plan
	GroupExprs() []Expr
	AggregateExprs() []AggregateExpr
	Child() Plan // convenience method because aggregate nodes have a single Plan
}
