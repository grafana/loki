package encoder

import (
	"bytes"
	"encoding/binary"
	"math/bits"
	"unsafe"
)

// The output of a marshaler is compacted and validated, as encoding/json does. Most of the outputs are compact
// already, so they are copied as they are after being checked: a scan by SIMD finds the bytes which would have
// to be rewritten or looked at, and a walk of the structure without a write validates the rest, the escapes of
// the strings included. The characters which compact escapes for HTML are escaped as the output is copied.
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

// controlChars are the control characters, which compact removes as white space or reports as errors: an output
// with one is left to compact. lookedAtByCompact are those and the bytes which the check must look at: an
// escape, whose sequence is validated, and, if HTML is escaped, the characters which compact escapes then:
// '<', '>', '&' and the first byte of U+2028 and U+2029, which starts other characters as well. Most of the
// outputs have none of them: their strings are walked to the next quote at once and they are copied as they are.
var (
	controlChars      = newByteClass()
	lookedAtByCompact = [2]*byteClass{
		newByteClass('\\'),
		newByteClass('\\', '<', '>', '&', 0xe2),
	}
)

// The walks of the strings of an output, as the type argument of isCompactJSON: each walk is compiled into a
// function of its own, so that the walk of the plain strings, which most of the outputs have, has neither a
// branch nor a call for the escapes in its loop. The types differ by their sizes, which the compiler knows for
// each of them: the branch on the size is resolved when the function of a walk is compiled.
type (
	// plainStrings have no escape: a string ends at the next quote.
	plainStrings struct{}
	// escapedStrings may have escapes, which are validated.
	escapedStrings struct{ _ byte }
)

// maxDepthOfCompactCheck is the nesting up to which the output is checked here: a deeper one is compacted.
const maxDepthOfCompactCheck = 64

// appendCompactOutput appends src, the output of a marshaler, to dst as compact appends it, if src is compact
// and valid JSON, and returns false without appending if it may not be: then src is to be compacted. The output
// of a trusted marshaler, valid and compact, is appended as it is if compact leaves it so.
func appendCompactOutput(dst, src []byte, escape, trusted bool) ([]byte, bool) {
	if len(src) == 0 {
		return dst, false
	}
	option := 0
	if escape {
		option = 1
	}
	if !lookedAtByCompact[option].has(src) {
		if !trusted && !isCompactJSON[plainStrings](src) {
			return dst, false
		}
		return append(dst, src...), true
	}
	if controlChars.has(src) || !isCompactJSON[escapedStrings](src) {
		return dst, false
	}
	if escape {
		return appendHTMLEscaped(dst, src), true
	}
	return append(dst, src...), true
}

// htmlOfCompact are the bytes which compact escapes for HTML in valid JSON, '<', '>' and '&', and the first byte
// of U+2028 and U+2029, as a table and as the tables of the loop of the escapes by SIMD, which stops at that byte
// since it has no sequence ( see escapeSequences ).
var (
	htmlOfCompact       = [256]bool{'<': true, '>': true, '&': true, 0xe2: true}
	htmlOfCompactTables = newNibbleTables(&htmlOfCompact)
)

// appendHTMLEscaped appends src, valid JSON, to dst with the characters which compact escapes for HTML escaped as
// it escapes them.
func appendHTMLEscaped(dst, src []byte) []byte {
	s := unsafe.String(unsafe.SliceData(src), len(src))
	n := len(s)
	i, j := 0, 0
	// limit is where the loop of the bytes gives the rest back to the loop by SIMD, which stops at the first byte
	// of U+2028 and U+2029, as appendNormalizedString does.
	limit := n
	if n >= 32 && hasEscapeLoop {
		dst, j = appendEscapedSIMD(dst, s, &htmlOfCompactTables, false)
		i = j
		limit = min(n, j+32)
	}
	for {
		for j < limit {
			c := s[j]
			if !htmlOfCompact[c] {
				j++
				continue
			}
			if c == 0xe2 {
				// U+2028 and U+2029 are E2 80 A8 and E2 80 A9.
				if j+2 < n && s[j+1] == 0x80 && s[j+2]&^1 == 0xa8 {
					dst = append(dst, s[i:j]...)
					dst = append(dst, `\u202`...)
					dst = append(dst, hex[s[j+2]&0xf])
					j += 3
					i = j
					continue
				}
				j++
				continue
			}
			dst = append(dst, s[i:j]...)
			seq := escapeSequences[c]
			l := len(dst)
			dst = binary.LittleEndian.AppendUint64(dst, seq)[:l+int(seq>>56)]
			j++
			i = j
		}
		if j >= n {
			break
		}
		dst = append(dst, s[i:j]...)
		var consumed int
		dst, consumed = appendEscapedSIMD(dst, s[j:], &htmlOfCompactTables, false)
		j += consumed
		i = j
		limit = min(n, j+32)
	}
	return append(dst, s[i:]...)
}

// isCompactJSON is whether src, which has no control character, is valid JSON without a byte to remove, whose
// strings are walked as S tells.
func isCompactJSON[S plainStrings | escapedStrings](src []byte) bool {
	var walk S
	plain := unsafe.Sizeof(walk) == unsafe.Sizeof(plainStrings{})
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
			if plain {
				i = quoteEnd(src, i+1)
			} else {
				i = escapedQuoteEnd(src, i+1)
			}
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
			if plain {
				i = quoteEnd(src, i+1)
			} else {
				i = escapedQuoteEnd(src, i+1)
			}
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
					if plain {
						i = quoteEnd(src, i+1)
					} else {
						i = escapedQuoteEnd(src, i+1)
					}
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
// looked for by words here, not by a call. The words are read as little-endian ones, so that the lowest byte
// found by bits.TrailingZeros64 is the first one in memory on a big-endian machine too; on a little-endian one
// the read is a load as it is.
func quoteEnd(src []byte, i int) int {
	n := len(src)
	p := unsafe.Pointer(unsafe.SliceData(src))
	for ; i+8 <= n; i += 8 {
		w := binary.LittleEndian.Uint64((*[8]byte)(unsafe.Add(p, i))[:]) ^ (lsb * '"')
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

// escapedQuoteEnd is quoteEnd for a string which may have escapes, which are validated.
func escapedQuoteEnd(src []byte, i int) int {
	n := len(src)
	p := unsafe.Pointer(unsafe.SliceData(src))
	for {
		for ; i+8 <= n; i += 8 {
			w := binary.LittleEndian.Uint64((*[8]byte)(unsafe.Add(p, i))[:])
			q := w ^ (lsb * '"')
			b := w ^ (lsb * '\\')
			// the lowest bit of the mask of each byte is exact, so the lowest one of both is.
			if mask := ((q-lsb)&^q | (b-lsb)&^b) & msb; mask != 0 {
				i += bits.TrailingZeros64(mask) / 8
				break
			}
		}
		for ; i < n && src[i] != '"' && src[i] != '\\'; i++ {
		}
		if i >= n {
			return -1
		}
		if src[i] == '"' {
			return i + 1
		}
		i = escapeEnd(src, i+1)
		if i < 0 {
			return -1
		}
	}
}

// escapeEnd returns the index after the escape of a string whose backslash is before i, or -1 if it is not an
// escape of JSON.
func escapeEnd(src []byte, i int) int {
	if i >= len(src) {
		return -1
	}
	switch src[i] {
	case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		return i + 1
	case 'u':
		if i+5 > len(src) {
			return -1
		}
		for _, c := range src[i+1 : i+5] {
			if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
				return -1
			}
		}
		return i + 5
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
