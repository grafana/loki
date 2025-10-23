package executor

import (
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func NewScalar(value types.Literal, rows int) arrow.Array {
	builder := array.NewBuilder(memory.DefaultAllocator, value.Type().ArrowType())

	switch builder := builder.(type) {
	case *array.NullBuilder:
		for range rows {
			builder.AppendNull()
		}
	case *array.BooleanBuilder:
		value := value.Any().(bool)
		for range rows {
			builder.Append(value)
		}
	case *array.StringBuilder:
		value := value.Any().(string)
		for range rows {
			builder.Append(value)
		}
	case *array.Int64Builder:
		var v int64
		switch value.Type() {
		case types.Loki.Integer:
			v = value.Any().(int64)
		case types.Loki.Duration:
			v = int64(value.Any().(types.Duration))
		case types.Loki.Bytes:
			v = int64(value.Any().(types.Bytes))
		}
		for range rows {
			builder.Append(v)
		}
	case *array.Float64Builder:
		value := value.Any().(float64)
		for range rows {
			builder.Append(value)
		}
	case *array.TimestampBuilder:
		value := value.Any().(types.Timestamp)
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
		return types.ColumnTypePrecedence(a.ct) - types.ColumnTypePrecedence(b.ct)
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
