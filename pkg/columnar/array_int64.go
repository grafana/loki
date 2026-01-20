package columnar

import "github.com/grafana/loki/v3/pkg/memory"

// Int64 is an [Array] of 64-bit signed integer values.
type Int64 struct {
	validity  memory.Bitmap // Empty when there's no nulls.
	values    []int64
	nullCount int
}

var _ Array = (*Int64)(nil)

// MakeInt64 creates a new Int64 array from the given values and optional validity
// bitmap.
//
// Int64 arrays made from memory owned by a [memory.Allocator] are invalidated
// when the allocator reclaims memory.
//
// If validity is of length zero, all elements are considered valid. Otherwise,
// MakeInt64 panics if the number of elements does not match the length of
// validity.
func MakeInt64(values []int64, validity memory.Bitmap) *Int64 {
	arr := &Int64{
		validity: validity,
		values:   values,
	}
	arr.init()
	return arr
}

//go:noinline
func (arr *Int64) init() {
	if arr.validity.Len() > 0 && arr.validity.Len() != len(arr.values) {
		panic("length mismatch between values and validity")
	}
	arr.nullCount = arr.validity.ClearCount()
}

// Len returns the total number of elements in the array.
func (arr *Int64) Len() int { return len(arr.values) }

// Nulls returns the number of null elements in the array. The number of
// non-null elements can be calculated from Len() - Nulls().
func (arr *Int64) Nulls() int { return arr.nullCount }

// Get returns the value at index i. If the element at index i is null, Get
// returns an undefined value.
//
// Get panics if i is out of range.
func (arr *Int64) Get(i int) int64 {
	return arr.values[i]
}

// IsNull returns true if the element at index i is null.
func (arr *Int64) IsNull(i int) bool {
	if arr.nullCount == 0 {
		return false
	}
	return !arr.validity.Get(i)
}

// Values returns the underlying array of values.
func (arr *Int64) Values() []int64 { return arr.values }

// Validity returns the validity bitmap of the array. The returned bitmap
// may be of length 0 if there are no nulls.
//
// A value of 1 in the Validity bitmap indicates that the corresponding
// element at that position is valid (not null).
func (arr *Int64) Validity() memory.Bitmap { return arr.validity }

// Kind returns the kind of Array being represented.
func (arr *Int64) Kind() Kind { return KindInt64 }
