package physical

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
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
				Left: NewLiteral(true),
			},
			expected: ExprTypeUnary,
		},
		{
			name: "BinaryExpression",
			expr: &BinaryExpr{
				Op:    types.BinaryOpEq,
				Left:  &ColumnExpr{Ref: types.ColumnRef{Column: "col", Type: types.ColumnTypeBuiltin}},
				Right: NewLiteral("foo"),
			},
			expected: ExprTypeBinary,
		},
		{
			name:     "LiteralExpression",
			expr:     NewLiteral("col"),
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
		require.Equal(t, datatype.Loki.Bool, literal.ValueType())
	})

	t.Run("float", func(t *testing.T) {
		var expr Expression = NewLiteral(123.456789)
		require.Equal(t, ExprTypeLiteral, expr.Type())
		literal, ok := expr.(LiteralExpression)
		require.True(t, ok)
		require.Equal(t, datatype.Loki.Float, literal.ValueType())
	})

	t.Run("integer", func(t *testing.T) {
		var expr Expression = NewLiteral(int64(123456789))
		require.Equal(t, ExprTypeLiteral, expr.Type())
		literal, ok := expr.(LiteralExpression)
		require.True(t, ok)
		require.Equal(t, datatype.Loki.Integer, literal.ValueType())
	})

	t.Run("timestamp", func(t *testing.T) {
		var expr Expression = NewLiteral(datatype.Timestamp(1741882435000000000))
		require.Equal(t, ExprTypeLiteral, expr.Type())
		literal, ok := expr.(LiteralExpression)
		require.True(t, ok)
		require.Equal(t, datatype.Loki.Timestamp, literal.ValueType())
	})

	t.Run("duration", func(t *testing.T) {
		var expr Expression = NewLiteral(datatype.Duration(3600))
		require.Equal(t, ExprTypeLiteral, expr.Type())
		literal, ok := expr.(LiteralExpression)
		require.True(t, ok)
		require.Equal(t, datatype.Loki.Duration, literal.ValueType())
	})

	t.Run("bytes", func(t *testing.T) {
		var expr Expression = NewLiteral(datatype.Bytes(1024))
		require.Equal(t, ExprTypeLiteral, expr.Type())
		literal, ok := expr.(LiteralExpression)
		require.True(t, ok)
		require.Equal(t, datatype.Loki.Bytes, literal.ValueType())
	})

	t.Run("string", func(t *testing.T) {
		var expr Expression = NewLiteral("loki")
		require.Equal(t, ExprTypeLiteral, expr.Type())
		literal, ok := expr.(LiteralExpression)
		require.True(t, ok)
		require.Equal(t, datatype.Loki.String, literal.ValueType())
	})
}
