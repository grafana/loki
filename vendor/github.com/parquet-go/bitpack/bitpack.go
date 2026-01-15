// Package bitpack implements efficient bit packing and unpacking routines for
// integers of various bit widths.
package bitpack

// Int is a type constraint representing the integer types that this package
// supports.
type Int interface {
	~int32 | ~uint32 | ~int64 | ~uint64 | ~int | ~uintptr
}

// ByteCount returns the number of bytes needed to hold the given bit count.
func ByteCount(bitCount uint) int {
	return int((bitCount + 7) / 8)
}
