package columnar

import (
	"fmt"
	"reflect"

	"github.com/grafana/loki/v3/pkg/memory"
)

// Numeric is a constraint for recognized numeric types.
type Numeric interface{ int64 | uint64 }

// Number is an [Array] of 64-bit unsigned [Numeric] values.
type Number[T Numeric] struct {
	validity  memory.Bitmap // Empty when there's no nulls.
	values    []T
	nullCount int
	kind      Kind // Determined in [Number.init] based on T.
}

var _ Array = (*Number[int64])(nil)

// MakeNumber creates a new Number array from the given values and optional
// validity bitmap.
//
// Number arrays made from memory owned by a [memory.Allocator] are invalidated
// when the allocator reclaims memory.
//
// If validity is of length zero, all elements are considered valid. Otherwise,
// MakeNumber panics if the number of elements does not match the length of
// validity.
func MakeNumber[T Numeric](values []T, validity memory.Bitmap) *Number[T] {
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

// Kind returns the kind of Array being represented.
func (arr *Number[T]) Kind() Kind { return arr.kind }
