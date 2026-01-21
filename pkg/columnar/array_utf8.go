package columnar

import "github.com/grafana/loki/v3/pkg/memory"

// UTF8 is an [Array] of UTF-8 encoded strings.
type UTF8 struct {
	validity  memory.Bitmap // Empty when there's no nulls.
	offsets   []int32
	data      []byte
	length    int
	nullCount int
}

var _ Array = (*UTF8)(nil)

// MakeUTF8 creates a new UTF8 array from the given data, offsets, and
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
// MakeUTF8 panics if the number of elements does not match the length of
// validity.
func MakeUTF8(data []byte, offsets []int32, validity memory.Bitmap) *UTF8 {
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

// Validity returns the validity bitmap of the array. The returned bitmap
// may be of length 0 if there are no nulls.
//
// A value of 1 in the Validity bitmap indicates that the corresponding
// element at that position is valid (not null).
func (arr *UTF8) Validity() memory.Bitmap { return arr.validity }

// Kind returns the kind of Array being represented.
func (arr *UTF8) Kind() Kind { return KindUTF8 }
