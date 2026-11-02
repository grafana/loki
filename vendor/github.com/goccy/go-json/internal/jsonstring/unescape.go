package jsonstring

import (
	"bytes"
	"encoding/binary"
	"math/bits"
	"unicode/utf8"
	"unsafe"
)

func char(ptr unsafe.Pointer, offset int) byte {
	return *(*byte)(unsafe.Add(ptr, offset))
}

// HexToInt is the value of each hexadecimal digit, and 0 for any other byte.
var HexToInt = [256]int{
	'0': 0,
	'1': 1,
	'2': 2,
	'3': 3,
	'4': 4,
	'5': 5,
	'6': 6,
	'7': 7,
	'8': 8,
	'9': 9,
	'A': 10,
	'B': 11,
	'C': 12,
	'D': 13,
	'E': 14,
	'F': 15,
	'a': 10,
	'b': 11,
	'c': 12,
	'd': 13,
	'e': 14,
	'f': 15,
}

// UnescapeMap is the byte of each escape sequence of one character after its backslash.
var UnescapeMap = [256]byte{
	'"':  '"',
	'\\': '\\',
	'/':  '/',
	'\'': '\'', // which Unquote takes, as encoding/json does
	'b':  '\b',
	'f':  '\f',
	'n':  '\n',
	'r':  '\r',
	't':  '\t',
}

func unsafeAdd(ptr unsafe.Pointer, offset int) unsafe.Pointer {
	return unsafe.Add(ptr, offset)
}

// copyToBackslash copies the bytes from src up to the first backslash of the n bytes, or all of them, to dst, and
// returns how many it copied: bytes.IndexByte and copy go by vectors. It is not inlined, so that UnescapeTo keeps
// its registers for the short runs, which most are.
//
//go:noinline
func copyToBackslash(dst, src unsafe.Pointer, n int) int {
	rest := unsafe.Slice((*byte)(src), n)
	if i := bytes.IndexByte(rest, '\\'); i >= 0 {
		n = i
	}
	copy(unsafe.Slice((*byte)(dst), n), rest[:n])
	return n
}

// UnescapeTo decodes the escapes of the bytes of an escaped string into out, which has room for as many bytes and
// is not the bytes themselves, and returns the length of the result, which is at most the length of the bytes.
//
// The escapes are valid and complete, as a scan of the string checked them, and the array of buf has a byte
// after them, as the closing quote of the string: a pointer moves up to the end of buf. first is the offset of
// the first backslash.
func UnescapeTo(out unsafe.Pointer, buf []byte, first int) int {
	p := unsafe.Pointer(unsafe.SliceData(buf))
	end := unsafeAdd(p, len(buf))
	// the bytes before the first escape, by words: a short string is not worth a call of memmove.
	i := 0
	for ; i+8 <= first; i += 8 {
		*(*uint64)(unsafeAdd(out, i)) = *(*uint64)(unsafeAdd(p, i))
	}
	for ; i < first; i++ {
		*(*byte)(unsafeAdd(out, i)) = *(*byte)(unsafeAdd(p, i))
	}
	src := unsafeAdd(p, first)
	dst := unsafeAdd(out, first)
	for src != end {
		// The bytes up to the next backslash are copied eight at a time, from the words which are all in the
		// string. A word is written whole, and the output goes on to its backslash: out has the length of buf, and
		// is never ahead of the bytes read, so that a word written at dst ends before out does. A run longer than
		// two words is looked through by bytes.IndexByte and moved by copy, which go by vectors.
		for words := 0; *(*byte)(src) != '\\' && uintptr(src)+8 <= uintptr(end); words++ {
			if words == 2 {
				n := copyToBackslash(dst, src, int(uintptr(end)-uintptr(src)))
				src = unsafeAdd(src, n)
				dst = unsafeAdd(dst, n)
				break
			}
			w := binary.LittleEndian.Uint64((*[8]byte)(src)[:])
			binary.LittleEndian.PutUint64((*[8]byte)(dst)[:], w)
			if backslash := firstByteMask(w, '\\'); backslash != 0 {
				n := bits.TrailingZeros64(backslash) / 8
				src = unsafeAdd(src, n)
				dst = unsafeAdd(dst, n)
				break
			}
			src = unsafeAdd(src, 8)
			dst = unsafeAdd(dst, 8)
		}
		if src == end {
			break
		}
		c := char(src, 0)
		if c == '\\' {
			escapeChar := char(src, 1)
			if escapeChar != 'u' {
				*(*byte)(dst) = UnescapeMap[escapeChar]
				src = unsafeAdd(src, 2)
				dst = unsafeAdd(dst, 1)
			} else {
				v1 := HexToInt[char(src, 2)]
				v2 := HexToInt[char(src, 3)]
				v3 := HexToInt[char(src, 4)]
				v4 := HexToInt[char(src, 5)]
				code := rune((v1 << 12) | (v2 << 8) | (v3 << 4) | v4)
				if code >= 0xd800 && code < 0xdc00 && uintptr(end)-uintptr(src) >= 12 {
					if char(src, 6) == '\\' && char(src, 7) == 'u' {
						v1 := HexToInt[char(src, 8)]
						v2 := HexToInt[char(src, 9)]
						v3 := HexToInt[char(src, 10)]
						v4 := HexToInt[char(src, 11)]
						lo := rune((v1 << 12) | (v2 << 8) | (v3 << 4) | v4)
						if lo >= 0xdc00 && lo < 0xe000 {
							code = (code-0xd800)<<10 | (lo - 0xdc00) + 0x10000
							src = unsafeAdd(src, 6)
						}
					}
				}
				var b [utf8.UTFMax]byte
				n := utf8.EncodeRune(b[:], code)
				switch n {
				case 4:
					*(*byte)(unsafeAdd(dst, 3)) = b[3]
					fallthrough
				case 3:
					*(*byte)(unsafeAdd(dst, 2)) = b[2]
					fallthrough
				case 2:
					*(*byte)(unsafeAdd(dst, 1)) = b[1]
					fallthrough
				case 1:
					*(*byte)(unsafeAdd(dst, 0)) = b[0]
				}
				src = unsafeAdd(src, 6)
				dst = unsafeAdd(dst, n)
			}
		} else {
			*(*byte)(dst) = c
			src = unsafeAdd(src, 1)
			dst = unsafeAdd(dst, 1)
		}
	}
	return int(uintptr(dst) - uintptr(out))
}

// firstByteMask returns the word which has the top bit of the first byte of w which is c, if it has one, and maybe
// the top bits of bytes after it: a subtraction borrows from the next byte only at a byte which is c.
func firstByteMask(w, c uint64) uint64 {
	x := w ^ (c * lsb)
	return (x - lsb) &^ x & msb
}

// AppendUnescaped appends the value of the bytes s of a JSON string, between its quotes, to dst, which doesn't
// overlap s. The escapes are valid and complete, as a scan of the string checked them, and the closing quote
// follows s in its array, as UnescapeTo needs.
func AppendUnescaped(dst, s []byte) []byte {
	first := bytes.IndexByte(s, '\\')
	if first < 0 {
		return append(dst, s...)
	}
	if cap(dst)-len(dst) < len(s) {
		dst = append(dst[:cap(dst)], make([]byte, len(s)-(cap(dst)-len(dst)))...)[:len(dst)]
	}
	n := UnescapeTo(unsafe.Add(unsafe.Pointer(unsafe.SliceData(dst)), len(dst)), s, first)
	return dst[:len(dst)+n]
}

var runeErrBytes = []byte(string(utf8.RuneError))

// AppendValidUTF8 appends b to dst, with every byte of an invalid UTF-8 sequence replaced by utf8.RuneError, as
// encoding/json does.
func AppendValidUTF8(dst, b []byte) []byte {
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size == 1 {
			dst = append(dst, runeErrBytes...)
		} else {
			dst = append(dst, b[i:i+size]...)
		}
		i += size
	}
	return dst
}

// ReplaceInvalidUTF8 replaces every byte of an invalid UTF-8 sequence of b[start:] by utf8.RuneError.
func ReplaceInvalidUTF8(b []byte, start int) []byte {
	if utf8.Valid(b[start:]) {
		return b
	}
	return AppendValidUTF8(b[:start], bytes.Clone(b[start:]))
}

// Unquote returns the value of the JSON string lit, with its quotes, and true; or false if lit is not one. It
// takes lit as encoding/json takes the string of a ,string option and of an encoding.TextUnmarshaler, which isn't
// checked before: it takes the escape \' too. The value is lit between its quotes if it needs no change, else
// new bytes, with every byte of invalid UTF-8 replaced by utf8.RuneError.
func Unquote(lit []byte) ([]byte, bool) {
	if len(lit) < 2 || lit[0] != '"' || lit[len(lit)-1] != '"' {
		return nil, false
	}
	s := lit[1 : len(lit)-1]
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"' || c < ' ':
			return nil, false
		case c == '\\':
			if i+1 == len(s) {
				return nil, false
			}
			switch s[i+1] {
			case '"', '\\', '/', '\'', 'b', 'f', 'n', 'r', 't':
				i++
			case 'u':
				if i+6 > len(s) || !isHex(s[i+2]) || !isHex(s[i+3]) || !isHex(s[i+4]) || !isHex(s[i+5]) {
					return nil, false
				}
				i += 5
			default:
				return nil, false
			}
		}
	}
	return UnquoteValid(lit), true
}

// UnquoteValid is Unquote of lit, which a scan found a JSON string.
func UnquoteValid(lit []byte) []byte {
	s := lit[1 : len(lit)-1]
	first := bytes.IndexByte(s, '\\')
	if first < 0 {
		if utf8.Valid(s) {
			return s
		}
		return AppendValidUTF8(make([]byte, 0, len(s)+2*utf8.UTFMax), s)
	}
	// the closing quote of lit follows s, as UnescapeTo needs
	out := make([]byte, len(s))
	out = out[:UnescapeTo(unsafe.Pointer(unsafe.SliceData(out)), s, first)]
	return ReplaceInvalidUTF8(out, 0)
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}
