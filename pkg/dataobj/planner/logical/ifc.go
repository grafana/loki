// Package logical implements logical query plan operations and expressions
package logical

import (
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
)

/*
Interface Design Pattern for Query Plans and Expressions

This package uses a type-based polymorphism pattern that allows for:

1. Extension - Adding new concrete implementations without modifying consumer code
2. Type Safety - Compile-time guarantees through interfaces and type assertions
3. API Surface Reduction - Exposing minimal interfaces to external consumers

The pattern works as follows:

- Common interfaces (Plan, Expr) define minimal methods required by all implementations
- Each interface includes a Type() method that returns an enum value (PlanType, ExprType)
- Based on the type, consumers can safely cast to more specific sub-interfaces
  (tableNode, filterNode, etc. for Plan; columnExpr, literalExpr, etc. for Expr)
- This avoids type-switching on concrete implementation types, which would require
  modification every time a new implementation is added

Example usage:
```
func processNode(p Plan) {
    switch p.Type() {
    case PlanTypeTable:
        // Safe to cast to tableNode interface
        tbl := p.(tableNode)
        // Use tableNode-specific methods
        processTable(tbl.TableName())
    case PlanTypeFilter:
        // Safe to cast to filterNode interface
        filter := p.(filterNode)
        // Use filterNode-specific methods
        processFilter(filter.FilterExpr())
    // ...and so on for other plan types
    }
}
```

This design allows new implementations to be added (e.g., a new JoinNode type)
without requiring changes to existing code that processes Plans, as long as the
new implementation fits into one of the existing type categories.

The same pattern applies to expressions through the Expr interface and ExprType enum.
*/

// Expr represents an expression that can be evaluated to produce a column
type Expr interface {
	// ToField converts the expression to a column schema
	ToField(Plan) schema.ColumnSchema
	// Type returns the type of the expression, which can be used to safely
	// cast to a more specific interface (columnExpr, literalExpr, etc.)
	Type() ExprType
}

// ExprType is an enum representing the type of expression.
// It allows consumers to determine the concrete type of an Expr
// and safely cast to the appropriate interface.
type ExprType int

const (
	ExprTypeInvalid   ExprType = iota
	ExprTypeColumn             // Represents a reference to a column in the input
	ExprTypeLiteral            // Represents a literal value
	ExprTypeBinaryOp           // Represents a binary operation (e.g., a + b)
	ExprTypeAggregate          // Represents an aggregate function (e.g., SUM(a))
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

// columnExpr is a specific interface for column reference expressions.
// Consumers can safely cast to this interface when Expr.Type() == ExprTypeColumn.
type columnExpr interface {
	Expr
	ColumnName() string
}

// literalExpr is a specific interface for literal value expressions.
// Consumers can safely cast to this interface when Expr.Type() == ExprTypeLiteral.
type literalExpr interface {
	Expr
	Literal() string
	ValueType() datasetmd.ValueType
}

// binaryOpExpr is a specific interface for binary operation expressions.
// Consumers can safely cast to this interface when Expr.Type() == ExprTypeBinaryOp.
type binaryOpExpr interface {
	Expr
	Name() string
	Op() string
	Left() Expr
	Right() Expr
}

// aggregateExpr is a specific interface for aggregate expressions.
// Consumers can safely cast to this interface when Expr.Type() == ExprTypeAggregate.
type aggregateExpr interface {
	Expr
	Name() string
	Op() AggregateOp
	Expr() Expr
}

// Plan represents a logical query plan node that can provide its schema and children.
// It serves as the base interface for all plan node types.
type Plan interface {
	// Type returns the type of the plan node, which can be used to safely
	// cast to a more specific interface (tableNode, filterNode, etc.)
	Type() PlanType
	// Schema returns the schema of the data produced by this plan node
	Schema() schema.Schema
}

// PlanType is an enum representing the type of plan node.
// It allows consumers to determine the concrete type of a Plan
// and safely cast to the appropriate interface.
type PlanType int

const (
	PlanTypeInvalid    PlanType = iota
	PlanTypeTable               // Represents a table scan operation
	PlanTypeFilter              // Represents a filter operation
	PlanTypeProjection          // Represents a projection operation
	PlanTypeAggregate           // Represents an aggregation operation
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

// tableNode is a specific interface for table scan nodes.
// Consumers can safely cast to this interface when Plan.Type() == PlanTypeTable.
type tableNode interface {
	Plan
	TableSchema() schema.Schema
	TableName() string
}

// filterNode is a specific interface for filter nodes.
// Consumers can safely cast to this interface when Plan.Type() == PlanTypeFilter.
type filterNode interface {
	Plan
	Child() Plan // convenience method because filter nodes have a single child Plan
	FilterExpr() Expr
}

// projectionNode is a specific interface for projection nodes.
// Consumers can safely cast to this interface when Plan.Type() == PlanTypeProjection.
type projectionNode interface {
	Plan
	Child() Plan // convenience method because projection nodes have a single child Plan
	ProjectExprs() []Expr
}

// aggregateNode is a specific interface for aggregate nodes.
// Consumers can safely cast to this interface when Plan.Type() == PlanTypeAggregate.
type aggregateNode interface {
	Plan
	GroupExprs() []Expr
	AggregateExprs() []AggregateExpr
	Child() Plan // convenience method because aggregate nodes have a single Plan
}
