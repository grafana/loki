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

func (e expressionEvaluator) eval(expr physical.Expression, input arrow.Record) (ColumnVector, error) {
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
		lhr, err := e.eval(expr.Left, input)
		if err != nil {
			return nil, err
		}

		fn, err := unaryFunctions.GetForSignature(expr.Op, lhr.Type())
		if err != nil {
			return nil, fmt.Errorf("failed to lookup unary function: %w", err)
		}
		return fn.Evaluate(lhr)

	case *physical.BinaryExpr:
		lhs, err := e.eval(expr.Left, input)
		if err != nil {
			return nil, err
		}
		rhs, err := e.eval(expr.Right, input)
		if err != nil {
			return nil, err
		}

		// At the moment we only support functions that accept the same input types.
		if lhs.Type().ID() != rhs.Type().ID() {
			return nil, fmt.Errorf("failed to lookup binary function for signature %v(%v,%v): types do not match", expr.Op, lhs.Type(), rhs.Type())
		}

		fn, err := binaryFunctions.GetForSignature(expr.Op, lhs.Type())
		if err != nil {
			return nil, fmt.Errorf("failed to lookup binary function for signature %v(%v,%v): %w", expr.Op, lhs.Type(), rhs.Type(), err)
		}
		return fn.Evaluate(lhs, rhs)
	}

	return nil, fmt.Errorf("unknown expression: %v", expr)
}

// newFunc returns a new function that can evaluate an input against a binded expression.
func (e expressionEvaluator) newFunc(expr physical.Expression) evalFunc {
	return func(input arrow.Record) (ColumnVector, error) {
		return e.eval(expr, input)
	}
}

type evalFunc func(input arrow.Record) (ColumnVector, error)

// ColumnVector represents columnar values from evaluated expressions.
type ColumnVector interface {
	// ToArray returns the underlying Arrow array representation of the column vector.
	ToArray() arrow.Array
	// Value returns the value at the specified index position in the column vector.
	Value(i int) any
	// Type returns the Arrow data type of the column vector.
	Type() arrow.DataType
	// Len returns the length of the vector
	Len() int64
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
func (v *Scalar) Value(_ int) any {
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

// Len implements ColumnVector.
func (v Scalar) Len() int64 {
	return v.rows
}

// Array represents a column of data, stored as an [arrow.Array].
type Array struct {
	array arrow.Array
	rows  int64
}

var _ ColumnVector = (*Array)(nil)

// ToArray implements ColumnVector.
func (a *Array) ToArray() arrow.Array {
	return a.array
}

// Value implements ColumnVector.
func (a *Array) Value(i int) any {
	if a.array.IsNull(i) || !a.array.IsValid(i) {
		return nil
	}

	switch arr := a.array.(type) {
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
	default:
		return nil
	}
}

// Type implements ColumnVector.
func (a *Array) Type() arrow.DataType {
	return a.array.DataType()
}

// Len implements ColumnVector.
func (a *Array) Len() int64 {
	return int64(a.array.Len())
}
