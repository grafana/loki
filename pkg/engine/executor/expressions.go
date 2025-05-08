package executor

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

type expressionEvaluator struct{}

func (e expressionEvaluator) eval(expr physical.Expression, input arrow.Record) (ColumnVector, error) {
	switch expr := expr.(type) {

	case *physical.LiteralExpr:
		return &Scalar{
			value: expr.Literal,
			rows:  input.NumRows(),
			ct:    types.ColumnTypeAmbiguous,
		}, nil

	case *physical.ColumnExpr:
		schema := input.Schema()
		for i := range input.NumCols() {
			if input.ColumnName(int(i)) == expr.Ref.Column {
				md := schema.Field(int(i)).Metadata
				dt, ok := md.GetValue(types.MetadataKeyColumnDataType)
				if !ok {
					continue
				}
				ct, ok := md.GetValue(types.MetadataKeyColumnType)
				if !ok {
					ct = types.ColumnTypeAmbiguous.String()
				}
				return &Array{
					array: input.Column(int(i)),
					dt:    datatype.FromString(dt),
					ct:    types.ColumnTypeFromString(ct),
					rows:  input.NumRows(),
				}, nil
			}
		}
		// A non-existent column is represented as a string scalar with zero-value.
		// This reflects current behaviour, where a label filter `| foo=""` would match all if `foo` is not defined.
		return &Scalar{
			value: datatype.NewLiteral(""),
			rows:  input.NumRows(),
			ct:    types.ColumnTypeGenerated,
		}, nil

	case *physical.UnaryExpr:
		lhr, err := e.eval(expr.Left, input)
		if err != nil {
			return nil, err
		}

		fn, err := unaryFunctions.GetForSignature(expr.Op, lhr.Type().ArrowType())
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
		if lhs.Type().ArrowType().ID() != rhs.Type().ArrowType().ID() {
			return nil, fmt.Errorf("failed to lookup binary function for signature %v(%v,%v): types do not match", expr.Op, lhs.Type().ArrowType(), rhs.Type().ArrowType())
		}

		fn, err := binaryFunctions.GetForSignature(expr.Op, lhs.Type().ArrowType())
		if err != nil {
			return nil, fmt.Errorf("failed to lookup binary function for signature %v(%v,%v): %w", expr.Op, lhs.Type().ArrowType(), rhs.Type().ArrowType(), err)
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
	// Type returns the Loki data type of the column vector.
	Type() datatype.DataType
	// ColumnType returns the type of column the vector originates from.
	ColumnType() types.ColumnType
	// Len returns the length of the vector
	Len() int64
}

// Scalar represents a single value repeated any number of times.
type Scalar struct {
	value datatype.Literal
	rows  int64
	ct    types.ColumnType
}

var _ ColumnVector = (*Scalar)(nil)

// ToArray implements ColumnVector.
func (v *Scalar) ToArray() arrow.Array {
	mem := memory.NewGoAllocator()
	builder := array.NewBuilder(mem, v.Type().ArrowType())
	defer builder.Release()

	switch builder := builder.(type) {
	case *array.NullBuilder:
		for range v.rows {
			builder.AppendNull()
		}
	case *array.BooleanBuilder:
		value := v.value.Any().(bool)
		for range v.rows {
			builder.Append(value)
		}
	case *array.StringBuilder:
		value := v.value.Any().(string)
		for range v.rows {
			builder.Append(value)
		}
	case *array.Int64Builder:
		value := v.value.Any().(int64)
		for range v.rows {
			builder.Append(value)
		}
	case *array.Float64Builder:
		value := v.value.Any().(float64)
		for range v.rows {
			builder.Append(value)
		}
	}
	return builder.NewArray()
}

// Value implements ColumnVector.
func (v *Scalar) Value(_ int) any {
	return v.value.Any()
}

// Type implements ColumnVector.
func (v *Scalar) Type() datatype.DataType {
	return v.value.Type()
}

// ColumnType implements ColumnVector.
func (v *Scalar) ColumnType() types.ColumnType {
	return v.ct
}

// Len implements ColumnVector.
func (v *Scalar) Len() int64 {
	return v.rows
}

// Array represents a column of data, stored as an [arrow.Array].
type Array struct {
	array arrow.Array
	dt    datatype.DataType
	ct    types.ColumnType
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
func (a *Array) Type() datatype.DataType {
	return a.dt
}

// ColumnType implements ColumnVector.
func (a *Array) ColumnType() types.ColumnType {
	return a.ct
}

// Len implements ColumnVector.
func (a *Array) Len() int64 {
	return int64(a.array.Len())
}
