package physical

import "fmt"

type ExpressionType uint32

const (
	EXPR_TYPE_UNARY ExpressionType = iota
	EXPR_TYPE_BINARY
	EXPR_TYPE_LITERAL
	EXPR_TYPE_COLUMN
)

func (t ExpressionType) String() string {
	switch t {
	case EXPR_TYPE_UNARY:
		return "UnaryExpression"
	case EXPR_TYPE_BINARY:
		return "BinaryExpression"
	case EXPR_TYPE_LITERAL:
		return "LiteralExpression"
	case EXPR_TYPE_COLUMN:
		return "ColumnExpression"
	default:
		return fmt.Sprintf("unknown expression type %d", t)
	}
}

type UnaryOpType uint32

const (
	UNARY_OP_NOT UnaryOpType = iota
	UNARY_OP_ABS
)

func (t UnaryOpType) String() string {
	switch t {
	case UNARY_OP_NOT:
		return "NOT"
	case UNARY_OP_ABS:
		return "ABS"
	default:
		return fmt.Sprintf("unknown unary operator type %d", t)
	}
}

type BinaryOpType uint32

const (
	BINARY_OP_EQ BinaryOpType = iota
	BINARY_OP_NEQ
	BINARY_OP_GT
	BINARY_OP_GTE
	BINARY_OP_LT
	BINARY_OP_LTE
	BINARY_OP_AND
	BINARY_OP_OR
	BINARY_OP_XOR
	BINARY_OP_NOT
	BINARY_OP_ADD
	BINARY_OP_SUB
	BINARY_OP_MUL
	BINARY_OP_DIV
	BINARY_OP_MOD
	BINARY_OP_MATCH_STR
	BINARY_OP_NMATCH_STR
	BINARY_OP_MATCH_RE
	BINARY_OP_NMATCH_RE
)

func (t BinaryOpType) String() string {
	switch t {
	case BINARY_OP_EQ:
		return "EQ"
	case BINARY_OP_NEQ:
		return "NEQ" // convenience for NOT(EQ(expr))
	case BINARY_OP_GT:
		return "GT"
	case BINARY_OP_GTE:
		return "GTE"
	case BINARY_OP_LT:
		return "LT" // convenience for NOT(GTE(expr))
	case BINARY_OP_LTE:
		return "LTE" // convenience for NOT(GT(expr))
	case BINARY_OP_AND:
		return "AND"
	case BINARY_OP_OR:
		return "OR"
	case BINARY_OP_XOR:
		return "XOR"
	case BINARY_OP_NOT:
		return "NOT"
	case BINARY_OP_ADD:
		return "ADD"
	case BINARY_OP_SUB:
		return "SUB"
	case BINARY_OP_MUL:
		return "MUL"
	case BINARY_OP_DIV:
		return "DIV"
	case BINARY_OP_MOD:
		return "MOD"
	case BINARY_OP_MATCH_STR:
		return "MATCH_STR"
	case BINARY_OP_NMATCH_STR:
		return "NMATCH_STR" // convenience for NOT(MATCH_STR(...))
	case BINARY_OP_MATCH_RE:
		return "MATCH_RE"
	case BINARY_OP_NMATCH_RE:
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
	return EXPR_TYPE_UNARY
}

type BinaryExpr[T BinaryOpType] struct {
	Left, Right Expression
	Op          T
}

func (*BinaryExpr[T]) isExpr()       {}
func (*BinaryExpr[T]) isBinaryExpr() {}
func (*BinaryExpr[T]) ID() ExpressionType {
	return EXPR_TYPE_BINARY
}

type LiteralExpr[T any] struct {
	value T
}

func (*LiteralExpr[T]) isExpr()        {}
func (*LiteralExpr[T]) isLiteralExpr() {}
func (*LiteralExpr[T]) ID() ExpressionType {
	return EXPR_TYPE_LITERAL
}

type ColumnExpr struct {
	name string
}

func (e *ColumnExpr) isExpr()       {}
func (e *ColumnExpr) isColumnExpr() {}
func (e *ColumnExpr) ID() ExpressionType {
	return EXPR_TYPE_COLUMN
}
