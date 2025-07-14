// Package bitpack implements efficient bit packing and unpacking routines for
// integers of various bit widths.
package bitpack

// ByteCount returns the number of bytes needed to hold the given bit count.
func ByteCount(bitCount uint) int {
	return int((bitCount + 7) / 8)
}
