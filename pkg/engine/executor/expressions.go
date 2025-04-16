package executor

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/errors"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

type expressionEvaluator struct{}

func (e *expressionEvaluator) eval(expr physical.Expression, input arrow.Record) (ColumnVector, error) {
	switch expr := expr.(type) {

	case *physical.LiteralExpr:
		return &Scalar{
			value: expr.Value,
			rows:  input.NumRows(),
		}, nil

	case *physical.ColumnExpr:
		for i := range input.NumCols() {
			if input.ColumnName(int(i)) == expr.Ref.Column {
				return &Array{
					array: input.Column(int(i)),
					rows:  input.NumRows(),
				}, nil
			}
		}
		return nil, fmt.Errorf("unknown column %s: %w", expr.Ref.String(), errors.ErrKey)

	case *physical.UnaryExpr:
		_, err := e.eval(expr.Left, input)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("failed to evaluate unary expression: %w", errors.ErrNotImplemented)

	case *physical.BinaryExpr:
		_, err := e.eval(expr.Left, input)
		if err != nil {
			return nil, err
		}
		_, err = e.eval(expr.Right, input)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("failed to evaluate binary expression: %w", errors.ErrNotImplemented)

	}

	return nil, fmt.Errorf("unknown expression: %v", expr)
}

// ColumnVector represents columnar values from evaluated expressions.
type ColumnVector interface {
	// ToArray returns the underlying Arrow array representation of the column vector.
	ToArray() arrow.Array
	// Value returns the value at the specified index position in the column vector.
	Value(i int64) any
	// Type returns the Arrow data type of the column vector.
	Type() arrow.DataType
}

// Scalar represents a single value repeated any number of times.
type Scalar struct {
	value types.Literal
	rows  int64
}

var _ ColumnVector = (*Scalar)(nil)

// ToArray implements ColumnVector.
func (v *Scalar) ToArray() arrow.Array {
	mem := memory.NewGoAllocator()
	builder := array.NewBuilder(mem, v.Type())
	defer builder.Release()

	for i := int64(0); i < v.rows; i++ {
		switch v.value.ValueType() {
		case types.ValueTypeBool:
			builder.(*array.BooleanBuilder).Append(v.value.Value.(bool))
		case types.ValueTypeStr:
			builder.(*array.StringBuilder).Append(v.value.Value.(string))
		case types.ValueTypeInt:
			builder.(*array.Int64Builder).Append(v.value.Value.(int64))
		case types.ValueTypeFloat:
			builder.(*array.Float64Builder).Append(v.value.Value.(float64))
		case types.ValueTypeTimestamp:
			builder.(*array.Uint64Builder).Append(v.value.Value.(uint64))
		default:
			builder.AppendNull()
		}
	}

	return builder.NewArray()
}

// Value implements ColumnVector.
func (v *Scalar) Value(_ int64) any {
	return v.value.Value
}

// Type implements ColumnVector.
func (v Scalar) Type() arrow.DataType {
	switch v.value.ValueType() {
	case types.ValueTypeBool:
		return arrow.FixedWidthTypes.Boolean
	case types.ValueTypeStr:
		return arrow.BinaryTypes.String
	case types.ValueTypeInt:
		return arrow.PrimitiveTypes.Int64
	case types.ValueTypeFloat:
		return arrow.PrimitiveTypes.Float64
	case types.ValueTypeTimestamp:
		return arrow.PrimitiveTypes.Uint64
	default:
		return arrow.Null
	}
}

// Array represents a column of data, stored as an [arrow.Array].
type Array struct {
	array arrow.Array
	rows  int64
}

// ToArray implements ColumnVector.
func (a *Array) ToArray() arrow.Array {
	return a.array
}

// Value implements ColumnVector.
func (a *Array) Value(i int64) any {
	return a.array.ValueStr(int(i))
}

// Type implements ColumnVector.
func (a *Array) Type() arrow.DataType {
	return a.array.DataType()
}

var _ ColumnVector = (*Array)(nil)
