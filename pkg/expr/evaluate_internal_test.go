package expr

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/memory"
)

func selectionMask(alloc *memory.Allocator, values ...bool) memory.Bitmap {
	bmap := memory.NewBitmap(alloc, len(values))
	for _, value := range values {
		bmap.Append(value)
	}
	return bmap
}

func TestEvaluateWithSelection(t *testing.T) {
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

	t.Run("binary masking", func(t *testing.T) {
		e := &Binary{
			Left:  &Column{Name: "age"},
			Op:    BinaryOpGT,
			Right: &Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 25)},
		}

		expect := columnartest.Array(t, columnar.KindBool, &alloc, true, nil, true)

		result, err := evaluateWithSelection(&alloc, e, record, selection)
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
		expr      Expression
		expect    columnar.Datum
	}{
		{
			name:      "nested AND with partial selection",
			selection: selectionMask(&alloc, true, false, true, true),
			// (age > 25 AND active)
			expr: &Binary{
				Left: &Binary{
					Left:  &Column{Name: "age"},
					Op:    BinaryOpGT,
					Right: &Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 25)},
				},
				Op:    BinaryOpAND,
				Right: &Column{Name: "active"},
			},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, nil, true, true),
		},
		{
			name:      "nested OR with partial selection",
			selection: selectionMask(&alloc, true, true, false, false),
			// (age < 30 OR active)
			expr: &Binary{
				Left: &Binary{
					Left:  &Column{Name: "age"},
					Op:    BinaryOpLT,
					Right: &Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 30)},
				},
				Op:    BinaryOpOR,
				Right: &Column{Name: "active"},
			},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, true, nil, nil),
		},
		{
			name:      "NOT with nested comparison and selection",
			selection: selectionMask(&alloc, false, true, true, false),
			// NOT(age == 25)
			expr: &Unary{
				Op: UnaryOpNOT,
				Value: &Binary{
					Left:  &Column{Name: "age"},
					Op:    BinaryOpEQ,
					Right: &Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 25)},
				},
			},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, false, true, nil),
		},
		{
			name:      "complex nested expression with selection",
			selection: selectionMask(&alloc, true, false, true, true),
			// ((age > 25 AND active) OR name == "Bob")
			expr: &Binary{
				Left: &Binary{
					Left: &Binary{
						Left:  &Column{Name: "age"},
						Op:    BinaryOpGT,
						Right: &Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 25)},
					},
					Op:    BinaryOpAND,
					Right: &Column{Name: "active"},
				},
				Op: BinaryOpOR,
				Right: &Binary{
					Left:  &Column{Name: "name"},
					Op:    BinaryOpEQ,
					Right: &Constant{Value: columnartest.Scalar(t, columnar.KindUTF8, "Bob")},
				},
			},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, nil, true, true),
		},
		{
			name:      "empty selection means all rows selected",
			selection: memory.Bitmap{},
			expr: &Binary{
				Left:  &Column{Name: "age"},
				Op:    BinaryOpGT,
				Right: &Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 25)},
			},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, true),
		},
		{
			name:      "all-false selection",
			selection: selectionMask(&alloc, false, false, false, false),
			expr: &Binary{
				Left:  &Column{Name: "age"},
				Op:    BinaryOpGT,
				Right: &Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 25)},
			},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil, nil),
		},
		{
			name:      "single-row selection (first)",
			selection: selectionMask(&alloc, true, false, false, false),
			expr: &Binary{
				Left:  &Column{Name: "name"},
				Op:    BinaryOpHasSubstrIgnoreCase,
				Right: &Constant{Value: columnartest.Scalar(t, columnar.KindUTF8, "li")},
			},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, true, nil, nil, nil),
		},
		{
			name:      "single-row selection (middle)",
			selection: selectionMask(&alloc, false, false, true, false),
			expr: &Binary{
				Left:  &Column{Name: "name"},
				Op:    BinaryOpHasSubstrIgnoreCase,
				Right: &Constant{Value: columnartest.Scalar(t, columnar.KindUTF8, "li")},
			},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, true, nil),
		},
		{
			name:      "single-row selection (last)",
			selection: selectionMask(&alloc, false, false, false, true),
			expr: &Binary{
				Left:  &Column{Name: "name"},
				Op:    BinaryOpHasSubstrIgnoreCase,
				Right: &Constant{Value: columnartest.Scalar(t, columnar.KindUTF8, "li")},
			},
			expect: columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, nil, false),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluateWithSelection(&alloc, tt.expr, record, tt.selection)
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
			{Name: "active"},
		}),
		4, // row count
		[]columnar.Array{
			// Column with null at position 1
			columnartest.Array(t, columnar.KindBool, &alloc, true, nil, false, true),
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
			expect:    columnartest.Array(t, columnar.KindBool, &alloc, true, nil, nil, nil),
		},
		{
			name:      "selection excludes null position",
			selection: selectionMask(&alloc, true, false, true, true),
			expect:    columnartest.Array(t, columnar.KindBool, &alloc, true, nil, false, true),
		},
		{
			name:      "selection overlaps null and valid values",
			selection: selectionMask(&alloc, false, true, true, false),
			expect:    columnartest.Array(t, columnar.KindBool, &alloc, nil, nil, false, nil),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Binary{
				Left:  &Column{Name: "active"},
				Op:    BinaryOpAND,
				Right: &Column{Name: "active"},
			}
			result, err := evaluateWithSelection(&alloc, e, record, tt.selection)
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
			e := &Column{Name: "age"}
			_, err := evaluateWithSelection(&alloc, e, record, tt.selection)
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
		e := &Constant{Value: columnartest.Scalar(t, columnar.KindUint64, 42)}
		expect := columnartest.Scalar(t, columnar.KindUint64, 42)

		result, err := evaluateWithSelection(&alloc, e, record, selection)
		require.NoError(t, err)
		columnartest.RequireDatumsEqual(t, expect, result)
	})
}
