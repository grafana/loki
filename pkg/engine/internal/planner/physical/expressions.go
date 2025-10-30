package physical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func NewLiteral(value types.LiteralType) *LiteralExpression {
	if value == nil {
		return &LiteralExpression{Kind: &LiteralExpression_NullLiteral{}}
	}
	switch val := any(value).(type) {
	case bool:
		return &LiteralExpression{Kind: &LiteralExpression_BoolLiteral{BoolLiteral: &BoolLiteral{Value: val}}}
	case string:
		return &LiteralExpression{Kind: &LiteralExpression_StringLiteral{StringLiteral: &StringLiteral{Value: val}}}
	case int64:
		return &LiteralExpression{Kind: &LiteralExpression_IntegerLiteral{IntegerLiteral: &IntegerLiteral{Value: val}}}
	case float64:
		return &LiteralExpression{Kind: &LiteralExpression_FloatLiteral{FloatLiteral: &FloatLiteral{Value: val}}}
	case types.Timestamp:
		return &LiteralExpression{Kind: &LiteralExpression_TimestampLiteral{TimestampLiteral: &TimestampLiteral{Value: int64(val)}}}
	case types.Duration:
		return &LiteralExpression{Kind: &LiteralExpression_DurationLiteral{DurationLiteral: &DurationLiteral{Value: int64(val)}}}
	case types.Bytes:
		return &LiteralExpression{Kind: &LiteralExpression_BytesLiteral{BytesLiteral: &BytesLiteral{Value: int64(val)}}}
	default:
		panic(fmt.Sprintf("invalid literal value type %T", value))
	}
}

func newColumnExpr(column string, ty ColumnType) *ColumnExpression {
	return &ColumnExpression{
		Name: column,
		Type: ty,
	}
}

func newBinaryExpr(left *Expression, right *Expression, op BinaryOp) *BinaryExpression {
	return &BinaryExpression{Left: left, Right: right, Op: op}
}
