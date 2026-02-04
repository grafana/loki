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

// TestEvaluate_ANDShortCircuit tests that AND operations optimize by skipping
// right-side evaluation for rows where left is false or null.
func TestEvaluate_ANDShortCircuit(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "left_bool"},
			{Name: "right_bool"},
			{Name: "text"},
		}),
		4, // row count
		[]columnar.Array{
			columnartest.Array(t, columnar.KindBool, &alloc, false, false, true, true),
			columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false),
			columnartest.Array(t, columnar.KindUTF8, &alloc, "apple", "banana", "cherry", "date"),
		},
	)

	t.Run("all-false left side with complex right side", func(t *testing.T) {
		// When left is all false, right should be skipped entirely
		// Expression: (false AND text =~ ".*") for all rows
		e := &expr.Binary{
			Left: &expr.Constant{Value: columnartest.Scalar(t, columnar.KindBool, false)},
			Op:   expr.BinaryOpAND,
			Right: &expr.Binary{
				Left:  &expr.Column{Name: "text"},
				Op:    expr.BinaryOpMatchRegex,
				Right: &expr.Regexp{Expression: regexp.MustCompile(".*")},
			},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, false, false, false, false)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("partially-false left side", func(t *testing.T) {
		// When left is partially false, right should only be evaluated for true rows
		// Expression: left_bool AND right_bool
		e := &expr.Binary{
			Left:  &expr.Column{Name: "left_bool"},
			Op:    expr.BinaryOpAND,
			Right: &expr.Column{Name: "right_bool"},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, false, false, true, false)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("all-true left side", func(t *testing.T) {
		// When left is all true, right must be evaluated for all rows (no optimization)
		// Expression: true AND right_bool
		e := &expr.Binary{
			Left:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindBool, true)},
			Op:    expr.BinaryOpAND,
			Right: &expr.Column{Name: "right_bool"},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("nested AND with cascading selection refinement", func(t *testing.T) {
		// Expression: (left_bool AND right_bool) AND (text =~ ".*r.*")
		// First AND refines selection, second AND further refines it
		e := &expr.Binary{
			Left: &expr.Binary{
				Left:  &expr.Column{Name: "left_bool"},
				Op:    expr.BinaryOpAND,
				Right: &expr.Column{Name: "right_bool"},
			},
			Op: expr.BinaryOpAND,
			Right: &expr.Binary{
				Left:  &expr.Column{Name: "text"},
				Op:    expr.BinaryOpMatchRegex,
				Right: &expr.Regexp{Expression: regexp.MustCompile(".*r.*")},
			},
		}

		// left_bool AND right_bool = [false, false, true, false]
		// Only row 2 (cherry) should evaluate regex, which matches
		expect := columnartest.Array(t, columnar.KindBool, &alloc, false, false, true, false)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("AND with expensive right side to demonstrate benefit", func(t *testing.T) {
		// Expression: left_bool AND (text =~ ".*r.*")
		// Regex should only evaluate for rows 2 and 3 where left_bool is true
		e := &expr.Binary{
			Left: &expr.Column{Name: "left_bool"},
			Op:   expr.BinaryOpAND,
			Right: &expr.Binary{
				Left:  &expr.Column{Name: "text"},
				Op:    expr.BinaryOpMatchRegex,
				Right: &expr.Regexp{Expression: regexp.MustCompile(".*r.*")},
			},
		}

		// left_bool = [false, false, true, true]
		// Right side only evaluated for rows 2,3: cherry=true, date=false
		expect := columnartest.Array(t, columnar.KindBool, &alloc, false, false, true, false)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("empty selection bitmap with AND", func(t *testing.T) {
		// Verify empty selection (all rows selected) still works correctly
		e := &expr.Binary{
			Left:  &expr.Column{Name: "left_bool"},
			Op:    expr.BinaryOpAND,
			Right: &expr.Column{Name: "right_bool"},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, false, false, true, false)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})
}

// TestEvaluate_ANDShortCircuit_WithNulls tests AND short-circuit behavior
// when left side contains null values.
func TestEvaluate_ANDShortCircuit_WithNulls(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "left_with_nulls"},
			{Name: "right_bool"},
		}),
		4, // row count
		[]columnar.Array{
			// left has nulls at positions 1 and 3
			columnartest.Array(t, columnar.KindBool, &alloc, false, nil, true, nil),
			columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, false),
		},
	)

	t.Run("nulls in left side remain null in result", func(t *testing.T) {
		// Expression: left_with_nulls AND right_bool
		// Nulls should propagate, and right should only evaluate for row 2 (true)
		e := &expr.Binary{
			Left:  &expr.Column{Name: "left_with_nulls"},
			Op:    expr.BinaryOpAND,
			Right: &expr.Column{Name: "right_bool"},
		}

		// Expected: false AND true = false
		//          null AND true = null
		//          true AND false = false
		//          null AND false = null
		expect := columnartest.Array(t, columnar.KindBool, &alloc, false, nil, false, nil)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})
}

// TestEvaluate_ANDShortCircuit_TableDriven provides comprehensive table-driven
// tests for various AND combinations.
func TestEvaluate_ANDShortCircuit_TableDriven(t *testing.T) {
	var alloc memory.Allocator

	tests := []struct {
		name      string
		leftVals  []interface{}
		rightVals []interface{}
		expect    []interface{}
	}{
		{
			name:      "all-false left with all-true right",
			leftVals:  []interface{}{false, false, false, false},
			rightVals: []interface{}{true, true, true, true},
			expect:    []interface{}{false, false, false, false},
		},
		{
			name:      "all-true left with mixed right",
			leftVals:  []interface{}{true, true, true, true},
			rightVals: []interface{}{true, false, true, false},
			expect:    []interface{}{true, false, true, false},
		},
		{
			name:      "partial left with all-true right",
			leftVals:  []interface{}{true, false, true, false},
			rightVals: []interface{}{true, true, true, true},
			expect:    []interface{}{true, false, true, false},
		},
		{
			name:      "nulls in left with all-true right",
			leftVals:  []interface{}{true, nil, false, nil},
			rightVals: []interface{}{true, true, true, true},
			expect:    []interface{}{true, nil, false, nil},
		},
		{
			name:      "nulls in right with all-true left",
			leftVals:  []interface{}{true, true, true, true},
			rightVals: []interface{}{true, nil, false, nil},
			expect:    []interface{}{true, nil, false, nil},
		},
		{
			name:      "mixed nulls in both sides",
			leftVals:  []interface{}{true, false, nil, true},
			rightVals: []interface{}{nil, true, true, nil},
			expect:    []interface{}{nil, false, nil, nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := columnar.NewRecordBatch(
				columnar.NewSchema([]columnar.Column{
					{Name: "left"},
					{Name: "right"},
				}),
				int64(len(tt.leftVals)), // row count
				[]columnar.Array{
					columnartest.Array(t, columnar.KindBool, &alloc, tt.leftVals...),
					columnartest.Array(t, columnar.KindBool, &alloc, tt.rightVals...),
				},
			)

			e := &expr.Binary{
				Left:  &expr.Column{Name: "left"},
				Op:    expr.BinaryOpAND,
				Right: &expr.Column{Name: "right"},
			}

			expect := columnartest.Array(t, columnar.KindBool, &alloc, tt.expect...)

			result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
			require.NoError(t, err)
			columnartest.RequireDatumsEqual(t, expect, result)
		})
	}
}

// TestEvaluate_ORShortCircuit tests that OR operations optimize by skipping
// right-side evaluation for rows where left is true.
func TestEvaluate_ORShortCircuit(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "left_bool"},
			{Name: "right_bool"},
			{Name: "text"},
		}),
		4, // row count
		[]columnar.Array{
			columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, false),
			columnartest.Array(t, columnar.KindBool, &alloc, false, true, false, true),
			columnartest.Array(t, columnar.KindUTF8, &alloc, "apple", "banana", "cherry", "date"),
		},
	)

	t.Run("all-true left side with complex right side", func(t *testing.T) {
		// When left is all true, right should be skipped entirely
		// Expression: (true OR text =~ ".*") for all rows
		e := &expr.Binary{
			Left: &expr.Constant{Value: columnartest.Scalar(t, columnar.KindBool, true)},
			Op:   expr.BinaryOpOR,
			Right: &expr.Binary{
				Left:  &expr.Column{Name: "text"},
				Op:    expr.BinaryOpMatchRegex,
				Right: &expr.Regexp{Expression: regexp.MustCompile(".*")},
			},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, true, true, true)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("partially-true left side", func(t *testing.T) {
		// When left is partially true, right should only be evaluated for false rows
		// Expression: left_bool OR right_bool
		e := &expr.Binary{
			Left:  &expr.Column{Name: "left_bool"},
			Op:    expr.BinaryOpOR,
			Right: &expr.Column{Name: "right_bool"},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, true)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("all-false left side", func(t *testing.T) {
		// When left is all false, right must be evaluated for all rows (no optimization)
		// Expression: false OR right_bool
		e := &expr.Binary{
			Left:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindBool, false)},
			Op:    expr.BinaryOpOR,
			Right: &expr.Column{Name: "right_bool"},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, false, true, false, true)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("nested OR with cascading selection refinement", func(t *testing.T) {
		// Expression: (left_bool OR right_bool) OR (text =~ ".*a.*")
		// First OR refines selection, second OR further refines it
		e := &expr.Binary{
			Left: &expr.Binary{
				Left:  &expr.Column{Name: "left_bool"},
				Op:    expr.BinaryOpOR,
				Right: &expr.Column{Name: "right_bool"},
			},
			Op: expr.BinaryOpOR,
			Right: &expr.Binary{
				Left:  &expr.Column{Name: "text"},
				Op:    expr.BinaryOpMatchRegex,
				Right: &expr.Regexp{Expression: regexp.MustCompile(".*a.*")},
			},
		}

		// left_bool OR right_bool = [true, true, false, true]
		// Only row 2 (cherry) should evaluate regex, which doesn't match
		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, true)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("OR with expensive right side to demonstrate benefit", func(t *testing.T) {
		// Expression: left_bool OR (text =~ ".*a.*")
		// Regex should only evaluate for rows 2 and 3 where left_bool is false
		e := &expr.Binary{
			Left: &expr.Column{Name: "left_bool"},
			Op:   expr.BinaryOpOR,
			Right: &expr.Binary{
				Left:  &expr.Column{Name: "text"},
				Op:    expr.BinaryOpMatchRegex,
				Right: &expr.Regexp{Expression: regexp.MustCompile(".*a.*")},
			},
		}

		// left_bool = [true, true, false, false]
		// Right side only evaluated for rows 2,3: cherry=false, date=true
		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, true)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("empty selection bitmap with OR", func(t *testing.T) {
		// Verify empty selection (all rows selected) still works correctly
		e := &expr.Binary{
			Left:  &expr.Column{Name: "left_bool"},
			Op:    expr.BinaryOpOR,
			Right: &expr.Column{Name: "right_bool"},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, true)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})
}

// TestEvaluate_ORShortCircuit_WithNulls tests OR short-circuit behavior
// when left side contains null values.
func TestEvaluate_ORShortCircuit_WithNulls(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "left_with_nulls"},
			{Name: "right_bool"},
		}),
		4, // row count
		[]columnar.Array{
			// left has nulls at positions 1 and 3
			columnartest.Array(t, columnar.KindBool, &alloc, true, nil, false, nil),
			columnartest.Array(t, columnar.KindBool, &alloc, false, false, true, true),
		},
	)

	t.Run("nulls in left side remain null in result", func(t *testing.T) {
		// Expression: left_with_nulls OR right_bool
		// Current OR behavior: if either side is null, result is null
		// Right should still evaluate for rows with false in left
		e := &expr.Binary{
			Left:  &expr.Column{Name: "left_with_nulls"},
			Op:    expr.BinaryOpOR,
			Right: &expr.Column{Name: "right_bool"},
		}

		// Expected: true OR false = true
		//          null OR false = null (null propagates)
		//          false OR true = true
		//          null OR true = null (null propagates)
		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, nil, true, nil)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})
}

// TestEvaluate_ORShortCircuit_TableDriven provides comprehensive table-driven
// tests for various OR combinations.
func TestEvaluate_ORShortCircuit_TableDriven(t *testing.T) {
	var alloc memory.Allocator

	tests := []struct {
		name      string
		leftVals  []interface{}
		rightVals []interface{}
		expect    []interface{}
	}{
		{
			name:      "all-true left with all-false right",
			leftVals:  []interface{}{true, true, true, true},
			rightVals: []interface{}{false, false, false, false},
			expect:    []interface{}{true, true, true, true},
		},
		{
			name:      "all-false left with mixed right",
			leftVals:  []interface{}{false, false, false, false},
			rightVals: []interface{}{true, false, true, false},
			expect:    []interface{}{true, false, true, false},
		},
		{
			name:      "partial left with all-false right",
			leftVals:  []interface{}{true, false, true, false},
			rightVals: []interface{}{false, false, false, false},
			expect:    []interface{}{true, false, true, false},
		},
		{
			name:      "nulls in left with all-false right",
			leftVals:  []interface{}{true, nil, false, nil},
			rightVals: []interface{}{false, false, false, false},
			expect:    []interface{}{true, nil, false, nil},
		},
		{
			name:      "nulls in right with all-false left",
			leftVals:  []interface{}{false, false, false, false},
			rightVals: []interface{}{false, nil, true, nil},
			expect:    []interface{}{false, nil, true, nil},
		},
		{
			name:      "nulls in left with true right values",
			leftVals:  []interface{}{true, nil, false, nil},
			rightVals: []interface{}{false, true, true, true},
			expect:    []interface{}{true, nil, true, nil},
		},
		{
			name:      "mixed nulls in both sides",
			leftVals:  []interface{}{false, true, nil, false},
			rightVals: []interface{}{nil, false, false, nil},
			expect:    []interface{}{nil, true, nil, nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := columnar.NewRecordBatch(
				columnar.NewSchema([]columnar.Column{
					{Name: "left"},
					{Name: "right"},
				}),
				int64(len(tt.leftVals)), // row count
				[]columnar.Array{
					columnartest.Array(t, columnar.KindBool, &alloc, tt.leftVals...),
					columnartest.Array(t, columnar.KindBool, &alloc, tt.rightVals...),
				},
			)

			e := &expr.Binary{
				Left:  &expr.Column{Name: "left"},
				Op:    expr.BinaryOpOR,
				Right: &expr.Column{Name: "right"},
			}

			expect := columnartest.Array(t, columnar.KindBool, &alloc, tt.expect...)

			result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
			require.NoError(t, err)
			columnartest.RequireDatumsEqual(t, expect, result)
		})
	}
}

// TestEvaluate_EdgeCases_SelectionLengthWithRefinedSelections tests that
// selection length mismatch detection still works properly when refined
// selections are used in short-circuit optimization.
func TestEvaluate_EdgeCases_SelectionLengthWithRefinedSelections(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "age"},
			{Name: "active"},
		}),
		4, // row count
		[]columnar.Array{
			columnartest.Array(t, columnar.KindUint64, &alloc, 30, 25, 35, 28),
			columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false),
		},
	)

	t.Run("valid selection with AND short-circuit", func(t *testing.T) {
		// Proper selection length should work with short-circuit optimization
		selection := selectionMask(&alloc, true, false, true, true)
		e := &expr.Binary{
			Left: &expr.Binary{
				Left:  &expr.Column{Name: "age"},
				Op:    expr.BinaryOpGT,
				Right: &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 25)},
			},
			Op:    expr.BinaryOpAND,
			Right: &expr.Column{Name: "active"},
		}

		result, err := expr.Evaluate(&alloc, e, record, selection)
		require.NoError(t, err)
		require.NotNil(t, result)
	})

	t.Run("valid selection with OR short-circuit", func(t *testing.T) {
		// Proper selection length should work with short-circuit optimization
		selection := selectionMask(&alloc, true, true, false, false)
		e := &expr.Binary{
			Left: &expr.Binary{
				Left:  &expr.Column{Name: "age"},
				Op:    expr.BinaryOpLT,
				Right: &expr.Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 30)},
			},
			Op:    expr.BinaryOpOR,
			Right: &expr.Column{Name: "active"},
		}

		result, err := expr.Evaluate(&alloc, e, record, selection)
		require.NoError(t, err)
		require.NotNil(t, result)
	})
}

// TestEvaluate_EdgeCases_DeeplyNestedExpressions tests that selection
// propagation works correctly through deeply nested AND/OR expressions.
func TestEvaluate_EdgeCases_DeeplyNestedExpressions(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "a"},
			{Name: "b"},
			{Name: "c"},
			{Name: "d"},
		}),
		4, // row count
		[]columnar.Array{
			columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, false),
			columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false),
			columnartest.Array(t, columnar.KindBool, &alloc, false, true, true, false),
			columnartest.Array(t, columnar.KindBool, &alloc, true, true, true, true),
		},
	)

	t.Run("deeply nested AND - 3 levels", func(t *testing.T) {
		// Expression: ((a AND b) AND c) AND d
		// Level 1: a AND b = [true, false, false, false]
		// Level 2: [true, false, false, false] AND c = [false, false, false, false]
		// Level 3: [false, false, false, false] AND d = [false, false, false, false]
		e := &expr.Binary{
			Left: &expr.Binary{
				Left: &expr.Binary{
					Left:  &expr.Column{Name: "a"},
					Op:    expr.BinaryOpAND,
					Right: &expr.Column{Name: "b"},
				},
				Op:    expr.BinaryOpAND,
				Right: &expr.Column{Name: "c"},
			},
			Op:    expr.BinaryOpAND,
			Right: &expr.Column{Name: "d"},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, false, false, false, false)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("deeply nested OR - 3 levels", func(t *testing.T) {
		// Expression: ((a OR b) OR c) OR d
		// Level 1: a OR b = [true, true, true, false]
		// Level 2: [true, true, true, false] OR c = [true, true, true, false]
		// Level 3: [true, true, true, false] OR d = [true, true, true, true]
		e := &expr.Binary{
			Left: &expr.Binary{
				Left: &expr.Binary{
					Left:  &expr.Column{Name: "a"},
					Op:    expr.BinaryOpOR,
					Right: &expr.Column{Name: "b"},
				},
				Op:    expr.BinaryOpOR,
				Right: &expr.Column{Name: "c"},
			},
			Op:    expr.BinaryOpOR,
			Right: &expr.Column{Name: "d"},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, true, true, true)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("deeply nested AND with 4 levels", func(t *testing.T) {
		// Expression: (((a AND b) AND c) AND d) AND a
		// Should propagate selections through 4 levels
		e := &expr.Binary{
			Left: &expr.Binary{
				Left: &expr.Binary{
					Left: &expr.Binary{
						Left:  &expr.Column{Name: "a"},
						Op:    expr.BinaryOpAND,
						Right: &expr.Column{Name: "b"},
					},
					Op:    expr.BinaryOpAND,
					Right: &expr.Column{Name: "c"},
				},
				Op:    expr.BinaryOpAND,
				Right: &expr.Column{Name: "d"},
			},
			Op:    expr.BinaryOpAND,
			Right: &expr.Column{Name: "a"},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, false, false, false, false)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})
}

// TestEvaluate_EdgeCases_MixedANDOR tests expressions that mix AND and OR
// operators in the same tree.
func TestEvaluate_EdgeCases_MixedANDOR(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "a"},
			{Name: "b"},
			{Name: "c"},
		}),
		4, // row count
		[]columnar.Array{
			columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, false),
			columnartest.Array(t, columnar.KindBool, &alloc, false, true, false, true),
			columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false),
		},
	)

	t.Run("AND then OR: (a AND b) OR c", func(t *testing.T) {
		// a AND b = [false, true, false, false]
		// [false, true, false, false] OR c = [true, true, true, false]
		e := &expr.Binary{
			Left: &expr.Binary{
				Left:  &expr.Column{Name: "a"},
				Op:    expr.BinaryOpAND,
				Right: &expr.Column{Name: "b"},
			},
			Op:    expr.BinaryOpOR,
			Right: &expr.Column{Name: "c"},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, true, true, false)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("OR then AND: (a OR b) AND c", func(t *testing.T) {
		// a OR b = [true, true, false, true]
		// [true, true, false, true] AND c = [true, false, false, false]
		e := &expr.Binary{
			Left: &expr.Binary{
				Left:  &expr.Column{Name: "a"},
				Op:    expr.BinaryOpOR,
				Right: &expr.Column{Name: "b"},
			},
			Op:    expr.BinaryOpAND,
			Right: &expr.Column{Name: "c"},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, false, false, false)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("complex mixed: (a AND b) OR (b AND c)", func(t *testing.T) {
		// a AND b = [false, true, false, false]
		// b AND c = [false, false, false, false]
		// [false, true, false, false] OR [false, false, false, false] = [false, true, false, false]
		e := &expr.Binary{
			Left: &expr.Binary{
				Left:  &expr.Column{Name: "a"},
				Op:    expr.BinaryOpAND,
				Right: &expr.Column{Name: "b"},
			},
			Op: expr.BinaryOpOR,
			Right: &expr.Binary{
				Left:  &expr.Column{Name: "b"},
				Op:    expr.BinaryOpAND,
				Right: &expr.Column{Name: "c"},
			},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, false, true, false, false)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})
}

// TestEvaluate_EdgeCases_ScalarConstants tests AND/OR with scalar constants
// to verify no regression in scalar handling.
func TestEvaluate_EdgeCases_ScalarConstants(t *testing.T) {
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

	t.Run("AND with true constant on left", func(t *testing.T) {
		// true AND active = active
		e := &expr.Binary{
			Left:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindBool, true)},
			Op:    expr.BinaryOpAND,
			Right: &expr.Column{Name: "active"},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("AND with false constant on left", func(t *testing.T) {
		// false AND active = false (all false)
		e := &expr.Binary{
			Left:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindBool, false)},
			Op:    expr.BinaryOpAND,
			Right: &expr.Column{Name: "active"},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, false, false, false)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("OR with true constant on left", func(t *testing.T) {
		// true OR active = true (all true)
		e := &expr.Binary{
			Left:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindBool, true)},
			Op:    expr.BinaryOpOR,
			Right: &expr.Column{Name: "active"},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, true, true)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("OR with false constant on left", func(t *testing.T) {
		// false OR active = active
		e := &expr.Binary{
			Left:  &expr.Constant{Value: columnartest.Scalar(t, columnar.KindBool, false)},
			Op:    expr.BinaryOpOR,
			Right: &expr.Column{Name: "active"},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("AND with constant on right", func(t *testing.T) {
		// active AND true = active
		e := &expr.Binary{
			Left:  &expr.Column{Name: "active"},
			Op:    expr.BinaryOpAND,
			Right: &expr.Constant{Value: columnartest.Scalar(t, columnar.KindBool, true)},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("OR with constant on right", func(t *testing.T) {
		// active OR false = active
		e := &expr.Binary{
			Left:  &expr.Column{Name: "active"},
			Op:    expr.BinaryOpOR,
			Right: &expr.Constant{Value: columnartest.Scalar(t, columnar.KindBool, false)},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})
}

// TestEvaluate_EdgeCases_NonExistentColumns tests AND/OR with column
// references that don't exist, verifying error handling.
func TestEvaluate_EdgeCases_NonExistentColumns(t *testing.T) {
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

	t.Run("AND with non-existent column on left", func(t *testing.T) {
		// Non-existent column produces null type which causes AND to error
		e := &expr.Binary{
			Left:  &expr.Column{Name: "nonexistent"},
			Op:    expr.BinaryOpAND,
			Right: &expr.Column{Name: "active"},
		}

		_, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "null")
	})

	t.Run("AND with non-existent column on right", func(t *testing.T) {
		// Non-existent column produces null type which causes AND to error
		e := &expr.Binary{
			Left:  &expr.Column{Name: "active"},
			Op:    expr.BinaryOpAND,
			Right: &expr.Column{Name: "nonexistent"},
		}

		_, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "null")
	})

	t.Run("OR with non-existent column on left", func(t *testing.T) {
		// Non-existent column produces null type which causes OR to error
		e := &expr.Binary{
			Left:  &expr.Column{Name: "nonexistent"},
			Op:    expr.BinaryOpOR,
			Right: &expr.Column{Name: "active"},
		}

		_, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "null")
	})

	t.Run("OR with non-existent column on right", func(t *testing.T) {
		// Non-existent column produces null type which causes OR to error
		e := &expr.Binary{
			Left:  &expr.Column{Name: "active"},
			Op:    expr.BinaryOpOR,
			Right: &expr.Column{Name: "nonexistent"},
		}

		_, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "null")
	})
}

// TestEvaluate_EdgeCases_ZeroRowBatch tests AND/OR with zero-row batches
// to ensure selection optimization doesn't break edge cases.
func TestEvaluate_EdgeCases_ZeroRowBatch(t *testing.T) {
	var alloc memory.Allocator

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "active"},
			{Name: "status"},
		}),
		0, // zero rows
		[]columnar.Array{
			columnartest.Array(t, columnar.KindBool, &alloc),
			columnartest.Array(t, columnar.KindBool, &alloc),
		},
	)

	t.Run("AND with zero-row batch", func(t *testing.T) {
		e := &expr.Binary{
			Left:  &expr.Column{Name: "active"},
			Op:    expr.BinaryOpAND,
			Right: &expr.Column{Name: "status"},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("OR with zero-row batch", func(t *testing.T) {
		e := &expr.Binary{
			Left:  &expr.Column{Name: "active"},
			Op:    expr.BinaryOpOR,
			Right: &expr.Column{Name: "status"},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})

	t.Run("nested AND/OR with zero-row batch", func(t *testing.T) {
		// (active AND status) OR active
		e := &expr.Binary{
			Left: &expr.Binary{
				Left:  &expr.Column{Name: "active"},
				Op:    expr.BinaryOpAND,
				Right: &expr.Column{Name: "status"},
			},
			Op:    expr.BinaryOpOR,
			Right: &expr.Column{Name: "active"},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc)

		result, err := expr.Evaluate(&alloc, e, record, memory.Bitmap{})
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})
}
