// Package unsafecast exposes functions to bypass the Go type system and perform
// conversions between types that would otherwise not be possible.
//
// The functions of this package are mostly useful as optimizations to avoid
// memory copies when converting between compatible memory layouts; for example,
// casting a [][16]byte to a []byte in order to use functions of the standard
// bytes package on the slices.
//
//	With great power comes great responsibility.
package unsafecast

import "unsafe"

// The slice type represents the memory layout of slices in Go. It is similar to
// reflect.SliceHeader but uses a unsafe.Pointer instead of uintptr to for the
// backing array to allow the garbage collector to track track the reference.
type slice struct {
	ptr unsafe.Pointer
	len int
	cap int
}

// Slice converts the data slice of type []From to a slice of type []To sharing
// the same backing array. The length and capacity of the returned slice are
// scaled according to the size difference between the source and destination
// types.
//
// Note that the function does not perform any checks to ensure that the memory
// layouts of the types are compatible, it is possible to cause memory
// corruption if the layouts mismatch (e.g. the pointers in the From are
// different than the pointers in To).
func Slice[To, From any](data []From) []To {
	// This function could use unsafe.Slice but it would drop the capacity
	// information, so instead we implement the type conversion.
	var zf From
	var zt To
	var s = slice{
		ptr: unsafe.Pointer(unsafe.SliceData(data)),
		len: int((uintptr(len(data)) * unsafe.Sizeof(zf)) / unsafe.Sizeof(zt)),
		cap: int((uintptr(cap(data)) * unsafe.Sizeof(zf)) / unsafe.Sizeof(zt)),
	}
	return *(*[]To)(unsafe.Pointer(&s))
}

// String converts a byte slice to a string value. The returned string shares
// the backing array of the byte slice.
//
// Programs using this function are responsible for ensuring that the data slice
// is not modified while the returned string is in use, otherwise the guarantee
// of immutability of Go string values will be violated, resulting in undefined
// behavior.
func String(data []byte) string {
	return unsafe.String(unsafe.SliceData(data), len(data))
}
