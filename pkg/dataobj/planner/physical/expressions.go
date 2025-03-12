package physical

import "fmt"

type ExpressionType uint32

const (
	ExprTypeUnary ExpressionType = iota
	ExprTypeBinary
	ExprTypeLiteral
	ExprTypeColumn
)

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

type UnaryOpType uint32

const (
	UnaryOpNot UnaryOpType = iota
	UnaryOpAbs
)

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

type BinaryOpType uint32

const (
	BinaryOpEq BinaryOpType = iota
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
	BinaryOpNmatchStr
	BinaryOpMatchRe
	BinaryOpNmatchRe
)

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
	case BinaryOpNmatchStr:
		return "NMATCH_STR" // convenience for NOT(MATCH_STR(...))
	case BinaryOpMatchRe:
		return "MATCH_RE"
	case BinaryOpNmatchRe:
		return "NMATCH_RE" // convenience for NOT(MATCH_RE(...))
	default:
		return fmt.Sprintf("unknown binary operator type %d", t)
	}
}

type Expression interface {
	ID() ExpressionType
	isExpr()
}

type UnaryExpression interface {
	Expression
	isUnaryExpr()
}

type BinaryExpression interface {
	Expression
	isBinaryExpr()
}

type LiteralExpression interface {
	Expression
	isLiteralExpr()
}

type ColumnExpression interface {
	Expression
	isColumnExpr()
}

type UnaryExpr[T UnaryOpType] struct {
	Left Expression
	Op   T
}

func (*UnaryExpr[T]) isExpr()      {}
func (*UnaryExpr[T]) isUnaryExpr() {}
func (*UnaryExpr[T]) ID() ExpressionType {
	return ExprTypeUnary
}

type BinaryExpr[T BinaryOpType] struct {
	Left, Right Expression
	Op          T
}

func (*BinaryExpr[T]) isExpr()       {}
func (*BinaryExpr[T]) isBinaryExpr() {}
func (*BinaryExpr[T]) ID() ExpressionType {
	return ExprTypeBinary
}

type LiteralExpr[T any] struct {
	value T
}

func (*LiteralExpr[T]) isExpr()        {}
func (*LiteralExpr[T]) isLiteralExpr() {}
func (*LiteralExpr[T]) ID() ExpressionType {
	return ExprTypeLiteral
}

type ColumnExpr struct {
	name string
}

func (e *ColumnExpr) isExpr()       {}
func (e *ColumnExpr) isColumnExpr() {}
func (e *ColumnExpr) ID() ExpressionType {
	return ExprTypeColumn
}
