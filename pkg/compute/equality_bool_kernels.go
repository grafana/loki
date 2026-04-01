package compute

import (
	"github.com/apache/arrow-go/v18/arrow/bitutil"

	"github.com/grafana/loki/v3/pkg/memory"
)

type boolEqualityKernel interface {
	DoSS(left, right bool) bool
	DoSA(out *memory.Bitmap, left bool, right memory.Bitmap)
	DoAS(out *memory.Bitmap, left memory.Bitmap, right bool)
	DoAA(out *memory.Bitmap, left, right memory.Bitmap)
}

var (
	boolEqualKernel    boolEqualityKernel = boolEqualKernelImpl{}
	boolNotEqualKernel boolEqualityKernel = boolNotEqualKernelImpl{}
)

type boolEqualKernelImpl struct{}

func (boolEqualKernelImpl) DoSS(left, right bool) bool { return left == right }

func (boolEqualKernelImpl) DoSA(out *memory.Bitmap, left bool, right memory.Bitmap) {
	out.Resize(right.Len())

	// TODO(rfratto): This would be way faster by doing an xnor over the words,
	// and projecting left to the word size.
	for i := range right.Len() {
		out.Set(i, right.Get(i) == left)
	}
}

func (boolEqualKernelImpl) DoAS(out *memory.Bitmap, left memory.Bitmap, right bool) {
	out.Resize(left.Len())

	// TODO(rfratto): This would be way faster by doing an xnor over the words,
	// and projecting right to the word size.
	for i := range left.Len() {
		out.Set(i, left.Get(i) == right)
	}
}

func (boolEqualKernelImpl) DoAA(out *memory.Bitmap, left, right memory.Bitmap) {
	if left.Len() != right.Len() {
		panic("unexpected length mismatch")
	}

	out.Resize(left.Len())

	var (
		leftBytes, leftOffset   = left.Bytes()
		rightBytes, rightOffset = right.Bytes()
		outBytes, outOffset     = out.Bytes()
	)

	bitutil.BitmapXnor(
		leftBytes,
		rightBytes,
		int64(leftOffset),
		int64(rightOffset),
		outBytes,
		int64(outOffset),
		int64(left.Len()), /* num values */
	)
}

type boolNotEqualKernelImpl struct{}

func (boolNotEqualKernelImpl) DoSS(left, right bool) bool { return left != right }

func (boolNotEqualKernelImpl) DoSA(out *memory.Bitmap, left bool, right memory.Bitmap) {
	out.Resize(right.Len())

	// TODO(rfratto): This would be way faster by doing an xor over the words,
	// and projecting left to the word size.
	for i := range right.Len() {
		out.Set(i, right.Get(i) != left)
	}
}

func (boolNotEqualKernelImpl) DoAS(out *memory.Bitmap, left memory.Bitmap, right bool) {
	out.Resize(left.Len())

	// TODO(rfratto): This would be way faster by doing an xor over the words,
	// and projecting right to the word size.
	for i := range left.Len() {
		out.Set(i, left.Get(i) != right)
	}
}

func (boolNotEqualKernelImpl) DoAA(out *memory.Bitmap, left, right memory.Bitmap) {
	if left.Len() != right.Len() {
		panic("unexpected length mismatch")
	}

	out.Resize(left.Len())

	var (
		leftBytes, leftOffset   = left.Bytes()
		rightBytes, rightOffset = right.Bytes()
		outBytes, outOffset     = out.Bytes()
	)

	bitutil.BitmapXor(
		leftBytes,
		rightBytes,
		int64(leftOffset),
		int64(rightOffset),
		outBytes,
		int64(outOffset),
		int64(left.Len()), /* num values */
	)
}
