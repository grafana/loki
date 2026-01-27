// Package matchutil provides optimized string matching utilities for the query engine.
package matchutil

import (
	"bytes"
	"unicode"
	"unicode/utf8"
)

// ContainsUpper checks if line contains substr using case-insensitive comparison.
// substr MUST already be uppercased by the caller.
//
// Implementation ported from pkg/logql/log/filter.go:containsLower
func ContainsUpper(line, substr []byte) bool {
	if len(substr) == 0 {
		return true
	}
	if len(substr) > len(line) {
		return false
	}

	substr = defensivelyMakeUpper(substr)

	// Fast path - try to find first byte of substr
	firstByte := substr[0]
	maxIndex := len(line) - len(substr)

	i := 0
	for i <= maxIndex {
		// Find potential first byte match
		c := line[i]
		// Fast path for ASCII - if c is lowercase letter, convert to uppercase
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		if c != firstByte {
			i++
			continue
		}

		// Found potential match, check rest of substr
		matched := true
		linePos := i
		substrPos := 0

		for linePos < len(line) && substrPos < len(substr) {
			c := line[linePos]
			s := substr[substrPos]

			// Fast path for ASCII
			if c < utf8.RuneSelf && s < utf8.RuneSelf {
				// Convert line char to uppercase if needed
				if c >= 'a' && c <= 'z' {
					c -= 'a' - 'A'
				}
				if c != s {
					matched = false
					break
				}
				linePos++
				substrPos++
				continue
			}

			// Slower Unicode path only when needed
			lr, lineSize := utf8.DecodeRune(line[linePos:])
			if lr == utf8.RuneError && lineSize == 1 {
				// Invalid UTF-8, treat as raw bytes
				// TODO: the uppercase check is used in 3 places, extract to a function.
				if c >= 'a' && c <= 'z' {
					c -= 'a' - 'A'
				}
				//TODO: next 6 lines are duplicated above, refactor?
				if c != s {
					matched = false
					break
				}
				linePos++
				substrPos++
				continue
			}

			mr, substrSize := utf8.DecodeRune(substr[substrPos:])
			if mr == utf8.RuneError && substrSize == 1 {
				// Invalid UTF-8 in pattern (shouldn't happen as substr should be valid)
				matched = false
				break
			}

			// Compare line rune converted to uppercase with pattern (which is already uppercase)
			if unicode.ToUpper(lr) != mr {
				matched = false
				break
			}

			linePos += lineSize
			substrPos += substrSize
		}

		if matched && substrPos == len(substr) {
			return true
		}
		i++
	}
	return false
}

// EqualUpper checks if line equals match using case-insensitive comparison.
// match MUST already be uppercased by the caller.
func EqualUpper(line, match []byte) bool {
	if len(line) != len(match) {
		return false
	}
	return ContainsUpper(line, match)
}

// ContainsUpper is currently only used by binary expressions created in
// the logical optimizer, which uses Go's regex parser. Go's regex
// parser upcases literals, as it relies on the "lowest" code point in
// the string's "fold cycle", which is the uppercase version, as A < a.
// ContainsUpper assumes that the match argument is already uppercased,
// and it should be because of the logical optimizer's use of Go's regex
// parser. However, just to be safe, we defensively check that the
// string is already uppercased (and for performance, assume if the
// first character is, the rest is too). This is defensive, to
// hopefully catch bugs if these functions are used outside of the regex
// simplification pass.
func defensivelyMakeUpper(s []byte) []byte {
	if len(s) == 0 {
		return s
	}

	if s[0] >= 'A' && s[0] <= 'Z' {
		return s
	}

	return bytes.ToUpper(s)
}
