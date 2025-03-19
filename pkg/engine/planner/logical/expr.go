package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

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
	ExprTypeSort               // Represents a sort expression
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
	case ExprTypeSort:
		return "Sort"
	default:
		return "Unknown"
	}
}

type Expr struct {
	ty  ExprType
	val any
}

func (e Expr) Type() ExprType {
	return e.ty
}

func (e Expr) ToField(p Plan) schema.ColumnSchema {
	switch e.ty {
	case ExprTypeColumn:
		return e.val.(*ColumnExpr).ToField(p)
	case ExprTypeLiteral:
		return e.val.(*LiteralExpr).ToField(p)
	case ExprTypeBinaryOp:
		return e.val.(*BinOpExpr).ToField(p)
	case ExprTypeAggregate:
		return e.val.(*AggregateExpr).ToField(p)
	default:
		panic(fmt.Sprintf("unsupported expression type: %d", e.ty))
	}
}

// shortcut: must be checked elsewhere
func (e Expr) Column() *ColumnExpr {
	if e.ty != ExprTypeColumn {
		panic(fmt.Sprintf("expression is not a column: %d", e.ty))
	}
	return e.val.(*ColumnExpr)
}

func NewColumnExpr(expr ColumnExpr) Expr {
	return Expr{
		ty:  ExprTypeColumn,
		val: &expr,
	}
}

func NewLiteralExpr(expr LiteralExpr) Expr {
	return Expr{
		ty:  ExprTypeLiteral,
		val: &expr,
	}
}

func NewBinOpExpr(expr BinOpExpr) Expr {
	return Expr{
		ty:  ExprTypeBinaryOp,
		val: &expr,
	}
}

func NewAggregateExpr(expr AggregateExpr) Expr {
	return Expr{
		ty:  ExprTypeAggregate,
		val: &expr,
	}
}

// shortcut: must be checked elsewhere
func (e Expr) Literal() *LiteralExpr {
	if e.ty != ExprTypeLiteral {
		panic(fmt.Sprintf("expression is not a literal: %d", e.ty))
	}
	return e.val.(*LiteralExpr)
}

// shortcut: must be checked elsewhere
func (e Expr) BinaryOp() *BinOpExpr {
	if e.ty != ExprTypeBinaryOp {
		panic(fmt.Sprintf("expression is not a binary operation: %d", e.ty))
	}
	return e.val.(*BinOpExpr)
}

// shortcut: must be checked elsewhere
func (e Expr) Aggregate() *AggregateExpr {
	if e.ty != ExprTypeAggregate {
		panic(fmt.Sprintf("expression is not an aggregate: %d", e.ty))
	}
	return e.val.(*AggregateExpr)
}

func (e Expr) Sort() *SortExpr {
	if e.ty != ExprTypeSort {
		panic(fmt.Sprintf("expression is not a sort: %d", e.ty))
	}
	return e.val.(*SortExpr)
}
