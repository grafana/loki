package compute

import (
	"github.com/apache/arrow-go/v18/arrow/bitutil"

	"github.com/grafana/loki/v3/pkg/memory"
)

type logicalKernel interface {
	// DoSS performs a logical operation on two scalar values.
	DoSS(left, right bool) bool

	// DoSA performs a logical operation on a scalar value and a bitmap array.
	DoSA(out *memory.Bitmap, left bool, right memory.Bitmap)

	// DoAS performs a logical operation on a bitmap array and a scalar value.
	DoAS(out *memory.Bitmap, left memory.Bitmap, right bool)

	// DoAA performs a logical operation on two bitmap arrays.
	DoAA(out *memory.Bitmap, left, right memory.Bitmap)
}

var (
	logicalAndKernel logicalKernel = logicalAndKernelImpl{}
	logicalOrKernel  logicalKernel = logicalOrKernelImpl{}
)

type logicalAndKernelImpl struct{}

func (logicalAndKernelImpl) DoSS(left, right bool) bool { return left && right }

func (logicalAndKernelImpl) DoSA(out *memory.Bitmap, left bool, right memory.Bitmap) {
	// For safety, make sure the length is set to 0 so we can call append
	// functions.
	out.Resize(0)

	if left {
		// (true AND <array>) means that the result is a copy of right.
		out.AppendBitmap(right)
	} else {
		// (false AND <array>) means that the result is all false.
		out.AppendCount(false, right.Len())
	}
}

func (logicalAndKernelImpl) DoAS(out *memory.Bitmap, left memory.Bitmap, right bool) {
	// For safety, make sure the length is set to 0 so we can call append
	// functions.
	out.Resize(0)

	if right {
		// (<array> AND true) means that the result is a copy of left.
		out.AppendBitmap(left)
	} else {
		// (<array> AND false) means that the result is all false.
		out.AppendCount(false, left.Len())
	}
}

func (logicalAndKernelImpl) DoAA(out *memory.Bitmap, left memory.Bitmap, right memory.Bitmap) {
	if left.Len() != right.Len() {
		panic("unexpected length mismatch")
	}

	out.Resize(left.Len())

	bitutil.BitmapAnd(
		left.Bytes(),
		right.Bytes(),
		0 /* left offset */, 0, /* right offset */
		out.Bytes(),
		0,                 /* out offset */
		int64(left.Len()), /* num values */
	)
}

type logicalOrKernelImpl struct{}

func (logicalOrKernelImpl) DoSS(left, right bool) bool { return left || right }

func (logicalOrKernelImpl) DoSA(out *memory.Bitmap, left bool, right memory.Bitmap) {
	// For safety, make sure the length is set to 0 so we can call append
	// functions.
	out.Resize(0)

	if left {
		// (true OR <array>) means that the result is all true.
		out.AppendCount(true, right.Len())
	} else {
		// (false OR <array>) means that the result is a copy of right.
		out.AppendBitmap(right)
	}
}

func (logicalOrKernelImpl) DoAS(out *memory.Bitmap, left memory.Bitmap, right bool) {
	// For safety, make sure the length is set to 0 so we can call append
	// functions.
	out.Resize(0)

	if right {
		// (<array> OR true) means that the result is all true.
		out.AppendCount(true, left.Len())
	} else {
		// (<array> OR false) means that the result is a copy of left.
		out.AppendBitmap(left)
	}
}

func (logicalOrKernelImpl) DoAA(out *memory.Bitmap, left memory.Bitmap, right memory.Bitmap) {
	if left.Len() != right.Len() {
		panic("unexpected length mismatch")
	}

	out.Resize(left.Len())

	bitutil.BitmapOr(
		left.Bytes(),
		right.Bytes(),
		0 /* left offset */, 0, /* right offset */
		out.Bytes(),
		0,                 /* out offset */
		int64(left.Len()), /* num values */
	)
}
