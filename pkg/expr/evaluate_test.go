package expr_test

import (
	"testing"

	"github.com/grafana/regexp"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/expr"
	"github.com/grafana/loki/v3/pkg/memory"
)

func selectionMask(alloc *memory.Allocator, values ...bool) memory.Bitmap {
	bmap := memory.NewBitmap(alloc, len(values))
	for _, value := range values {
		bmap.Append(value)
	}
	return bmap
}

// TestEvaluate performs a basic end-to-end test of expression evaluation.
func TestEvaluate(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "name"},
			{Name: "age"},
		}),
		3, // row count
		[]columnar.Array{
			columnartest.Array(t, columnar.KindUTF8, &alloc, "Peter", "Paul", "Mary"),
			columnartest.Array(t, columnar.KindUint64, &alloc, 30, 25, 43),
		},
	)

	// (name != "Paul" AND age > 25)
	e := &expr.Binary{
		Left: &expr.Binary{
			Left:  &expr.Column{Name: "name"},
			Op:    expr.BinaryOpNEQ,
			Right: &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUTF8, "Paul")},
		},
		Op: expr.BinaryOpAND,
		Right: &expr.Binary{
			Left:  &expr.Column{Name: "age"},
			Op:    expr.BinaryOpGT,
			Right: &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 25)},
		},
	}

	expect := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true)

	result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireDatumsEqual(t, expect, result)
}

func TestEvaluate_Constant(t *testing.T) {
	var alloc memory.Allocator

	e := &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 42)}

	expect := columnartest.Scalar(t, columnar.KindUint64, 42)

	result, err := expr.Evaluate(&alloc, e, nil, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireDatumsEqual(t, expect, result)
}

func TestEvaluate_Column(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "name"},
			{Name: "age"},
			{Name: "city"},
		}),
		3, // row count
		[]columnar.Array{
			columnartest.Array(t, columnar.KindUTF8, &alloc, "Alice", "Bob", "Charlie"),
			columnartest.Array(t, columnar.KindUint64, &alloc, 30, 25, 35),
			columnartest.Array(t, columnar.KindUTF8, &alloc, "NYC", "LA", "SF"),
		},
	)

	t.Run("existing column", func(t *testing.T) {
		e := &expr.Column{Name: "age"}

		expect := columnartest.Array(t, columnar.KindUint64, &alloc, 30, 25, 35)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("non-existing column", func(t *testing.T) {
		e := &expr.Column{Name: "nonexistent"}

		expect := columnartest.Array(t, columnar.KindNull, &alloc, nil, nil, nil)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})
}

func TestEvaluate_Unary(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "active"},
		}),
		3, // row count
		[]columnar.Array{
			columnartest.Array(t, columnar.KindBool, &alloc, true, false, true),
		},
	)

	e := &expr.Unary{
		Op:    expr.UnaryOpNOT,
		Value: &expr.Column{Name: "active"},
	}

	expect := columnartest.Array(t, columnar.KindBool, &alloc, false, true, false)

	result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireDatumsEqual(t, expect, result)
}

func TestEvaluate_Binary(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "name"},
			{Name: "age"},
			{Name: "active"},
		}),
		3, // row count
		[]columnar.Array{
			columnartest.Array(t, columnar.KindUTF8, &alloc, "Alice", "Bob", "Charlie"),
			columnartest.Array(t, columnar.KindUint64, &alloc, 30, 25, 35),
			columnartest.Array(t, columnar.KindBool, &alloc, true, false, true),
		},
	)

	tests := []struct {
		op     expr.BinaryOp
		left   expr.Expression
		right  expr.Expression
		expect columnar.Datum
	}{
		{
			op:     expr.BinaryOpEQ,
			left:   &expr.Column{Name: "age"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 30)},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, false),
		},
		{
			op:     expr.BinaryOpNEQ,
			left:   &expr.Column{Name: "age"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 30)},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, true),
		},
		{
			op:     expr.BinaryOpGT,
			left:   &expr.Column{Name: "age"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 25)},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, true),
		},
		{
			op:     expr.BinaryOpGTE,
			left:   &expr.Column{Name: "age"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 30)},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, true),
		},
		{
			op:     expr.BinaryOpLT,
			left:   &expr.Column{Name: "age"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 30)},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, false, true, false),
		},
		{
			op:     expr.BinaryOpLTE,
			left:   &expr.Column{Name: "age"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 30)},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, true, false),
		},
		{
			op:     expr.BinaryOpAND,
			left:   &expr.Column{Name: "active"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindBool, true)},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, true),
		},
		{
			op:     expr.BinaryOpOR,
			left:   &expr.Column{Name: "active"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindBool, false)},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, true),
		},
		{
			op:     expr.BinaryOpMatchRegex,
			left:   &expr.Column{Name: "name"},
			right:  &expr.Regexp{Expression: regexp.MustCompile("(?i)((al|ch).*)")},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, true),
		},
		{
			op:     expr.BinaryOpHasSubstr,
			left:   &expr.Column{Name: "name"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUTF8, "li")},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, true),
		},
		{
			op:     expr.BinaryOpHasSubstrIgnoreCase,
			left:   &expr.Column{Name: "name"},
			right:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUTF8, "LI")},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.op.String(), func(t *testing.T) {
			e := &expr.Binary{
				Left:  tt.left,
				Op:    tt.op,
				Right: tt.right,
			}

			result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
			require.NoError(t, err)
			columnartest.RequireDatumsEqual(t, tt.expect, result)
		})
	}
}

func TestEvaluate_Selection(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "name"},
			{Name: "age"},
		}),
		3, // row count
		[]columnar.Array{
			columnartest.Array(t, columnar.KindUTF8, &alloc, "Peter", "Paul", "Mary"),
			columnartest.Array(t, columnar.KindUint64, &alloc, 30, 25, 43),
		},
	)

	selection := selectionMask(&alloc, true, false, true)

	t.Run("column masking", func(t *testing.T) {
		e := &expr.Column{Name: "age"}

		expect := columnartest.Array(t, columnar.KindUint64, &alloc, 30, nil, 43)

		result, err := expr.Evaluate(&alloc, e, record, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("binary masking", func(t *testing.T) {
		e := &expr.Binary{
			Left:  &expr.Column{Name: "age"},
			Op:    expr.BinaryOpGT,
			Right: &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 25)},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, nil, true)

		result, err := expr.Evaluate(&alloc, e, record, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})
}

// TestEvaluate_SelectionNestedExpressions tests that selection is correctly
// threaded through complex nested expressions.
func TestEvaluate_SelectionNestedExpressions(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "name"},
			{Name: "age"},
			{Name: "active"},
		}),
		4, // row count
		[]columnar.Array{
			columnartest.Array(t, columnar.KindUTF8, &alloc, "Alice", "Bob", "Charlie", "Dave"),
			columnartest.Array(t, columnar.KindUint64, &alloc, 30, 25, 35, 28),
			columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, true),
		},
	)

	tests := []struct {
		name      string
		selection memory.Bitmap
		expr      expr.Expression
		expect    columnar.Datum
	}{
		{
			name:      "nested AND with partial selection",
			selection: selectionMask(&alloc, true, false, true, true),
			// (age > 25 AND active)
			expr: &expr.Binary{
				Left: &expr.Binary{
					Left:  &expr.Column{Name: "age"},
					Op:    expr.BinaryOpGT,
					Right: &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 25)},
				},
				Op:    expr.BinaryOpAND,
				Right: &expr.Column{Name: "active"},
			},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, nil, true, true),
		},
		{
			name:      "nested OR with partial selection",
			selection: selectionMask(&alloc, true, true, false, false),
			// (age < 30 OR active)
			expr: &expr.Binary{
				Left: &expr.Binary{
					Left:  &expr.Column{Name: "age"},
					Op:    expr.BinaryOpLT,
					Right: &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 30)},
				},
				Op:    expr.BinaryOpOR,
				Right: &expr.Column{Name: "active"},
			},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, true, nil, nil),
		},
		{
			name:      "NOT with nested comparison and selection",
			selection: selectionMask(&alloc, false, true, true, false),
			// NOT(age == 25)
			expr: &expr.Unary{
				Op: expr.UnaryOpNOT,
				Value: &expr.Binary{
					Left:  &expr.Column{Name: "age"},
					Op:    expr.BinaryOpEQ,
					Right: &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 25)},
				},
			},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, false, true, nil),
		},
		{
			name:      "complex nested expression with selection",
			selection: selectionMask(&alloc, true, false, true, true),
			// ((age > 25 AND active) OR name == "Bob")
			expr: &expr.Binary{
				Left: &expr.Binary{
					Left: &expr.Binary{
						Left:  &expr.Column{Name: "age"},
						Op:    expr.BinaryOpGT,
						Right: &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 25)},
					},
					Op:    expr.BinaryOpAND,
					Right: &expr.Column{Name: "active"},
				},
				Op: expr.BinaryOpOR,
				Right: &expr.Binary{
					Left:  &expr.Column{Name: "name"},
					Op:    expr.BinaryOpEQ,
					Right: &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUTF8, "Bob")},
				},
			},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, nil, true, true),
		},
		{
			name:      "empty selection means all rows selected",
			selection: memory.Bitmap{},
			expr: &expr.Binary{
				Left:  &expr.Column{Name: "age"},
				Op:    expr.BinaryOpGT,
				Right: &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 25)},
			},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, true),
		},
		{
			name:      "all-false selection",
			selection: selectionMask(&alloc, false, false, false, false),
			expr: &expr.Binary{
				Left:  &expr.Column{Name: "age"},
				Op:    expr.BinaryOpGT,
				Right: &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 25)},
			},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil, nil),
		},
		{
			name:      "single-row selection (first)",
			selection: selectionMask(&alloc, true, false, false, false),
			expr: &expr.Binary{
				Left:  &expr.Column{Name: "name"},
				Op:    expr.BinaryOpHasSubstrIgnoreCase,
				Right: &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUTF8, "li")},
			},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, nil, nil, nil),
		},
		{
			name:      "single-row selection (middle)",
			selection: selectionMask(&alloc, false, false, true, false),
			expr: &expr.Binary{
				Left:  &expr.Column{Name: "name"},
				Op:    expr.BinaryOpHasSubstrIgnoreCase,
				Right: &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUTF8, "li")},
			},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, true, nil),
		},
		{
			name:      "single-row selection (last)",
			selection: selectionMask(&alloc, false, false, false, true),
			expr: &expr.Binary{
				Left:  &expr.Column{Name: "name"},
				Op:    expr.BinaryOpHasSubstrIgnoreCase,
				Right: &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUTF8, "li")},
			},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil, false),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := expr.Evaluate(&alloc, tt.expr, record, tt.selection)
			require.NoError(t, err)
			columnartest.RequireDatumsEqual(t, tt.expect, result)
		})
	}
}

// TestMaskDatumSelection tests selection application to column data via
// Evaluate() with Column expressions, exercising applySelectionToColumn
// with various selection patterns and data types.
func TestMaskDatumSelection(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "bool_col"},
			{Name: "int64_col"},
			{Name: "uint64_col"},
			{Name: "utf8_col"},
			{Name: "null_col"},
		}),
		4, // row count
		[]columnar.Array{
			columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false),
			columnartest.Array(t, columnar.KindInt64, &alloc, 10, 20, 30, 40),
			columnartest.Array(t, columnar.KindUint64, &alloc, 100, 200, 300, 400),
			columnartest.Array(t, columnar.KindUTF8, &alloc, "a", "b", "c", "d"),
			columnartest.Array(t, columnar.KindNull, &alloc, nil, nil, nil, nil),
		},
	)

	tests := []struct {
		name       string
		columnName string
		selection  memory.Bitmap
		expect     columnar.Datum
	}{
		{
			name:       "Bool column with partial selection",
			columnName: "bool_col",
			selection:  selectionMask(&alloc, true, false, true, false),
			expect:     columnartest.Array(t, columnar.KindBool, &alloc, true, nil, true, nil),
		},
		{
			name:       "Int64 column with partial selection",
			columnName: "int64_col",
			selection:  selectionMask(&alloc, false, true, false, true),
			expect:     columnartest.Array(t, columnar.KindInt64, &alloc, nil, 20, nil, 40),
		},
		{
			name:       "Uint64 column with all-true selection",
			columnName: "uint64_col",
			selection:  selectionMask(&alloc, true, true, true, true),
			expect:     columnartest.Array(t, columnar.KindUint64, &alloc, 100, 200, 300, 400),
		},
		{
			name:       "UTF8 column with all-false selection",
			columnName: "utf8_col",
			selection:  selectionMask(&alloc, false, false, false, false),
			expect:     columnartest.Array(t, columnar.KindUTF8, &alloc, nil, nil, nil, nil),
		},
		{
			name:       "Null column with partial selection",
			columnName: "null_col",
			selection:  selectionMask(&alloc, true, false, true, false),
			expect:     columnartest.Array(t, columnar.KindNull, &alloc, nil, nil, nil, nil),
		},
		{
			name:       "Bool column with all selection",
			columnName: "bool_col",
			selection:  memory.Bitmap{},
			expect:     columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false),
		},
		{
			name:       "UTF8 column with single-row selection",
			columnName: "utf8_col",
			selection:  selectionMask(&alloc, false, true, false, false),
			expect:     columnartest.Array(t, columnar.KindUTF8, &alloc, nil, "b", nil, nil),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &expr.Column{Name: tt.columnName}
			result, err := expr.Evaluate(&alloc, e, record, tt.selection)
			require.NoError(t, err)
			columnartest.RequireDatumsEqual(t, tt.expect, result)
		})
	}
}

// TestMaskDatumSelection_WithNullValues tests maskDatumSelection when the
// column already has null values (validity bitmap).
func TestMaskDatumSelection_WithNullValues(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "age"},
		}),
		4, // row count
		[]columnar.Array{
			// Column with null at position 1
			columnartest.Array(t, columnar.KindUint64, &alloc, 30, nil, 35, 28),
		},
	)

	tests := []struct {
		name      string
		selection memory.Bitmap
		expect    columnar.Datum
	}{
		{
			name:      "selection includes null position",
			selection: selectionMask(&alloc, true, true, false, false),
			expect:    columnartest.Array(t, columnar.KindUint64, &alloc, 30, nil, nil, nil),
		},
		{
			name:      "selection excludes null position",
			selection: selectionMask(&alloc, true, false, true, true),
			expect:    columnartest.Array(t, columnar.KindUint64, &alloc, 30, nil, 35, 28),
		},
		{
			name:      "selection overlaps null and valid values",
			selection: selectionMask(&alloc, false, true, true, false),
			expect:    columnartest.Array(t, columnar.KindUint64, &alloc, nil, nil, 35, nil),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &expr.Column{Name: "age"}
			result, err := expr.Evaluate(&alloc, e, record, tt.selection)
			require.NoError(t, err)
			columnartest.RequireDatumsEqual(t, tt.expect, result)
		})
	}
}

// TestEvaluate_SelectionLengthValidation tests that selection length mismatches
// are properly detected and reported.
func TestEvaluate_SelectionLengthValidation(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "age"},
		}),
		4, // row count
		[]columnar.Array{
			columnartest.Array(t, columnar.KindUint64, &alloc, 30, 25, 35, 28),
		},
	)

	tests := []struct {
		name      string
		selection memory.Bitmap
		wantErr   string
	}{
		{
			name:      "selection too short",
			selection: selectionMask(&alloc, true, false),
			wantErr:   "selection length mismatch: 2 != 4",
		},
		{
			name:      "selection too long",
			selection: selectionMask(&alloc, true, false, true, false, true),
			wantErr:   "selection length mismatch: 5 != 4",
		},
		{
			name:      "empty selection is valid (means all rows selected)",
			selection: memory.Bitmap{},
			wantErr:   "", // no error expected
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &expr.Column{Name: "age"}
			_, err := expr.Evaluate(&alloc, e, record, tt.selection)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

// TestEvaluate_SelectionWithScalars tests that selection does not affect
// scalar results (constants remain scalars).
func TestEvaluate_SelectionWithScalars(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "age"},
		}),
		4, // row count
		[]columnar.Array{
			columnartest.Array(t, columnar.KindUint64, &alloc, 30, 25, 35, 28),
		},
	)

	selection := selectionMask(&alloc, true, false, true, false)

	t.Run("constant expression returns scalar regardless of selection", func(t *testing.T) {
		e := &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 42)}
		expect := columnartest.Scalar(t, columnar.KindUint64, 42)

		result, err := expr.Evaluate(&alloc, e, record, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})
}
