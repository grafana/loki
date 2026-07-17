package expr

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/memory"
)

// Reduce evaluates e by calling fn to resolve each child expression into a
// [columnar.Datum], then computing the parent operation on those results.
//
// Reduce is used when the caller wants to hook into evaluating sub-expressions.
//
// Leaf expressions (Constant, Identity, Column) have no children to reduce
// and are delegated directly to fn.
func Reduce(alloc *memory.Allocator, e Expression, fn func(Expression) (columnar.Datum, error), selection memory.Bitmap) (columnar.Datum, error) {
	switch e := e.(type) {
	case *Constant, *Identity, *Column:
		return fn(e)
	case *Binary:
		return reduceBinary(alloc, e, fn, selection)
	case *Unary:
		return reduceUnary(alloc, e, fn, selection)
	case *MakeStruct:
		return reduceMakeStruct(alloc, e, fn)
	case *Extract:
		return reduceExtract(alloc, e, fn)
	case *Include:
		return reduceInclude(alloc, e, fn)
	case *Exclude:
		return reduceExclude(alloc, e, fn)
	default:
		panic(fmt.Sprintf("Reduce called on leaf expression %T; use fn to handle leaves", e))
	}
}

func reduceBinary(alloc *memory.Allocator, e *Binary, fn func(Expression) (columnar.Datum, error), selection memory.Bitmap) (columnar.Datum, error) {
	// Special operators where the right side is not evaluated to a Datum.
	// This mirrors evaluateSpecialBinary in evaluate.go.
	switch e.Op {
	case BinaryOpMatchRegex:
		left, err := fn(e.Left)
		if err != nil {
			return nil, err
		}
		right, ok := e.Right.(*Regexp)
		if !ok {
			return nil, fmt.Errorf("right-hand side of regex match must be a Regexp, got %T", e.Right)
		}
		return compute.RegexpMatch(alloc, left, right.Expression, selection)

	case BinaryOpIn:
		left, err := fn(e.Left)
		if err != nil {
			return nil, err
		}
		right, ok := e.Right.(*ValueSet)
		if !ok {
			return nil, fmt.Errorf("right-hand side of in must be a ValueSet, got %T", e.Right)
		}
		return compute.IsMember(alloc, left, right.Values, selection)
	}

	left, err := fn(e.Left)
	if err != nil {
		return nil, err
	}
	right, err := fn(e.Right)
	if err != nil {
		return nil, err
	}

	switch e.Op {
	case BinaryOpEQ:
		return compute.Equals(alloc, left, right, selection)
	case BinaryOpNEQ:
		return compute.NotEquals(alloc, left, right, selection)
	case BinaryOpGT:
		return compute.GreaterThan(alloc, left, right, selection)
	case BinaryOpGTE:
		return compute.GreaterOrEqual(alloc, left, right, selection)
	case BinaryOpLT:
		return compute.LessThan(alloc, left, right, selection)
	case BinaryOpLTE:
		return compute.LessOrEqual(alloc, left, right, selection)
	case BinaryOpAND:
		return compute.And(alloc, left, right, selection)
	case BinaryOpOR:
		return compute.Or(alloc, left, right, selection)
	case BinaryOpHasSubstr:
		return compute.Substr(alloc, left, right, selection)
	case BinaryOpHasSubstrIgnoreCase:
		return compute.SubstrInsensitive(alloc, left, right, selection)
	default:
		return nil, fmt.Errorf("unexpected binary operator %s", e.Op)
	}
}

func reduceUnary(alloc *memory.Allocator, e *Unary, fn func(Expression) (columnar.Datum, error), selection memory.Bitmap) (columnar.Datum, error) {
	value, err := fn(e.Value)
	if err != nil {
		return nil, err
	}

	switch e.Op {
	case UnaryOpNOT:
		return compute.Not(alloc, value, selection)
	default:
		return nil, fmt.Errorf("unexpected unary operator %s", e.Op)
	}
}

func reduceMakeStruct(_ *memory.Allocator, e *MakeStruct, fn func(Expression) (columnar.Datum, error)) (columnar.Datum, error) {
	if len(e.Names) != len(e.Values) {
		return nil, fmt.Errorf("MakeStruct: names and values count mismatch: %d != %d", len(e.Names), len(e.Values))
	}
	if err := validateUniqueNames(e.Names); err != nil {
		return nil, fmt.Errorf("MakeStruct: %w", err)
	}

	columns := make([]columnar.Column, len(e.Names))
	fields := make([]columnar.Array, len(e.Values))

	for i, name := range e.Names {
		val, err := fn(e.Values[i])
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", name, err)
		}
		arr, ok := val.(columnar.Array)
		if !ok {
			return nil, fmt.Errorf("MakeStruct: field %q must be an array, got %T", name, val)
		}
		columns[i] = columnar.Column{Name: name}
		fields[i] = arr
	}

	length := 0
	if len(fields) > 0 {
		length = fields[0].Len()
	}

	schema := columnar.NewSchema(columns)
	return columnar.NewStruct(schema, fields, length, memory.Bitmap{}), nil
}

func reduceExtract(alloc *memory.Allocator, e *Extract, fn func(Expression) (columnar.Datum, error)) (columnar.Datum, error) {
	value, err := fn(e.Value)
	if err != nil {
		return nil, err
	}

	s, ok := value.(*columnar.Struct)
	if !ok {
		return nil, fmt.Errorf("Extract requires Struct, got %T", value)
	}

	columnIndex := -1
	if schema := s.Schema(); schema != nil {
		_, columnIndex = schema.ColumnIndex(e.Name)
	}

	if columnIndex == -1 {
		validity := memory.NewBitmap(alloc, s.Len())
		validity.AppendCount(false, s.Len())
		return columnar.NewNull(validity), nil
	}

	return projectStructField(alloc, s, columnIndex)
}

func reduceInclude(_ *memory.Allocator, e *Include, fn func(Expression) (columnar.Datum, error)) (columnar.Datum, error) {
	value, err := fn(e.Value)
	if err != nil {
		return nil, err
	}
	if err := validateUniqueNames(e.Names); err != nil {
		return nil, fmt.Errorf("Include: %w", err)
	}

	s, ok := value.(*columnar.Struct)
	if !ok {
		return nil, fmt.Errorf("Include requires Struct, got %T", value)
	}

	var (
		columns []columnar.Column
		fields  []columnar.Array
	)
	for _, name := range e.Names {
		_, idx := s.Schema().ColumnIndex(name)
		if idx == -1 {
			continue
		}
		columns = append(columns, columnar.Column{Name: name})
		fields = append(fields, s.Field(idx))
	}

	schema := columnar.NewSchema(columns)
	return columnar.NewStruct(schema, fields, s.Len(), s.Validity()), nil
}

func validateUniqueNames(names []string) error {
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, ok := seen[name]; ok {
			return fmt.Errorf("duplicate field name %q", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func reduceExclude(_ *memory.Allocator, e *Exclude, fn func(Expression) (columnar.Datum, error)) (columnar.Datum, error) {
	value, err := fn(e.Value)
	if err != nil {
		return nil, err
	}

	s, ok := value.(*columnar.Struct)
	if !ok {
		return nil, fmt.Errorf("Exclude requires Struct, got %T", value)
	}

	excludeSet := make(map[string]struct{}, len(e.Names))
	for _, name := range e.Names {
		excludeSet[name] = struct{}{}
	}

	var (
		columns []columnar.Column
		fields  []columnar.Array
	)
	for i := range s.NumFields() {
		col := s.Schema().Column(i)
		if _, excluded := excludeSet[col.Name]; excluded {
			continue
		}
		columns = append(columns, col)
		fields = append(fields, s.Field(i))
	}

	schema := columnar.NewSchema(columns)
	return columnar.NewStruct(schema, fields, s.Len(), s.Validity()), nil
}
