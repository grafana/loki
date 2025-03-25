package physical

import (
	"fmt"
	"strconv"

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

// UnaryOpType represents the operator of a [UnaryExpression].
type UnaryOpType uint32

const (
	_ UnaryOpType = iota // zero-value is an invalid value

	UnaryOpNot // Logical NOT operation (!)
	UnaryOpAbs // Absolute operation (abs)
)

// String returns the string representation of the [UnaryOpType].
func (t UnaryOpType) String() string {
	switch t {
	case UnaryOpNot:
		return "NOT"
	case UnaryOpAbs:
		return "ABS"
	default:
		panic(fmt.Sprintf("unknown unary operator type %d", t))
	}
}

// BinaryOpType represents the operator of a [BinaryExpression].
type BinaryOpType uint32

const (
	_ BinaryOpType = iota // zero-value is an invalid type

	BinaryOpEq          // Equality comparison (==).
	BinaryOpNeq         // Inequality comparison (!=).
	BinaryOpGt          // Greater than comparison (>).
	BinaryOpGte         // Greater than or equal comparison (>=).
	BinaryOpLt          // Less than comparison (<).
	BinaryOpLte         // Less than or equal comparison (<=).
	BinaryOpAnd         // Logical AND operation (&&).
	BinaryOpOr          // Logical OR operation (||).
	BinaryOpXor         // Logical XOR operation (^).
	BinaryOpNot         // Logicaal NOT operation (!).
	BinaryOpAdd         // Addition operation (+).
	BinaryOpSub         // Subtraction operation (-).
	BinaryOpMul         // Multiplication operation (*).
	BinaryOpDiv         // Division operation (/).
	BinaryOpMod         // Modulo operation (%).
	BinaryOpMatchStr    // String matching comparision (|=).
	BinaryOpNotMatchStr // String not-matching comparison (!=).
	BinaryOpMatchRe     // Regex matching comparison (=~).
	BinaryOpNotMatchRe  // Regex not-matching comparison (!~).
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
		panic(fmt.Sprintf("unknown binary operator type %d", t))
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
	ValueType() types.ValueType
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
	Op UnaryOpType
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
	Op          BinaryOpType
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
	Value any
}

func (*LiteralExpr) isExpr()        {}
func (*LiteralExpr) isLiteralExpr() {}

// String returns the string representation of the literal value.
func (e *LiteralExpr) String() string {
	switch v := e.Value.(type) {
	case nil:
		return "NULL"
	case bool:
		return strconv.FormatBool(v)
	case string:
		return fmt.Sprintf(`"%s"`, v)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case []byte:
		return fmt.Sprintf(`"%s"`, string(v))
	default:
		return "invalid"
	}
}

// ID returns the type of the [LiteralExpr].
func (*LiteralExpr) Type() ExpressionType {
	return ExprTypeLiteral
}

// ValueType returns the kind of value represented by the literal.
func (e *LiteralExpr) ValueType() types.ValueType {
	switch e.Value.(type) {
	case nil:
		return types.ValueTypeNull
	case bool:
		return types.ValueTypeBool
	case string:
		return types.ValueTypeStr
	case int64:
		return types.ValueTypeInt
	case uint64:
		return types.ValueTypeTimestamp
	case []byte:
		return types.ValueTypeBytes
	default:
		return types.ValueTypeInvalid
	}
}

// Convenience function for creating a NULL literal.
func NullLiteral() *LiteralExpr {
	return &LiteralExpr{Value: nil}
}

// Convenience function for creating a bool literal.
func BoolLiteral(v bool) *LiteralExpr {
	return &LiteralExpr{Value: v}
}

// Convenience function for creating a string literal.
func StringLiteral(v string) *LiteralExpr {
	return &LiteralExpr{Value: v}
}

// Convenience function for creating a timestamp literal.
func TimestampLiteral(v uint64) *LiteralExpr {
	return &LiteralExpr{Value: v}
}

// Convenience function for creating an integer literal.
func IntLiteral(v int64) *LiteralExpr {
	return &LiteralExpr{Value: v}
}

// ColumnExpr is an expression that implements the [ColumnExpr] interface.
type ColumnExpr struct {
	Name       string
	ColumnType types.ColumnType
}

func (e *ColumnExpr) isExpr()       {}
func (e *ColumnExpr) isColumnExpr() {}

// String returns the string representation of the column expression.
// It contains of the name of the column and its type, joined by a dot (`.`).
func (e *ColumnExpr) String() string {
	return fmt.Sprintf("%s.%s", e.Name, e.ColumnType)
}

// ID returns the type of the [ColumnExpr].
func (e *ColumnExpr) Type() ExpressionType {
	return ExprTypeColumn
}
