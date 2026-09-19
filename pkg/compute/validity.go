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

// computeValiditySA determines an output validity bitmap from a null check and
// a validity bitmap.
func computeValiditySA(alloc *memory.Allocator, leftNull bool, right memory.Bitmap) memory.Bitmap {
	// Even if right.Len is empty, we may still need up to one element depending
	// on the value of leftNull.
	//
	// The only case where we can return an empty bitmap is if left is not null
	// and right is empty.
	maxSize := max(1, right.Len())

	switch {
	case leftNull:
		// If the scalar value is null, everything is null.
		validity := memory.NewBitmap(alloc, maxSize)
		validity.AppendCount(false, maxSize)
		return validity

	case right.Len() == 0:
		// We don't have an input bitmap, meaning everything is valid.
		return memory.Bitmap{}

	default:
		// left is valid, so the final bitmap depends on the values from right.
		validity := memory.NewBitmap(alloc, maxSize)
		validity.AppendBitmap(right)
		return validity
	}
}

// computeValidityAS determines an output validity bitmap from a validity bitmap
// and a null check.
func computeValidityAS(alloc *memory.Allocator, left memory.Bitmap, rightNull bool) memory.Bitmap {
	// Even if left.Len is empty, we may still need up to one element depending
	// on the value of rightNull.
	//
	// The only case where we can return an empty bitmap is if left is empty and
	// right is not null.
	maxSize := max(1, left.Len())

	switch {
	case rightNull:
		// If the scalar value is null, everything is null.
		validity := memory.NewBitmap(alloc, maxSize)
		validity.AppendCount(false, maxSize)
		return validity

	case left.Len() == 0:
		// We don't have an input bitmap, meaning everything is valid.
		return memory.Bitmap{}

	default:
		// right is valid, so the final bitmap depends on the values from left.
		validity := memory.NewBitmap(alloc, maxSize)
		validity.AppendBitmap(left)
		return validity
	}
}

// computeValidityAA determines an output validity bitmap from two input
// validity bitmaps. The result is a logical AND of the validity; a slot is only
// valid if both inputs are valid.
func computeValidityAA(alloc *memory.Allocator, left, right memory.Bitmap) (memory.Bitmap, error) {
	leftLen, rightLen := left.Len(), right.Len()
	outLen := max(leftLen, rightLen)

	// A validity bitmap can have a length of zero to indicate that all values
	// are valid. We only want to validate the length of two non-empty bitmaps.
	if leftLen > 0 && rightLen > 0 && leftLen != rightLen {
		return memory.Bitmap{}, fmt.Errorf("validity bitmap length mismatch: %d != %d", left.Len(), right.Len())
	}

	switch {
	case leftLen > 0 && rightLen > 0:
		validity := memory.NewBitmap(alloc, outLen)
		validity.Resize(outLen)

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
			int64(outLen), /* num values */
		)

		return validity, nil

	case leftLen > 0:
		// Make a copy of the left validity; everything from right is valid.
		validity := memory.NewBitmap(alloc, left.Len())
		validity.AppendBitmap(left)
		return validity, nil

	case rightLen > 0:
		// Make a copy of the right validity; everything from left is valid.
		validity := memory.NewBitmap(alloc, right.Len())
		validity.AppendBitmap(right)
		return validity, nil

	case leftLen == 0 && rightLen == 0:
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
