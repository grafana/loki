package columnar

import (
	"github.com/grafana/loki/v3/pkg/memory"
)

// Bool is an [Array] of bit-packed boolean values.
type Bool struct {
	validity  memory.Bitmap // Empty when there's no nulls.
	values    memory.Bitmap
	nullCount int
}

var _ Array = (*Bool)(nil)

// MakeBool creates a new Bool array from the given values and optional validity
// bitmap.
//
// Bool arrays made from memory owned by a [memory.Allocator] are invalidated
// when the allocator reclaims memory.
//
// If validity is of length zero, all elements are considered valid. Otherwise,
// MakeBool panics if the number of elements does not match the length of
// validity.
func MakeBool(values, validity memory.Bitmap) *Bool {
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

func (arr *Bool) isDatum() {}
func (arr *Bool) isArray() {}
