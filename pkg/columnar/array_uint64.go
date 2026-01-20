package columnar

import "github.com/grafana/loki/v3/pkg/memory"

// Uint64 is an [Array] of 64-bit unsigned integer values.
type Uint64 struct {
	validity  memory.Bitmap // Empty when there's no nulls.
	values    []uint64
	nullCount int
}

var _ Array = (*Uint64)(nil)

// MakeUint64 creates a new Uint64 array from the given values and optional validity
// bitmap.
//
// Uint64 arrays made from memory owned by a [memory.Allocator] are invalidated
// when the allocator reclaims memory.
//
// If validity is of length zero, all elements are considered valid. Otherwise,
// MakeUint64 panics if the number of elements does not match the length of
// validity.
func MakeUint64(values []uint64, validity memory.Bitmap) *Uint64 {
	arr := &Uint64{
		validity: validity,
		values:   values,
	}
	arr.init()
	return arr
}

//go:noinline
func (arr *Uint64) init() {
	if arr.validity.Len() > 0 && arr.validity.Len() != len(arr.values) {
		panic("length mismatch between values and validity")
	}
	arr.nullCount = arr.validity.ClearCount()
}

// Len returns the total number of elements in the array.
func (arr *Uint64) Len() int { return len(arr.values) }

// Nulls returns the number of null elements in the array. The number of
// non-null elements can be calculated from Len() - Nulls().
func (arr *Uint64) Nulls() int { return arr.nullCount }

// Get returns the value at index i. If the element at index i is null, Get
// returns an undefined value.
//
// Get panics if i is out of range.
func (arr *Uint64) Get(i int) uint64 {
	return arr.values[i]
}

// IsNull returns true if the element at index i is null.
func (arr *Uint64) IsNull(i int) bool {
	if arr.nullCount == 0 {
		return false
	}
	return !arr.validity.Get(i)
}

// Values returns the underlying array of values.
func (arr *Uint64) Values() []uint64 { return arr.values }

// Validity returns the validity bitmap of the array. The returned bitmap
// may be of length 0 if there are no nulls.
//
// A value of 1 in the Validity bitmap indicates that the corresponding
// element at that position is valid (not null).
func (arr *Uint64) Validity() memory.Bitmap { return arr.validity }

// Kind returns the kind of Array being represented.
func (arr *Uint64) Kind() Kind { return KindUint64 }
