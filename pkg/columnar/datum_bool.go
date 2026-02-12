package columnar

import (
	"github.com/grafana/loki/v3/pkg/memory"
)

// BoolScalar is a [Scalar] representing a boolean value.
type BoolScalar struct {
	Value bool // Value of the scalar.
	Null  bool // True if the scalar is null.
}

// Kind implements [Datum] and returns [KindBool].
func (s *BoolScalar) Kind() Kind { return KindBool }

// IsNull implements [Scalar] and returns bs.Null.
func (s *BoolScalar) IsNull() bool { return s.Null }

func (s *BoolScalar) isDatum()  {}
func (s *BoolScalar) isScalar() {}

// Bool is an [Array] of bit-packed boolean values.
type Bool struct {
	validity  memory.Bitmap // Empty when there's no nulls.
	values    memory.Bitmap
	nullCount int
}

var _ Array = (*Bool)(nil)

// NewBool creates a new Bool array from the given values and optional validity
// bitmap.
//
// Bool arrays made from memory owned by a [memory.Allocator] are invalidated
// when the allocator reclaims memory.
//
// If validity is of length zero, all elements are considered valid. Otherwise,
// NewBool panics if the number of elements does not match the length of
// validity.
func NewBool(values, validity memory.Bitmap) *Bool {
	arr := &Bool{
		validity: validity,
		values:   values,
	}
	arr.init()
	return arr
}

//go:noinline
func (arr *Bool) init() {
	if arr.validity.Len() > 0 && arr.validity.Len() != arr.values.Len() {
		panic("length mismatch between values and validity")
	}
	arr.nullCount = arr.validity.ClearCount()
}

// Len returns the total number of elements in the array.
func (arr *Bool) Len() int { return arr.values.Len() }

// Nulls returns the number of null elements in the array. The number of
// non-null elements can be calculated from Len() - Nulls().
func (arr *Bool) Nulls() int { return arr.nullCount }

// Get returns the value at index i. If the element at index i is null, Get
// returns an undefined value.
//
// Get panics if i is out of range.
func (arr *Bool) Get(i int) bool {
	return arr.values.Get(i)
}

// IsNull returns true if the element at index i is null.
func (arr *Bool) IsNull(i int) bool {
	if arr.nullCount == 0 {
		return false
	}
	return !arr.validity.Get(i)
}

// Values returns the underlying values bitmap.
func (arr *Bool) Values() memory.Bitmap { return arr.values }

// Validity returns the validity bitmap of the array. The returned bitmap
// may be of length 0 if there are no nulls.
//
// A value of 1 in the Validity bitmap indicates that the corresponding
// element at that position is valid (not null).
func (arr *Bool) Validity() memory.Bitmap { return arr.validity }

// Kind returns the kind of Array being represented.
func (arr *Bool) Kind() Kind { return KindBool }

// Size returns the size in bytes of arr's buffers.
func (arr *Bool) Size() int {
	var (
		validitySize = arr.validity.Len() / 8
		valuesSize   = arr.values.Len() / 8
	)
	return validitySize + valuesSize
}

// Slice returns a slice of arr from i to j.
func (arr *Bool) Slice(i, j int) Array {
	if i < 0 || j < i || j > arr.Len() {
		panic(errorSliceBounds{i, j, arr.Len()})
	}
	var (
		validity = sliceValidity(arr.validity, i, j)
		values   = arr.values.Slice(i, j)
	)
	return NewBool(*values, validity)
}

func sliceValidity(validity memory.Bitmap, i, j int) memory.Bitmap {
	if validity.Len() == 0 {
		return memory.Bitmap{}
	}
	return *validity.Slice(i, j)
}

func (arr *Bool) isDatum() {}
func (arr *Bool) isArray() {}

// A BoolBuilder assists with constructing a [Bool] array. A BoolBuilder must be
// constructed by calling [NewBoolBuilder].
type BoolBuilder struct {
	alloc *memory.Allocator

	validity memory.Bitmap
	values   memory.Bitmap
}

var _ Builder = (*BoolBuilder)(nil)

// NewBoolBuilder creates a new BoolBuilder for constructing a [Bool] array.
func NewBoolBuilder(alloc *memory.Allocator) *BoolBuilder {
	return &BoolBuilder{
		alloc:    alloc,
		validity: memory.NewBitmap(alloc, 0),
		values:   memory.NewBitmap(alloc, 0),
	}
}

// Grow increases b's capacity, if necessary, to guarantee space for another n
// elements. After Grow(n), at least n elements can be appended to b without
// another allocation. If n is negative or too large to allocate the memory,
// Grow panics.
func (b *BoolBuilder) Grow(n int) {
	if !b.needGrow(n) {
		return
	}

	b.validity.Grow(n)
	b.values.Grow(n)
}

func (b *BoolBuilder) needGrow(n int) bool {
	return b.values.Len()+n > b.values.Cap()
}

// AppendNull adds a new null element to b.
func (b *BoolBuilder) AppendNull() {
	if b.needGrow(1) {
		b.Grow(1)
	}

	b.validity.AppendUnsafe(false)
	b.values.AppendUnsafe(false)
}

// AppendNulls appends the given number of null elements to b.
func (b *BoolBuilder) AppendNulls(count int) {
	if b.needGrow(count) {
		b.Grow(count)
	}

	b.validity.AppendCount(false, count)
	b.values.AppendCount(false, count)
}

// AppendValue adds a new element to b.
func (b *BoolBuilder) AppendValue(v bool) {
	if b.needGrow(1) {
		b.Grow(1)
	}

	// We can use unsafe appends here because we guarantee in the check above
	// that there's enough capacity. This saves 40% of CPU time.
	b.validity.AppendUnsafe(true)
	b.values.AppendUnsafe(v)
}

// AppendValue adds a new element to b.
func (b *BoolBuilder) AppendValueCount(v bool, count int) {
	if b.needGrow(count) {
		b.Grow(count)
	}

	// We can use unsafe appends here because we guarantee in the check above
	// that there's enough capacity. This saves 40% of CPU time.
	b.validity.AppendCountUnsafe(true, count)
	b.values.AppendCountUnsafe(v, count)
}

// Build returns the constructed array. After calling Build, the builder
// is reset to an initial state.
func (b *BoolBuilder) BuildArray() Array { return b.Build() }

// Build returns the constructed [Bool] array. After calling Build, the builder
// is reset to an initial state.
func (b *BoolBuilder) Build() *Bool {
	// Move the original bitmaps to the constructed array, then reset the
	// builder's bitmaps since they've been moved.
	arr := NewBool(b.values, b.validity)
	b.validity = memory.NewBitmap(b.alloc, 0)
	b.values = memory.NewBitmap(b.alloc, 0)
	return arr
}
