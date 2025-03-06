package physical

type ExpressionType uint32

const (
	EXPR_TYPE_UNARY ExpressionType = iota
	EXPR_TYPE_BINARY
	EXPR_TYPE_LITERAL
	EXPR_TYPE_COLUMN
)

type UnaryOpType uint32

const (
	UNARY_OP_NOT UnaryOpType = iota
	UNARY_OP_ABS
)

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

type BinaryOperationType uint32

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
	isLiteralExpr()
}

type ColumnExpression interface {
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
