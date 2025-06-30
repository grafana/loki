package executor

import (
	"fmt"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
)

// Column type precedence for ambiguous column resolution (highest to lowest):
// Generated > Parsed > Metadata > Label > Builtin
const (
	PrecedenceGenerated = iota // 0 - highest precedence
	PrecedenceParsed           // 1
	PrecedenceMetadata         // 2
	PrecedenceLabel            // 3
	PrecedenceBuiltin          // 4 - lowest precedence
)

func getColumnTypePrecedence(ct types.ColumnType) int {
	switch ct {
	case types.ColumnTypeGenerated:
		return PrecedenceGenerated
	case types.ColumnTypeParsed:
		return PrecedenceParsed
	case types.ColumnTypeMetadata:
		return PrecedenceMetadata
	case types.ColumnTypeLabel:
		return PrecedenceLabel
	default:
		return PrecedenceBuiltin // Default to lowest precedence
	}
}

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
		fieldIndices := input.Schema().FieldIndices(expr.Ref.Column)
		if len(fieldIndices) > 0 {
			// For non-ambiguous look-ups, look for an exact match
			if expr.Ref.Type != types.ColumnTypeAmbiguous {
				for _, idx := range fieldIndices {
					field := input.Schema().Field(idx)
					dt, ok := field.Metadata.GetValue(types.MetadataKeyColumnDataType)
					if !ok {
						continue
					}

					ct, ok := field.Metadata.GetValue(types.MetadataKeyColumnType)
					if !ok || ct != expr.Ref.Type.String() {
						continue
					}

					return &Array{
						array: input.Column(idx),
						dt:    datatype.FromString(dt),
						ct:    types.ColumnTypeFromString(ct),
						rows:  input.NumRows(),
					}, nil
				}
			} else {
				// For ambiguous columns, collect all matching columns and order by precedence
				var vecs []ColumnVector
				for _, idx := range fieldIndices {
					field := input.Schema().Field(idx)
					dt, ok := field.Metadata.GetValue(types.MetadataKeyColumnDataType)
					if !ok {
						continue
					}

					ct, ok := field.Metadata.GetValue(types.MetadataKeyColumnType)
					if !ok {
						continue
					}

					// TODO(ashwanth): Support other data types in CoalesceVector.
					// For now, ensure all vectors are strings to avoid type conflicts.
					if datatype.String.String() != dt {
						return nil, fmt.Errorf("column %s has datatype %s, but expression expects string", expr.Ref.Column, dt)
					}

					vecs = append(vecs, &Array{
						array: input.Column(idx),
						dt:    datatype.FromString(dt),
						ct:    types.ColumnTypeFromString(ct),
						rows:  input.NumRows(),
					})
				}

				if len(vecs) > 1 {
					// Multiple matches - sort by precedence and create CoalesceVector
					slices.SortFunc(vecs, func(a, b ColumnVector) int {
						return getColumnTypePrecedence(a.ColumnType()) - getColumnTypePrecedence(b.ColumnType())
					})

					return &CoalesceVector{
						vectors: vecs,
						rows:    input.NumRows(),
					}, nil
				} else if len(vecs) == 1 {
					return vecs[0], nil
				}
			}

		}

		// A non-existent column is represented as a string scalar with zero-value.
		// This reflects current behaviour, where a label filter `| foo=""` would match all if `foo` is not defined.
		return &Scalar{
			value: datatype.NewStringLiteral(""),
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

// CoalesceVector represents multiple columns with the same name but different [types.ColumnType]
// Vectors are ordered by precedence (highest precedence first).
type CoalesceVector struct {
	vectors []ColumnVector // Ordered by precedence (Generated first, Label last)
	rows    int64
}

var _ ColumnVector = (*CoalesceVector)(nil)

// ToArray implements [ColumnVector].
func (m *CoalesceVector) ToArray() arrow.Array {
	mem := memory.NewGoAllocator()
	builder := array.NewBuilder(mem, m.Type().ArrowType())
	defer builder.Release()

	// use Value() method which already handles precedence logic
	for i := 0; i < int(m.rows); i++ {
		val := m.Value(i)
		if val == nil {
			builder.AppendNull()
		} else {
			// [CoalesceVector] only supports [datatype.String] for now
			if strVal, ok := val.(string); ok {
				builder.(*array.StringBuilder).Append(strVal)
			} else {
				// Fallback: convert to string representation
				builder.(*array.StringBuilder).Append(fmt.Sprintf("%v", val))
			}
		}
	}

	return builder.NewArray()
}

// Value returns the value at the specified index position considering the precedence rules.
func (m *CoalesceVector) Value(i int) any {
	// Try each vector in precedence order
	for _, vec := range m.vectors {
		if val := vec.Value(i); val != nil {
			return val
		}
	}
	return nil
}

// Type implements ColumnVector.
func (m *CoalesceVector) Type() datatype.DataType {
	// TODO: Support other data types in CoalesceVector.
	return datatype.String
}

// ColumnType implements ColumnVector.
func (m *CoalesceVector) ColumnType() types.ColumnType {
	return types.ColumnTypeAmbiguous
}

// Len implements ColumnVector.
func (m *CoalesceVector) Len() int64 {
	return m.rows
}
