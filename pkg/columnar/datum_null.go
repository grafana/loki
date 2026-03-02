package columnar

import "github.com/grafana/loki/v3/pkg/memory"

// NullScalar is a [Scalar] representing an untyped null value.
type NullScalar struct{}

var _ Scalar = (*NullScalar)(nil)

// Kind implements [Datum] and returns [KindNull].
func (s *NullScalar) Kind() Kind { return KindNull }

// IsNull implements [Scalar] and returns true.
func (s *NullScalar) IsNull() bool { return true }

func (s *NullScalar) isDatum()  {}
func (s *NullScalar) isScalar() {}

// Null is an [Array] of null values.
type Null struct {
	validity  memory.Bitmap
	nullCount int
}

var _ Array = (*Null)(nil)

// NewNull creates a new Null array with the given validity bitmap. NewNull
// panics if validity contains any bit set to true.
func NewNull(validity memory.Bitmap) *Null {
	arr := &Null{
		validity: validity,
	}
	arr.init()
	return arr
}

//go:noinline
func (arr *Null) init() {
	if arr.validity.SetCount() > 0 {
		panic("found Null array with non-null values in validity bitmap")
	}
	arr.nullCount = arr.validity.Len()
}

// Len returns the number of values in the array.
func (arr *Null) Len() int { return arr.nullCount }

// Nulls returns the number of values in the array.
func (arr *Null) Nulls() int { return arr.nullCount }

// IsNull returns true for all values in the array.
func (arr *Null) IsNull(_ int) bool { return true }

// Kind returns [KindNull].
func (arr *Null) Kind() Kind { return KindNull }

// Validity returns arr's validity bitmap.
func (arr *Null) Validity() memory.Bitmap { return arr.validity }

// Size returns the size in bytes of the array's buffers.
func (arr *Null) Size() int { return arr.validity.Len() / 8 }

// Slice returns a slice of arr from i to j.
func (arr *Null) Slice(i, j int) Array {
	if i < 0 || j < i || j > arr.Len() {
		panic(errorSliceBounds{i, j, arr.Len()})
	}
	return NewNull(sliceValidity(arr.validity, i, j))
}

func (arr *Null) isDatum() {}
func (arr *Null) isArray() {}

// A NullBuilder assists with constructing a [Null] array. A NullBuilder must be
// constructed by calling [NewNullBuilder].
type NullBuilder struct {
	alloc    *memory.Allocator
	validity memory.Bitmap
}

var _ Builder = (*NullBuilder)(nil)

// NewNullBuilder creates a new NullBuilder for constructing a [Null] array.
func NewNullBuilder(alloc *memory.Allocator) *NullBuilder {
	return &NullBuilder{
		alloc:    alloc,
		validity: memory.NewBitmap(alloc, 0),
	}
}

// Grow increases b's capacity, if necessary, to guarantee space for another n
// elements. After Grow(n), at least n elements can be appended to b without
// another allocation. If n is negative or too large to allocate the memory,
// Grow panics.
func (b *NullBuilder) Grow(n int) {
	if !b.needGrow(n) {
		return
	}
	b.validity.Grow(n)
}

func (b *NullBuilder) needGrow(n int) bool {
	return b.validity.Len()+n > b.validity.Cap()
}

// AppendNull adds a new null element to b.
func (b *NullBuilder) AppendNull() {
	if b.needGrow(1) {
		b.Grow(1)
	}
	b.validity.AppendUnsafe(false)
}

// AppendNulls appends the given number of null elements to b.
func (b *NullBuilder) AppendNulls(count int) {
	if b.needGrow(count) {
		b.Grow(count)
	}
	b.validity.AppendCount(false, count)
}

// Build returns the constructed array. After calling Build, the builder
// is reset to an initial state.
func (b *NullBuilder) BuildArray() Array { return b.Build() }

// Build returns the constructed [Null] array. After calling Build, the builder
// is reset to an initial state.
func (b *NullBuilder) Build() *Null {
	// Move the original bitmap to the constructed array, then reset the
	// builder's bitmap since it's been moved.
	arr := NewNull(b.validity)
	b.validity = memory.NewBitmap(b.alloc, 0)
	return arr
}
