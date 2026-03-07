package compute

import (
	"fmt"
	"iter"

	"github.com/apache/arrow-go/v18/arrow/bitutil"

	"github.com/grafana/loki/v3/pkg/memory"
)

// computeValiditySS determines an output validity based on two null checks.
func computeValiditySS(leftNull, rightNull bool) bool {
	return !leftNull && !rightNull
}

// computeValiditySAA determines an output validity bitmap from a null check, a validity bitmap, and a selection vector.
func computeValiditySAA(alloc *memory.Allocator, leftNull bool, right memory.Bitmap, selection memory.Bitmap) (memory.Bitmap, error) {
	if right.ClearCount() > 0 && selection.ClearCount() > 0 && right.Len() != selection.Len() {
		return memory.Bitmap{}, fmt.Errorf("validity bitmap and selection length mismatch")
	}

	switch {
	case leftNull:
		// If the scalar value is null, everything is null.
		maxSize := max(1, right.Len(), selection.Len())
		validity := memory.NewBitmap(alloc, maxSize)
		validity.AppendCount(false, maxSize)
		return validity, nil

	default:
		return computeValidityAA(alloc, right, selection)
	}
}

// computeValidityASA determines an output validity bitmap from a validity bitmap, a null check, and a selection vector.
func computeValidityASA(alloc *memory.Allocator, left memory.Bitmap, rightNull bool, selection memory.Bitmap) (memory.Bitmap, error) {
	return computeValiditySAA(alloc, rightNull, left, selection)
}

// computeValidityAAA determines an output validity bitmap from two input validity bitmaps and a selection vector.
// The result is a logical AND of all three bitmaps.
func computeValidityAAA(alloc *memory.Allocator, left memory.Bitmap, right memory.Bitmap, selection memory.Bitmap) (memory.Bitmap, error) {
	validity, err := computeValidityAA(alloc, left, right)
	if err != nil {
		return memory.Bitmap{}, err
	}
	validity, err = computeValidityAA(alloc, validity, selection)
	if err != nil {
		return memory.Bitmap{}, fmt.Errorf("selection: %w", err)
	}

	return validity, nil
}

// computeValidityAA determines an output validity bitmap from two input
// validity bitmaps. The result is a logical AND of the validity; a slot is only
// valid if both inputs are valid.
func computeValidityAA(alloc *memory.Allocator, left, right memory.Bitmap) (memory.Bitmap, error) {
	leftHasNulls, rightHasNulls := left.ClearCount() > 0, right.ClearCount() > 0
	leftLen, rightLen := left.Len(), right.Len()

	// A validity bitmap can have a length of zero to indicate that all values
	// are valid. We only want to validate the length of two non-empty bitmaps.
	if leftLen > 0 && rightLen > 0 && leftLen != rightLen {
		return memory.Bitmap{}, fmt.Errorf("validity bitmap length mismatch: %d != %d", leftLen, rightLen)
	}

	switch {
	case leftHasNulls && rightHasNulls:
		validity := memory.NewBitmap(alloc, leftLen)
		validity.Resize(leftLen)

		var (
			leftBytes, leftOffset   = left.Bytes()
			rightBytes, rightOffset = right.Bytes()
			outBytes, outOffset     = validity.Bytes()
		)

		bitutil.BitmapAnd(
			leftBytes,
			rightBytes,
			int64(leftOffset),
			int64(rightOffset),
			outBytes,
			int64(outOffset),
			int64(leftLen), /* num values */
		)

		return validity, nil

	case leftHasNulls:
		// Make a copy of the left validity; everything from right is valid.
		return left.Clone(alloc), nil

	case rightHasNulls:
		// Make a copy of the right validity; everything from left is valid.
		return right.Clone(alloc), nil

	case !leftHasNulls && !rightHasNulls:
		return memory.Bitmap{}, nil
	}

	panic("unreachable")
}

// iterTrue returns an iterator that provides indices of the set bits in provided [bitmap].
// If all bits are set (bitmap.Len() == 0) it fallbacks to a simple iterator over
// all the elements.
func iterTrue(bitmap memory.Bitmap, bitmapLen int) iter.Seq[int] {
	if bitmap.Len() == 0 {
		return func(yield func(int) bool) {
			for i := range bitmapLen {
				yield(i)
			}
		}
	}

	return bitmap.IterValues(true)
}
