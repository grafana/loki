package columnar

import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/grafana/loki/v3/pkg/memory"
)

// Numeric is a constraint for recognized numeric types.
type Numeric interface{ int64 | uint64 }

// NumberScalar is a [Scalar] representing a [Numeric] value.
type NumberScalar[T Numeric] struct {
	Value T    // Value of the scalar.
	Null  bool // True if the scalar is null.

	kind Kind // Cached kind.
}

var _ Scalar = (*NumberScalar[int64])(nil)

// Kind implements [Datum] and returns the kind matching T.
func (s *NumberScalar[T]) Kind() Kind {
	if s.kind == KindNull {
		s.init()
	}
	return s.kind
}

//go:noinline
func (s *NumberScalar[T]) init() {
	var zero T
	switch reflect.TypeOf(zero).Kind() {
	case reflect.Int64:
		s.kind = KindInt64
	case reflect.Uint64:
		s.kind = KindUint64
	default:
		panic(fmt.Sprintf("unsupported type %T", zero))
	}
}

// IsNull implements [Scalar] and returns s.Null.
func (s *NumberScalar[T]) IsNull() bool { return s.Null }

func (s *NumberScalar[T]) isDatum()  {}
func (s *NumberScalar[T]) isScalar() {}

// Number is an [Array] of 64-bit unsigned [Numeric] values.
type Number[T Numeric] struct {
	validity  memory.Bitmap // Empty when there's no nulls.
	values    []T
	nullCount int
	kind      Kind // Determined in [Number.init] based on T.
}

var _ Array = (*Number[int64])(nil)

// NewNumber creates a new Number array from the given values and optional
// validity bitmap.
//
// Number arrays made from memory owned by a [memory.Allocator] are invalidated
// when the allocator reclaims memory.
//
// If validity is of length zero, all elements are considered valid. Otherwise,
// NewNumber panics if the number of elements does not match the length of
// validity.
func NewNumber[T Numeric](values []T, validity memory.Bitmap) *Number[T] {
	arr := &Number[T]{
		validity: validity,
		values:   values,
	}
	arr.init()
	return arr
}

//go:noinline
func (arr *Number[T]) init() {
	if arr.validity.Len() > 0 && arr.validity.Len() != len(arr.values) {
		panic("length mismatch between values and validity")
	}
	arr.nullCount = arr.validity.ClearCount()

	var zero T
	switch reflect.TypeOf(zero).Kind() {
	case reflect.Int64:
		arr.kind = KindInt64
	case reflect.Uint64:
		arr.kind = KindUint64
	default:
		panic(fmt.Sprintf("unsupported type %T", zero))
	}
}

// Len returns the total number of elements in the array.
func (arr *Number[T]) Len() int { return len(arr.values) }

// Nulls returns the number of null elements in the array. The number of
// non-null elements can be calculated from Len() - Nulls().
func (arr *Number[T]) Nulls() int { return arr.nullCount }

// Get returns the value at index i. If the element at index i is null, Get
// returns an undefined value.
//
// Get panics if i is out of range.
func (arr *Number[T]) Get(i int) T {
	return arr.values[i]
}

// IsNull returns true if the element at index i is null.
func (arr *Number[T]) IsNull(i int) bool {
	if arr.nullCount == 0 {
		return false
	}
	return !arr.validity.Get(i)
}

// Values returns the underlying array of values.
func (arr *Number[T]) Values() []T { return arr.values }

// Validity returns the validity bitmap of the array. The returned bitmap
// may be of length 0 if there are no nulls.
//
// A value of 1 in the Validity bitmap indicates that the corresponding
// element at that position is valid (not null).
func (arr *Number[T]) Validity() memory.Bitmap { return arr.validity }

// Size returns the size in bytes of the array's buffers.
func (arr *Number[T]) Size() int {
	var zero T

	var (
		validitySize = arr.validity.Len() / 8
		valuesSize   = len(arr.values) * int(unsafe.Sizeof(zero))
	)
	return validitySize + valuesSize
}

// Kind returns the kind of Array being represented.
func (arr *Number[T]) Kind() Kind { return arr.kind }

// Slice returns a slice of arr from i to j.
func (arr *Number[T]) Slice(i, j int) Array {
	if i < 0 || j < i || j > arr.Len() {
		panic(errorSliceBounds{i, j, arr.Len()})
	}
	var (
		validity = sliceValidity(arr.validity, i, j)
		values   = arr.values[i:j:j]
	)
	return NewNumber[T](values, validity)
}

func (arr *Number[T]) isDatum() {}
func (arr *Number[T]) isArray() {}

// A NumberBuilder assists with constructing a [Number] array. A NumberBuilder
// must be constructed by calling [NewNumberBuilder].
type NumberBuilder[T Numeric] struct {
	alloc *memory.Allocator

	validity memory.Bitmap
	values   memory.Buffer[T]
}

var _ Builder = (*NumberBuilder[int64])(nil)

// NewNumberBuilder creates a new NumberBuilder for constructing a [Number]
// array.
func NewNumberBuilder[T Numeric](alloc *memory.Allocator) *NumberBuilder[T] {
	return &NumberBuilder[T]{
		alloc:    alloc,
		validity: memory.NewBitmap(alloc, 0),
		values:   memory.NewBuffer[T](alloc, 0),
	}
}

// Grow increases b's capacity, if necessary, to guarantee space for another n
// elements. After Grow(n), at least n elements can be appended to b without
// another allocation. If n is negative or too large to allocate the memory,
// Grow panics.
func (b *NumberBuilder[T]) Grow(n int) {
	if !b.needGrow(n) {
		return
	}

	b.validity.Grow(n)
	b.values.Grow(n)
}

func (b *NumberBuilder[T]) needGrow(n int) bool {
	return b.values.Len()+n > b.values.Cap()
}

// AppendNull adds a new null element to b.
func (b *NumberBuilder[T]) AppendNull() {
	if b.needGrow(1) {
		b.Grow(1)
	}

	var zero T
	b.validity.AppendUnsafe(false)
	b.values.Push(zero)
}

// AppendNulls appends the given number of null elements to b.
func (b *NumberBuilder[T]) AppendNulls(count int) {
	if b.needGrow(count) {
		b.Grow(count)
	}

	var zero T
	b.validity.AppendCount(false, count)
	b.values.AppendCount(zero, count)
}

// AppendValue adds a new element to b.
func (b *NumberBuilder[T]) AppendValue(v T) {
	if b.needGrow(1) {
		b.Grow(1)
	}

	// We can use unsafe appends here because we guarantee in the check above
	// that there's enough capacity. This saves 40% of CPU time.
	b.validity.AppendUnsafe(true)
	b.values.Push(v)
}

// AppendValue adds a new element to b.
func (b *NumberBuilder[T]) AppendValueCount(v T, count int) {
	if b.needGrow(count) {
		b.Grow(count)
	}

	// We can use unsafe appends here because we guarantee in the check above
	// that there's enough capacity. This saves 40% of CPU time.
	b.validity.AppendCountUnsafe(true, count)
	b.values.AppendCount(v, count)
}

// BuildArray returns the constructed array. After calling Build, the builder
// is reset to an initial state.
func (b *NumberBuilder[T]) BuildArray() Array { return b.Build() }

// Build returns the constructed [Number] array. After calling Build, the builder
// is reset to an initial state.
func (b *NumberBuilder[T]) Build() *Number[T] {
	// Move the original bitmaps to the constructed array, then reset the
	// builder's bitmaps since they've been moved.
	arr := NewNumber[T](b.values.Data(), b.validity)
	b.validity = memory.NewBitmap(b.alloc, 0)
	b.values = memory.NewBuffer[T](b.alloc, 0)
	return arr
}
