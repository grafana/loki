// Package sliceclear defines a function to clear and zero out a slice.
package sliceclear

// Clear zeroes out all values in s and returns s[:0]. Clear allows memory of
// previous elements in the slice to be reclaimed by the garbage collector
// while still allowing the underlying slice memory to be reused.
func Clear[Slice ~[]E, E any](s Slice) Slice {
	clear(s)
	return s[:0]
}
