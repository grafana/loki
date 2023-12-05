package search

import (
	"bytes"
)

var (
	index func([]byte, []byte) int64 = func(haystack []byte, needle []byte) int64 { return int64(bytes.Index(haystack, needle)) }
)

// Index returns the first position the needle is in the haystack or -1 if
// needle was not found.
func Index(haystack []byte, needle []byte) int64 {
	return index(haystack, needle)
}

