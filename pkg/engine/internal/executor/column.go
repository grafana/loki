package executor

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

type ColumnVector interface {
	Ident() *semconv.Identifier
	Field() arrow.Field
	Array() arrow.Array

	// TODO(chaudum): Should we expose convenience functions?
	// Len() int
	// LokiType() types.DataType
	// ArrayType() array.Type
}

func ColumnFrom(field arrow.Field, array arrow.Array) *Column {
	ident := semconv.MustParseFQN(field.Name)
	return &Column{
		ident: ident,
		array: array,
		field: field,
	}
}

func NewColumn(ident *semconv.Identifier, array arrow.Array) *Column {
	return &Column{
		ident: ident,
		array: array,
		field: semconv.FieldFromIdent(ident, true),
	}
}

type Column struct {
	ident *semconv.Identifier
	field arrow.Field
	array arrow.Array
}

// Column implements [ColumnVector].
var _ ColumnVector = (*Column)(nil)

func (c *Column) Ident() *semconv.Identifier {
	return c.ident
}

func (c *Column) Field() arrow.Field {
	return c.field
}

func (c *Column) Array() arrow.Array {
	return c.array
}

func (c *Column) ArrowType() arrow.DataType {
	return c.ident.DataType().ArrowType()
}

func (c *Column) LokiType() types.DataType {
	return c.ident.DataType()
}

func (c *Column) ColumnType() types.ColumnType {
	return c.ident.ColumnType()
}

func (c *Column) Len() int {
	return c.array.Len()
}

func (c *Column) Value(i int) any {
	if c.array.IsNull(i) || !c.array.IsValid(i) {
		return nil
	}

	switch arr := c.array.(type) {
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
	ident := semconv.NewIdentifier("__scalar__", types.ColumnTypeGenerated, value.Type())
	return &Column{
		ident: ident,
		array: scalarArray(value, rows),
		field: semconv.FieldFromIdent(ident, true),
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

func NewCoalesce(columns []*Column) *Column {
	if len(columns) == 0 {
		return nil
	}
	if len(columns) == 1 {
		return columns[0]
	}
	ident := semconv.NewIdentifier(fmt.Sprintf("__coalesce_%s__", columns[0].Ident().ShortName()), types.ColumnTypeAmbiguous, columns[0].LokiType())
	return &Column{
		ident: ident,
		array: coalesceArray(columns),
		field: semconv.FieldFromIdent(ident, true),
	}
}

func coalesceArray(columns []*Column) arrow.Array {
	builder := array.NewBuilder(memory.DefaultAllocator, columns[0].Array().DataType()).(*array.StringBuilder)

	// use Value() method which already handles precedence logic
	for i := 0; i < columns[0].Len(); i++ {
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

func firstNotNullValue(i int, columns []*Column) any {
	for _, col := range columns {
		val := col.Value(i)
		if val != nil {
			return val
		}
	}
	return nil
}
