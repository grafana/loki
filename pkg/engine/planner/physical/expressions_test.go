package physical

import (
	"testing"
	"time"

	"github.com/apache/arrow/go/v18/arrow"
	"github.com/apache/arrow/go/v18/arrow/array"
	"github.com/apache/arrow/go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/errors"
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

func TestEvaluateLiteralExpression(t *testing.T) {
	for _, tt := range []struct {
		name      string
		value     any
		arrowType arrow.Type
	}{
		{
			name:      "null",
			value:     nil,
			arrowType: arrow.NULL,
		},
		{
			name:      "bool",
			value:     true,
			arrowType: arrow.BOOL,
		},
		{
			name:      "str",
			value:     "loki",
			arrowType: arrow.STRING,
		},
		{
			name:      "int",
			value:     int64(123456789),
			arrowType: arrow.INT64,
		},
		{
			name:      "float",
			value:     float64(123.456789),
			arrowType: arrow.FLOAT64,
		},
		{
			name:      "timestamp",
			value:     uint64(1744612881740032450),
			arrowType: arrow.UINT64,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			literal := NewLiteral(tt.value)

			n := len(words)
			rec := batch(n, time.Now())
			colVec, err := literal.Evaluate(rec)
			require.NoError(t, err)
			require.Equal(t, tt.arrowType, colVec.Type())

			for i := range n {
				val := colVec.Value(int64(i))
				require.Equal(t, tt.value, val)
			}
		})
	}
}

func TestEvaluateColumnExpression(t *testing.T) {
	t.Run("invalid", func(t *testing.T) {
		colExpr := newColumnExpr("does_not_exist", types.ColumnTypeBuiltin)

		n := len(words)
		rec := batch(n, time.Now())
		_, err := colExpr.Evaluate(rec)
		require.Equal(t, err, errors.ErrKey)
	})

	t.Run("string(log)", func(t *testing.T) {
		colExpr := newColumnExpr("log", types.ColumnTypeBuiltin)

		n := len(words)
		rec := batch(n, time.Now())
		colVec, err := colExpr.Evaluate(rec)
		require.NoError(t, err)
		require.Equal(t, arrow.STRING, colVec.Type())

		for i := range n {
			val := colVec.Value(int64(i))
			require.Equal(t, words[i%len(words)], val)
		}
	})
}

var words = []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}

func batch(n int, now time.Time) arrow.Record {
	// 1. Create a memory allocator
	mem := memory.NewGoAllocator()

	// 2. Define the schema
	schema := arrow.NewSchema(
		[]arrow.Field{
			{Name: "log", Type: arrow.BinaryTypes.String},
			{Name: "timestamp", Type: arrow.PrimitiveTypes.Uint64},
		},
		nil, // No metadata
	)

	// 3. Create builders for each column

	logBuilder := array.NewStringBuilder(mem)
	defer logBuilder.Release()

	tsBuilder := array.NewUint64Builder(mem)
	defer tsBuilder.Release()

	// 4. Append data to the builders
	logs := make([]string, n)
	ts := make([]uint64, n)

	for i := range n {
		logs[i] = words[i%len(words)]
		ts[i] = uint64(now.Add(time.Duration(i) * time.Second).UnixNano())
	}

	tsBuilder.AppendValues(ts, nil)
	logBuilder.AppendValues(logs, nil)

	// 5. Build the arrays
	logArray := logBuilder.NewArray()
	defer logArray.Release()

	tsArray := tsBuilder.NewArray()
	defer tsArray.Release()

	// 6. Create the record
	columns := []arrow.Array{logArray, tsArray}
	record := array.NewRecord(schema, columns, int64(n))

	return record
}
