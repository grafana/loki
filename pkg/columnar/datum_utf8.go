package columnar

import (
	"github.com/grafana/loki/v3/pkg/memory"
)

// UTF8Scalar is a [Scalar] representing a [UTF8] value.
type UTF8Scalar struct {
	Value []byte // Value of the scalar.
	Null  bool   // True if the scalar is null.
}

var _ Scalar = (*UTF8Scalar)(nil)

// Kind implements [Datum] and returns [KindUTF8].
func (s *UTF8Scalar) Kind() Kind { return KindUTF8 }

// IsNull implements [Scalar] and returns s.Null.
func (s *UTF8Scalar) IsNull() bool { return s.Null }

func (s *UTF8Scalar) isDatum()  {}
func (s *UTF8Scalar) isScalar() {}

// UTF8 is an [Array] of UTF-8 encoded strings.
type UTF8 struct {
	validity  memory.Bitmap // Empty when there's no nulls.
	offsets   []int32
	data      []byte
	length    int
	nullCount int
}

var _ Array = (*UTF8)(nil)

// NewUTF8 creates a new UTF8 array from the given data, offsets, and
// optional validity bitmap.
//
// UTF8 arrays made from memory owned by a [memory.Allocator] are invalidated
// when the allocator reclaims memory.
//
// Each UTF-8 string goes from data[offsets[i] : offsets[i+1]]. Offsets must
// be monotonically increasing, even for null values. The offsets slice is not
// validated for correctness.
//
// If validity is of length zero, all elements are considered valid. Otherwise,
// NewUTF8 panics if the number of elements does not match the length of
// validity.
func NewUTF8(data []byte, offsets []int32, validity memory.Bitmap) *UTF8 {
	arr := &UTF8{
		validity: validity,
		offsets:  offsets,
		data:     data,
	}
	arr.init()
	return arr
}

//go:noinline
func (arr *UTF8) init() {
	// Moving initialization of additional fields to a non-inlined init method
	// improved the performance of the plain bytes decoder in dataset by 10%.

	numElements := max(0, len(arr.offsets)-1)
	if arr.validity.Len() > 0 && arr.validity.Len() != numElements {
		panic("length mismatch with validity")
	}

	arr.length = numElements
	arr.nullCount = arr.validity.ClearCount()
}

// Len returns the total number of elements in the array.
func (arr *UTF8) Len() int { return arr.length }

// Nulls returns the number of null elements in the array. The number of
// non-null elements can be calculated from Len() - Nulls().
func (arr *UTF8) Nulls() int { return arr.nullCount }

// Get returns the value at index i. If the element at index i is null, Get
// returns an empty string.
//
// Get panics if i is out of range.
func (arr *UTF8) Get(i int) []byte {
	var (
		start = arr.offsets[i]
		end   = arr.offsets[i+1]
	)
	return arr.data[start:end]
}

// IsNull returns true if the element at index i is null.
func (arr *UTF8) IsNull(i int) bool {
	if arr.nullCount == 0 {
		return false
	}
	return !arr.validity.Get(i)
}

// Data returns the underlying packed UTF8 bytes.
func (arr *UTF8) Data() []byte { return arr.data }

// Offsets returns the underlying offsets array.
func (arr *UTF8) Offsets() []int32 { return arr.offsets }

// Size returns the size in bytes of the array's buffers.
func (arr *UTF8) Size() int {
	var (
		validitySize = arr.validity.Len() / 8
		dataSize     = len(arr.data)
		offsetsSize  = len(arr.offsets) * 4 // *4 for int32
	)
	return validitySize + dataSize + offsetsSize
}

// Validity returns the validity bitmap of the array. The returned bitmap
// may be of length 0 if there are no nulls.
//
// A value of 1 in the Validity bitmap indicates that the corresponding
// element at that position is valid (not null).
func (arr *UTF8) Validity() memory.Bitmap { return arr.validity }

// Kind returns the kind of Array being represented.
func (arr *UTF8) Kind() Kind { return KindUTF8 }

func (arr *UTF8) isDatum() {}
func (arr *UTF8) isArray() {}

// A UTF8Builder assists with constructing a [UTF8] array. A UTF8Builder must be
// constructed by calling [NewUTF8Builder].
type UTF8Builder struct {
	alloc *memory.Allocator

	validity memory.Bitmap
	offsets  memory.Buffer[int32]
	data     memory.Buffer[byte]

	lastOffset int32
}

var _ Builder = (*UTF8Builder)(nil)

// NewUTF8Builder creates a new UTF8Builder for constructing a [UTF8] array.
func NewUTF8Builder(alloc *memory.Allocator) *UTF8Builder {
	return &UTF8Builder{
		alloc:    alloc,
		validity: memory.NewBitmap(alloc, 0),
		offsets:  memory.NewBuffer[int32](alloc, 0),
		data:     memory.NewBuffer[byte](alloc, 0),
	}
}

// Grow increases b's capacity, if necessary, to guarantee space for another n
// elements. After Grow(n), at least n elements can be appended to b without
// another allocation. If n is negative or too large to allocate the memory,
// Grow panics.
func (b *UTF8Builder) Grow(n int) {
	// b.offsets has an extra element for the starting offset.
	if !b.needGrow(n + 1) {
		return
	}

	b.validity.Grow(n)
	b.offsets.Grow(n + 1)
}

func (b *UTF8Builder) needGrow(n int) bool {
	return b.offsets.Len()+n > b.offsets.Cap()
}

// GrowData increases b's bytes capacity, if necessary, to guarantee space
// for another n bytes of data. After GrowData(n), at least n bytes can be
// appended to b (across all UTF8 values) without another allocation. If n is
// negative or too large to allocate the memory, GrowData panics.
//
// GrowData only impacts capacity of string data. Use [UTF8Builder.Grow] to
// reserve space for more elements.
func (b *UTF8Builder) GrowData(n int) {
	if !b.needGrowData(n) {
		return
	}
	b.data.Grow(n)
}

func (b *UTF8Builder) needGrowData(n int) bool {
	return b.data.Len()+n > b.data.Cap()
}

// AppendNull adds a new null element to b.
func (b *UTF8Builder) AppendNull() {
	if b.needGrow(1) {
		b.Grow(1)
	}
	b.initOffsets()

	b.validity.AppendUnsafe(false)
	b.offsets.Push(b.lastOffset)
}

func (b *UTF8Builder) initOffsets() {
	// For the first element, we need to push an initial offset of 0. All
	// elements after that will push the offset where that string ends (the
	// length).
	if b.offsets.Len() == 0 {
		b.offsets.Push(0)
	}
}

// AppendNulls appends the given number of null elements to b.
func (b *UTF8Builder) AppendNulls(count int) {
	if b.needGrow(count) {
		b.Grow(count)
	}
	b.initOffsets()

	b.validity.AppendCount(false, count)
	b.offsets.AppendCount(b.lastOffset, count)
}

// AppendValue adds a new non-null element to b.
func (b *UTF8Builder) AppendValue(v []byte) {
	dataSize := len(v)

	if b.needGrow(1) {
		b.Grow(1)
	}
	if b.needGrowData(dataSize) {
		b.GrowData(dataSize)
	}
	b.initOffsets()

	b.lastOffset += int32(dataSize)

	// We can use unsafe appends here because we guarantee in the check above
	// that there's enough capacity. This saves 40% of CPU time.
	b.validity.AppendUnsafe(true)
	b.data.Append(v...)
	b.offsets.Push(b.lastOffset)
}

// BuildArray returns the constructed array. After calling Build, the builder
// is reset to an initial state.
func (b *UTF8Builder) BuildArray() Array { return b.Build() }

// Build returns the constructed [UTF8] array. After calling Build, the builder
// is reset to an initial state.
func (b *UTF8Builder) Build() *UTF8 {
	// Move the original bitmaps to the constructed array, then reset the
	// builder's bitmaps since they've been moved.
	arr := NewUTF8(b.data.Data(), b.offsets.Data(), b.validity)
	b.validity = memory.NewBitmap(b.alloc, 0)
	b.offsets = memory.NewBuffer[int32](b.alloc, 0)
	b.data = memory.NewBuffer[byte](b.alloc, 0)
	b.lastOffset = 0
	return arr
}
