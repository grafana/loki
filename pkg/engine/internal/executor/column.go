package executor

import (
	"fmt"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

type ColumnVector interface {
	arrow.Array
	Impl() arrow.Array
}

func NewColumn(array arrow.Array) *Column {
	return &Column{
		Array: array,
	}
}

type Column struct {
	arrow.Array
}

// Column implements [ColumnVector].
var _ ColumnVector = (*Column)(nil)

func (c *Column) Impl() arrow.Array {
	return c.Array
}

func (c *Column) Value(i int) any {
	if c.Array.IsNull(i) || !c.Array.IsValid(i) {
		return nil
	}

	switch arr := c.Array.(type) {
	case *array.Boolean:
		return arr.Value(i)
	case *array.String:
		return arr.Value(i)
	case *array.Int64:
		return arr.Value(i)
	case *array.Uint64:
		return arr.Value(i)
	case *array.Float64:
		return arr.Value(i)
	case *array.Timestamp:
		return arr.Value(i)
	default:
		return nil
	}
}

func NewScalar(value types.Literal, rows int) *Column {
	return &Column{
		Array: scalarArray(value, rows),
	}
}

func scalarArray(value types.Literal, rows int) arrow.Array {
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

func NewCoalesce(columns []*columnWithType) *Column {
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
	return &Column{
		Array: coalesceArray(columns),
	}
}

func coalesceArray(columns []*columnWithType) arrow.Array {
	builder := array.NewBuilder(memory.DefaultAllocator, columns[0].col.DataType()).(*array.StringBuilder)

	// use Value() method which already handles precedence logic
	for i := 0; i < columns[0].col.Len(); i++ {
		val := firstNotNullValue(i, columns)
		if val == nil {
			builder.AppendNull()
			continue
		}
		if strVal, ok := val.(string); ok {
			builder.Append(strVal)
		} else {
			// Fallback: convert to string representation
			builder.Append(fmt.Sprintf("%v", val))
		}
	}
	return builder.NewArray()
}

func firstNotNullValue(i int, columns []*columnWithType) any {
	for _, col := range columns {
		val := col.col.Value(i)
		if val != nil {
			return val
		}
	}
	return nil
}
