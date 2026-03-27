// Package unsafecast provides utilties for performing unsafe type casts.
package unsafecast

import "unsafe"

// Sizeof returns the size of T in bytes.
func Sizeof[T any]() uintptr {
	var zero T
	return unsafe.Sizeof(zero)
}

// Slice reinterprets a slice of one type as a slice of another type. Slice
// does not perform any type conversion or validation; it simply reinterprets
// the underlying memory.
//
// The length and capacity of the output slice are scaled according to the
// sizes of the From and To types.
func Slice[From, To any](in []From) []To {
	var (
		fromSize = int(Sizeof[From]())
		toSize   = int(Sizeof[To]())

		toLen = len(in) * fromSize / toSize
		toCap = cap(in) * fromSize / toSize
	)

	outPointer := (*To)(unsafe.Pointer(unsafe.SliceData(in)))
	return unsafe.Slice(outPointer, toCap)[:toLen]
}
