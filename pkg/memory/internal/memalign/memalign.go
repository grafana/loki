// Package memalign provides utilities for aligning memory.
package memalign

// Align rounds up n to the next multiple of 64. 64-byte alignment is used to
// match modern CPU cache line sizes.
func Align(n int) int {
	return (n + 63) &^ 63
}

// Align64 is like [Align] but for uint64 values.
func Align64(n uint64) uint64 {
	return (n + 63) &^ 63
}
