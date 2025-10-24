package physical

import (
	"fmt"
	"strings"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// ExpressionType represents the type of expression in the physical plan.
type ExpressionType uint32

const (
	_ ExpressionType = iota // zero-value is an invalid type

	ExprTypeUnary
	ExprTypeBinary
	ExprTypeFunction
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
	case ExprTypeFunction:
		return "FunctionExpression"
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
	Clone() Expression
	Type() ExpressionType
	isExpr()
}

func cloneExpressions[E Expression](exprs []E) []E {
	clonedExprs := make([]E, len(exprs))
	for i, expr := range exprs {
		clonedExprs[i] = expr.Clone().(E)
	}
	return clonedExprs
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

// FunctionExpression is the common interface for all function expressions in a
// physical plan.
type FunctionExpression interface {
	Expression
	isFunctionExpr()
}

// LiteralExpression is the common interface for all literal expressions in a
// physical plan.
type LiteralExpression interface {
	Expression
	ValueType() types.DataType
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

// Clone returns a copy of the [UnaryExpr].
func (e *UnaryExpr) Clone() Expression {
	return &UnaryExpr{
		Left: e.Left.Clone(),
		Op:   e.Op,
	}
}

func (e *UnaryExpr) String() string {
	return fmt.Sprintf("%s(%s)", e.Op, e.Left)
}

// Type returns the type of the [UnaryExpr].
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

// Clone returns a copy of the [BinaryExpr].
func (e *BinaryExpr) Clone() Expression {
	return &BinaryExpr{
		Left:  e.Left.Clone(),
		Right: e.Right.Clone(),
		Op:    e.Op,
	}
}

func (e *BinaryExpr) String() string {
	return fmt.Sprintf("%s(%s, %s)", e.Op, e.Left, e.Right)
}

// Type returns the type of the [BinaryExpr].
func (*BinaryExpr) Type() ExpressionType {
	return ExprTypeBinary
}

// LiteralExpr is an expression that implements the [LiteralExpression] interface.
type LiteralExpr struct {
	types.Literal
}

func (*LiteralExpr) isExpr()        {}
func (*LiteralExpr) isLiteralExpr() {}

// Clone returns a copy of the [LiteralExpr].
func (e *LiteralExpr) Clone() Expression {
	// No need to clone literals.
	return &LiteralExpr{Literal: e.Literal}
}

// String returns the string representation of the literal value.
func (e *LiteralExpr) String() string {
	return e.Literal.String()
}

// Type returns the type of the [LiteralExpr].
func (*LiteralExpr) Type() ExpressionType {
	return ExprTypeLiteral
}

// ValueType returns the kind of value represented by the literal.
func (e *LiteralExpr) ValueType() types.DataType {
	return e.Literal.Type()
}

func NewLiteral(value types.LiteralType) *LiteralExpr {
	if value == nil {
		return &LiteralExpr{Literal: types.NewNullLiteral()}
	}
	return &LiteralExpr{Literal: types.NewLiteral(value)}
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

// Clone returns a copy of the [ColumnExpr].
func (e *ColumnExpr) Clone() Expression {
	return &ColumnExpr{Ref: e.Ref}
}

// String returns the string representation of the column expression.
// It contains of the name of the column and its type, joined by a dot (`.`).
func (e *ColumnExpr) String() string {
	return e.Ref.String()
}

// Type returns the type of the [ColumnExpr].
func (e *ColumnExpr) Type() ExpressionType {
	return ExprTypeColumn
}

// FunctionExpr is an expression that implements the [FunctionExpression] interface.
type FunctionExpr struct {
	// Op is the function operation to apply to the parameters
	Op types.FunctionOp

	// Expressions are the parameters paaws to the function
	Expressions []Expression
}

func (*FunctionExpr) isExpr()         {}
func (*FunctionExpr) isFunctionExpr() {}

// Clone returns a copy of the [FunctionExpr].
func (e *FunctionExpr) Clone() Expression {
	params := make([]Expression, len(e.Expressions))
	for i, param := range e.Expressions {
		params[i] = param.Clone()
	}

	return &FunctionExpr{
		Expressions: params,
		Op:          e.Op,
	}
}

func (e *FunctionExpr) String() string {
	exprs := make([]string, len(e.Expressions))
	for i, expr := range e.Expressions {
		exprs[i] = expr.String()
	}
	return fmt.Sprintf("%s(%s)", e.Op, strings.Join(exprs, ", "))
}

// Type returns the type of the [FunctionExpr].
func (*FunctionExpr) Type() ExpressionType {
	return ExprTypeFunction
}

type NamedLiteralExpr struct {
	types.Literal
	Name string
}

func (*NamedLiteralExpr) isExpr()        {}
func (*NamedLiteralExpr) isLiteralExpr() {}

// Clone returns a copy of the [NamedLiteralExpr].
func (e *NamedLiteralExpr) Clone() Expression {
	// No need to clone literals.
	return &NamedLiteralExpr{Literal: e.Literal, Name: e.Name}
}

// String returns the string representation of the literal value.
func (e *NamedLiteralExpr) String() string {
	return fmt.Sprintf("%s: %s", e.Name, e.Literal.String())
}

// Type returns the type of the [FuntionLiteralParameterExpr].
func (*NamedLiteralExpr) Type() ExpressionType {
	return ExprTypeLiteral
}

// ValueType returns the kind of value represented by the literal.
func (e *NamedLiteralExpr) ValueType() types.DataType {
	return e.Literal.Type()
}
