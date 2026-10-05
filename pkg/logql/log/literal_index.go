package log

import "bytes"

// NewLiteralIndex returns a function that reports the index of the first
// instance of lit in a haystack, or -1. It searches the way the contains line
// filter does.
func NewLiteralIndex(lit []byte) func([]byte) int {
	return func(haystack []byte) int { return bytes.Index(haystack, lit) }
}
