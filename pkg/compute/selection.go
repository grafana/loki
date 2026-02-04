package compute

import (
	"github.com/apache/arrow-go/v18/arrow/bitutil"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// ApplySelectionToBoolArray applies a selection bitmap to a boolean array,
// marking unselected rows as null in the result.
func ApplySelectionToBoolArray(alloc *memory.Allocator, arr *columnar.Bool, selection memory.Bitmap) (*columnar.Bool, error) {
	if selection.Len() == 0 {
		return arr, nil
	}
	validity, err := computeValidityAA(alloc, arr.Validity(), selection)
	if err != nil {
		return nil, err
	}
	return columnar.NewBool(arr.Values(), validity), nil
}

// ApplySelectionToNumberArray applies a selection bitmap to a numeric array,
// marking unselected rows as null in the result.
func ApplySelectionToNumberArray[T columnar.Numeric](alloc *memory.Allocator, arr *columnar.Number[T], selection memory.Bitmap) (*columnar.Number[T], error) {
	if selection.Len() == 0 {
		return arr, nil
	}
	validity, err := computeValidityAA(alloc, arr.Validity(), selection)
	if err != nil {
		return nil, err
	}
	return columnar.NewNumber(arr.Values(), validity), nil
}

// ApplySelectionToUTF8Array applies a selection bitmap to a UTF8 array,
// marking unselected rows as null in the result.
func ApplySelectionToUTF8Array(alloc *memory.Allocator, arr *columnar.UTF8, selection memory.Bitmap) (*columnar.UTF8, error) {
	if selection.Len() == 0 {
		return arr, nil
	}
	validity, err := computeValidityAA(alloc, arr.Validity(), selection)
	if err != nil {
		return nil, err
	}
	return columnar.NewUTF8(arr.Data(), arr.Offsets(), validity), nil
}

// CombineSelectionsAnd creates a refined selection bitmap for AND short-circuit
// optimization. Returns a bitmap where a bit is true only if:
//   - The original selection has a true bit at that position (or originalSelection is empty)
//   - The left result is true (not false or null) at that position
//
// This allows skipping evaluation of the right side for rows where left is
// false or null, since those rows are deterministic for AND operations.
//
// Empty bitmap (Len() == 0) represents "all selected".
func CombineSelectionsAnd(alloc *memory.Allocator, left columnar.Datum, originalSelection memory.Bitmap) (memory.Bitmap, error) {
	// Handle scalar case - scalars don't refine selection
	if _, isScalar := left.(columnar.Scalar); isScalar {
		return originalSelection, nil
	}

	// Must be a boolean array at this point
	leftArr, ok := left.(*columnar.Bool)
	if !ok {
		return memory.Bitmap{}, nil
	}

	length := leftArr.Len()
	leftValues := leftArr.Values()
	leftValidity := leftArr.Validity()

	allSelected := originalSelection.Len() == 0
	leftAllNotNulls := leftValidity.Len() == 0

	switch {
	case allSelected && leftAllNotNulls:
		return leftValues, nil
	case allSelected && !leftAllNotNulls:
		// Need to combine left values AND left validity (skip false and null)
		refinedSelection := memory.NewBitmap(alloc, length)
		refinedSelection.Resize(length)
		bitutil.BitmapAnd(
			leftValues.Bytes(),
			leftValidity.Bytes(),
			0, 0,
			refinedSelection.Bytes(),
			0,
			int64(length),
		)
		return refinedSelection, nil
	case !allSelected && leftAllNotNulls:
		// No nulls, combine original selection and left values
		refinedSelection := memory.NewBitmap(alloc, length)
		refinedSelection.Resize(length)

		bitutil.BitmapAnd(
			originalSelection.Bytes(),
			leftValues.Bytes(),
			0, 0,
			refinedSelection.Bytes(),
			0,
			int64(length),
		)

		return refinedSelection, nil
	case !allSelected && !leftAllNotNulls:
		refinedSelection := memory.NewBitmap(alloc, length)
		refinedSelection.Resize(length)
		tempBitmap := memory.NewBitmap(alloc, length)
		tempBitmap.Resize(length)

		bitutil.BitmapAnd(
			leftValues.Bytes(),
			leftValidity.Bytes(),
			0, 0,
			tempBitmap.Bytes(),
			0,
			int64(length),
		)

		bitutil.BitmapAnd(
			originalSelection.Bytes(),
			tempBitmap.Bytes(),
			0, 0,
			refinedSelection.Bytes(),
			0,
			int64(length),
		)

		return refinedSelection, nil
	default:
		panic("unreachable")
	}
}

// CombineSelectionsOr creates a refined selection bitmap for OR short-circuit
// optimization. Returns a bitmap where a bit is true only if:
//   - The original selection has a true bit at that position (or originalSelection is empty)
//   - The left result is NOT true at that position (false or null)
//
// This allows skipping evaluation of the right side for rows where left is
// already true, since those rows are deterministic for OR operations.
// Null rows still need right evaluation (null OR true = true).
//
// Empty bitmap (Len() == 0) represents "all selected".
func CombineSelectionsOr(alloc *memory.Allocator, left columnar.Datum, originalSelection memory.Bitmap) (memory.Bitmap, error) {
	// Handle scalar case - scalars don't refine selection
	if _, isScalar := left.(columnar.Scalar); isScalar {
		return originalSelection, nil
	}

	// Must be a boolean array at this point
	leftArr, ok := left.(*columnar.Bool)
	if !ok {
		return memory.Bitmap{}, nil
	}

	length := leftArr.Len()
	leftValues := leftArr.Values()
	leftValidity := leftArr.Validity()

	// Create a bitmap representing rows where left is true AND valid (not null)
	leftTrueBits := memory.NewBitmap(alloc, length)
	leftTrueBits.Resize(length)

	if leftValidity.Len() == 0 {
		// No nulls, just use values bitmap (represents where left is true)
		bitutil.CopyBitmap(leftValues.Bytes(), 0, length, leftTrueBits.Bytes(), 0)
	} else {
		// Combine values AND validity to get rows where left is true AND valid
		bitutil.BitmapAnd(
			leftValues.Bytes(),
			leftValidity.Bytes(),
			0, 0,
			leftTrueBits.Bytes(),
			0,
			int64(length),
		)
	}

	// Now invert leftTrueBits to get rows where left is NOT true (false or null)
	leftNotTrueBits := memory.NewBitmap(alloc, length)
	leftNotTrueBits.Resize(length)
	bitutil.InvertBitmap(leftTrueBits.Bytes(), 0, length, leftNotTrueBits.Bytes(), 0)

	// If original selection is empty (all selected), return leftNotTrueBits
	if originalSelection.Len() == 0 {
		return leftNotTrueBits, nil
	}

	// Combine original selection AND leftNotTrueBits
	refinedSelection := memory.NewBitmap(alloc, length)
	refinedSelection.Resize(length)

	bitutil.BitmapAnd(
		originalSelection.Bytes(),
		leftNotTrueBits.Bytes(),
		0, 0,
		refinedSelection.Bytes(),
		0,
		int64(length),
	)

	return refinedSelection, nil
}
