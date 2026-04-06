package compute

import (
	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

type numericEqualityKernel[T columnar.Numeric] interface {
	DoSS(left, right T) bool
	DoSA(out *memory.Bitmap, left T, right []T)
	DoAS(out *memory.Bitmap, left []T, right T)
	DoAA(out *memory.Bitmap, left, right []T)
}

var (
	int64EqualKernel    numericEqualityKernel[int64] = numericEqualKernelImpl[int64]{}
	int64NotEqualKernel numericEqualityKernel[int64] = numericNotEqualKernelImpl[int64]{}
	int64GTEKernel      numericEqualityKernel[int64] = numericGTEKernelImpl[int64]{}
	int64GTKernel       numericEqualityKernel[int64] = numericGTKernelImpl[int64]{}
	int64LTEKernel      numericEqualityKernel[int64] = numericLTEKernelImpl[int64]{}
	int64LTKernel       numericEqualityKernel[int64] = numericLTKernelImpl[int64]{}

	uint64EqualKernel    numericEqualityKernel[uint64] = numericEqualKernelImpl[uint64]{}
	uint64NotEqualKernel numericEqualityKernel[uint64] = numericNotEqualKernelImpl[uint64]{}
	uint64GTEKernel      numericEqualityKernel[uint64] = numericGTEKernelImpl[uint64]{}
	uint64GTKernel       numericEqualityKernel[uint64] = numericGTKernelImpl[uint64]{}
	uint64LTEKernel      numericEqualityKernel[uint64] = numericLTEKernelImpl[uint64]{}
	uint64LTKernel       numericEqualityKernel[uint64] = numericLTKernelImpl[uint64]{}
)

type numericEqualKernelImpl[T columnar.Numeric] struct{}

func (numericEqualKernelImpl[T]) DoSS(left, right T) bool {
	return left == right
}

func (numericEqualKernelImpl[T]) DoSA(out *memory.Bitmap, left T, right []T) {
	out.Resize(len(right))

	for i := range right {
		out.Set(i, left == right[i])
	}
}

func (numericEqualKernelImpl[T]) DoAS(out *memory.Bitmap, left []T, right T) {
	out.Resize(len(left))

	for i := range len(left) {
		out.Set(i, left[i] == right)
	}
}

func (numericEqualKernelImpl[T]) DoAA(out *memory.Bitmap, left, right []T) {
	if len(left) != len(right) {
		panic("invalid length")
	}

	out.Resize(len(left))

	for i := range len(left) {
		out.Set(i, left[i] == right[i])
	}
}

type numericNotEqualKernelImpl[T columnar.Numeric] struct{}

func (numericNotEqualKernelImpl[T]) DoSS(left, right T) bool {
	return left != right
}

func (numericNotEqualKernelImpl[T]) DoSA(out *memory.Bitmap, left T, right []T) {
	out.Resize(len(right))

	for i := range right {
		out.Set(i, left != right[i])
	}
}

func (numericNotEqualKernelImpl[T]) DoAS(out *memory.Bitmap, left []T, right T) {
	out.Resize(len(left))

	for i := range len(left) {
		out.Set(i, left[i] != right)
	}
}

func (numericNotEqualKernelImpl[T]) DoAA(out *memory.Bitmap, left, right []T) {
	if len(left) != len(right) {
		panic("invalid length")
	}

	out.Resize(len(left))

	for i := range len(left) {
		out.Set(i, left[i] != right[i])
	}
}

type numericGTKernelImpl[T columnar.Numeric] struct{}

func (numericGTKernelImpl[T]) DoSS(left, right T) bool {
	return left > right
}

func (numericGTKernelImpl[T]) DoSA(out *memory.Bitmap, left T, right []T) {
	out.Resize(len(right))

	for i := range right {
		out.Set(i, left > right[i])
	}
}

func (numericGTKernelImpl[T]) DoAS(out *memory.Bitmap, left []T, right T) {
	out.Resize(len(left))

	for i := range len(left) {
		out.Set(i, left[i] > right)
	}
}

func (numericGTKernelImpl[T]) DoAA(out *memory.Bitmap, left, right []T) {
	if len(left) != len(right) {
		panic("invalid length")
	}

	out.Resize(len(left))

	for i := range len(left) {
		out.Set(i, left[i] > right[i])
	}
}

type numericGTEKernelImpl[T columnar.Numeric] struct{}

func (numericGTEKernelImpl[T]) DoSS(left, right T) bool {
	return left >= right
}

func (numericGTEKernelImpl[T]) DoSA(out *memory.Bitmap, left T, right []T) {
	out.Resize(len(right))

	for i := range right {
		out.Set(i, left >= right[i])
	}
}

func (numericGTEKernelImpl[T]) DoAS(out *memory.Bitmap, left []T, right T) {
	out.Resize(len(left))

	for i := range len(left) {
		out.Set(i, left[i] >= right)
	}
}

func (numericGTEKernelImpl[T]) DoAA(out *memory.Bitmap, left, right []T) {
	if len(left) != len(right) {
		panic("invalid length")
	}

	out.Resize(len(left))

	for i := range len(left) {
		out.Set(i, left[i] >= right[i])
	}
}

type numericLTKernelImpl[T columnar.Numeric] struct{}

func (numericLTKernelImpl[T]) DoSS(left, right T) bool {
	return left < right
}

func (numericLTKernelImpl[T]) DoSA(out *memory.Bitmap, left T, right []T) {
	out.Resize(len(right))

	for i := range right {
		out.Set(i, left < right[i])
	}
}

func (numericLTKernelImpl[T]) DoAS(out *memory.Bitmap, left []T, right T) {
	out.Resize(len(left))

	for i := range len(left) {
		out.Set(i, left[i] < right)
	}
}

func (numericLTKernelImpl[T]) DoAA(out *memory.Bitmap, left, right []T) {
	if len(left) != len(right) {
		panic("invalid length")
	}

	out.Resize(len(left))

	for i := range len(left) {
		out.Set(i, left[i] < right[i])
	}
}

type numericLTEKernelImpl[T columnar.Numeric] struct{}

func (numericLTEKernelImpl[T]) DoSS(left, right T) bool {
	return left <= right
}

func (numericLTEKernelImpl[T]) DoSA(out *memory.Bitmap, left T, right []T) {
	out.Resize(len(right))

	for i := range right {
		out.Set(i, left <= right[i])
	}
}

func (numericLTEKernelImpl[T]) DoAS(out *memory.Bitmap, left []T, right T) {
	out.Resize(len(left))

	for i := range len(left) {
		out.Set(i, left[i] <= right)
	}
}

func (numericLTEKernelImpl[T]) DoAA(out *memory.Bitmap, left, right []T) {
	if len(left) != len(right) {
		panic("invalid length")
	}

	out.Resize(len(left))

	for i := range len(left) {
		out.Set(i, left[i] <= right[i])
	}
}
