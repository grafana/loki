// Package columnar provides utilities for working with columnar in-memory
// arrays.
//
// Columnar types are Arrow-compatible. The columnar package exists to provide a
// Loki-optimized APIs for columnar data that is based on [memory.Allocator] for
// memory management rather than arrow-go's reference counting.
//
// Package columnar is EXPERIMENTAL and currently only intended to be used by
// [github.com/grafana/loki/v3/pkg/dataobj].
package columnar

import "github.com/grafana/loki/v3/pkg/memory"

// A Datum is a piece of data which either represents an [Array] or a [Scalar].
type Datum interface {
	isDatum() // Type marker method.

	// Kind returns the Kind of value represented by the Datum.
	Kind() Kind
}

// An Array is a sequence of elements of the same data type.
type Array interface {
	Datum
	isArray() // Type marker method.

	// Len returns the total number of elements in the array.
	Len() int

	// Nulls returns the number of null elements in the array. The number of
	// non-null elements can be calculated from Len() - Nulls().
	Nulls() int

	// IsNull returns true if the element at index i is null.
	IsNull(i int) bool

	// Size returns the total consumed size of the array's buffers in bytes.
	//
	// It doesn't account for the size of the Array type itself, or any unused
	// bytes (such as padding past the length up to the capacity of buffers).
	Size() int

	// Validity returns the validity bitmap of the array. The returned bitmap
	// may be of length 0 if there are no nulls.
	//
	// A value of 1 in the Validity bitmap indicates that the corresponding
	// element at that position is valid (not null).
	Validity() memory.Bitmap
}

// A Scalar is a single element of a specific data type.
type Scalar interface {
	Datum
	isScalar() // Type marker method.

	// IsNull returns true if the scalar is a null value. IsNull can return true
	// for non-null data types.
	IsNull() bool
}

// A Builder assists with constructing arrays. Builder implementations typically
// have more methods.
type Builder interface {
	// AppendNull adds a new null element to the Builder.
	AppendNull()

	// AppendNulls appends the given number of null elements to the Builder.
	AppendNulls(count int)

	// Build returns the constructed Array. After calling Build, the builder is
	// reset to an initial state and can be reused.
	BuildArray() Array
}
