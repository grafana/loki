package physical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func NewLiteral(value types.LiteralType) *physicalpb.LiteralExpression {
	if value == nil {
		return &physicalpb.LiteralExpression{Kind: &physicalpb.LiteralExpression_NullLiteral{}}
	}
	switch val := any(value).(type) {
	case bool:
		return &physicalpb.LiteralExpression{Kind: &physicalpb.LiteralExpression_BoolLiteral{BoolLiteral: &physicalpb.BoolLiteral{Value: val}}}
	case string:
		return &physicalpb.LiteralExpression{Kind: &physicalpb.LiteralExpression_StringLiteral{StringLiteral: &physicalpb.StringLiteral{Value: val}}}
	case int64:
		return &physicalpb.LiteralExpression{Kind: &physicalpb.LiteralExpression_IntegerLiteral{IntegerLiteral: &physicalpb.IntegerLiteral{Value: val}}}
	case float64:
		return &physicalpb.LiteralExpression{Kind: &physicalpb.LiteralExpression_FloatLiteral{FloatLiteral: &physicalpb.FloatLiteral{Value: val}}}
	case types.Timestamp:
		return &physicalpb.LiteralExpression{Kind: &physicalpb.LiteralExpression_TimestampLiteral{TimestampLiteral: &physicalpb.TimestampLiteral{Value: int64(val)}}}
	case types.Duration:
		return &physicalpb.LiteralExpression{Kind: &physicalpb.LiteralExpression_DurationLiteral{DurationLiteral: &physicalpb.DurationLiteral{Value: int64(val)}}}
	case types.Bytes:
		return &physicalpb.LiteralExpression{Kind: &physicalpb.LiteralExpression_BytesLiteral{BytesLiteral: &physicalpb.BytesLiteral{Value: int64(val)}}}
	default:
		panic(fmt.Sprintf("invalid literal value type %T", value))
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

func newColumnExpr(column string, ty physicalpb.ColumnType) *physicalpb.ColumnExpression {
	return &physicalpb.ColumnExpression{
		Name: column,
		Type: ty,
	}
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

// ID returns the type of the [BinaryExpr].
func (*BinaryExpr) Type() ExpressionType {
	return ExprTypeBinary
}

// LiteralExpr is an expression that implements the [LiteralExpression] interface.
type LiteralExpr struct {
	types.Literal
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

// String returns the string representation of the column expression.
// It contains of the name of the column and its type, joined by a dot (`.`).
func (e *ColumnExpr) String() string {
	return e.Ref.String()
}

// ID returns the type of the [ColumnExpr].
func (e *ColumnExpr) Type() ExpressionType {
	return ExprTypeColumn
}
