// Package strutil provides some miscellaneous string functions.
//
package strutil

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

var asciiSpace = [256]uint8{'\t': 1, '\n': 1, '\v': 1, '\f': 1, '\r': 1, ' ': 1}

// TrimSpace returns a slice of the string s, with all leading and
// trailing white space removed, as defined by Unicode. (It behaves
// the same as Go's strings.TrimSpace, but has a fast path for ASCII
// strings so is much faster for common cases.)
func TrimSpace(s string) string {
	// Fast path for ASCII: look for the first ASCII non-space byte
	start := 0
	for ; start < len(s); start++ {
		c := s[start]
		if c >= utf8.RuneSelf {
			// If we run into a non-ASCII byte, fall back to the
			// slower unicode-aware method on the remaining bytes
			return strings.TrimFunc(s[start:], unicode.IsSpace)
		}
		if asciiSpace[c] == 0 {
			break
		}
	}

	// Now look for the first ASCII non-space byte from the end
	stop := len(s)
	for ; stop > start; stop-- {
		c := s[stop-1]
		if c >= utf8.RuneSelf {
			return strings.TrimFunc(s[start:stop], unicode.IsSpace)
		}
		if asciiSpace[c] == 0 {
			break
		}
	}

	// At this point s[start:stop] starts and ends with an ASCII
	// non-space bytes, so we're done. Non-ASCII cases have already
	// been handled above.
	return s[start:stop]
}

// IsASCIISpace returns true if c represents an ASCII whitespace byte.
func IsASCIISpace(c byte) bool {
	return asciiSpace[c] != 0
}
