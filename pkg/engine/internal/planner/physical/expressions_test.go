package physical

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util"
)

func TestBinaryExprClampTimestamp(t *testing.T) {
	tr := TimeRange{
		Start: time.Date(2026, 3, 14, 16, 43, 30, 0, time.UTC),
		End:   time.Date(2026, 3, 14, 16, 48, 0, 0, time.UTC),
	}
	col := newColumnExpr(types.ColumnNameBuiltinTimestamp, types.ColumnTypeBuiltin)
	early := types.Timestamp(time.Date(2026, 3, 11, 12, 41, 44, 719000000, time.UTC).UnixNano())
	late := types.Timestamp(time.Date(2026, 3, 18, 9, 41, 44, 976217699, time.UTC).UnixNano())

	t.Run("GTE before range clamps to start", func(t *testing.T) {
		e := &BinaryExpr{Left: col, Right: NewLiteral(early), Op: types.BinaryOpGte}
		got := e.Clamp(tr).String()
		want := fmt.Sprintf("GTE(%s, %s)", col, util.FormatTimeRFC3339Nano(tr.Start))
		require.Equal(t, want, got)
		require.Equal(t, fmt.Sprintf("GTE(%s, %s)", col, util.FormatTimeRFC3339Nano(time.Unix(0, int64(early)))), e.String())
	})

	t.Run("LT after range clamps to end", func(t *testing.T) {
		e := &BinaryExpr{Left: col, Right: NewLiteral(late), Op: types.BinaryOpLt}
		got := e.Clamp(tr).String()
		want := fmt.Sprintf("LT(%s, %s)", col, util.FormatTimeRFC3339Nano(tr.End))
		require.Equal(t, want, got)
	})

	t.Run("zero TimeRange leaves expression unchanged", func(t *testing.T) {
		e := &BinaryExpr{Left: col, Right: NewLiteral(early), Op: types.BinaryOpGte}
		require.Equal(t, e.String(), e.Clamp(TimeRange{}).String())
	})

	t.Run("non-timestamp binary expr unchanged", func(t *testing.T) {
		e := &BinaryExpr{
			Left:  newColumnExpr("level", types.ColumnTypeLabel),
			Right: NewLiteral("info"),
			Op:    types.BinaryOpEq,
		}
		require.Equal(t, e.String(), e.Clamp(tr).String())
	})
}

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
		require.Equal(t, types.Loki.Bool, literal.ValueType())
	})

	t.Run("float", func(t *testing.T) {
		var expr Expression = NewLiteral(123.456789)
		require.Equal(t, ExprTypeLiteral, expr.Type())
		literal, ok := expr.(LiteralExpression)
		require.True(t, ok)
		require.Equal(t, types.Loki.Float, literal.ValueType())
	})

	t.Run("integer", func(t *testing.T) {
		var expr Expression = NewLiteral(int64(123456789))
		require.Equal(t, ExprTypeLiteral, expr.Type())
		literal, ok := expr.(LiteralExpression)
		require.True(t, ok)
		require.Equal(t, types.Loki.Integer, literal.ValueType())
	})

	t.Run("timestamp", func(t *testing.T) {
		var expr Expression = NewLiteral(types.Timestamp(1741882435000000000))
		require.Equal(t, ExprTypeLiteral, expr.Type())
		literal, ok := expr.(LiteralExpression)
		require.True(t, ok)
		require.Equal(t, types.Loki.Timestamp, literal.ValueType())
	})

	t.Run("duration", func(t *testing.T) {
		var expr Expression = NewLiteral(types.Duration(3600))
		require.Equal(t, ExprTypeLiteral, expr.Type())
		literal, ok := expr.(LiteralExpression)
		require.True(t, ok)
		require.Equal(t, types.Loki.Duration, literal.ValueType())
	})

	t.Run("bytes", func(t *testing.T) {
		var expr Expression = NewLiteral(types.Bytes(1024))
		require.Equal(t, ExprTypeLiteral, expr.Type())
		literal, ok := expr.(LiteralExpression)
		require.True(t, ok)
		require.Equal(t, types.Loki.Bytes, literal.ValueType())
	})

	t.Run("string", func(t *testing.T) {
		var expr Expression = NewLiteral("loki")
		require.Equal(t, ExprTypeLiteral, expr.Type())
		literal, ok := expr.(LiteralExpression)
		require.True(t, ok)
		require.Equal(t, types.Loki.String, literal.ValueType())
	})
}
