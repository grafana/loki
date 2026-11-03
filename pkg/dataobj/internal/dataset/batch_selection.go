package dataset

import (
	"github.com/apache/arrow-go/v18/arrow/bitutil"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// A selection is a bitmap over the rows of a read window. A set bit selects the
// row. A selection of length zero selects every row of the window, which
// matches the selection vector convention of package compute and costs
// nothing to build or combine.
//
// A selection that selects no row is a full-length bitmap with no bit set. It
// is never the zero value.

// selectionCount returns how many of the n rows of a window sel selects.
func selectionCount(sel memory.Bitmap, n int) int {
	if sel.Len() == 0 {
		return n
	}
	return sel.SetCount()
}

// forEachSelected calls fn with the index of each row that sel selects, in
// ascending order.
func forEachSelected(sel memory.Bitmap, n int, fn func(i int)) {
	if sel.Len() == 0 {
		for i := range n {
			fn(i)
		}
		return
	}
	for i := range sel.IterValues(true) {
		fn(i)
	}
}

// newSelection returns a selection of n rows that all have the given value.
func newSelection(alloc *memory.Allocator, n int, value bool) memory.Bitmap {
	bm := memory.NewBitmap(alloc, n)
	bm.AppendCount(value, n)
	return bm
}

// andSelection returns the rows that both a and b select.
func andSelection(alloc *memory.Allocator, n int, a, b memory.Bitmap) memory.Bitmap {
	switch {
	case a.Len() == 0:
		return b
	case b.Len() == 0:
		return a
	}

	out := memory.NewBitmap(alloc, n)
	out.Resize(n)

	var (
		aBytes, aOff     = a.Bytes()
		bBytes, bOff     = b.Bytes()
		outBytes, outOff = out.Bytes()
	)
	bitutil.BitmapAnd(aBytes, bBytes, int64(aOff), int64(bOff), outBytes, int64(outOff), int64(n))
	return out
}

// orSelection returns the rows that a or b select. Both must be subsets of
// outer. A selection of length zero selects all of outer.
func orSelection(alloc *memory.Allocator, n int, outer, a, b memory.Bitmap) memory.Bitmap {
	if a.Len() == 0 || b.Len() == 0 {
		return outer
	}

	out := memory.NewBitmap(alloc, n)
	out.Resize(n)

	var (
		aBytes, aOff     = a.Bytes()
		bBytes, bOff     = b.Bytes()
		outBytes, outOff = out.Bytes()
	)
	bitutil.BitmapOr(aBytes, bBytes, int64(aOff), int64(bOff), outBytes, int64(outOff), int64(n))
	return out
}

// andNotSelection returns the rows that a selects and b does not.
func andNotSelection(alloc *memory.Allocator, n int, a, b memory.Bitmap) memory.Bitmap {
	if b.Len() == 0 {
		return newSelection(alloc, n, false)
	}

	out := memory.NewBitmap(alloc, n)
	out.Resize(n)
	outBytes, outOff := out.Bytes()
	bBytes, bOff := b.Bytes()

	if a.Len() == 0 {
		bitutil.InvertBitmap(bBytes, bOff, n, outBytes, outOff)
		return out
	}

	aBytes, aOff := a.Bytes()
	bitutil.BitmapAndNot(aBytes, bBytes, int64(aOff), int64(bOff), outBytes, int64(outOff), int64(n))
	return out
}

// leafSelection converts the result of a comparison kernel into a selection.
//
// The kernel leaves the values of rows outside sel undefined, so leafSelection
// intersects the result with sel. A row where the kernel input was null
// passes if nullPasses is true.
func leafSelection(alloc *memory.Allocator, n int, res columnar.Datum, nullPasses bool, sel memory.Bitmap) memory.Bitmap {
	arr := res.(*columnar.Bool)
	values := arr.Values()

	if arr.Nulls() > 0 {
		validity := arr.Validity()

		masked := memory.NewBitmap(alloc, n)
		masked.Resize(n)

		var (
			valuesBytes, valuesOff     = values.Bytes()
			validityBytes, validityOff = validity.Bytes()
			maskedBytes, maskedOff     = masked.Bytes()
		)
		bitutil.BitmapAnd(valuesBytes, validityBytes, int64(valuesOff), int64(validityOff), maskedBytes, int64(maskedOff), int64(n))

		if nullPasses {
			// masked |= ^validity
			inverted := memory.NewBitmap(alloc, n)
			inverted.Resize(n)
			invertedBytes, invertedOff := inverted.Bytes()
			bitutil.InvertBitmap(validityBytes, validityOff, n, invertedBytes, invertedOff)
			bitutil.BitmapOr(maskedBytes, invertedBytes, int64(maskedOff), int64(invertedOff), maskedBytes, int64(maskedOff), int64(n))
		}
		values = masked
	}

	return andSelection(alloc, n, values, sel)
}
