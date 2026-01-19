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

// An Array is a sequence of elements of the same data type.
type Array interface {
	// Len returns the total number of elements in the array.
	Len() int

	// Nulls returns the number of null elements in the array. The number of
	// non-null elements can be calculated from Len() - Nulls().
	Nulls() int

	// IsNull returns true if the element at index i is null.
	IsNull(i int) bool

	// Validity returns the validity bitmap of the array. The returned bitmap
	// may be of length 0 if there are no nulls.
	//
	// A value of 1 in the Validity bitmap indicates that the corresponding
	// element at that position is valid (not null).
	Validity() memory.Bitmap

	// Kind returns the kind of Array being represented.
	Kind() Kind
}
