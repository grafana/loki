package parquet

import (
	"unsafe"

	"github.com/parquet-go/parquet-go/sparse"
)

// makeArrayVlaue constructs a sparse.Array from a slice of Value,
// using the offset to locate the field within the Value struct that
// the sparse array is indexing.
func makeArrayValue(values []Value, offset uintptr) sparse.Array {
	ptr := unsafe.Pointer(unsafe.SliceData(values))
	return sparse.UnsafeArray(unsafe.Add(ptr, offset), len(values), unsafe.Sizeof(Value{}))
}

func makeArray(base unsafe.Pointer, length int, offset uintptr) sparse.Array {
	return sparse.UnsafeArray(base, length, offset)
}

func makeArrayFromSlice[T any](s []T) sparse.Array {
	return makeArray(unsafe.Pointer(unsafe.SliceData(s)), len(s), unsafe.Sizeof(*new(T)))
}

func makeArrayFromPointer[T any](v *T) sparse.Array {
	return makeArray(unsafe.Pointer(v), 1, unsafe.Sizeof(*v))
}
