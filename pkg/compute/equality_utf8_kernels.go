package compute

import (
	"bytes"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

type utf8EqualityKernel interface {
	DoSS(left, right []byte) bool
	DoSA(out *memory.Bitmap, left []byte, right *columnar.UTF8)
	DoAS(out *memory.Bitmap, left *columnar.UTF8, right []byte)
	DoAA(out *memory.Bitmap, left, right *columnar.UTF8)
}

var (
	utf8EqualKernel    utf8EqualityKernel = utf8EqualKernelImpl{}
	utf8NotEqualKernel utf8EqualityKernel = utf8NotEqualKernelImpl{}
	utf8GTKernel       utf8EqualityKernel = utf8GTKernelImpl{}
	utf8GTEKernel      utf8EqualityKernel = utf8GTEKernelImpl{}
	utf8LTKernel       utf8EqualityKernel = utf8LTKernelImpl{}
	utf8LTEKernel      utf8EqualityKernel = utf8LTEKernelImpl{}
)

type utf8EqualKernelImpl struct{}

func (utf8EqualKernelImpl) DoSS(left, right []byte) bool { return bytes.Equal(left, right) }

func (utf8EqualKernelImpl) DoSA(out *memory.Bitmap, left []byte, right *columnar.UTF8) {
	out.Resize(right.Len())

	for i := range right.Len() {
		out.Set(i, bytes.Equal(left, right.Get(i)))
	}
}

func (utf8EqualKernelImpl) DoAS(out *memory.Bitmap, left *columnar.UTF8, right []byte) {
	out.Resize(left.Len())

	for i := range left.Len() {
		out.Set(i, bytes.Equal(left.Get(i), right))
	}
}

func (utf8EqualKernelImpl) DoAA(out *memory.Bitmap, left, right *columnar.UTF8) {
	out.Resize(left.Len())

	for i := range left.Len() {
		out.Set(i, bytes.Equal(left.Get(i), right.Get(i)))
	}
}

type utf8NotEqualKernelImpl struct{}

func (utf8NotEqualKernelImpl) DoSS(left, right []byte) bool { return !bytes.Equal(left, right) }

func (utf8NotEqualKernelImpl) DoSA(out *memory.Bitmap, left []byte, right *columnar.UTF8) {
	out.Resize(right.Len())

	for i := range right.Len() {
		out.Set(i, !bytes.Equal(left, right.Get(i)))
	}
}

func (utf8NotEqualKernelImpl) DoAS(out *memory.Bitmap, left *columnar.UTF8, right []byte) {
	out.Resize(left.Len())

	for i := range left.Len() {
		out.Set(i, !bytes.Equal(left.Get(i), right))
	}
}

func (utf8NotEqualKernelImpl) DoAA(out *memory.Bitmap, left, right *columnar.UTF8) {
	out.Resize(left.Len())

	for i := range left.Len() {
		out.Set(i, !bytes.Equal(left.Get(i), right.Get(i)))
	}
}

type utf8GTKernelImpl struct{}

func (utf8GTKernelImpl) DoSS(left, right []byte) bool { return bytes.Compare(left, right) > 0 }

func (utf8GTKernelImpl) DoSA(out *memory.Bitmap, left []byte, right *columnar.UTF8) {
	out.Resize(right.Len())

	for i := range right.Len() {
		out.Set(i, bytes.Compare(left, right.Get(i)) > 0)
	}
}

func (utf8GTKernelImpl) DoAS(out *memory.Bitmap, left *columnar.UTF8, right []byte) {
	out.Resize(left.Len())

	for i := range left.Len() {
		out.Set(i, bytes.Compare(left.Get(i), right) > 0)
	}
}

func (utf8GTKernelImpl) DoAA(out *memory.Bitmap, left, right *columnar.UTF8) {
	out.Resize(left.Len())

	for i := range left.Len() {
		out.Set(i, bytes.Compare(left.Get(i), right.Get(i)) > 0)
	}
}

type utf8GTEKernelImpl struct{}

func (utf8GTEKernelImpl) DoSS(left, right []byte) bool { return bytes.Compare(left, right) >= 0 }

func (utf8GTEKernelImpl) DoSA(out *memory.Bitmap, left []byte, right *columnar.UTF8) {
	out.Resize(right.Len())

	for i := range right.Len() {
		out.Set(i, bytes.Compare(left, right.Get(i)) >= 0)
	}
}

func (utf8GTEKernelImpl) DoAS(out *memory.Bitmap, left *columnar.UTF8, right []byte) {
	out.Resize(left.Len())

	for i := range left.Len() {
		out.Set(i, bytes.Compare(left.Get(i), right) >= 0)
	}
}

func (utf8GTEKernelImpl) DoAA(out *memory.Bitmap, left, right *columnar.UTF8) {
	out.Resize(left.Len())

	for i := range left.Len() {
		out.Set(i, bytes.Compare(left.Get(i), right.Get(i)) >= 0)
	}
}

type utf8LTKernelImpl struct{}

func (utf8LTKernelImpl) DoSS(left, right []byte) bool { return bytes.Compare(left, right) < 0 }

func (utf8LTKernelImpl) DoSA(out *memory.Bitmap, left []byte, right *columnar.UTF8) {
	out.Resize(right.Len())

	for i := range right.Len() {
		out.Set(i, bytes.Compare(left, right.Get(i)) < 0)
	}
}

func (utf8LTKernelImpl) DoAS(out *memory.Bitmap, left *columnar.UTF8, right []byte) {
	out.Resize(left.Len())

	for i := range left.Len() {
		out.Set(i, bytes.Compare(left.Get(i), right) < 0)
	}
}

func (utf8LTKernelImpl) DoAA(out *memory.Bitmap, left, right *columnar.UTF8) {
	out.Resize(left.Len())

	for i := range left.Len() {
		out.Set(i, bytes.Compare(left.Get(i), right.Get(i)) < 0)
	}
}

type utf8LTEKernelImpl struct{}

func (utf8LTEKernelImpl) DoSS(left, right []byte) bool { return bytes.Compare(left, right) <= 0 }

func (utf8LTEKernelImpl) DoSA(out *memory.Bitmap, left []byte, right *columnar.UTF8) {
	out.Resize(right.Len())

	for i := range right.Len() {
		out.Set(i, bytes.Compare(left, right.Get(i)) <= 0)
	}
}

func (utf8LTEKernelImpl) DoAS(out *memory.Bitmap, left *columnar.UTF8, right []byte) {
	out.Resize(left.Len())

	for i := range left.Len() {
		out.Set(i, bytes.Compare(left.Get(i), right) <= 0)
	}
}

func (utf8LTEKernelImpl) DoAA(out *memory.Bitmap, left, right *columnar.UTF8) {
	out.Resize(left.Len())

	for i := range left.Len() {
		out.Set(i, bytes.Compare(left.Get(i), right.Get(i)) <= 0)
	}
}
