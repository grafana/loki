package executor

import (
	"fmt"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

type expressionEvaluator struct {
	allocator memory.Allocator
}

func newExpressionEvaluator(allocator memory.Allocator) expressionEvaluator {
	if allocator == nil {
		allocator = memory.DefaultAllocator
	}

	return expressionEvaluator{
		allocator: allocator,
	}
}

func (e expressionEvaluator) eval(expr physical.Expression, input arrow.Record) (ColumnVector, error) {
	switch expr := expr.(type) {

	case *physical.LiteralExpr:
		return &Scalar{
			value: expr.Literal,
			rows:  input.NumRows(),
			ct:    types.ColumnTypeAmbiguous,
		}, nil

	case *physical.ColumnExpr:
		colIdent := semconv.NewIdentifier(expr.Ref.Column, expr.Ref.Type, types.Loki.String)

		// For non-ambiguous columns, we can look up the column in the schema by its fully qualified name.
		if expr.Ref.Type != types.ColumnTypeAmbiguous {
			for idx, field := range input.Schema().Fields() {
				ident, err := semconv.ParseFQN(field.Name)
				if err != nil {
					return nil, fmt.Errorf("failed to parse column %s: %w", field.Name, err)
				}
				if ident.ShortName() == colIdent.ShortName() && ident.ColumnType() == colIdent.ColumnType() {
					arr := input.Column(idx)
					arr.Retain()
					return &Array{
						array: arr,
						dt:    ident.DataType(),
						ct:    ident.ColumnType(),
						rows:  input.NumRows(),
					}, nil
				}
			}
		}

		// For ambiguous columns, we need to filter on the name and type and combine matching columns into a CoalesceVector.
		if expr.Ref.Type == types.ColumnTypeAmbiguous {
			var fieldIndices []int
			var fieldIdents []*semconv.Identifier

			for idx, field := range input.Schema().Fields() {
				ident, err := semconv.ParseFQN(field.Name)
				if err != nil {
					return nil, fmt.Errorf("failed to parse column %s: %w", field.Name, err)
				}
				if ident.ShortName() == colIdent.ShortName() {
					fieldIndices = append(fieldIndices, idx)
					fieldIdents = append(fieldIdents, ident)
				}
			}

			// Collect all matching columns and order by precedence
			var vecs []ColumnVector
			for i := range fieldIndices {
				idx := fieldIndices[i]
				ident := fieldIdents[i]

				// TODO(ashwanth): Support other data types in CoalesceVector.
				// For now, ensure all vectors are strings to avoid type conflicts.
				if ident.DataType() != types.Loki.String {
					return nil, fmt.Errorf("column %s has datatype %s, but expression expects %s", ident.ShortName(), ident.DataType(), types.Loki.String)
				}

				arr := input.Column(idx)
				arr.Retain()
				vecs = append(vecs, &Array{
					array: arr,
					dt:    ident.DataType(),
					ct:    ident.ColumnType(),
					rows:  input.NumRows(),
				})
			}

			if len(vecs) == 1 {
				return vecs[0], nil
			}

			if len(vecs) > 1 {
				// Multiple matches - sort by precedence and create CoalesceVector
				slices.SortFunc(vecs, func(a, b ColumnVector) int {
					return types.ColumnTypePrecedence(a.ColumnType()) - types.ColumnTypePrecedence(b.ColumnType())
				})
				return &CoalesceVector{
					vectors: vecs,
					rows:    input.NumRows(),
				}, nil
			}

		}

		// A non-existent column is represented as a string scalar with zero-value.
		// This reflects current behaviour, where a label filter `| foo=""` would match all if `foo` is not defined.
		return &Scalar{
			value: types.NewLiteral(""),
			rows:  input.NumRows(),
			ct:    types.ColumnTypeGenerated,
		}, nil

	case *physical.UnaryExpr:
		lhr, err := e.eval(expr.Left, input)
		if err != nil {
			return nil, err
		}
		defer lhr.Release()

		fn, err := unaryFunctions.GetForSignature(expr.Op, lhr.Type().ArrowType())
		if err != nil {
			return nil, fmt.Errorf("failed to lookup unary function: %w", err)
		}
		return fn.Evaluate(lhr, e.allocator)

	case *physical.BinaryExpr:
		lhs, err := e.eval(expr.Left, input)
		if err != nil {
			return nil, err
		}
		defer lhs.Release()

		rhs, err := e.eval(expr.Right, input)
		if err != nil {
			return nil, err
		}
		defer rhs.Release()

		// At the moment we only support functions that accept the same input types.
		// TODO(chaudum): Compare Loki type, not Arrow type
		if lhs.Type().ArrowType().ID() != rhs.Type().ArrowType().ID() {
			return nil, fmt.Errorf("failed to lookup binary function for signature %v(%v,%v): types do not match", expr.Op, lhs.Type(), rhs.Type())
		}

		// TODO(chaudum): Resolve function by Loki type
		fn, err := binaryFunctions.GetForSignature(expr.Op, lhs.Type().ArrowType())
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
	// Type returns the Loki data type of the column vector.
	Type() types.DataType
	// ColumnType returns the type of column the vector originates from.
	ColumnType() types.ColumnType
	// Len returns the length of the vector
	Len() int64
	// Release decreases the reference count by 1 on underlying Arrow array
	Release()
}

// Scalar represents a single value repeated any number of times.
type Scalar struct {
	value types.Literal
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
func (v *Scalar) Type() types.DataType {
	return v.value.Type()
}

// ColumnType implements ColumnVector.
func (v *Scalar) ColumnType() types.ColumnType {
	return v.ct
}

// Release implements ColumnVector.
func (v *Scalar) Release() {
}

// Len implements ColumnVector.
func (v *Scalar) Len() int64 {
	return v.rows
}

// Array represents a column of data, stored as an [arrow.Array].
type Array struct {
	array arrow.Array
	dt    types.DataType
	ct    types.ColumnType
	rows  int64
}

var _ ColumnVector = (*Array)(nil)

// ToArray implements ColumnVector.
func (a *Array) ToArray() arrow.Array {
	a.array.Retain()
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
func (a *Array) Type() types.DataType {
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

// Release implements ColumnVector.
func (a *Array) Release() {
	a.array.Release()
}

// ArrayStruct represents multiple columns of data, stored as an [array.Struct].
// It implements the ColumnVector interface while preserving access to individual fields.
type ArrayStruct struct {
	array *array.Struct
	ct    types.ColumnType
	rows  int64
}

var _ ColumnVector = (*ArrayStruct)(nil)

// NewArrayStruct creates a new ArrayStruct from an array.Struct
func NewArrayStruct(arr *array.Struct, ct types.ColumnType) *ArrayStruct {
	return &ArrayStruct{
		array: arr,
		ct:    ct,
		rows:  int64(arr.Len()),
	}
}

// ToArray implements ColumnVector.
// Returns the underlying struct array.
func (a *ArrayStruct) ToArray() arrow.Array {
	a.array.Retain()
	return a.array
}

// Value implements ColumnVector.
// Returns a map of field names to values at the specified index.
func (a *ArrayStruct) Value(i int) any {
	if a.array.IsNull(i) || !a.array.IsValid(i) {
		return nil
	}

	// Return a map representing the struct's fields at this index
	result := make(map[string]any)
	structType := a.array.DataType().(*arrow.StructType)

	for fieldIdx := 0; fieldIdx < a.array.NumField(); fieldIdx++ {
		field := a.array.Field(fieldIdx)
		if field.IsNull(i) {
			continue
		}
		fieldMeta := structType.Field(fieldIdx)

		// Extract value from the field array at index i
		var value any
		switch arr := field.(type) {
		case *array.Boolean:
			value = arr.Value(i)
		case *array.String:
			value = arr.Value(i)
		case *array.Int64:
			value = arr.Value(i)
		case *array.Uint64:
			value = arr.Value(i)
		case *array.Float64:
			value = arr.Value(i)
		default:
			value = nil
		}
		result[fieldMeta.Name] = value
	}

	return result
}

// Type implements ColumnVector.
func (a *ArrayStruct) Type() types.DataType {
	dt := a.array.DataType()
	st, ok := dt.(*arrow.StructType)
	if !ok {
		return nil
	}
	return types.NewStructType(st)
}

// ColumnType implements ColumnVector.
func (a *ArrayStruct) ColumnType() types.ColumnType {
	return a.ct
}

// Len implements ColumnVector.
func (a *ArrayStruct) Len() int64 {
	return a.rows
}

// Release decreases the reference count by 1 on underlying Arrow array
func (a *ArrayStruct) Release() {
	a.array.Release()
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
			// [CoalesceVector] only supports [types.String] for now
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
func (m *CoalesceVector) Type() types.DataType {
	// TODO: Support other data types in CoalesceVector.
	return types.Loki.String
}

// ColumnType implements ColumnVector.
func (m *CoalesceVector) ColumnType() types.ColumnType {
	return types.ColumnTypeAmbiguous
}

// Len implements ColumnVector.
func (m *CoalesceVector) Len() int64 {
	return m.rows
}

// Release implements ColumnVector.
func (m *CoalesceVector) Release() {
}
