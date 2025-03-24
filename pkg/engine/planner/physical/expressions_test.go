package physical

import (
	"testing"

	"github.com/stretchr/testify/require"
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
				Op:   UnaryOpNot,
				Left: &LiteralExpr[bool]{Value: true},
			},
			expected: ExprTypeUnary,
		},
		{
			name: "BinaryExpression",
			expr: &BinaryExpr{
				Op:    BinaryOpEq,
				Left:  &ColumnExpr{Name: "col"},
				Right: &LiteralExpr[string]{Value: "foo"},
			},
			expected: ExprTypeBinary,
		},
		{
			name: "LiteralExpression",
			expr: &LiteralExpr[string]{
				Value: "col",
			},
			expected: ExprTypeLiteral,
		},
		{
			name: "ColumnExpression",
			expr: &ColumnExpr{
				Name: "log",
			},
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

	t.Run("bool", func(t *testing.T) {
		var expr Expression = newBooleanLiteral(true)
		require.Equal(t, ExprTypeLiteral, expr.Type())
		literal, ok := expr.(LiteralExpression)
		require.True(t, ok)
		require.Equal(t, ValueTypeBool, literal.ValueType())
	})

	t.Run("int64", func(t *testing.T) {
		var expr Expression = newInt64Literal(123456789)
		require.Equal(t, ExprTypeLiteral, expr.Type())
		literal, ok := expr.(LiteralExpression)
		require.True(t, ok)
		require.Equal(t, ValueTypeInt64, literal.ValueType())
	})

	t.Run("timestamp", func(t *testing.T) {
		var expr Expression = newTimestampLiteral(1741882435000000000)
		require.Equal(t, ExprTypeLiteral, expr.Type())
		literal, ok := expr.(LiteralExpression)
		require.True(t, ok)
		require.Equal(t, ValueTypeTimestamp, literal.ValueType())
	})

	t.Run("string", func(t *testing.T) {
		var expr Expression = newStringLiteral("loki")
		require.Equal(t, ExprTypeLiteral, expr.Type())
		literal, ok := expr.(LiteralExpression)
		require.True(t, ok)
		require.Equal(t, ValueTypeString, literal.ValueType())
	})
}
