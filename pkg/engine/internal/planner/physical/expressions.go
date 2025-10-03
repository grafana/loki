package physical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// ExpressionType represents the type of expression in the physical plan.
type ExpressionType uint32

const (
	_ ExpressionType = iota // zero-value is an invalid type

	ExprTypeUnary
	ExprTypeBinary
	ExprTypeLiteral
	ExprTypeColumn
)

// String returns the string representation of the [ExpressionType].
func (t ExpressionType) String() string {
	switch t {
	case ExprTypeUnary:
		return "UnaryExpression"
	case ExprTypeBinary:
		return "BinaryExpression"
	case ExprTypeLiteral:
		return "LiteralExpression"
	case ExprTypeColumn:
		return "ColumnExpression"
	default:
		panic(fmt.Sprintf("unknown expression type %d", t))
	}
}

// Expression is the common interface for all expressions in a physical plan.
type Expression interface {
	fmt.Stringer
	Type() ExpressionType
	isExpr()
}

// UnaryExpression is the common interface for all unary expressions in a
// physical plan.
type UnaryExpression interface {
	Expression
	isUnaryExpr()
}

// BinaryExpression is the common interface for all binary expressions in a
// physical plan.
type BinaryExpression interface {
	Expression
	isBinaryExpr()
}

// LiteralExpression is the common interface for all literal expressions in a
// physical plan.
type LiteralExpression interface {
	Expression
	ValueType() datatype.DataType
	isLiteralExpr()
}

// ColumnExpression is the common interface for all column expressions in a
// physical plan.
type ColumnExpression interface {
	Expression
	isColumnExpr()
}

// UnaryExpr is an expression that implements the [UnaryExpression] interface.
type UnaryExpr struct {
	// Left is the expression being operated on
	Left Expression
	// Op is the unary operator to apply to the expression
	Op types.UnaryOp
}

func (*UnaryExpr) isExpr()      {}
func (*UnaryExpr) isUnaryExpr() {}

func (e *UnaryExpr) String() string {
	return fmt.Sprintf("%s(%s)", e.Op, e.Left)
}

// ID returns the type of the [UnaryExpr].
func (*UnaryExpr) Type() ExpressionType {
	return ExprTypeUnary
}

// BinaryExpr is an expression that implements the [BinaryExpression] interface.
type BinaryExpr struct {
	Left, Right Expression
	Op          types.BinaryOp
}

func (*BinaryExpr) isExpr()       {}
func (*BinaryExpr) isBinaryExpr() {}

func (e *BinaryExpr) String() string {
	return fmt.Sprintf("%s(%s, %s)", e.Op, e.Left, e.Right)
}

// ID returns the type of the [BinaryExpr].
func (*BinaryExpr) Type() ExpressionType {
	return ExprTypeBinary
}

// LiteralExpr is an expression that implements the [LiteralExpression] interface.
type LiteralExpr struct {
	datatype.Literal
}

func (*LiteralExpr) isExpr()        {}
func (*LiteralExpr) isLiteralExpr() {}

// String returns the string representation of the literal value.
func (e *LiteralExpr) String() string {
	return e.Literal.String()
}

// ID returns the type of the [LiteralExpr].
func (*LiteralExpr) Type() ExpressionType {
	return ExprTypeLiteral
}

// ValueType returns the kind of value represented by the literal.
func (e *LiteralExpr) ValueType() datatype.DataType {
	return e.Literal.Type()
}

func NewLiteral(value datatype.LiteralType) *LiteralExpr {
	if value == nil {
		return &LiteralExpr{Literal: datatype.NewNullLiteral()}
	}
	return &LiteralExpr{Literal: datatype.NewLiteral(value)}
}

// ColumnExpr is an expression that implements the [ColumnExpr] interface.
type ColumnExpr struct {
	Ref types.ColumnRef
}

func newColumnExpr(column string, ty types.ColumnType) *ColumnExpr {
	return &ColumnExpr{
		Ref: types.ColumnRef{
			Column: column,
			Type:   ty,
		},
	}
}

func (e *ColumnExpr) isExpr()       {}
func (e *ColumnExpr) isColumnExpr() {}

// String returns the string representation of the column expression.
// It contains of the name of the column and its type, joined by a dot (`.`).
func (e *ColumnExpr) String() string {
	return e.Ref.String()
}

// ID returns the type of the [ColumnExpr].
func (e *ColumnExpr) Type() ExpressionType {
	return ExprTypeColumn
}
