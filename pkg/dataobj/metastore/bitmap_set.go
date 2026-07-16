package metastore

import (
	"github.com/apache/arrow-go/v18/arrow/bitutil"

	"github.com/grafana/loki/v3/pkg/memory"
)

// unionInto adds every bit set in src to dst and returns dst. It allocates a
// copy when dst is nil and grows dst only when src is longer.
func unionInto(dst, src *memory.Bitmap) *memory.Bitmap {
	if src == nil {
		return dst
	}
	if dst == nil {
		return src.Clone(nil)
	}

	if src.Len() > dst.Len() {
		oldLen := dst.Len()
		dst.Resize(src.Len())
		// Allocator-backed memory may be reused without being zeroed.
		dst.SetRange(oldLen, dst.Len(), false)
	}

	dstData, dstOffset := dst.Bytes()
	srcData, srcOffset := src.Bytes()
	bitutil.BitmapOr(
		dstData,
		srcData,
		int64(dstOffset),
		int64(srcOffset),
		dstData,
		int64(dstOffset),
		int64(src.Len()),
	)
	return dst
}

// intersectInto retains in dst only the bits also set in src and returns dst.
// It grows dst when src is longer and clears dst's unmatched trailing bits when
// src is shorter.
func intersectInto(dst, src *memory.Bitmap) *memory.Bitmap {
	if dst == nil {
		if src == nil {
			return nil
		}
		out := memory.NewBitmap(nil, src.Len())
		out.Resize(src.Len())
		out.SetRange(0, out.Len(), false)
		return &out
	}
	if src == nil {
		dst.SetRange(0, dst.Len(), false)
		return dst
	}

	if src.Len() > dst.Len() {
		oldLen := dst.Len()
		dst.Resize(src.Len())
		// Allocator-backed memory may be reused without being zeroed.
		dst.SetRange(oldLen, dst.Len(), false)
	}

	dstData, dstOffset := dst.Bytes()
	srcData, srcOffset := src.Bytes()
	bitutil.BitmapAnd(
		dstData,
		srcData,
		int64(dstOffset),
		int64(srcOffset),
		dstData,
		int64(dstOffset),
		int64(src.Len()),
	)
	if src.Len() < dst.Len() {
		dst.SetRange(src.Len(), dst.Len(), false)
	}
	return dst
}

// differenceBitmaps returns a new bitmap holding the bits set in a but not in b
// (a AND NOT b). A nil operand is treated as empty, and the shorter operand is
// zero-extended to the longer.
func differenceBitmaps(a, b *memory.Bitmap) *memory.Bitmap {
	return combineBitmaps(bitutil.BitmapAndNot, a, b)
}

func combineBitmaps(
	op func(left, right []byte, leftOffset, rightOffset int64, out []byte, outOffset, length int64),
	a, b *memory.Bitmap,
) *memory.Bitmap {
	left, right := alignBitmaps(a, b)

	out := memory.NewBitmap(nil, left.Len())
	out.Resize(left.Len())

	leftData, leftOffset := left.Bytes()
	rightData, rightOffset := right.Bytes()
	outData, outOffset := out.Bytes()
	op(
		leftData,
		rightData,
		int64(leftOffset),
		int64(rightOffset),
		outData,
		int64(outOffset),
		int64(out.Len()),
	)
	return &out
}

// alignBitmaps returns a and b as values, zero-extending the shorter to match
// the longer. A nil bitmap becomes an empty one.
func alignBitmaps(a, b *memory.Bitmap) (memory.Bitmap, memory.Bitmap) {
	var left, right memory.Bitmap
	if a != nil {
		left = *a
	}
	if b != nil {
		right = *b
	}
	if left.Len() == right.Len() {
		return left, right
	}
	n := max(left.Len(), right.Len())
	return extendBitmap(left, n), extendBitmap(right, n)
}

// extendBitmap returns a length-n copy of b with the trailing bits cleared,
// leaving b unmodified.
func extendBitmap(b memory.Bitmap, n int) memory.Bitmap {
	if b.Len() >= n {
		return b
	}
	out := memory.NewBitmap(nil, n)
	out.AppendBitmap(b)
	out.AppendCount(false, n-b.Len())
	return out
}
