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

		if expr.Ref.Type != types.ColumnTypeAmbiguous {
			// For non-ambiguous columns, find the exact match
			for i := range input.NumCols() {
				if input.ColumnName(int(i)) == expr.Ref.Column {
					md := schema.Field(int(i)).Metadata
					dt, ok := md.GetValue(types.MetadataKeyColumnDataType)
					if !ok {
						continue
					}

					ct, ok := md.GetValue(types.MetadataKeyColumnType)
					if !ok {
						continue
					}

					if ct != expr.Ref.Type.String() {
						return nil, fmt.Errorf("column %s has type %s, but expression expects type %s", expr.Ref.Column, ct, expr.Ref.Type)
					}

					return &Array{
						array: input.Column(int(i)),
						dt:    datatype.FromString(dt),
						ct:    types.ColumnTypeFromString(ct),
						rows:  input.NumRows(),
					}, nil
				}
			}
		} else {
			// For ambiguous columns, collect all matching columns and order by precedence
			precedence := map[types.ColumnType]int{
				types.ColumnTypeGenerated: 5,
				types.ColumnTypeParsed:    4,
				types.ColumnTypeMetadata:  3,
				types.ColumnTypeLabel:     2,
				types.ColumnTypeBuiltin:   1,
			}

			var matchingColumns []struct {
				index      int
				columnType types.ColumnType
				dt         string
			}

			for i := range input.NumCols() {
				if input.ColumnName(int(i)) == expr.Ref.Column {
					md := schema.Field(int(i)).Metadata
					dt, ok := md.GetValue(types.MetadataKeyColumnDataType)
					if !ok {
						continue
					}

					// TODO: Support other data types in MultiColumnVector
					// For now, ensure all vectors are strings to avoid type conflicts
					if datatype.String.String() != dt {
						return nil, fmt.Errorf("column %s has datatype %s, but expression expects string", expr.Ref.Column, dt)
					}

					if ct, ok := md.GetValue(types.MetadataKeyColumnType); !ok {
						continue
					} else {
						columnType := types.ColumnTypeFromString(ct)
						// Only include columns that have a defined precedence
						if _, exists := precedence[columnType]; exists {
							matchingColumns = append(matchingColumns, struct {
								index      int
								columnType types.ColumnType
								dt         string
							}{
								index:      int(i),
								columnType: columnType,
								dt:         dt,
							})
						}
					}
				}
			}

			if len(matchingColumns) == 0 {
				// No matching columns found, fall through to default scalar
			} else if len(matchingColumns) == 1 {
				// Single match - return a regular Array for efficiency
				match := matchingColumns[0]
				return &Array{
					array: input.Column(match.index),
					dt:    datatype.FromString(match.dt),
					ct:    match.columnType,
					rows:  input.NumRows(),
				}, nil
			} else {
				// Multiple matches - sort by precedence and create MultiColumnVector
				// Sort in descending order of precedence (highest first)
				for i := 0; i < len(matchingColumns)-1; i++ {
					for j := i + 1; j < len(matchingColumns); j++ {
						if precedence[matchingColumns[i].columnType] < precedence[matchingColumns[j].columnType] {
							matchingColumns[i], matchingColumns[j] = matchingColumns[j], matchingColumns[i]
						}
					}
				}

				// Create vectors for each matching column
				vectors := make([]ColumnVector, len(matchingColumns))
				for i, match := range matchingColumns {
					vectors[i] = &Array{
						array: input.Column(match.index),
						dt:    datatype.FromString(match.dt),
						ct:    match.columnType,
						rows:  input.NumRows(),
					}
				}

				return &MultiColumnVector{
					vectors: vectors,
					rows:    input.NumRows(),
				}, nil
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

// MultiColumnVector represents multiple columns with the same name but different types,
// ordered by precedence (highest precedence first).
type MultiColumnVector struct {
	vectors []ColumnVector // Ordered by precedence (Generated first, Label last)
	rows    int64
}

var _ ColumnVector = (*MultiColumnVector)(nil)

// ToArray implements ColumnVector.
func (m *MultiColumnVector) ToArray() arrow.Array {
	// TODO: Create a new array by materializing the final column values considering per-row precedence.
	panic("ToArray() is not implemented for MultiColumnVector.")
}

// Value implements ColumnVector.
func (m *MultiColumnVector) Value(i int) any {
	// Try each vector in precedence order
	for _, vec := range m.vectors {
		// TODO: Check for isNull() instead
		if val := vec.Value(i); val != nil {
			return val
		}
	}
	return nil
}

// Type implements ColumnVector.
func (m *MultiColumnVector) Type() datatype.DataType {
	// TODO: Support other data types in MultiColumnVector
	return datatype.String
}

// ColumnType implements ColumnVector.
func (m *MultiColumnVector) ColumnType() types.ColumnType {
	return types.ColumnTypeAmbiguous
}

// Len implements ColumnVector.
func (m *MultiColumnVector) Len() int64 {
	return m.rows
}
