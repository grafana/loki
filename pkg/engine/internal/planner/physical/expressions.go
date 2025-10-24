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

func newColumnExpr(column string, ty physicalpb.ColumnType) *physicalpb.ColumnExpression {
	return &physicalpb.ColumnExpression{
		Name: column,
		Type: ty,
	}
}

func newBinaryExpr(left *physicalpb.Expression, right *physicalpb.Expression, op physicalpb.BinaryOp) *physicalpb.BinaryExpression {
	return &physicalpb.BinaryExpression{Left: left, Right: right, Op: op}
}
