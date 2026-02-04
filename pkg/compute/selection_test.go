package compute_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/memory"
)

func selectionMask(alloc *memory.Allocator, values ...bool) memory.Bitmap {
	bmap := memory.NewBitmap(alloc, len(values))
	for _, value := range values {
		bmap.Append(value)
	}
	return bmap
}

// requireBitmapsEqual checks that two bitmaps have the same values
func requireBitmapsEqual(t *testing.T, expected, actual memory.Bitmap) {
	t.Helper()
	require.Equal(t, expected.Len(), actual.Len(), "bitmap lengths differ")
	for i := 0; i < expected.Len(); i++ {
		require.Equal(t, expected.Get(i), actual.Get(i), "bitmap value differs at index %d", i)
	}
}

func TestCombineSelectionsAnd_EmptyLeftBitmap(t *testing.T) {
	var alloc memory.Allocator

	// When originalSelection is empty (all selected), refined selection should be
	// based solely on left's true values (excluding false and null)
	leftArr := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false).(*columnar.Bool)
	emptySelection := memory.Bitmap{}

	refined, err := compute.CombineSelectionsAnd(&alloc, leftArr, emptySelection)
	require.NoError(t, err)

	// Should select only rows where left is true (indices 0 and 2)
	expected := selectionMask(&alloc, true, false, true, false)
	requireBitmapsEqual(t, expected, refined)
}

func TestCombineSelectionsAnd_EmptyRightBitmap(t *testing.T) {
	var alloc memory.Allocator

	// When left is all true, refined selection should match original selection
	leftArr := columnartest.Array(t, columnar.KindBool, &alloc, true, true, true, true).(*columnar.Bool)
	originalSelection := selectionMask(&alloc, true, false, true, false)

	refined, err := compute.CombineSelectionsAnd(&alloc, leftArr, originalSelection)
	require.NoError(t, err)

	// Should be the same as original since left is all true
	requireBitmapsEqual(t, originalSelection, refined)
}

func TestCombineSelectionsAnd_BothBitmapsPopulated(t *testing.T) {
	var alloc memory.Allocator

	// Original selection: [T, F, T, T]
	// Left result:        [T, T, F, T]
	// Expected refined:   [T, F, F, T] (AND of both)
	leftArr := columnartest.Array(t, columnar.KindBool, &alloc, true, true, false, true).(*columnar.Bool)
	originalSelection := selectionMask(&alloc, true, false, true, true)

	refined, err := compute.CombineSelectionsAnd(&alloc, leftArr, originalSelection)
	require.NoError(t, err)

	expected := selectionMask(&alloc, true, false, false, true)
	requireBitmapsEqual(t, expected, refined)
}

func TestCombineSelectionsAnd_WithNulls(t *testing.T) {
	var alloc memory.Allocator

	// Left with nulls: [T, null, F, T]
	// Original selection: [T, T, T, T]
	// Expected refined: [T, F, F, T] (null treated as false for AND)
	leftArr := columnartest.Array(t, columnar.KindBool, &alloc, true, nil, false, true).(*columnar.Bool)
	originalSelection := selectionMask(&alloc, true, true, true, true)

	refined, err := compute.CombineSelectionsAnd(&alloc, leftArr, originalSelection)
	require.NoError(t, err)

	expected := selectionMask(&alloc, true, false, false, true)
	requireBitmapsEqual(t, expected, refined)
}

func TestCombineSelectionsAnd_ScalarInput(t *testing.T) {
	var alloc memory.Allocator

	// Scalars don't refine selection - should return original
	leftScalar := columnartest.Scalar(t, columnar.KindBool, true).(*columnar.BoolScalar)
	originalSelection := selectionMask(&alloc, true, false, true, false)

	refined, err := compute.CombineSelectionsAnd(&alloc, leftScalar, originalSelection)
	require.NoError(t, err)

	requireBitmapsEqual(t, originalSelection, refined)
}

func TestCombineSelectionsOr_EmptyLeftBitmap(t *testing.T) {
	var alloc memory.Allocator

	// When originalSelection is empty (all selected), refined selection should
	// exclude rows where left is true (since they're already determined)
	leftArr := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false).(*columnar.Bool)
	emptySelection := memory.Bitmap{}

	refined, err := compute.CombineSelectionsOr(&alloc, leftArr, emptySelection)
	require.NoError(t, err)

	// Should select only rows where left is NOT true (indices 1 and 3)
	expected := selectionMask(&alloc, false, true, false, true)
	requireBitmapsEqual(t, expected, refined)
}

func TestCombineSelectionsOr_PartiallyTrueLeft(t *testing.T) {
	var alloc memory.Allocator

	// Original selection: [T, T, T, T]
	// Left result:        [T, F, T, F]
	// Expected refined:   [F, T, F, T] (evaluate right only where left is not true)
	leftArr := columnartest.Array(t, columnar.KindBool, &alloc, true, false, true, false).(*columnar.Bool)
	originalSelection := selectionMask(&alloc, true, true, true, true)

	refined, err := compute.CombineSelectionsOr(&alloc, leftArr, originalSelection)
	require.NoError(t, err)

	expected := selectionMask(&alloc, false, true, false, true)
	requireBitmapsEqual(t, expected, refined)
}

func TestCombineSelectionsOr_AllTrueLeft(t *testing.T) {
	var alloc memory.Allocator

	// When left is all true, no rows need right evaluation
	leftArr := columnartest.Array(t, columnar.KindBool, &alloc, true, true, true, true).(*columnar.Bool)
	originalSelection := selectionMask(&alloc, true, true, true, true)

	refined, err := compute.CombineSelectionsOr(&alloc, leftArr, originalSelection)
	require.NoError(t, err)

	// All rows should be unselected (no evaluation needed)
	expected := selectionMask(&alloc, false, false, false, false)
	requireBitmapsEqual(t, expected, refined)
}

func TestCombineSelectionsOr_WithNulls(t *testing.T) {
	var alloc memory.Allocator

	// Left with nulls: [T, null, F, T]
	// Original selection: [T, T, T, T]
	// Expected refined: [F, T, T, F] (null needs right evaluation: null OR true = true)
	leftArr := columnartest.Array(t, columnar.KindBool, &alloc, true, nil, false, true).(*columnar.Bool)
	originalSelection := selectionMask(&alloc, true, true, true, true)

	refined, err := compute.CombineSelectionsOr(&alloc, leftArr, originalSelection)
	require.NoError(t, err)

	expected := selectionMask(&alloc, false, true, true, false)
	requireBitmapsEqual(t, expected, refined)
}

func TestCombineSelectionsOr_ScalarInput(t *testing.T) {
	var alloc memory.Allocator

	// Scalars don't refine selection - should return original
	leftScalar := columnartest.Scalar(t, columnar.KindBool, false).(*columnar.BoolScalar)
	originalSelection := selectionMask(&alloc, true, false, true, false)

	refined, err := compute.CombineSelectionsOr(&alloc, leftScalar, originalSelection)
	require.NoError(t, err)

	requireBitmapsEqual(t, originalSelection, refined)
}

// Table-driven test for AND combinations
func TestCombineSelectionsAnd_TableDriven(t *testing.T) {
	tests := []struct {
		name              string
		leftValues        []interface{}
		originalSelection []bool
		expectedRefined   []bool
	}{
		{
			name:              "all false left",
			leftValues:        []interface{}{false, false, false, false},
			originalSelection: []bool{true, true, true, true},
			expectedRefined:   []bool{false, false, false, false},
		},
		{
			name:              "all true left",
			leftValues:        []interface{}{true, true, true, true},
			originalSelection: []bool{true, true, true, true},
			expectedRefined:   []bool{true, true, true, true},
		},
		{
			name:              "partial left",
			leftValues:        []interface{}{true, false, true, false},
			originalSelection: []bool{true, true, true, true},
			expectedRefined:   []bool{true, false, true, false},
		},
		{
			name:              "with nulls",
			leftValues:        []interface{}{true, nil, false, true},
			originalSelection: []bool{true, true, true, true},
			expectedRefined:   []bool{true, false, false, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var alloc memory.Allocator
			leftArr := columnartest.Array(t, columnar.KindBool, &alloc, tt.leftValues...).(*columnar.Bool)
			originalSelection := selectionMask(&alloc, tt.originalSelection...)

			refined, err := compute.CombineSelectionsAnd(&alloc, leftArr, originalSelection)
			require.NoError(t, err)

			expected := selectionMask(&alloc, tt.expectedRefined...)
			requireBitmapsEqual(t, expected, refined)
		})
	}
}

// Table-driven test for OR combinations
func TestCombineSelectionsOr_TableDriven(t *testing.T) {
	tests := []struct {
		name              string
		leftValues        []interface{}
		originalSelection []bool
		expectedRefined   []bool
	}{
		{
			name:              "all false left",
			leftValues:        []interface{}{false, false, false, false},
			originalSelection: []bool{true, true, true, true},
			expectedRefined:   []bool{true, true, true, true},
		},
		{
			name:              "all true left",
			leftValues:        []interface{}{true, true, true, true},
			originalSelection: []bool{true, true, true, true},
			expectedRefined:   []bool{false, false, false, false},
		},
		{
			name:              "partial left",
			leftValues:        []interface{}{true, false, true, false},
			originalSelection: []bool{true, true, true, true},
			expectedRefined:   []bool{false, true, false, true},
		},
		{
			name:              "with nulls",
			leftValues:        []interface{}{true, nil, false, true},
			originalSelection: []bool{true, true, true, true},
			expectedRefined:   []bool{false, true, true, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var alloc memory.Allocator
			leftArr := columnartest.Array(t, columnar.KindBool, &alloc, tt.leftValues...).(*columnar.Bool)
			originalSelection := selectionMask(&alloc, tt.originalSelection...)

			refined, err := compute.CombineSelectionsOr(&alloc, leftArr, originalSelection)
			require.NoError(t, err)

			expected := selectionMask(&alloc, tt.expectedRefined...)
			requireBitmapsEqual(t, expected, refined)
		})
	}
}
