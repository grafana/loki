package arrowagg

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Arrays allows for aggregating a set of [arrow.Array]s together into a new,
// combined array.
type Arrays struct {
	mem memory.Allocator
	dt  arrow.DataType

	in    []arrow.Array
	nrows int // Total number of rows in the builder.
}

// NewArrays creates a new [Arrays] that aggregates a set of arrays of the same
// data type. The data type of incoming arrays is not checked until calling
// [Arrays.Flush].
func NewArrays(mem memory.Allocator, dt arrow.DataType) *Arrays {
	return &Arrays{mem: mem, dt: dt}
}

// Append appends the entirety of the given array to the builder. The data type
// of arr is not checked until calling [arrayBuilder.Flush].
func (a *Arrays) Append(arr arrow.Array) {
	arr.Retain()
	a.in = append(a.in, arr)
}

// AppendSlice appends a slice of the given array to the builder. The data type
// of arr is not checked until calling [arrayBuilder.Flush].
func (a *Arrays) AppendSlice(arr arrow.Array, i, j int64) {
	a.nrows += max(0, int(j-i))
	a.in = append(a.in, array.NewSlice(arr, i, j))
}

// AppendNulls appends n null values to the builer.
func (a *Arrays) AppendNulls(n int) {
	a.nrows += n
	a.in = append(a.in, array.MakeArrayOfNull(a.mem, a.dt, n))
}

// Len returns the total number of rows currently appended to the builder.
func (a *Arrays) Len() int { return a.nrows }

// Aggregate all appended arrays into a single array. The returned array must
// be Release'd after use. If no arrays have been appended, Aggregate returns a
// zero-length array.
//
// Aggregate returns an error if any of the appended arrays do not match the
// data type passed to [NewArrays].
//
// After calling Aggregate, a is reset and can be reused to append more arrays.
// This reset is done even if Aggregate returns an error.
func (a *Arrays) Aggregate() (arrow.Array, error) {
	if len(a.in) == 0 {
		return array.MakeArrayOfNull(a.mem, a.dt, 0), nil
	}

	defer a.Reset()
	return array.Concatenate(a.in, a.mem)
}

// Reset releases all arrays currently appended to a and resets it for reuse.
func (a *Arrays) Reset() {
	for _, arr := range a.in {
		arr.Release()
	}
	clear(a.in)
	a.in = a.in[:0]
	a.nrows = 0
}
