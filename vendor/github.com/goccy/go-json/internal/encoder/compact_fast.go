package encoder

import (
	"bytes"
	"encoding/binary"
	"math/bits"
	"unsafe"

	"github.com/goccy/go-json/internal/jsonstring"
)

// The output of a marshaler is compacted and validated, as encoding/json does. Most of the outputs are compact
// already, so they are copied as they are after being checked: a scan by SIMD finds the bytes which would have
// to be rewritten or looked at, and a walk of the structure without a write validates the rest, the escapes of
// the strings included. The characters which compact escapes for HTML are escaped as the output is copied.
// Anything else is left to compact, which rewrites and reports the errors.

// controlChars are the control characters, which compact removes as white space or reports as errors: an output
// with one is left to compact. lookedAtByCompact are those and the bytes which the check must look at: an
// escape, whose sequence is validated, and, if HTML is escaped, the characters which compact escapes then:
// '<', '>', '&' and the first byte of U+2028 and U+2029, which starts other characters as well. Most of the
// outputs have none of them: their strings are walked to the next quote at once and they are copied as they are.
var (
	controlChars      = jsonstring.NewByteClass()
	lookedAtByCompact = [2]*jsonstring.ByteClass{
		jsonstring.NewByteClass('\\'),
		jsonstring.NewByteClass('\\', '<', '>', '&', 0xe2),
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

// maxDepthOfCompactScan is the nesting up to which isCompactJSON scans the output: a deeper one is compacted.
const maxDepthOfCompactScan = 64

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
	if !lookedAtByCompact[option].Has(src) {
		if !trusted && !isCompactJSON[plainStrings](src) {
			return dst, false
		}
		return append(dst, src...), true
	}
	if controlChars.Has(src) || !isCompactJSON[escapedStrings](src) {
		return dst, false
	}
	if escape {
		return jsonstring.AppendHTMLEscaped(dst, src), true
	}
	return append(dst, src...), true
}

// isCompactJSON is whether src, which has no control character, is valid JSON without a byte to remove, whose
// strings are walked as S tells.
func isCompactJSON[S plainStrings | escapedStrings](src []byte) bool {
	var walk S
	plain := unsafe.Sizeof(walk) == unsafe.Sizeof(plainStrings{})
	var stack [maxDepthOfCompactScan]byte // the closing brackets of the values being read
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
			if depth == maxDepthOfCompactScan {
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
			if depth == maxDepthOfCompactScan {
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
