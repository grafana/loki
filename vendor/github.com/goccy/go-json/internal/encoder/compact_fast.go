package encoder

import (
	"bytes"
	"math/bits"
	"unsafe"
)

// The output of a marshaler is compacted and validated, as encoding/json does. Most of the outputs are compact
// already, so they are copied as they are after being checked: a scan by SIMD finds the bytes which would have
// to be rewritten or looked at carefully, and a walk of the structure without a write validates the rest.
// Anything else is left to compact, which rewrites and reports the errors.

// byteClass is a set of bytes, as a table of the bytes and as the tables of the scan by SIMD.
type byteClass struct {
	table  [256]bool
	tables nibbleTables
}

func newByteClass(bytes ...byte) *byteClass {
	c := &byteClass{}
	for _, b := range bytes {
		c.table[b] = true
	}
	for b := 0; b < 0x20; b++ {
		c.table[b] = true
	}
	for b := 0x80; b < 0x100; b++ {
		c.table[b] = true
	}
	c.tables = newNibbleTables(&c.table)
	return c
}

// has is whether a byte of src is in the class.
func (c *byteClass) has(src []byte) bool {
	n := len(src)
	if n == 0 {
		return false
	}
	p := unsafe.Pointer(unsafe.SliceData(src))
	if found, ok := scanBytesSIMD(p, n, &c.tables); ok {
		return found
	}
	for _, b := range src {
		if c.table[b] {
			return true
		}
	}
	return false
}

// rewrittenByCompact are the bytes, with the control characters and the bytes which are not ASCII, which
// compact rewrites or looks at: an escape, and the characters escaped for HTML if the option is set.
var rewrittenByCompact = [2]*byteClass{
	newByteClass('\\'),
	newByteClass('\\', '<', '>', '&'),
}

// maxDepthOfCompactCheck is the nesting up to which the output is checked here: a deeper one is compacted.
const maxDepthOfCompactCheck = 64

// appendCompactOutput appends src, the output of a marshaler, to dst if src is compact and valid JSON, and
// returns false without appending if it may not be: then src is to be compacted.
func appendCompactOutput(dst, src []byte, escape bool) ([]byte, bool) {
	if len(src) == 0 {
		return dst, false
	}
	class := rewrittenByCompact[0]
	if escape {
		class = rewrittenByCompact[1]
	}
	if class.has(src) {
		return dst, false
	}
	if !isCompactJSON(src) {
		return dst, false
	}
	return append(dst, src...), true
}

// isCompactJSON is whether src, which has no control character, no escape and no byte which is not ASCII,
// is valid JSON without a byte to remove.
func isCompactJSON(src []byte) bool {
	var stack [maxDepthOfCompactCheck]byte // the closing brackets of the values being read
	depth := 0
	n := len(src)
	i := 0
	for {
		// a value.
		if i >= n {
			return false
		}
		switch c := src[i]; {
		case c == '"':
			i = quoteEnd(src, i+1)
			if i < 0 {
				return false
			}
		case c == '{':
			if depth == maxDepthOfCompactCheck {
				return false
			}
			stack[depth] = '}'
			depth++
			i++
			if i < n && src[i] == '}' {
				i++
				depth--
				break
			}
			// a key.
			if i >= n || src[i] != '"' {
				return false
			}
			i = quoteEnd(src, i+1)
			if i < 0 {
				return false
			}
			if i >= n || src[i] != ':' {
				return false
			}
			i++
			continue
		case c == '[':
			if depth == maxDepthOfCompactCheck {
				return false
			}
			stack[depth] = ']'
			depth++
			i++
			if i < n && src[i] == ']' {
				i++
				depth--
				break
			}
			continue
		case c == 't':
			if !bytes.HasPrefix(src[i:], []byte("true")) {
				return false
			}
			i += 4
		case c == 'f':
			if !bytes.HasPrefix(src[i:], []byte("false")) {
				return false
			}
			i += 5
		case c == 'n':
			if !bytes.HasPrefix(src[i:], []byte("null")) {
				return false
			}
			i += 4
		case c == '-' || ('0' <= c && c <= '9'):
			j := numberEnd(src, i)
			if j < 0 {
				return false
			}
			i = j
		default:
			return false
		}
		// after a value.
		for {
			if depth == 0 {
				return i == n
			}
			if i >= n {
				return false
			}
			switch src[i] {
			case ',':
				i++
				if stack[depth-1] == '}' {
					// a key.
					if i >= n || src[i] != '"' {
						return false
					}
					i = quoteEnd(src, i+1)
					if i < 0 {
						return false
					}
					if i >= n || src[i] != ':' {
						return false
					}
					i++
				}
			case stack[depth-1]:
				depth--
				i++
				continue
			default:
				return false
			}
			break
		}
	}
}

// quoteEnd returns the index after the quote which ends the string whose content starts at i, or -1.
// The string has no escape, so the quote is the next one. Most of the strings are short, so the quote is
// looked for by words here, not by a call.
func quoteEnd(src []byte, i int) int {
	n := len(src)
	p := unsafe.Pointer(unsafe.SliceData(src))
	for ; i+8 <= n; i += 8 {
		w := *(*uint64)(unsafe.Add(p, i)) ^ (lsb * '"')
		if mask := (w - lsb) &^ w & msb; mask != 0 {
			return i + bits.TrailingZeros64(mask)/8 + 1
		}
	}
	for ; i < n; i++ {
		if src[i] == '"' {
			return i + 1
		}
	}
	return -1
}

// numberEnd returns the index after the number which starts at i, or -1 if it is not a number of JSON.
func numberEnd(src []byte, i int) int {
	n := len(src)
	if src[i] == '-' {
		i++
		if i >= n {
			return -1
		}
	}
	switch {
	case src[i] == '0':
		i++
	case '1' <= src[i] && src[i] <= '9':
		i++
		for i < n && '0' <= src[i] && src[i] <= '9' {
			i++
		}
	default:
		return -1
	}
	if i < n && src[i] == '.' {
		i++
		if i >= n || src[i] < '0' || src[i] > '9' {
			return -1
		}
		for i < n && '0' <= src[i] && src[i] <= '9' {
			i++
		}
	}
	if i < n && (src[i] == 'e' || src[i] == 'E') {
		i++
		if i < n && (src[i] == '+' || src[i] == '-') {
			i++
		}
		if i >= n || src[i] < '0' || src[i] > '9' {
			return -1
		}
		for i < n && '0' <= src[i] && src[i] <= '9' {
			i++
		}
	}
	return i
}
