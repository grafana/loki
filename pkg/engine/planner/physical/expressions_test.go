package physical

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func TestExpressionTypes(t *testing.T) {
	tests := []struct {
		name     string
		expr     Expression
		expected ExpressionType
	}{
		{
			name: "UnaryExpression",
			expr: &UnaryExpr{
				Op:   types.UnaryOpNot,
				Left: &LiteralExpr{Value: types.BoolLiteral(true)},
			},
			expected: ExprTypeUnary,
		},
		{
			name: "BinaryExpression",
			expr: &BinaryExpr{
				Op:    types.BinaryOpEq,
				Left:  &ColumnExpr{Ref: types.ColumnRef{Column: "col", Type: types.ColumnTypeBuiltin}},
				Right: &LiteralExpr{Value: types.StringLiteral("foo")},
			},
			expected: ExprTypeBinary,
		},
		{
			name:     "LiteralExpression",
			expr:     &LiteralExpr{Value: types.StringLiteral("col")},
			expected: ExprTypeLiteral,
		},
		{
			name:     "ColumnExpression",
			expr:     &ColumnExpr{Ref: types.ColumnRef{Column: "col", Type: types.ColumnTypeBuiltin}},
			expected: ExprTypeColumn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.expr.Type())
			require.Equal(t, tt.name, tt.expr.Type().String())
		})
	}
}

func TestLiteralExpr(t *testing.T) {

	t.Run("boolean", func(t *testing.T) {
		var expr Expression = NewLiteral(true)
		require.Equal(t, ExprTypeLiteral, expr.Type())
		literal, ok := expr.(LiteralExpression)
		require.True(t, ok)
		require.Equal(t, types.ValueTypeBool, literal.ValueType())
	})

	t.Run("float", func(t *testing.T) {
		var expr Expression = NewLiteral(123.456789)
		require.Equal(t, ExprTypeLiteral, expr.Type())
		literal, ok := expr.(LiteralExpression)
		require.True(t, ok)
		require.Equal(t, types.ValueTypeFloat, literal.ValueType())
	})

	t.Run("integer", func(t *testing.T) {
		var expr Expression = NewLiteral(int64(123456789))
		require.Equal(t, ExprTypeLiteral, expr.Type())
		literal, ok := expr.(LiteralExpression)
		require.True(t, ok)
		require.Equal(t, types.ValueTypeInt, literal.ValueType())
	})

	t.Run("timestamp", func(t *testing.T) {
		var expr Expression = NewLiteral(uint64(1741882435000000000))
		require.Equal(t, ExprTypeLiteral, expr.Type())
		literal, ok := expr.(LiteralExpression)
		require.True(t, ok)
		require.Equal(t, types.ValueTypeTimestamp, literal.ValueType())
	})

	t.Run("string", func(t *testing.T) {
		var expr Expression = NewLiteral("loki")
		require.Equal(t, ExprTypeLiteral, expr.Type())
		literal, ok := expr.(LiteralExpression)
		require.True(t, ok)
		require.Equal(t, types.ValueTypeStr, literal.ValueType())
	})
}
