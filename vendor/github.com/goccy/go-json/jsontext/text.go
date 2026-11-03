package jsontext

import (
	"encoding/binary"
	"io"
	"math"
	"math/bits"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"github.com/goccy/go-json/internal/floatfmt"
	"github.com/goccy/go-json/internal/jsonnum"
	"github.com/goccy/go-json/internal/jsonstring"
)

// The scanners of the text of a token take the text from its first byte, and return the number of bytes which
// they took and an error. At an error, the number is the position of the error in the text: the character which
// is invalid, or, at the end of the text, the end of what is complete.

// isSpace reports whether c is JSON white space.
func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

// skipWS is skipSpace, which it calls only where b[i] is white space: it is inlined, so that a token without
// white space before it costs no call.
func skipWS(b []byte, i int) int {
	if i < len(b) && b[i] <= ' ' {
		return skipSpace(b, i)
	}
	return i
}

// skipSpace returns the position of the first byte of b from i which is not white space, or len(b). The
// indentation after a line feed is taken 8 spaces or 8 tabs at a time.
func skipSpace(b []byte, i int) int {
	for i < len(b) {
		c := b[i]
		if c > ' ' || !isSpace(c) {
			break
		}
		i++
		if c == '\n' && i < len(b) && (b[i] == ' ' || b[i] == '\t') {
			i = skipIndent(b, i)
		}
	}
	return i
}

// skipIndent returns the end of the run of the byte at b[i], a space or a tab, as the indentation of a line has
// it, looked at 8 bytes at a time.
func skipIndent(b []byte, i int) int {
	run := uint64(b[i]) * 0x0101010101010101
	for i+8 <= len(b) {
		// the bytes of the run are 0 in x, and the first one which is not is its lowest byte which is not.
		if x := binary.LittleEndian.Uint64(b[i:]) ^ run; x != 0 {
			return i + bits.TrailingZeros64(x)/8
		}
		i += 8
	}
	return i
}

// scanLiteral takes the literal lit ( null, true or false ) at the start of b.
func scanLiteral(b []byte, lit string) (int, error) {
	// the comparisons with constants, which are words, not calls
	switch {
	case len(b) >= 4 && (lit == "null" && string(b[:4]) == "null" || lit == "true" && string(b[:4]) == "true"):
		return 4, nil
	case len(b) >= 5 && lit == "false" && string(b[:5]) == "false":
		return 5, nil
	}
	for i := 0; i < len(lit); i++ {
		if i == len(b) {
			return i, io.ErrUnexpectedEOF
		}
		if b[i] != lit[i] {
			return i, invalidLiteralChar(b[i:], lit, i)
		}
	}
	return len(lit), nil
}

// literalOf is the literal of kind k.
func literalOf(k Kind) string {
	switch k {
	case KindNull:
		return "null"
	case KindTrue:
		return "true"
	}
	return "false"
}

// numState is the progress of a scan of a JSON number: what comes next, and the end of the longest complete
// number which was taken.
type numState struct {
	next numPart
	done int
}

type numPart uint8

const (
	numSign     numPart = iota // a minus sign or the first digit
	numFirst                   // the first digit
	numInt                     // the digits of the integer after the first one, which is not 0
	numAfterInt                // a fraction, an exponent or the end
	numDot                     // the first digit of the fraction
	numFrac                    // the digits of the fraction after the first one
	numAfterFrc                // an exponent or the end
	numE                       // the sign of the exponent or its first digit
	numExpFirst                // the first digit of the exponent
	numExp                     // the digits of the exponent after the first one
)

// scanNumber takes the JSON number at the start of b. At an error, the number is the position of the invalid
// character, or, if b ends before the number is complete, the end of the longest complete number which b
// starts with, with io.ErrUnexpectedEOF. A number which may continue after the end of b is returned without an
// error: a caller which may read more input continues it by scanNumberFrom.
func scanNumber(b []byte) (int, error) {
	n, st, err := scanNumberFrom(b, 0, numState{})
	if err == io.ErrUnexpectedEOF {
		n = st.done
	}
	return n, err
}

// inDigits reports whether the number is in a run of digits, which more digits continue without a change of state.
func (st numState) inDigits() bool {
	return st.next == numInt || st.next == numFrac || st.next == numExp
}

// scanNumberFrom continues the scan of the number which starts b at b[i:], where st was.
func scanNumberFrom(b []byte, i int, st numState) (int, numState, error) {
	// the digits which continue a run of digits to the end of b, as a slow reader gives them, change no state
	if st.inDigits() {
		if i = jsonnum.ScanDigits(b, i); i == len(b) {
			st.done = i
			return i, st, nil
		}
	}
	for {
		if i == len(b) {
			switch st.next {
			case numSign, numFirst, numDot, numE, numExpFirst:
				return i, st, io.ErrUnexpectedEOF
			}
			return i, st, nil
		}
		c := b[i]
		switch st.next {
		case numSign:
			st.next = numFirst
			if c == '-' {
				i++
			}
			continue
		case numFirst:
			if c == '0' {
				i++
				st.next = numAfterInt
			} else if '1' <= c && c <= '9' {
				i = jsonnum.ScanDigits(b, i+1)
				st.next = numInt
			} else {
				return i, st, invalidChar(b[i:], placeInNumber)
			}
			st.done = i
			continue
		case numInt:
			i = jsonnum.ScanDigits(b, i)
			st.done = i
			if i == len(b) {
				continue
			}
			st.next = numAfterInt
			continue
		case numAfterInt, numAfterFrc:
			if c == '.' && st.next == numAfterInt {
				st.next = numDot
			} else if c == 'e' || c == 'E' {
				st.next = numE
			} else {
				return i, st, nil
			}
			i++
			continue
		case numDot:
			if c < '0' || '9' < c {
				return i, st, invalidChar(b[i:], placeInNumber)
			}
			st.next = numFrac
			i++
			continue
		case numFrac:
			i = jsonnum.ScanDigits(b, i)
			st.done = i
			if i < len(b) {
				st.next = numAfterFrc
			}
			continue
		case numE:
			st.next = numExpFirst
			if c == '+' || c == '-' {
				i++
			}
			continue
		case numExpFirst:
			if c < '0' || '9' < c {
				return i, st, invalidChar(b[i:], placeInNumber)
			}
			st.next = numExp
			i++
			continue
		case numExp:
			i = jsonnum.ScanDigits(b, i)
			if i < len(b) {
				return i, st, nil
			}
			continue
		}
	}
}

// isInteger reports whether the JSON number b has no fraction and no exponent.
func isInteger(b []byte) bool {
	for _, c := range b {
		if c == '.' || c == 'e' || c == 'E' {
			return false
		}
	}
	return true
}

// strFlags describe a JSON string.
type strFlags uint8

const (
	strEscaped      strFlags = 1 << iota // it has an escape sequence
	strNonCanonical                      // AppendQuote writes its value otherwise
	strInvalidUTF8                       // it has invalid UTF-8, or an escaped surrogate which is not in a pair
	strHighCut                           // without strValid, a high surrogate ends the buffer, which a low one may pair
)

// stringClass is the class of each byte in a JSON string: 0 for a character taken as it is, 1 for an escape
// or a byte which ends the characters, 2 for the start of a character which is not ASCII.
var stringClass = func() [256]uint8 {
	var t [256]uint8
	for c := range t {
		switch {
		case c < ' ':
			t[c] = 1
		case c == '"', c == '\\':
			t[c] = 1
		case c >= utf8.RuneSelf:
			t[c] = 2
		}
	}
	return t
}()

// The masks of the bytes of a word of 8 bytes, which is looked at by arithmetic on all its bytes at once. A mask
// has the top bit of each byte which is found: the first one is exact, and the ones after it may be not, as a
// borrow may spread from a byte which is found to the next one. The words are loaded in little-endian order,
// so that the first byte is the lowest one on any machine.
const (
	lsb = 0x0101010101010101
	msb = 0x8080808080808080
)

// zeroBytes has the top bit of the bytes of w which are 0.
func zeroBytes(w uint64) uint64 {
	return (w - lsb) &^ w & msb
}

// specialBytes has the top bit of the bytes of w which a string doesn't take as they are: a quote, a backslash,
// a control character or a byte of 0x80 or more.
func specialBytes(w uint64) uint64 {
	// a byte below 0x20 borrows in w - 0x20 per byte; the top bits of w are the bytes of 0x80 or more.
	return zeroBytes(w^('"'*lsb)) | zeroBytes(w^('\\'*lsb)) | (w-' '*lsb)&^w&msb | w&msb
}

// plainEnd returns the position of the first byte of b from i which is not ASCII taken as it is in a string: a
// quote, a backslash, a control character or a byte of 0x80 or more; or len(b).
func plainEnd(b []byte, i int) int {
	for i+8 <= len(b) {
		if m := specialBytes(binary.LittleEndian.Uint64(b[i:])); m != 0 {
			return i + bits.TrailingZeros64(m)/8
		}
		i += 8
	}
	for i < len(b) && stringClass[b[i]] == 0 {
		i++
	}
	return i
}

// simpleStringEnd returns the end of the string which starts b, which starts with a quote, if it is short and its
// characters are ASCII to take as they are, and true; or else the position from which scanStringFrom continues,
// and false. It scans the characters 8 bytes at a time, as scanStringFrom does, which then takes the rest.
func simpleStringEnd(b []byte) (int, bool) {
	i := 1
	if i < len(b) && stringClass[b[i]] == 0 {
		i = plainEnd(b, 2)
	}
	if i < len(b) && b[i] == '"' {
		return i + 1, true
	}
	return i, false
}

// strMode is how a string is scanned.
type strMode uint8

const (
	strValid strMode = 1 << iota // invalid UTF-8 and escaped surrogates which are not in pairs are errors
	strMore                      // the input may continue after the end of the buffer
)

func strModeOf(validUTF8, more bool) strMode {
	var m strMode
	if validUTF8 {
		m |= strValid
	}
	if more {
		m |= strMore
	}
	return m
}

// scanString takes the JSON string at the start of b, which starts with a quote. With strValid, invalid UTF-8
// and escaped surrogates which are not in pairs are errors; else they are reported by strInvalidUTF8. If b
// ends before the string, the error is io.ErrUnexpectedEOF at the start of the character which is not
// complete, from which scanStringFrom continues when there is more input. A character which may be complete
// only with more input is taken as it is unless the mode has strMore.
func scanString(b []byte, mode strMode) (int, strFlags, error) {
	return scanStringFrom(b, 1, 0, mode)
}

// scanStringFrom continues the scan of the string which starts b at b[i:], with the flags of b[:i].
func scanStringFrom(b []byte, i int, flags strFlags, mode strMode) (int, strFlags, error) {
	if flags&strHighCut != 0 {
		var cut bool
		if i, flags, cut = pairHighCut(b, i, flags); cut {
			return i, flags, io.ErrUnexpectedEOF
		}
	}
	for {
		// the characters taken as they are: ASCII other than a quote, a backslash and a control character
		if i < len(b) && stringClass[b[i]] == 0 {
			i = plainEnd(b, i)
		}
		if i == len(b) {
			return i, flags, io.ErrUnexpectedEOF
		}
		switch c := b[i]; {
		case c == '"':
			return i + 1, flags, nil
		case c == '\\':
			// \uXXXX of a character which is not a surrogate, as most escape sequences are, without a call
			if i+6 <= len(b) && b[i+1] == 'u' {
				if r := hex4(b[i+2:]); r >= 0 && !utf16.IsSurrogate(r) {
					flags |= strEscaped
					if !isCanonicalEscape(b[i:i+6], r) {
						flags |= strNonCanonical
					}
					i += 6
					continue
				}
			}
			n, r, err := scanEscape(b[i:], mode)
			if err != nil {
				return i, flags, err
			}
			flags |= strEscaped
			if r == highCut {
				flags |= strHighCut | strNonCanonical
			} else if r < 0 {
				flags |= strInvalidUTF8 | strNonCanonical
			} else if !isCanonicalEscape(b[i:i+n], r) {
				flags |= strNonCanonical
			}
			i += n
		case c < ' ':
			return i, flags, invalidChar(b[i:], placeInString)
		default:
			// a run of characters which are not ASCII, and ASCII ones, validated at once, and a character at a
			// time only if it is not valid.
			end := charsEnd(b, i)
			if utf8.Valid(b[i:end]) {
				i = end
				continue
			}
			for i < end {
				r, n := utf8.DecodeRune(b[i:end])
				if r == utf8.RuneError && n == 1 {
					if end == len(b) && !utf8.FullRune(b[i:end]) {
						return i, flags, io.ErrUnexpectedEOF
					}
					if mode&strValid != 0 {
						return i, flags, errInvalidUTF8
					}
					flags |= strInvalidUTF8 | strNonCanonical
				}
				i += n
			}
		}
	}
}

// charsEnd returns the position of the first byte of b from i which is a quote, a backslash or a control
// character, or len(b): the characters before it are taken as they are, once they are valid UTF-8.
func charsEnd(b []byte, i int) int {
	for i+8 <= len(b) {
		w := binary.LittleEndian.Uint64(b[i:])
		if m := zeroBytes(w^('"'*lsb)) | zeroBytes(w^('\\'*lsb)) | (w-' '*lsb)&^w&msb; m != 0 {
			return i + bits.TrailingZeros64(m)/8
		}
		i += 8
	}
	for i < len(b) && (b[i] >= ' ' && b[i] != '"' && b[i] != '\\') {
		i++
	}
	return i
}

// isCanonicalEscape reports whether the escape sequence e of r is the one which AppendQuote writes.
func isCanonicalEscape(e []byte, r rune) bool {
	if r >= ' ' && r != '"' && r != '\\' {
		return false // written as it is
	}
	switch r {
	case '"', '\\', '\b', '\f', '\n', '\r', '\t':
		return len(e) == 2
	}
	return len(e) == 6 && e[4] == hexDigits[r>>4] && e[5] == hexDigits[r&0xf]
}

const hexDigits = "0123456789abcdef"

// highCut is the rune of scanEscape for a high surrogate which the end of the buffer cuts from the escape
// sequence which may pair it, without strValid: encoding/json/jsontext takes it alone, and so does the scan,
// which pairs it when it continues.
const highCut = -2

// pairHighCut takes the escape sequence at b[i:] after a high surrogate which was cut from it, as the flags tell:
// it pairs a low surrogate, else the high one is not in a pair. It reports whether b ends in it.
func pairHighCut(b []byte, i int, flags strFlags) (int, strFlags, bool) {
	if i+6 > len(b) && isPrefixOfLowSurrogate(b[i:]) {
		return i, flags, true
	}
	flags &^= strHighCut
	if i+6 <= len(b) && b[i] == '\\' && b[i+1] == 'u' {
		if r := hex4(b[i+2:]); r >= 0xdc00 && r < 0xe000 {
			return i + 6, flags, false
		}
	}
	return i, flags | strInvalidUTF8, false
}

// scanEscape takes the escape sequence at the start of b, and returns its length and the rune: -1 for an
// escaped surrogate which is not in a pair, which is an error with strValid, and highCut.
func scanEscape(b []byte, mode strMode) (int, rune, error) {
	if len(b) < 2 {
		return 0, 0, io.ErrUnexpectedEOF
	}
	switch b[1] {
	case '"', '\\', '/':
		return 2, rune(b[1]), nil
	case 'b':
		return 2, '\b', nil
	case 'f':
		return 2, '\f', nil
	case 'n':
		return 2, '\n', nil
	case 'r':
		return 2, '\r', nil
	case 't':
		return 2, '\t', nil
	case 'u':
		// a cut escape sequence waits for more input while its bytes may still be valid: scanHex4 reports the
		// error of a byte which is not a digit at once, as encoding/json/jsontext does.
		r, err := scanHex4(b)
		if err != nil {
			return 0, 0, err
		}
		if !utf16.IsSurrogate(r) {
			return 6, r, nil
		}
		// a surrogate pair is two escape sequences, which the error shows: the input waits for the second one
		// while it may be the escape sequence of a low surrogate, or, at the end of the input, it is cut.
		// Without strValid, a high surrogate is taken alone, as encoding/json/jsontext takes it.
		if len(b) < 12 && isPrefixOfLowSurrogate(b[6:]) {
			if mode&strValid != 0 {
				return 0, 0, io.ErrUnexpectedEOF
			}
			if r < 0xdc00 && mode&strMore != 0 {
				return 6, highCut, nil
			}
		}
		if r < 0xdc00 && len(b) >= 12 && b[6] == '\\' && b[7] == 'u' {
			if r2, err := scanHex4(b[6:]); err == nil {
				if pair := utf16.DecodeRune(r, r2); pair != utf8.RuneError {
					return 12, pair, nil
				}
			}
		}
		if mode&strValid != 0 {
			return 0, 0, invalidEscape(b[:min(len(b), 12)], "surrogate pair")
		}
		return 6, -1, nil
	}
	return 0, 0, invalidEscape(b[:2], "escape sequence")
}

// isPrefixOfLowSurrogate reports whether b may be the start of the escape sequence of a low surrogate, from
// \udc00 to \udfff.
func isPrefixOfLowSurrogate(b []byte) bool {
	for i, c := range b {
		switch {
		case i == 0 && c != '\\', i == 1 && c != 'u', i == 2 && c|0x20 != 'd':
			return false
		case i == 3 && (hexValue(c) < 0xc), i > 3 && hexValue(c) < 0:
			return false
		}
	}
	return true
}

// hex4 is the value of the 4 hexadecimal digits at the start of b, or -1 if one is not a digit: the digits are
// taken at once, as a byte which is not a digit is negative in the table, and so is their OR.
func hex4(b []byte) rune {
	d0, d1, d2, d3 := hexTable[b[0]], hexTable[b[1]], hexTable[b[2]], hexTable[b[3]]
	if d0|d1|d2|d3 < 0 {
		return -1
	}
	return rune(d0)<<12 | rune(d1)<<8 | rune(d2)<<4 | rune(d3)
}

// scanHex4 is the value of the escape sequence \uXXXX at the start of b.
func scanHex4(b []byte) (rune, error) {
	if len(b) >= 6 {
		if r := hex4(b[2:]); r >= 0 {
			return r, nil
		}
	}
	var r rune
	for i := 2; i < 6; i++ {
		if i == len(b) {
			return 0, io.ErrUnexpectedEOF
		}
		v := hexValue(b[i])
		if v < 0 {
			return 0, invalidEscape(b[:min(len(b), 6)], "escape sequence")
		}
		r = r<<4 | rune(v)
	}
	return r, nil
}

func hexValue(c byte) int {
	return int(hexTable[c])
}

// hexTable is the value of each byte as a hexadecimal digit, or -1.
var hexTable = func() [256]int8 {
	var t [256]int8
	for c := range t {
		switch {
		case '0' <= c && c <= '9':
			t[c] = int8(c - '0')
		case 'a' <= c && c <= 'f':
			t[c] = int8(c - 'a' + 10)
		case 'A' <= c && c <= 'F':
			t[c] = int8(c - 'A' + 10)
		default:
			t[c] = -1
		}
	}
	return t
}()

// appendUnquoted appends the value of the JSON string s, which scanString took with the flags f without an
// error: an escaped surrogate which is not in a pair and invalid UTF-8 are appended as U+FFFD. dst doesn't
// overlap s.
func appendUnquoted(dst, s []byte, f strFlags) []byte {
	v := s[1 : len(s)-1]
	start := len(dst)
	if f&strEscaped != 0 {
		dst = jsonstring.AppendUnescaped(dst, v)
	} else {
		dst = append(dst, v...)
	}
	if f&strInvalidUTF8 != 0 {
		// the value of an escape sequence is valid UTF-8, which the bytes before or after it don't make invalid
		dst = jsonstring.ReplaceInvalidUTF8(dst, start)
	}
	return dst
}

// escapeFlags are the characters which a quoted string escapes besides the ones which JSON requires.
type escapeFlags uint8

const (
	escapeHTML escapeFlags = 1 << iota // '<', '>' and '&'
	escapeJS                           // U+2028 and U+2029
)

func escapesOf(c *config) escapeFlags {
	var e escapeFlags
	if c.Has(escapeForHTML) {
		e |= escapeHTML
	}
	if c.Has(escapeForJS) {
		e |= escapeJS
	}
	return e
}

// appendQuoted appends s as a JSON string of the canonical form of RFC 8785, section 3.2.2.2, with the
// escapes of esc. Invalid UTF-8 is written as U+FFFD, and reported by false.
func appendQuoted(dst []byte, s string, esc escapeFlags) ([]byte, bool) {
	return jsonstring.AppendQuoted(jsonstring.TextEscaper(esc&escapeHTML != 0, esc&escapeJS != 0), dst, s)
}

// overlaps reports whether the bytes of a and the array of b, to its capacity, share memory.
func overlaps(a, b []byte) bool {
	if len(a) == 0 || cap(b) == 0 {
		return false
	}
	a0, b0 := uintptr(unsafe.Pointer(unsafe.SliceData(a))), uintptr(unsafe.Pointer(unsafe.SliceData(b)))
	return a0 < b0+uintptr(cap(b)) && b0 < a0+uintptr(len(a))
}

// readOnlyBytes is the bytes of s, a string or a []byte, without a copy: they must not be written to. A string
// and a slice both start with the pointer to their bytes, which has the length after it.
func readOnlyBytes[Bytes ~[]byte | ~string](s Bytes) []byte {
	return unsafe.Slice(*(**byte)(unsafe.Pointer(&s)), len(s))
}

// rawStringKept reports whether appendRawString appends the string s as it is.
func rawStringKept(s []byte, f strFlags, esc escapeFlags, preserve bool) bool {
	return (preserve || f&strNonCanonical == 0) && (esc == 0 || !needsExtraEscape(s, esc))
}

// appendRawString appends the JSON string s, which scanString took with the flags f, as the options want it:
// requoted unless it is canonical, or as it is with PreserveRawStrings, whose escapes are kept and whose invalid
// UTF-8 is kept as it is; the characters of esc are escaped in any case.
func appendRawString(dst, s []byte, f strFlags, esc escapeFlags, preserve bool) []byte {
	if rawStringKept(s, f, esc, preserve) {
		return append(dst, s...)
	}
	if !preserve {
		var buf [64]byte
		v := appendUnquoted(buf[:0], s, f)
		dst, _ = appendQuoted(dst, unsafe.String(unsafe.SliceData(v), len(v)), esc)
		return dst
	}
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\\':
			i += 2 // an escape sequence, whose second character is not one of esc
			continue
		case c == '<' || c == '>' || c == '&':
			if esc&escapeHTML != 0 {
				dst = append(dst, s[start:i]...)
				dst = appendEscapedHTML(dst, c)
				i++
				start = i
				continue
			}
		case c == 0xe2 && esc&escapeJS != 0 && i+2 < len(s) && s[i+1] == 0x80 && (s[i+2] == 0xa8 || s[i+2] == 0xa9):
			dst = append(dst, s[start:i]...)
			dst = append(dst, `\u202`...)
			dst = append(dst, hexDigits[s[i+2]-0xa0])
			i += 3
			start = i
			continue
		}
		i++
	}
	return append(dst, s[start:]...)
}

func appendEscapedHTML(dst []byte, c byte) []byte {
	return append(dst, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
}

// needsExtraEscape reports whether the string s has a character of esc which is not escaped.
func needsExtraEscape(s []byte, esc escapeFlags) bool {
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\':
			i++
		case c == '<' || c == '>' || c == '&':
			if esc&escapeHTML != 0 {
				return true
			}
		case c == 0xe2:
			if esc&escapeJS != 0 && i+2 < len(s) && s[i+1] == 0x80 && (s[i+2] == 0xa8 || s[i+2] == 0xa9) {
				return true
			}
		}
	}
	return false
}

// appendFloatText appends f as a JSON number of the given precision, as ECMA-262 formats numbers, except that
// -0 is -0: NaN and the infinities are appended as "NaN", "Infinity" and "-Infinity", without quotes.
func appendFloatText(dst []byte, f float64, bits int) []byte {
	switch {
	case math.IsNaN(f):
		return append(dst, "NaN"...)
	case math.IsInf(f, +1):
		return append(dst, "Infinity"...)
	case math.IsInf(f, -1):
		return append(dst, "-Infinity"...)
	}
	return floatfmt.AppendFloat(dst, f, bits)
}

// appendCanonicalNumber appends the JSON number b in the canonical form of RFC 8785: as a float64, saturated at
// the largest finite values, with -0 as 0.
func appendCanonicalNumber(dst, b []byte) []byte {
	f := parseFloat64(b)
	if math.IsInf(f, 0) {
		f = math.Copysign(math.MaxFloat64, f)
	}
	if f == 0 {
		f = 0
	}
	return floatfmt.AppendFloat(dst, f, 64)
}
