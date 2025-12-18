package jsonlite

import (
	"fmt"
	"math/bits"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"
)

const (
	// UTF-16 surrogate pair boundaries (from Unicode standard)
	surrogateMin    = 0xD800 // Start of high surrogate range
	lowSurrogateMin = 0xDC00 // Start of low surrogate range
	lowSurrogateMax = 0xDFFF // End of low surrogate range
)

// Unquote removes quotes from a JSON string and processes escape sequences.
// Returns an error if the string is not properly quoted or contains invalid escapes.
// When the string contains no escape sequences, returns a zero-copy substring.
func Unquote(s string) (string, error) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", fmt.Errorf("invalid quoted string: %s", s)
	}
	s = s[1 : len(s)-1]
	// Fast path: check if string needs unescaping (has backslash or control chars)
	if !escaped(s) {
		return s, nil
	}
	b, err := unquote(make([]byte, 0, len(s)), s)
	return string(b), err
}

// AppendUnquote appends the unquoted string to the buffer.
// Returns an error if the string is not properly quoted or contains invalid escapes.
func AppendUnquote(b []byte, s string) ([]byte, error) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return b, fmt.Errorf("invalid quoted string: %s", s)
	}
	s = s[1 : len(s)-1]
	// Fast path: check if string needs unescaping
	if !escaped(s) {
		return append(b, s...), nil
	}
	return unquote(b, s)
}

func escaped(s string) bool {
	// SIMD-like scanning for backslash or control characters.
	// The bit tricks only work correctly when all bytes are < 0x80,
	// so we also check for high bytes and fall back to byte-by-byte.
	var i int
	if len(s) >= 8 {
		chunks := unsafe.Slice((*uint64)(unsafe.Pointer(unsafe.StringData(s))), len(s)/8)
		for _, n := range chunks {
			// Check for high bytes (>= 0x80), backslash, or control chars
			mask := n | below(n, 0x20) | contains(n, '\\')
			if (mask & msb) != 0 {
				return true
			}
		}
		i = len(chunks) * 8
	}

	for ; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == '\\' {
			return true
		}
	}

	return false
}

// unescapeIndex checks if the string content needs unescaping.
// Returns -1 if no unescaping needed, or the index of the first problematic byte.
// A string needs unescaping if it contains backslash or control characters (< 0x20).
func unescapeIndex(s string) int {
	// SIMD-like scanning for backslash or control characters.
	// The bit tricks only work correctly when all bytes are < 0x80,
	// so we also check for high bytes and fall back to byte-by-byte.
	var i int
	if len(s) >= 8 {
		chunks := unsafe.Slice((*uint64)(unsafe.Pointer(unsafe.StringData(s))), len(s)/8)
		for j, n := range chunks {
			// Check for high bytes (>= 0x80), backslash, or control chars
			mask := n | below(n, 0x20) | contains(n, '\\')
			if (mask & msb) != 0 {
				// Found something in this chunk - check byte at the position
				k := j*8 + bits.TrailingZeros64(mask&msb)/8
				c := s[k]
				switch {
				case c < 0x20, c == '\\':
					return k
				default:
					// High byte (>= 0x80) - scan rest of chunk byte by byte
					for k++; k < (j+1)*8; k++ {
						c := s[k]
						if c < 0x20 || c == '\\' {
							return k
						}
					}
				}
			}
		}
		i = len(chunks) * 8
	}

	for ; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == '\\' {
			return i
		}
	}

	return -1
}

// unquote processes escape sequences in content and appends to b.
// content should not include the surrounding quotes.
func unquote(b []byte, s string) ([]byte, error) {
	for len(s) > 0 {
		i := unescapeIndex(s)
		if i < 0 {
			return append(b, s...), nil
		}

		b = append(b, s[:i]...)
		c := s[i]
		if c < 0x20 {
			return b, fmt.Errorf("invalid control character in string")
		}
		if i+1 >= len(s) {
			return b, fmt.Errorf("invalid escape sequence at end of string")
		}

		switch c := s[i+1]; c {
		case '"', '\\', '/':
			b = append(b, c)
			s = s[i+2:]
		case 'b':
			b = append(b, '\b')
			s = s[i+2:]
		case 'f':
			b = append(b, '\f')
			s = s[i+2:]
		case 'n':
			b = append(b, '\n')
			s = s[i+2:]
		case 'r':
			b = append(b, '\r')
			s = s[i+2:]
		case 't':
			b = append(b, '\t')
			s = s[i+2:]
		case 'u':
			if i+6 > len(s) {
				return b, fmt.Errorf("invalid unicode escape sequence")
			}
			r1, ok := parseHex4(s[i+2 : i+6])
			if !ok {
				return b, fmt.Errorf("invalid unicode escape sequence")
			}

			// Check for UTF-16 surrogate pair
			if utf16.IsSurrogate(r1) {
				// Low surrogate without high surrogate is an error
				if r1 >= lowSurrogateMin {
					return b, fmt.Errorf("invalid surrogate pair: unexpected low surrogate")
				}
				// High surrogate, look for low surrogate
				if i+12 > len(s) || s[i+6] != '\\' || s[i+7] != 'u' {
					return b, fmt.Errorf("invalid surrogate pair: missing low surrogate")
				}
				r2, ok := parseHex4(s[i+8 : i+12])
				if !ok {
					return b, fmt.Errorf("invalid unicode escape sequence in surrogate pair")
				}
				if r2 < lowSurrogateMin || r2 > lowSurrogateMax {
					return b, fmt.Errorf("invalid surrogate pair: low surrogate out of range")
				}
				// Decode the surrogate pair
				b = utf8.AppendRune(b, utf16.DecodeRune(r1, r2))
				s = s[i+12:]
			} else {
				b = utf8.AppendRune(b, r1)
				s = s[i+6:]
			}
		default:
			return b, fmt.Errorf("invalid escape character: %q", c)
		}
	}
	return b, nil
}

// parseHex4 parses a 4-character hex string into a rune.
// Returns the rune and true on success, or 0 and false on failure.
func parseHex4(s string) (rune, bool) {
	if len(s) < 4 {
		return 0, false
	}
	var r rune
	for i := range 4 {
		r <<= 4
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			r |= rune(c - '0')
		case c >= 'a' && c <= 'f':
			r |= rune(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			r |= rune(c - 'A' + 10)
		default:
			return 0, false
		}
	}
	return r, true
}
