package encoder

import (
	"bytes"
	"encoding/binary"
	"math/bits"
	"unicode/utf8"
	"unsafe"

	"github.com/goccy/go-json/internal/jsonstring"
)

// The raw value which a method or a function of the v2 semantics returns is formatted as the options want it,
// which checks it as well: valid JSON, valid UTF-8 and no duplicate names. Most of the values are compact
// already, which are checked here by a walk, as the outputs of the marshalers of v1 are ( see
// appendCompactOutput ), and appended as they are, but for the strings with escapes, which are written again in
// the form of the formatting: the escapes which it doesn't write are written as the characters they are.

// escapedInRaw are the characters which the escaping of the options escapes, in a raw value which is appended
// by the formatting otherwise ( see AppendFormattedRaw ): U+2028 and U+2029 ( EscapeForJS ), whose first byte is
// 0xe2, and '<', '>' and '&' ( EscapeForHTML ). The control characters, which are in each class, are not
// appended here anyway.
var escapedInRaw = [4]*jsonstring.ByteClass{
	nil,
	jsonstring.NewByteClass(0xe2),
	jsonstring.NewByteClass('<', '>', '&'),
	jsonstring.NewByteClass('<', '>', '&', 0xe2),
}

// notPlainRaw are the bytes which aren't ASCII and the control characters, which the strings of a raw value are
// scanned for if it has them ( see rawWalk.stringEnd ), and notPureRaw those and the escapes, which they are
// scanned for if it has them: most values have none, and their strings end at the next quote.
var (
	notPlainRaw = jsonstring.NewByteClass(highBytes()...)
	notPureRaw  = jsonstring.NewByteClass(append(highBytes(), '\\')...)
)

func highBytes() []byte {
	b := make([]byte, 0, 129)
	for c := 0x80; c <= 0xff; c++ {
		b = append(b, byte(c))
	}
	return b
}

// maxNamesOfRawWalk is the number of the names of the objects being read, and maxDepthOfRawWalk the depth of the
// values, up to which a raw value is appended here: a larger one is formatted.
const (
	maxNamesOfRawWalk = 64
	maxDepthOfRawWalk = 64
)

// rawLevels are the levels of the walk of a raw value ( see rawWalk ), which the runtime context keeps, so that
// they are not cleared for every value: the walk reads only what it wrote. stack are the closing brackets of the
// values being read; names are the offsets of the names of the objects being read, and first the index in names
// of the first name of each level.
type rawLevels struct {
	stack [maxDepthOfRawWalk]byte
	first [maxDepthOfRawWalk]int32
	names [maxNamesOfRawWalk]int32
}

// AppendFormattedRaw appends src, a raw value, to dst as the formatting of the v2 semantics with the escaping of
// ctx appends it, and reports true, if src is compact, valid JSON whose strings are valid UTF-8 without a control
// character or an escape of a surrogate, whose names have no escape, and whose objects have no duplicate names.
// Otherwise it reports false, and src is to be formatted: dst may have been written after its length then.
//
// It reads src once, and once more for the escaping of the options, if any, and for UTF-8, if src has a byte
// which is not ASCII.
func AppendFormattedRaw(ctx *RuntimeContext, dst, src []byte) ([]byte, bool) {
	flags := ctx.Option.Flag
	escape := 0
	if flags&NormalizeUTF8Option != 0 {
		escape |= 1
	}
	if flags&HTMLEscapeOption != 0 {
		escape |= 2
	}
	if len(src) == 0 || (escape != 0 && escapedInRaw[escape].Has(src)) {
		return dst, false
	}
	// a value of ASCII without a control character and an escape, as most are, is found by SIMD: its strings end
	// at the next quote, and it is appended as it is. The strings of the other values of ASCII without a control
	// character are scanned for the quotes and the escapes only.
	if !notPureRaw.Has(src) {
		if !walkRaw[plainStrings](nil, src, ctx.levelsOfRaw()) {
			return dst, false
		}
		return append(dst, src...), true
	}
	w := rawWalk{ctx: ctx, src: src, dst: dst, ascii: true, escape: escape, plain: !notPlainRaw.Has(src)}
	if !walkRaw[escapedStrings](&w, src, ctx.levelsOfRaw()) || !(w.ascii || utf8.Valid(src)) {
		return dst, false
	}
	return append(w.dst, src[w.copied:]...), true
}

// rawWalk is the state of the walk of AppendFormattedRaw over src: dst is the output, which has src up to copied,
// with the strings of escapes before it written again.
type rawWalk struct {
	ctx    *RuntimeContext
	src    []byte
	dst    []byte
	copied int
	// ascii is whether every byte read is ASCII.
	ascii bool
	// escape is the escaping of the options ( see escapedInRaw ), and plain is whether src has only ASCII
	// without a control character.
	escape int
	plain  bool
}

// walkRaw reports whether src is a value which AppendFormattedRaw appends, whose levels are read in lv. The
// strings are walked as S tells: by w, which writes the strings of escapes again, or, for plainStrings, to the
// next quote, without w, which is nil, for src of ASCII without a control character and an escape.
//
// The walk goes from a value to what follows it by the labels: a name of an object is read only where one is next,
// without a branch for it at every value.
func walkRaw[S plainStrings | escapedStrings](w *rawWalk, src []byte, lv *rawLevels) bool {
	var walk S
	pure := unsafe.Sizeof(walk) == unsafe.Sizeof(plainStrings{})
	n := len(src)
	// the state of the levels, in registers: first is the index of the first name of the innermost level, whose
	// value lv.first keeps for the level around it.
	depth, first, numNames := 0, int32(0), int32(0)
	i := 0
value:
	if i >= n {
		return false
	}
	switch c := src[i]; {
	case c == '"':
		if pure {
			if i = quoteEnd(src, i+1); i < 0 {
				return false
			}
			break
		}
		end, escape := w.stringEnd(i + 1)
		if end < 0 {
			return false
		}
		if escape != 0 && !w.requote(i, end, escape) {
			return false
		}
		i = end
	case c == '{':
		if depth == maxDepthOfRawWalk {
			return false
		}
		lv.stack[depth] = '}'
		lv.first[depth], first = first, numNames
		depth++
		i++
		if i < n && src[i] == '}' {
			i++
			depth--
			first = lv.first[depth]
			break
		}
		goto name
	case c == '[':
		if depth == maxDepthOfRawWalk {
			return false
		}
		lv.stack[depth] = ']'
		lv.first[depth], first = first, numNames
		depth++
		i++
		if i < n && src[i] == ']' {
			i++
			depth--
			first = lv.first[depth]
			break
		}
		goto value
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
		if i = numberEnd(src, i); i < 0 {
			return false
		}
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
			if lv.stack[depth-1] == '}' {
				goto name
			}
			goto value
		case lv.stack[depth-1]:
			depth--
			numNames, first = first, lv.first[depth]
			i++
		default:
			return false
		}
	}
name:
	// a name, which must be new in the object, before its value.
	if i >= n || src[i] != '"' || numNames == maxNamesOfRawWalk {
		return false
	}
	{
		var end, escape int
		if pure {
			end = quoteEnd(src, i+1)
		} else {
			end, escape = w.stringEnd(i + 1)
		}
		if end < 0 || escape != 0 {
			return false
		}
		size := end - i
		for _, start := range lv.names[first:numNames] {
			// the name with its quotes, which has no escape: the quote ends the one before as well. The first
			// characters, which differ for most names, are compared without a call.
			if s := int(start); s+size < end && src[s+1] == src[i+1] && src[s+size-1] == '"' && bytes.Equal(src[s:s+size], src[i:end]) {
				return false
			}
		}
		lv.names[numNames] = int32(i)
		numNames++
		if end >= n || src[end] != ':' {
			return false
		}
		i = end + 1
	}
	goto value
}

// stringEnd returns the index after the quote which ends the string whose content starts at i, and whether the
// string has an escape; or -1 if the string has a control character or an invalid escape, or an escape of a
// surrogate, which the formatting reports or writes as it is, or doesn't end. It clears ascii if the string has a
// byte which is not ASCII. The bytes are looked at by words: the lowest byte of the mask of each kind is exact.
func (w *rawWalk) stringEnd(i int) (int, int) {
	if w.plain {
		return w.plainStringEnd(i)
	}
	src := w.src
	n := len(src)
	p := unsafe.Pointer(unsafe.SliceData(src))
	escape := 0 // the index of the first escape
	for {
		for ; i+8 <= n; i += 8 {
			x := binary.LittleEndian.Uint64((*[8]byte)(unsafe.Add(p, i))[:])
			q := x ^ (lsb * '"')
			b := x ^ (lsb * '\\')
			mask := ((q-lsb)&^q | (b-lsb)&^b | (x-lsb*0x20)&^x) & msb
			if w.ascii {
				mask |= x & msb
			}
			if mask != 0 {
				i += bits.TrailingZeros64(mask) / 8
				break
			}
		}
		for ; i < n; i++ {
			if c := src[i]; c == '"' || c == '\\' || c < 0x20 || (c >= 0x80 && w.ascii) {
				break
			}
		}
		if i >= n {
			return -1, 0
		}
		switch c := src[i]; {
		case c == '"':
			return i + 1, escape
		case c == '\\':
			if i+1 < n && src[i+1] == 'u' && i+3 < n && (src[i+2]|0x20) == 'd' && src[i+3] >= '8' {
				// \uD800 to \uDFFF: a surrogate, which the formatting checks with the one after it.
				return -1, 0
			}
			if escape == 0 {
				escape = i
			}
			if i = escapeEnd(src, i+1); i < 0 {
				return -1, 0
			}
		case c >= 0x80:
			w.ascii = false
			i++
		default:
			return -1, 0
		}
	}
}

// plainStringEnd is stringEnd for src of ASCII without a control character: the words are looked at for the quote
// and the escape only.
func (w *rawWalk) plainStringEnd(i int) (int, int) {
	src := w.src
	n := len(src)
	p := unsafe.Pointer(unsafe.SliceData(src))
	escape := 0 // the index of the first escape
	for {
		for ; i+8 <= n; i += 8 {
			x := binary.LittleEndian.Uint64((*[8]byte)(unsafe.Add(p, i))[:])
			q := x ^ (lsb * '"')
			b := x ^ (lsb * '\\')
			if mask := ((q-lsb)&^q | (b-lsb)&^b) & msb; mask != 0 {
				i += bits.TrailingZeros64(mask) / 8
				break
			}
		}
		for ; i < n && src[i] != '"' && src[i] != '\\'; i++ {
		}
		if i >= n {
			return -1, 0
		}
		if src[i] == '"' {
			return i + 1, escape
		}
		if i+1 < n && src[i+1] == 'u' && i+3 < n && (src[i+2]|0x20) == 'd' && src[i+3] >= '8' {
			// \uD800 to \uDFFF: a surrogate, which the formatting checks with the one after it.
			return -1, 0
		}
		if escape == 0 {
			escape = i
		}
		if i = escapeEnd(src, i+1); i < 0 {
			return -1, 0
		}
	}
}

// requote writes the string src[start:end], which has escapes, as the formatting writes it: an escaped character
// which needs no escape as it is, and the others by the escapes of the escapers of the strings, in one pass. With
// the escaping of the options, the string is written by the escaper of the options.
//
// first is the index of the first escape of the string, before which it is copied as it is.
func (w *rawWalk) requote(start, end, first int) bool {
	if w.escape != 0 {
		w.dst = append(w.dst, w.src[w.copied:start]...)
		w.copied = end
		ctx := w.ctx
		ctx.MarshalBuf = jsonstring.AppendUnescaped(ctx.MarshalBuf[:0], w.src[start+1:end-1])
		var valid bool
		w.dst, valid = jsonstring.AppendQuoted(textEscaper(ctx), w.dst, unsafe.String(unsafe.SliceData(ctx.MarshalBuf), len(ctx.MarshalBuf)))
		return valid
	}
	// the output before the string, the quote and the characters before the first escape.
	dst := append(w.dst, w.src[w.copied:first]...)
	w.copied = end
	// the escapes are valid, of no surrogate ( see stringEnd ), and the rest is checked for UTF-8 with the value.
	s := w.src[first : end-1]
	for {
		j := bytes.IndexByte(s, '\\')
		if j < 0 {
			dst = append(dst, s...)
			break
		}
		dst = append(dst, s[:j]...)
		r, size := rune(s[j+1]), 2
		switch r {
		case 'b':
			r = '\b'
		case 'f':
			r = '\f'
		case 'n':
			r = '\n'
		case 'r':
			r = '\r'
		case 't':
			r = '\t'
		case 'u':
			r, size = hexRune(s[j+2:j+6]), 6
		}
		switch {
		case r == '"' || r == '\\' || r < 0x20:
			dst = jsonstring.AppendASCIIEscape(dst, byte(r))
		case r < utf8.RuneSelf:
			dst = append(dst, byte(r))
		default:
			dst = utf8.AppendRune(dst, r)
		}
		s = s[j+size:]
	}
	w.dst = append(dst, '"')
	return true
}

// hexRune returns the character of the four hexadecimal digits of an escape, which are valid.
func hexRune(h []byte) rune {
	var r rune
	for _, c := range h[:4] {
		switch {
		case c <= '9':
			c -= '0'
		case c <= 'F':
			c -= 'A' - 10
		default:
			c -= 'a' - 10
		}
		r = r<<4 | rune(c)
	}
	return r
}
