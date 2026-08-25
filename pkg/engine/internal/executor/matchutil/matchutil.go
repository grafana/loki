// Package matchutil provides optimized string matching utilities for the query engine.
package matchutil

import (
	"unicode"
	"unicode/utf8"
)

// toUpperASCII converts an ASCII lowercase letter to uppercase.
// If the byte is not a lowercase ASCII letter, it returns the byte unchanged.
func toUpperASCII(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - ('a' - 'A')
	}
	return c
}

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

	firstByte := substr[0]

	// ContainsUpper is currently only used for regex simplification.
	// Go's regex parser upcases literals when processing a
	// case-insensitive regex, as it relies on the "lowest" code point in
	// the string's "fold cycle", which is the uppercase version, as A < a.
	// ContainsUpper assumes that the match argument is already uppercased,
	// and it should be because of the logical optimizer's use of Go's
	// regex parser.
	if firstByte >= 'a' && firstByte <= 'z' {
		panic("substr argument to ContainsUpper must be uppercased")
	}

	maxIndex := len(line) - len(substr)
	i := 0

	// Fast path - try to find first byte of substr
	for i <= maxIndex {
		// Find potential first byte match
		c := line[i]
		// Fast path for ASCII - if c is lowercase letter, convert to uppercase
		c = toUpperASCII(c)
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
				c = toUpperASCII(c)
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
				c = toUpperASCII(c)
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
