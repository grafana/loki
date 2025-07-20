// Package sliceclear provides a way to clear and truncate the length of a
// slice.
package sliceclear

// Clear zeroes out all values in s and returns s[:0]. Clear allows memory of
// previous elements in the slice to be reclained by the garbage collector
// while still allowing the underlying slice memory to be reused.
func Clear[Slice ~[]E, E any](s Slice) Slice {
	clear(s)
	return s[:0]
}
