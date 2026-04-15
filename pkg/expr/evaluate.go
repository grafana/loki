package expr

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/memory"
)

// Evaluate processes expr against the provided input datum, producing a datum
// as a result using alloc.
//
// The input datum determines how expressions resolve:
//
//   - [Identity] resolves to the input datum directly.
//   - [Column] requires the input to be a [*columnar.Struct] and looks up
//     the field by name.
//
// When selection is non-empty, only selected rows (where the bit is set) are
// guaranteed to have meaningful results in compute operations. Unselected rows
// have undefined values. Use [compute.Filter] on the return datum to
// materialize a selection. A zero-value [memory.Bitmap] selects all rows.
//
// The return type of Evaluate depends on the expression provided. See the
// documentation for implementations of Expression for what they produce when
// evaluated.
func Evaluate(alloc *memory.Allocator, expr Expression, input columnar.Datum, selection memory.Bitmap) (columnar.Datum, error) {
	return evaluateWithSelection(alloc, expr, input, selection)
}

// inputNumRows returns the number of rows for the given input datum.
func inputNumRows(input columnar.Datum) int {
	switch v := input.(type) {
	case columnar.Array:
		return v.Len()
	case columnar.Scalar:
		return 1
	default:
		return 0
	}
}

func evaluateWithSelection(alloc *memory.Allocator, expr Expression, input columnar.Datum, selection memory.Bitmap) (columnar.Datum, error) {
	nrows := inputNumRows(input)
	if selection.Len() > 0 && selection.Len() != nrows {
		return nil, fmt.Errorf("selection length mismatch: %d != %d", selection.Len(), nrows)
	}

	switch expr := expr.(type) {
	case *Constant:
		return expr.Value, nil

	case *Identity:
		return input, nil

	case *Column:
		s, ok := input.(*columnar.Struct)
		if !ok {
			return nil, fmt.Errorf("expected Struct input, got %T", input)
		}

		columnIndex := -1
		if schema := s.Schema(); schema != nil {
			_, columnIndex = schema.ColumnIndex(expr.Name)
		}

		if columnIndex == -1 {
			validity := memory.NewBitmap(alloc, s.Len())
			validity.AppendCount(false, s.Len())
			return columnar.NewNull(validity), nil
		}

		return s.Field(columnIndex), nil

	case *Extract:
		return evaluateExtract(alloc, expr, input, selection)

	case *Include:
		return evaluateInclude(alloc, expr, input, selection)

	case *Exclude:
		return evaluateExclude(alloc, expr, input, selection)

	case *MakeStruct:
		return evaluateMakeStruct(alloc, expr, input, selection)

	case *Unary:
		return evaluateUnary(alloc, expr, input, selection)

	case *Binary:
		return evaluateBinary(alloc, expr, input, selection)

	case *Regexp:
		return nil, fmt.Errorf("regexp can only be evaluated as the right-hand side of regex match operations")

	default:
		panic(fmt.Sprintf("unexpected expression type %T", expr))
	}
}

func evaluateExtract(alloc *memory.Allocator, expr *Extract, input columnar.Datum, selection memory.Bitmap) (columnar.Datum, error) {
	value, err := evaluateWithSelection(alloc, expr.Value, input, selection)
	if err != nil {
		return nil, err
	}

	s, ok := value.(*columnar.Struct)
	if !ok {
		return nil, fmt.Errorf("expected Struct input, got %T", value)
	}

	_, columnIndex := s.Schema().ColumnIndex(expr.Name)
	if columnIndex == -1 {
		validity := memory.NewBitmap(alloc, s.Len())
		validity.AppendCount(false, s.Len())
		return columnar.NewNull(validity), nil
	}

	return s.Field(columnIndex), nil
}

func evaluateInclude(alloc *memory.Allocator, expr *Include, input columnar.Datum, selection memory.Bitmap) (columnar.Datum, error) {
	value, err := evaluateWithSelection(alloc, expr.Value, input, selection)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(expr.Names))
	for _, name := range expr.Names {
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("duplicate field name %q", name)
		}
		seen[name] = struct{}{}
	}

	s, ok := value.(*columnar.Struct)
	if !ok {
		return nil, fmt.Errorf("expected Struct input, got %T", value)
	}

	var (
		columns []columnar.Column
		fields  []columnar.Array
	)
	for _, name := range expr.Names {
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

func evaluateMakeStruct(alloc *memory.Allocator, expr *MakeStruct, input columnar.Datum, selection memory.Bitmap) (columnar.Datum, error) {
	if len(expr.Names) != len(expr.Values) {
		return nil, fmt.Errorf("names and values count mismatch: %d != %d", len(expr.Names), len(expr.Values))
	}

	seen := make(map[string]struct{}, len(expr.Names))
	for _, name := range expr.Names {
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("duplicate field name %q", name)
		}
		seen[name] = struct{}{}
	}

	var (
		length = inputNumRows(input)

		columns = make([]columnar.Column, len(expr.Names))
		fields  = make([]columnar.Array, len(expr.Values))
	)
	for i, name := range expr.Names {
		val, err := evaluateWithSelection(alloc, expr.Values[i], input, selection)
		if err != nil {
			return nil, err
		}
		arr, ok := val.(columnar.Array)
		if !ok {
			return nil, fmt.Errorf("value %d (%q) must be an array, got %T", i, name, val)
		}
		columns[i] = columnar.Column{Name: name}
		fields[i] = arr

		if arr.Len() != length {
			return nil, fmt.Errorf("value %d (%q) must have length %d, got %d", i, name, length, arr.Len())
		}
	}

	schema := columnar.NewSchema(columns)
	return columnar.NewStruct(schema, fields, length, memory.Bitmap{}), nil
}

func evaluateExclude(alloc *memory.Allocator, expr *Exclude, input columnar.Datum, selection memory.Bitmap) (columnar.Datum, error) {
	value, err := evaluateWithSelection(alloc, expr.Value, input, selection)
	if err != nil {
		return nil, err
	}

	s, ok := value.(*columnar.Struct)
	if !ok {
		return nil, fmt.Errorf("expected Struct input, got %T", value)
	}

	excludeSet := make(map[string]struct{}, len(expr.Names))
	for _, name := range expr.Names {
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

func evaluateUnary(alloc *memory.Allocator, expr *Unary, input columnar.Datum, selection memory.Bitmap) (columnar.Datum, error) {
	switch expr.Op {
	case UnaryOpNOT:
		value, err := evaluateWithSelection(alloc, expr.Value, input, selection)
		if err != nil {
			return nil, err
		}
		return compute.Not(alloc, value, selection)
	default:
		return nil, fmt.Errorf("unexpected unary operator %s", expr.Op)
	}
}

func evaluateBinary(alloc *memory.Allocator, expr *Binary, input columnar.Datum, selection memory.Bitmap) (columnar.Datum, error) {
	// Check for special operators that need different handling of their arguments.
	switch expr.Op {
	case BinaryOpMatchRegex, BinaryOpIn:
		return evaluateSpecialBinary(alloc, expr, input, selection)
	}

	// TODO(rfratto): If expr.Op is [BinaryOpAND] or [BinaryOpOR], we can
	// propagate selection vectors to avoid unnecessary evaluations.
	left, err := evaluateWithSelection(alloc, expr.Left, input, selection)
	if err != nil {
		return nil, err
	}

	right, err := evaluateWithSelection(alloc, expr.Right, input, selection)
	if err != nil {
		return nil, err
	}

	switch expr.Op {
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
		return nil, fmt.Errorf("unexpected binary operator %s", expr.Op)
	}
}

// evaluateSpecialBinary evaluates binary expressions for which one of the
// arguments does not evaluate into an expression of its own.
func evaluateSpecialBinary(alloc *memory.Allocator, expr *Binary, input columnar.Datum, selection memory.Bitmap) (columnar.Datum, error) {
	switch expr.Op {
	case BinaryOpMatchRegex:
		left, err := evaluateWithSelection(alloc, expr.Left, input, selection)
		if err != nil {
			return nil, err
		}

		right, ok := expr.Right.(*Regexp)
		if !ok {
			return nil, fmt.Errorf("right-hand side of regex match operation must be a regexp, got %T", expr.Right)
		}

		return compute.RegexpMatch(alloc, left, right.Expression, selection)
	case BinaryOpIn:
		left, err := evaluateWithSelection(alloc, expr.Left, input, selection)
		if err != nil {
			return nil, err
		}

		right, ok := expr.Right.(*ValueSet)
		if !ok {
			return nil, fmt.Errorf("right-hand side of in operation must be a ValueSet, got %T", expr.Right)
		}

		return compute.IsMember(alloc, left, right.Values, selection)
	}

	return nil, fmt.Errorf("unexpected binary operator %s", expr.Op)
}
