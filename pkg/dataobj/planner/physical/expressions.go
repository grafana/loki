package physical

import "fmt"

// ValueType represents the type of a literal in a literal expression.
type ValueType uint32

const (
	_ ValueType = iota // zero-value is an invalid type
	ValueTypeBool
	ValueTypeInt64
	ValueTypeTimestamp
	ValueTypeString
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
		return fmt.Sprintf("unknown expression type %d", t)
	}
}

// UnaryOpType represents the operator of a [UnaryExpression].
type UnaryOpType uint32

const (
	_ UnaryOpType = iota // zero-value is an invalid value
	UnaryOpNot
	UnaryOpAbs
)

// String returns the string representation of the [UnaryOpType].
func (t UnaryOpType) String() string {
	switch t {
	case UnaryOpNot:
		return "NOT"
	case UnaryOpAbs:
		return "ABS"
	default:
		return fmt.Sprintf("unknown unary operator type %d", t)
	}
}

// BinaryOpType represents the operator of a [BinaryExpression].
type BinaryOpType uint32

const (
	_ BinaryOpType = iota // zero-value is an invalid type
	BinaryOpEq
	BinaryOpNeq
	BinaryOpGt
	BinaryOpGte
	BinaryOpLt
	BinaryOpLte
	BinaryOpAnd
	BinaryOpOr
	BinaryOpXor
	BinaryOpNot
	BinaryOpAdd
	BinaryOpSub
	BinaryOpMul
	BinaryOpDiv
	BinaryOpMod
	BinaryOpMatchStr
	BinaryOpNotMatchStr
	BinaryOpMatchRe
	BinaryOpNotMatchRe
)

// String returns the string representation of the [BinaryOpType].
func (t BinaryOpType) String() string {
	switch t {
	case BinaryOpEq:
		return "EQ"
	case BinaryOpNeq:
		return "NEQ" // convenience for NOT(EQ(expr))
	case BinaryOpGt:
		return "GT"
	case BinaryOpGte:
		return "GTE"
	case BinaryOpLt:
		return "LT" // convenience for NOT(GTE(expr))
	case BinaryOpLte:
		return "LTE" // convenience for NOT(GT(expr))
	case BinaryOpAnd:
		return "AND"
	case BinaryOpOr:
		return "OR"
	case BinaryOpXor:
		return "XOR"
	case BinaryOpNot:
		return "NOT"
	case BinaryOpAdd:
		return "ADD"
	case BinaryOpSub:
		return "SUB"
	case BinaryOpMul:
		return "MUL"
	case BinaryOpDiv:
		return "DIV"
	case BinaryOpMod:
		return "MOD"
	case BinaryOpMatchStr:
		return "MATCH_STR"
	case BinaryOpNotMatchStr:
		return "NOT_MATCH_STR" // convenience for NOT(MATCH_STR(...))
	case BinaryOpMatchRe:
		return "MATCH_RE"
	case BinaryOpNotMatchRe:
		return "NOT_MATCH_RE" // convenience for NOT(MATCH_RE(...))
	default:
		return fmt.Sprintf("unknown binary operator type %d", t)
	}
}

// Expression is the common interface for all expressions in a physcial plan.
type Expression interface {
	Type() ExpressionType
	isExpr()
}

// UnaryExpression is the common interface for all unary expressions in a
// physcial plan.
type UnaryExpression interface {
	Expression
	isUnaryExpr()
}

// BinaryExpression is the common interface for all binary expressions in a
// physcial plan.
type BinaryExpression interface {
	Expression
	isBinaryExpr()
}

// LiteralExpression is the common interface for all literal expressions in a
// physcial plan.
type LiteralExpression interface {
	Expression
	ValueType() ValueType
	isLiteralExpr()
}

// ColumnExpression is the common interface for all column expressions in a
// physcial plan.
type ColumnExpression interface {
	Expression
	isColumnExpr()
}

// UnaryExpr is an expression that implements the [UnaryExpression] interface.
type UnaryExpr struct {
	// Left is the expression being operated on
	Left Expression
	// Op is the unary operator to apply to the expression
	Op UnaryOpType
}

func (*UnaryExpr) isExpr()      {}
func (*UnaryExpr) isUnaryExpr() {}

// ID returns the type of the [UnaryExpr].
func (*UnaryExpr) Type() ExpressionType {
	return ExprTypeUnary
}

// BinaryExpr is an expression that implements the [BinaryExpression] interface.
type BinaryExpr struct {
	Left, Right Expression
	Op          BinaryOpType
}

func (*BinaryExpr) isExpr()       {}
func (*BinaryExpr) isBinaryExpr() {}

// ID returns the type of the [BinaryExpr].
func (*BinaryExpr) Type() ExpressionType {
	return ExprTypeBinary
}

// LiteralExpr is an expression that implements the [LiteralExpression] interface.
type LiteralExpr[T bool | int64 | uint64 | string] struct {
	Value T
	ty    ValueType
}

func (*LiteralExpr[T]) isExpr()        {}
func (*LiteralExpr[T]) isLiteralExpr() {}

// ID returns the type of the [LiteralExpr].
func (*LiteralExpr[T]) Type() ExpressionType {
	return ExprTypeLiteral
}

// ValueType returns the type of the literal value.
func (e *LiteralExpr[T]) ValueType() ValueType {
	return e.ty
}

// newBooleanLiteral is a convenience function for creating boolean literals.
func newBooleanLiteral(value bool) *LiteralExpr[bool] {
	return &LiteralExpr[bool]{
		Value: value,
		ty:    ValueTypeBool,
	}
}

// newInt64Literal is a convenience function for creating int64 literals.
func newInt64Literal(value int64) *LiteralExpr[int64] {
	return &LiteralExpr[int64]{
		Value: value,
		ty:    ValueTypeInt64,
	}
}

// newTimestampLiteral is a convenience function for creating timestamp literals.
func newTimestampLiteral(value uint64) *LiteralExpr[uint64] {
	return &LiteralExpr[uint64]{
		Value: value,
		ty:    ValueTypeTimestamp,
	}
}

// newStringLiteral is a convenience function for creating string literals.
func newStringLiteral(value string) *LiteralExpr[string] {
	return &LiteralExpr[string]{
		Value: value,
		ty:    ValueTypeString,
	}
}

// ColumnExpr is an expression that implements the [ColumnExpr] interface.
type ColumnExpr struct {
	Name string
}

func (e *ColumnExpr) isExpr()       {}
func (e *ColumnExpr) isColumnExpr() {}

// ID returns the type of the [ColumnExpr].
func (e *ColumnExpr) Type() ExpressionType {
	return ExprTypeColumn
}
