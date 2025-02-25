package logical

import (
	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
)

type nodeType int

const (
	nodeTypeInvalid nodeType = iota
	nodeTypeTable
	nodeTypeFilter
	nodeTypeProjection
	nodeTypeAggregate
)

// compile-time checks to ensure types implement the ast interface
var (
	_ ast = (tableNode)(nil)
	_ ast = (filterNode)(nil)
	_ ast = (projectionNode)(nil)
	_ ast = (aggregateNode)(nil)
)

type ast interface {
	Type() nodeType
	ASTChildren() []ast
}

type tableNode interface {
	ast
	TableSchema() schema.Schema
	TableName() string
}

type filterNode interface {
	ast
	FilterExpr() Expr
}

type projectionNode interface {
	ast
	ProjectExprs() []Expr
}

type aggregateNode interface {
	ast
	GroupExprs() []Expr
	AggregateExprs() []AggregateExpr
}

// NodeTypeName returns the string name of a nodeType
func NodeTypeName(t nodeType) string {
	switch t {
	case nodeTypeTable:
		return "Table"
	case nodeTypeFilter:
		return "Filter"
	case nodeTypeProjection:
		return "Projection"
	case nodeTypeAggregate:
		return "Aggregate"
	default:
		return "Unknown"
	}
}
