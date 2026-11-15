package jsonstring

import (
	"encoding/binary"
	"unsafe"
)

// ByteClass is a set of bytes, which has the control characters, as a table of the bytes and as the tables of
// the scan by SIMD.
type ByteClass struct {
	table  [256]bool
	tables nibbleTables
}

// NewByteClass is the class of the bytes and of the control characters.
func NewByteClass(bytes ...byte) *ByteClass {
	c := &ByteClass{}
	for _, b := range bytes {
		c.table[b] = true
	}
	for b := 0; b < 0x20; b++ {
		c.table[b] = true
	}
	c.tables = newNibbleTables(&c.table)
	return c
}

// Has is whether a byte of src is in the class.
func (c *ByteClass) Has(src []byte) bool {
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

// htmlOfCompact are the bytes which compact escapes for HTML in valid JSON, '<', '>' and '&', and the first byte
// of U+2028 and U+2029, as a table and as the tables of the loop of the escapes by SIMD, which stops at that byte
// since it has no sequence ( see escapeSequences ).
var (
	htmlOfCompact       = [256]bool{'<': true, '>': true, '&': true, 0xe2: true}
	htmlOfCompactTables = newNibbleTables(&htmlOfCompact)
)

// AppendHTMLEscaped appends src to dst with '<', '>', '&', U+2028 and U+2029 escaped, wherever they are, as
// encoding/json escapes them for HTML: as \u003c, \u003e, \u0026, \u2028 and \u2029.
func AppendHTMLEscaped(dst, src []byte) []byte {
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
