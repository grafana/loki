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
