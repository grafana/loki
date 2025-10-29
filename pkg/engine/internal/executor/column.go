package executor

import (
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
)

func NewScalar(value physicalpb.LiteralExpression, rows int) arrow.Array {
	var tmp arrow.DataType
	switch value.Kind.(type) {
	case *physicalpb.LiteralExpression_NullLiteral:
		tmp = arrow.Null
	case *physicalpb.LiteralExpression_BoolLiteral:
		tmp = arrow.FixedWidthTypes.Boolean
	case *physicalpb.LiteralExpression_StringLiteral:
		tmp = arrow.BinaryTypes.String
	case *physicalpb.LiteralExpression_IntegerLiteral, *physicalpb.LiteralExpression_DurationLiteral, *physicalpb.LiteralExpression_BytesLiteral:
		tmp = arrow.PrimitiveTypes.Int64
	case *physicalpb.LiteralExpression_FloatLiteral:
		tmp = arrow.PrimitiveTypes.Float64
	case *physicalpb.LiteralExpression_TimestampLiteral:
		tmp = arrow.FixedWidthTypes.Timestamp_ns
	}
	builder := array.NewBuilder(memory.DefaultAllocator, tmp)

	switch builder := builder.(type) {
	case *array.NullBuilder:
		for range rows {
			builder.AppendNull()
		}
	case *array.BooleanBuilder:
		value := value.GetBoolLiteral().Value
		for range rows {
			builder.Append(value)
		}
	case *array.StringBuilder:
		value := value.GetStringLiteral().Value
		for range rows {
			builder.Append(value)
		}
	case *array.Int64Builder:
		var v int64
		switch value.Kind.(type) {
		case *physicalpb.LiteralExpression_IntegerLiteral:
			v = value.GetIntegerLiteral().Value
		case *physicalpb.LiteralExpression_DurationLiteral:
			v = value.GetDurationLiteral().Value
		case *physicalpb.LiteralExpression_BytesLiteral:
			v = int64(value.GetBytesLiteral().Value)
		}
		for range rows {
			builder.Append(v)
		}
	case *array.Float64Builder:
		value := value.GetFloatLiteral().Value
		for range rows {
			builder.Append(value)
		}
	case *array.TimestampBuilder:
		value := value.GetTimestampLiteral().Value
		for range rows {
			builder.Append(arrow.Timestamp(value))
		}
	}
	return builder.NewArray()
}

func NewCoalesce(columns []*columnWithType) arrow.Array {
	if len(columns) == 0 {
		return nil
	}
	if len(columns) == 1 {
		return columns[0].col
	}

	// Sort columns by precedence
	slices.SortFunc(columns, func(a, b *columnWithType) int {
		return physicalpb.ColumnTypePrecedence(a.ct) - physicalpb.ColumnTypePrecedence(b.ct)
	})

	// Only string columns are supported
	builder := array.NewBuilder(memory.DefaultAllocator, columns[0].col.DataType()).(*array.StringBuilder)
	for i := 0; i < columns[0].col.Len(); i++ {
		val, isNull := firstNotNullValue(i, columns)
		if isNull {
			builder.AppendNull()
			continue
		}
		builder.Append(val)
	}
	return builder.NewArray()
}

func firstNotNullValue(i int, columns []*columnWithType) (string, bool) {
	for _, col := range columns {
		if col.col.IsNull(i) || !col.col.IsValid(i) {
			continue
		}
		return col.col.(*array.String).Value(i), false
	}
	return "", true
}
