// This files's string processing codes are inspired by https://github.com/segmentio/encoding.
// The license notation is as follows.
//
// # MIT License
//
// Copyright (c) 2019 Segment.io, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
package jsonstring

import (
	"encoding/binary"
	"math/bits"
	"unicode/utf8"
	"unsafe"
)

const (
	lsb = 0x0101010101010101
	msb = 0x8080808080808080
)

var hex = "0123456789abcdef"

// Escaper is what decides whether a string has a byte to escape, and escapes it, for a combination of the options
// of encoding/json or of encoding/json/jsontext.
type Escaper struct {
	// chars are three characters to escape in addition to the control characters, '"' and '\\',
	// repeated in every byte of a word. They are '<', '>' and '&', or '"' for nothing more.
	chars [3]uint64
	// high is msb if a byte which is not ASCII is to be looked at, to normalize UTF-8, or 0.
	high uint64
	// table is whether a byte may need an escape.
	table *[256]bool
	// tables is table as the tables of the scan by SIMD.
	tables nibbleTables
	// replacement is what a byte of invalid UTF-8 is replaced by, if UTF-8 is validated.
	replacement string
	// escapeJS is whether U+2028 and U+2029 are escaped, if UTF-8 is validated.
	escapeJS bool
	// appendEscaped appends a string which may have a byte to escape, and reports whether it is valid UTF-8, which
	// it is unless UTF-8 is validated.
	appendEscaped func(e *Escaper, buf []byte, s string) ([]byte, bool)
	// invalid is whether an escaper of stringEscapeStrict wrote a string of invalid UTF-8, after invalidOut.
	invalid    bool
	invalidOut []byte
}

// The bits of the index of an escaper of stringEscapes. An escaper of encoding/json normalizes UTF-8 as it does:
// a byte of invalid UTF-8 is replaced by invalidUTF8, and U+2028 and U+2029 are escaped. An escaper of
// encoding/json/jsontext always validates UTF-8, replaces a byte of invalid UTF-8 by U+FFFD, which it records with
// stringEscapeStrict as well ( see Escaper.Invalid ), and escapes U+2028 and U+2029 with stringEscapeJS.
const (
	stringEscapeNormalize = 1 << iota
	stringEscapeHTML
	stringEscapeText
	stringEscapeStrict
	stringEscapeJS = stringEscapeNormalize

	// the bits of the options of the encoder, which EscaperOf takes.
	EscapeNormalize = stringEscapeNormalize
	EscapeHTML      = stringEscapeHTML
	EscapeText      = stringEscapeText
	EscapeStrict    = stringEscapeStrict

	numStringEscapes = 2 * stringEscapeStrict
)

// strict returns the escaper e which records the first string of invalid UTF-8 it writes ( see Invalid ). It is
// used only as an escaper of its own, which a single encoding writes with ( see CopyOf ): the ones of
// stringEscapes are only copied.
func strict(e Escaper) Escaper {
	appendEscaped := e.appendEscaped
	e.appendEscaped = func(e *Escaper, buf []byte, s string) ([]byte, bool) {
		out, valid := appendEscaped(e, buf, s)
		if !valid && !e.invalid {
			e.invalid, e.invalidOut = true, buf
		}
		return out, valid
	}
	return e
}

// CopyOf sets e to a copy of the escaper of the options in flags ( see EscaperOf ), which records the first string
// of invalid UTF-8 it writes if EscapeStrict is set: an escaper of its own, as the record is.
func (e *Escaper) CopyOf(flags uint) {
	*e = stringEscapes[flags%numStringEscapes]
}

// Invalid returns the output before the first string of invalid UTF-8 which the escaper wrote, which it wrote with
// U+FFFD, and true; or false if it wrote none. Only an escaper of EscapeStrict records it.
func (e *Escaper) Invalid() ([]byte, bool) {
	return e.invalidOut, e.invalid
}

// HasInvalid reports whether the escaper recorded a string of invalid UTF-8 ( see Invalid ).
func (e *Escaper) HasInvalid() bool {
	return e.invalid
}

// ClearInvalid clears the record of a string of invalid UTF-8.
func (e *Escaper) ClearInvalid() {
	if e.invalid {
		e.invalid, e.invalidOut = false, nil
	}
}

func init() {
	for i := range stringEscapeStrict {
		stringEscapes[stringEscapeStrict|i] = strict(stringEscapes[i])
	}
}

// stringEscapes is indexed by the combination of the bits of the options. The ones of stringEscapeStrict are made
// by init.
var stringEscapes = [numStringEscapes]Escaper{
	0: {
		chars:         [3]uint64{lsb * '"', lsb * '"', lsb * '"'},
		table:         &needEscape,
		appendEscaped: appendString,
	},
	stringEscapeNormalize: {
		chars:         [3]uint64{lsb * '"', lsb * '"', lsb * '"'},
		high:          msb,
		table:         &needEscapeNormalizeUTF8,
		replacement:   invalidUTF8,
		escapeJS:      true,
		appendEscaped: appendNormalizedString,
	},
	stringEscapeHTML: {
		chars:         [3]uint64{lsb * '<', lsb * '>', lsb * '&'},
		table:         &needEscapeHTML,
		appendEscaped: appendHTMLString,
	},
	stringEscapeHTML | stringEscapeNormalize: {
		chars:         [3]uint64{lsb * '<', lsb * '>', lsb * '&'},
		high:          msb,
		table:         &needEscapeHTMLNormalizeUTF8,
		replacement:   invalidUTF8,
		escapeJS:      true,
		appendEscaped: appendNormalizedHTMLString,
	},
	stringEscapeText: {
		chars:         [3]uint64{lsb * '"', lsb * '"', lsb * '"'},
		high:          msb,
		table:         &needEscapeNormalizeUTF8,
		replacement:   "\ufffd",
		appendEscaped: appendNormalizedString,
	},
	stringEscapeText | stringEscapeJS: {
		chars:         [3]uint64{lsb * '"', lsb * '"', lsb * '"'},
		high:          msb,
		table:         &needEscapeNormalizeUTF8,
		replacement:   "\ufffd",
		escapeJS:      true,
		appendEscaped: appendNormalizedString,
	},
	stringEscapeText | stringEscapeHTML: {
		chars:         [3]uint64{lsb * '<', lsb * '>', lsb * '&'},
		high:          msb,
		table:         &needEscapeHTMLNormalizeUTF8,
		replacement:   "\ufffd",
		appendEscaped: appendNormalizedHTMLString,
	},
	stringEscapeText | stringEscapeHTML | stringEscapeJS: {
		chars:         [3]uint64{lsb * '<', lsb * '>', lsb * '&'},
		high:          msb,
		table:         &needEscapeHTMLNormalizeUTF8,
		replacement:   "\ufffd",
		escapeJS:      true,
		appendEscaped: appendNormalizedHTMLString,
	},
}

// V1Escaper is the escaper of encoding/json with the options: '<', '>' and '&' escaped for HTML if html is 1, and
// UTF-8 normalized if normalize is 1.
func V1Escaper(html, normalize uint) *Escaper {
	return &stringEscapes[(html<<1|normalize)&3]
}

// EscaperOf is the escaper of the options in flags, whose lowest bits are the ones of the index of stringEscapes:
// V1Escaper, or TextEscaper if EscapeText is set. It is small enough to be inlined into the VM of the encoder,
// which takes the escaper of the options for every string: it is not kept by the VM, which would have to restore
// it after each opcode.
func EscaperOf(flags uint) *Escaper {
	return &stringEscapes[flags%numStringEscapes]
}

// TextEscaper is the escaper of encoding/json/jsontext with the options: '<', '>' and '&' escaped for HTML, and
// U+2028 and U+2029 for JavaScript.
func TextEscaper(html, js bool) *Escaper {
	index := stringEscapeText
	if html {
		index |= stringEscapeHTML
	}
	if js {
		index |= stringEscapeJS
	}
	return &stringEscapes[index]
}

// escapeSequences are the escapes of the bytes which the loop of the escapes by SIMD writes ( see
// appendEscapedSIMD ): the bytes of the escape of a byte, in the order of a little-endian word, and its length in
// the top byte. They are the ones of the appendString functions. A byte which is not ASCII is 0: it is left to
// the caller, which escapes it if UTF-8 is normalized.
var escapeSequences = func() [256]uint64 {
	var seqs [256]uint64
	for c := 0; c < 0x80; c++ {
		var seq string
		switch c {
		case '"', '\\':
			seq = string([]byte{'\\', byte(c)})
		case '\n':
			seq = `\n`
		case '\r':
			seq = `\r`
		case '\t':
			seq = `\t`
		case '\b':
			seq = backspaceEscape
		case '\f':
			seq = formFeedEscape
		case '<', '>', '&':
			seq = `\u00` + string([]byte{hex[c>>4], hex[c&0xF]})
		default:
			if c < 0x20 {
				seq = `\u00` + string([]byte{hex[c>>4], hex[c&0xF]})
			}
		}
		var w uint64
		for i := len(seq) - 1; i >= 0; i-- {
			w = w<<8 | uint64(seq[i])
		}
		seqs[c] = w | uint64(len(seq))<<56
	}
	return seqs
}()

// AppendASCIIEscape appends the escape sequence of the character c of ASCII, which every escaper escapes: '"',
// '\\' or a control character, as the escapers write it.
func AppendASCIIEscape(dst []byte, c byte) []byte {
	seq := escapeSequences[c&0x7f]
	for n := int(seq >> 56); n > 0; n-- {
		dst = append(dst, byte(seq))
		seq >>= 8
	}
	return dst
}

// escapeTables are the tables of stringEscapes, which the functions of the escapes refer to by this array:
// stringEscapes refers to the functions, so they can't refer to it.
var escapeTables [numStringEscapes]*nibbleTables

func init() {
	for i := range stringEscapes {
		stringEscapes[i].tables = newNibbleTables(stringEscapes[i].table)
		escapeTables[i] = &stringEscapes[i].tables
	}
}

// The functions below return the mask of the bytes of a word which may need an escape: the most significant bit
// of such a byte is set. A byte after one to escape may be set too, because of the borrow of the subtractions.
// A mask is made of parts, each of which is small enough to be inlined: a call for every word costs more than
// the mask itself.
//
// `x - lsb` sets the most significant bit of a byte of x which is zero, and of one which already has it.
// If UTF-8 is normalized, a byte which is not ASCII is looked at anyway, so the latter doesn't matter and the
// loose masks are enough. Otherwise `&^ x` leaves only the former, so that a string which is not ASCII is not
// taken for one to escape.

// looseCommonMask is the mask of the control characters, '"', '\\' and the bytes which are not ASCII.
func looseCommonMask(n uint64) uint64 {
	return n | (n - lsb*0x20) | ((n ^ (lsb * '"')) - lsb) | ((n ^ (lsb * '\\')) - lsb)
}

// looseCharsMask is the mask of chars, and of the bytes which are not ASCII.
func (e *Escaper) looseCharsMask(n uint64) uint64 {
	return ((n ^ e.chars[0]) - lsb) | ((n ^ e.chars[1]) - lsb) | ((n ^ e.chars[2]) - lsb)
}

// exactCommonMask is the mask of the control characters, '"' and '\\'.
func exactCommonMask(n uint64) uint64 {
	quote := n ^ (lsb * '"')
	backslash := n ^ (lsb * '\\')
	return ((n - lsb*0x20) &^ n) | ((quote - lsb) &^ quote) | ((backslash - lsb) &^ backslash)
}

// exactCharsMask is the mask of chars.
func (e *Escaper) exactCharsMask(n uint64) uint64 {
	c0, c1, c2 := n^e.chars[0], n^e.chars[1], n^e.chars[2]
	return ((c0 - lsb) &^ c0) | ((c1 - lsb) &^ c1) | ((c2 - lsb) &^ c2)
}

// hasExactEscape is whether a word of the string has a byte to escape, not counting the bytes which are not
// ASCII. The string is 8 bytes or longer.
func (e *Escaper) hasExactEscape(src unsafe.Pointer, n int) bool {
	i := 0
	for ; i+8 <= n; i += 8 {
		if w := *(*uint64)(unsafe.Add(src, i)); (exactCommonMask(w)|e.exactCharsMask(w))&msb != 0 {
			return true
		}
	}
	if i < n {
		w := *(*uint64)(unsafe.Add(src, n-8))
		return (exactCommonMask(w)|e.exactCharsMask(w))&msb != 0
	}
	return false
}

// hasLooseEscape is whether a word of the string may have a byte to escape, or has a byte which is not ASCII.
// The string is 8 bytes or longer. It is cheaper than hasExactEscape, and it is exact for a string of ASCII,
// which most of the strings are.
func (e *Escaper) hasLooseEscape(src unsafe.Pointer, n int) bool {
	i := 0
	for ; i+8 <= n; i += 8 {
		if w := *(*uint64)(unsafe.Add(src, i)); (looseCommonMask(w)|e.looseCharsMask(w))&msb != 0 {
			return true
		}
	}
	if i < n {
		// the last word overlaps the previous one.
		w := *(*uint64)(unsafe.Add(src, n-8))
		return (looseCommonMask(w)|e.looseCharsMask(w))&msb != 0
	}
	return false
}

// maxInlineCopySize is the size of the longest string which is copied without calling memmove.
const maxInlineCopySize = 16

// growForString returns buf which has the capacity to append n bytes, growing as append does.
//
//go:noinline
func growForString(buf []byte, n int) []byte {
	grown := make([]byte, len(buf), 2*cap(buf)+n)
	copy(grown, buf)
	return grown
}

// AppendQuoted appends the string as a JSON string, escaped by the escaper, and reports whether it is valid UTF-8,
// which it is unless the escaper validates it. The result of the function of the escapes is returned as it is,
// without a look at it. The escaper is the first argument, as the context is of the functions of the VM of the
// encoder, which then keeps the buffer in the registers it has it in.
//
// Most of the strings have nothing to escape and are short, so that case is handled here. A string of up to
// maxOnePassLength bytes is looked at and written to the capacity of the buffer in one pass, by bytes, by the
// halves of a word or by words, and nothing is called; a longer one is looked at by SIMD ( or by words ) and
// then copied. A string which may have a byte to escape is left to the function for the options, and what was
// written for it is left out of the buffer.
func AppendQuoted(escape *Escaper, buf []byte, s string) ([]byte, bool) {
	n := len(s)
	if n == 0 {
		// no byte to escape: the options are not looked at.
		return append(buf, '"', '"'), true
	}

	src := unsafe.Pointer(unsafe.StringData(s))
	if n < 4 {
		table := escape.table
		for i := 0; i < n; i++ {
			if table[*(*byte)(unsafe.Add(src, i))] {
				return escape.appendEscaped(escape, buf, s)
			}
		}
		if l := len(buf); cap(buf)-l >= n+2 {
			// the first, the middle and the last byte are every byte of 1 to 3 bytes.
			buf = buf[:l+n+2]
			p := unsafe.Pointer(unsafe.SliceData(buf))
			*(*byte)(unsafe.Add(p, l)) = '"'
			*(*byte)(unsafe.Add(p, l+1)) = *(*byte)(src)
			*(*byte)(unsafe.Add(p, l+1+(n>>1))) = *(*byte)(unsafe.Add(src, n>>1))
			*(*byte)(unsafe.Add(p, l+n)) = *(*byte)(unsafe.Add(src, n-1))
			*(*byte)(unsafe.Add(p, l+n+1)) = '"'
			return buf, true
		}
	} else if n < 8 {
		// the two halves overlap, so that the word has every byte of the string and nothing else.
		w := uint64(*(*uint32)(src)) | uint64(*(*uint32)(unsafe.Add(src, n-4)))<<32
		if (looseCommonMask(w)|escape.looseCharsMask(w))&msb != 0 &&
			(escape.high != 0 || (exactCommonMask(w)|escape.exactCharsMask(w))&msb != 0) {
			return escape.appendEscaped(escape, buf, s)
		}
		if l := len(buf); cap(buf)-l >= n+2 {
			// the halves of the word are written where they were read from.
			buf = buf[:l+n+2]
			p := unsafe.Pointer(unsafe.SliceData(buf))
			*(*byte)(unsafe.Add(p, l)) = '"'
			*(*uint32)(unsafe.Add(p, l+1)) = uint32(w)
			*(*uint32)(unsafe.Add(p, l+n-3)) = uint32(w >> 32)
			*(*byte)(unsafe.Add(p, l+n+1)) = '"'
			return buf, true
		}
	} else if l := len(buf); n <= maxOnePassLength && cap(buf)-l >= n+2 {
		// the first and the last word and the ones between them, which overlap: 8 to maxOnePassLength bytes, 31
		// at most, are 2 to 4 words. A buffer without the capacity is grown below.
		dst := unsafe.Add(unsafe.Pointer(unsafe.SliceData(buf)), l+1)
		first, last := *(*uint64)(src), *(*uint64)(unsafe.Add(src, n-8))
		*(*uint64)(dst) = first
		*(*uint64)(unsafe.Add(dst, n-8)) = last
		mask := looseCommonMask(first) | escape.looseCharsMask(first) | looseCommonMask(last) | escape.looseCharsMask(last)
		if n > 16 {
			w := *(*uint64)(unsafe.Add(src, 8))
			*(*uint64)(unsafe.Add(dst, 8)) = w
			mask |= looseCommonMask(w) | escape.looseCharsMask(w)
			if n > 24 {
				w := *(*uint64)(unsafe.Add(src, 16))
				*(*uint64)(unsafe.Add(dst, 16)) = w
				mask |= looseCommonMask(w) | escape.looseCharsMask(w)
			}
		}
		if mask&msb != 0 && (escape.high != 0 || escape.hasExactEscape(src, n)) {
			// a byte which is not ASCII is to be looked at only if UTF-8 is normalized.
			return escape.appendEscaped(escape, buf, s)
		}
		// the position is taken from buf again, which is kept across the call above, so that nothing more is.
		l := len(buf)
		buf = buf[:l+n+2]
		p := unsafe.Pointer(unsafe.SliceData(buf))
		*(*byte)(unsafe.Add(p, l)) = '"'
		*(*byte)(unsafe.Add(p, l+n+1)) = '"'
		return buf, true
	} else if found, ok := scanBytesSIMD(src, n, &escape.tables); ok {
		if found {
			return escape.appendEscaped(escape, buf, s)
		}
	} else if escape.hasLooseEscape(src, n) && (escape.high != 0 || escape.hasExactEscape(src, n)) {
		// a byte which is not ASCII is to be looked at only if UTF-8 is normalized.
		return escape.appendEscaped(escape, buf, s)
	}

	l := len(buf)
	if cap(buf)-l < n+2 {
		buf = growForString(buf, n+2)
	}
	buf = buf[:l+n+2]
	dst := unsafe.Pointer(unsafe.SliceData(buf[l:]))
	*(*byte)(dst) = '"'
	dst = unsafe.Add(dst, 1)
	switch {
	case n > maxInlineCopySize:
		copy(buf[l+1:], s)
	case n >= 8:
		// the two words overlap.
		*(*uint64)(dst) = *(*uint64)(src)
		*(*uint64)(unsafe.Add(dst, n-8)) = *(*uint64)(unsafe.Add(src, n-8))
	case n >= 4:
		*(*uint32)(dst) = *(*uint32)(src)
		*(*uint32)(unsafe.Add(dst, n-4)) = *(*uint32)(unsafe.Add(src, n-4))
	case n > 0:
		// the first, the middle and the last byte are every byte of 1 to 3 bytes.
		*(*byte)(dst) = *(*byte)(src)
		*(*byte)(unsafe.Add(dst, n>>1)) = *(*byte)(unsafe.Add(src, n>>1))
		*(*byte)(unsafe.Add(dst, n-1)) = *(*byte)(unsafe.Add(src, n-1))
	}
	*(*byte)(unsafe.Add(dst, n)) = '"'
	return buf, true
}

// skipPlainRunes returns the index of the first byte of s from j which may need an escape by table, or len(s),
// taking the characters of commonRuneSize as bytes which need none: the characters of a text of CJK are skipped by
// this loop, called once for a run of them, and not one by one by the loop of the escapes, which then keeps its
// layout for the bytes of ASCII. The loop of the escapes passes s up to its limit, so that it gives the rest of a
// long string back to the loop by SIMD as before.
//
//go:noinline
func skipPlainRunes(s string, j int, table *[256]bool, html bool) int {
	for j < len(s) {
		c := s[j]
		if !table[c] {
			if j+8 > len(s) {
				j++
				continue
			}
			// the bytes of ASCII up to the next one which may need an escape or isn't ASCII, by a word: the first
			// byte of the mask is exact.
			w := wordAt(s, j)
			var m uint64
			if html {
				m = maskNormalizedHTML(w)
			} else {
				m = maskNormalized(w)
			}
			if m == 0 {
				j += 8
			} else {
				j += bits.TrailingZeros64(m) / 8
			}
			continue
		}
		size := commonRuneSize(s, j)
		if size == 0 {
			break
		}
		j += size
	}
	return j
}

// appendSpecial appends the character at the start of rest, which decodeRuneInString found invalid UTF-8, U+2028
// or U+2029, after the bytes before it, as the escaper writes it, and returns the number of its bytes; or 0 if the
// escaper writes it as it is, and then appends nothing. A byte of invalid UTF-8 is replaced. U+2028 and U+2029 are technically valid characters in JSON strings, but don't work in JSONP,
// which has to be evaluated as JavaScript, and can lead to security holes there ( see
// http://timelessrepo.com/json-isnt-a-javascript-subset ): encoding/json escapes them. It is apart from the loops
// of the escapes, which then keep their registers for the bytes which most strings have.
//
//go:noinline
func (e *Escaper) appendSpecial(buf []byte, before, rest string, state decodeRuneState) ([]byte, int) {
	if state == runeErrorState {
		return append(append(buf, before...), e.replacement...), 1
	}
	if !e.escapeJS {
		return buf, 0
	}
	// E2 80 A8 or E2 80 A9
	buf = append(append(buf, before...), `\u202`...)
	return append(buf, hex[rest[2]&0xf]), 3
}

// minRunLength is the length of a string from which appendNormalizedRuns appends it, if there is no loop of the
// escapes by SIMD: the characters of a shorter one are gone through one by one for less.
const minRunLength = 32

// appendNormalizedRuns is appendNormalizedString and appendNormalizedHTMLString, by table and html, for a long
// string without the loop of the escapes by SIMD: the bytes of ASCII are skipped 8 at a time, and a run of
// characters which are not ASCII up to the next byte of ASCII to escape is taken at once if it is valid UTF-8
// ( see plainRunEnd ), as text which is not ASCII mostly is. It is apart from the loops for the short strings,
// which then keep their registers.
func appendNormalizedRuns(e *Escaper, buf []byte, s string, table *[256]bool, html bool) ([]byte, bool) {
	buf = append(buf, '"')
	valid := true
	i, j := 0, 0
	// validEnd is the end of the run of characters which plainRunEnd found valid UTF-8, and invalidEnd the end
	// of the one which it found invalid, which is gone through by the characters and not looked at again.
	validEnd, invalidEnd := 0, 0
	for j < len(s) {
		if !table[s[j]] {
			// the bytes of ASCII which need no escape, 8 at a time: the mask has the bytes which may need one
			for j+8 <= len(s) {
				w := wordAt(s, j)
				var m uint64
				if html {
					m = maskNormalizedHTML(w)
				} else {
					m = maskNormalized(w)
				}
				if m != 0 {
					j += bits.TrailingZeros64(m) / 8
					break
				}
				j += 8
			}
			if j >= len(s) || !table[s[j]] {
				j++
				continue
			}
		}
		c := s[j]
		if seq := escapeSequences[c]; seq != 0 {
			buf = append(buf, s[i:j]...)
			l := len(buf)
			buf = binary.LittleEndian.AppendUint64(buf, seq)[:l+int(seq>>56)]
			j++
			i = j
			continue
		}
		if k := skipPlainRunes(s, j, table, html); k != j {
			j = k
			continue
		}
		if j >= invalidEnd {
			// a character which skipPlainRunes doesn't take: the characters up to the next byte of ASCII to escape
			// are taken at once if they are valid UTF-8, up to U+2028 or U+2029 if they are escaped.
			k, v := e.plainRunEnd(s, j, validEnd, html)
			if v < 0 {
				invalidEnd = -v
			} else if validEnd = v; k != j {
				j = k
				continue
			}
		}
		state, size := decodeRuneInString(s[j:])
		if state != validUTF8State {
			// invalid UTF-8, U+2028 or U+2029, which the escaper writes
			var n int
			if buf, n = e.appendSpecial(buf, s[i:j], s[j:], state); n != 0 {
				valid = valid && state != runeErrorState
				j += n
				i = j
				continue
			}
		}
		j += size
	}
	return append(append(buf, s[i:]...), '"'), valid
}

// plainRunEnd returns the end of the characters of s from j, which starts with a byte which is not ASCII, which
// are appended as they are: the run up to the next byte of ASCII which the escaper escapes, if it is valid UTF-8,
// up to U+2028 or U+2029 if the escaper escapes them. It returns too the end of the run which is known to be
// valid UTF-8, which is validEnd if j is before it, or the negative end of a run which is not valid UTF-8, which
// the caller then goes through by the characters. Valid UTF-8 is checked by utf8.ValidString, which takes ASCII
// 8 bytes at a time.
//
//go:noinline
func (e *Escaper) plainRunEnd(s string, j, validEnd int, html bool) (int, int) {
	if j >= validEnd {
		end := asciiEscapeEnd(s, j, html)
		if !utf8.ValidString(s[j:end]) {
			return j, -end
		}
		validEnd = end
	}
	if e.escapeJS {
		if k := indexLineSeparator(s[j:validEnd]); k >= 0 {
			return j + k, validEnd
		}
	}
	return validEnd, validEnd
}

// indexLineSeparator returns the position of the first U+2028 or U+2029 of s, E2 80 A8 or E2 80 A9, or -1. It
// looks at 8 bytes at a time for 0xE2, which starts many other characters, and at the bytes of a word with one.
func indexLineSeparator(s string) int {
	k := 0
	for ; k+8 <= len(s); k += 8 {
		// the zero bytes of x are the bytes 0xE2, and the mask may have a byte after one more
		if x := wordAt(s, k) ^ (lsb * 0xe2); (x-lsb)&^x&msb == 0 {
			continue
		}
		for i := k; i < k+8; i++ {
			if s[i] == 0xe2 && i+2 < len(s) && s[i+1] == 0x80 && s[i+2]&^1 == 0xa8 {
				return i
			}
		}
	}
	for ; k+2 < len(s); k++ {
		if s[k] == 0xe2 && s[k+1] == 0x80 && s[k+2]&^1 == 0xa8 {
			return k
		}
	}
	return -1
}

// asciiEscapeEnd returns the position of the first byte of s from j which is a byte of ASCII to escape: a control
// character, '"', '\\', or, for HTML, '<', '>' or '&'; or len(s). It looks at 8 bytes at a time.
func asciiEscapeEnd(s string, j int, html bool) int {
	for ; j+8 <= len(s); j += 8 {
		w := wordAt(s, j)
		m := exactCommonMask(w)
		if html {
			m |= htmlCharsMask(w)
		}
		if m &= msb; m != 0 {
			return j + bits.TrailingZeros64(m)/8
		}
	}
	for ; j < len(s); j++ {
		if c := s[j]; c < ' ' || c == '"' || c == '\\' || html && (c == '<' || c == '>' || c == '&') {
			break
		}
	}
	return j
}

// htmlCharsMask is the exact mask of '<', '>' and '&'.
func htmlCharsMask(n uint64) uint64 {
	c0, c1, c2 := n^(lsb*'<'), n^(lsb*'>'), n^(lsb*'&')
	return ((c0 - lsb) &^ c0) | ((c1 - lsb) &^ c1) | ((c2 - lsb) &^ c2)
}

// commonRuneSize returns the size of the character at s[j] if it is one of two bytes, or one of three bytes whose
// first byte is 0xE3 to 0xEC, 0xEE or 0xEF, as the characters of CJK are, and its next bytes are continuation
// bytes, or 0. Such a character is valid UTF-8 and is not escaped whatever its continuation bytes are, so it is
// written as it is without decodeRuneInString, which is not inlined.
func commonRuneSize(s string, j int) int {
	c := s[j]
	if c-0xc2 < 0xe0-0xc2 {
		if j+1 < len(s) && s[j+1]^0x80 < 0x40 {
			return 2
		}
	} else if c-0xe3 < 0xed-0xe3 || c-0xee < 2 {
		if j+2 < len(s) && (s[j+1]^0x80)|(s[j+2]^0x80) < 0x40 {
			return 3
		}
	}
	return 0
}

func appendNormalizedHTMLString(e *Escaper, buf []byte, s string) ([]byte, bool) {
	valLen := len(s)
	if valLen == 0 {
		return append(buf, `""`...), true
	}
	if valLen >= minRunLength && !hasEscapeLoop {
		return appendNormalizedRuns(e, buf, s, &needEscapeHTMLNormalizeUTF8, true)
	}
	buf = append(buf, '"')
	valid := true
	var i int
	// the bytes up to the first one which may need an escape are skipped by SIMD, or by words.
	j := 0
	// limit is where the loop of the bytes gives the rest of a long string back to the loop of the escapes by
	// SIMD, which stops at a block of 32 bytes with invalid UTF-8, U+2028 or U+2029: the loop of the bytes
	// escapes that block, so that the loop by SIMD is called once for 32 bytes at most.
	limit := valLen
	if valLen >= 32 && hasEscapeLoop {
		// a long string is escaped by SIMD, up to a byte which is left to this loop
		buf, j = appendEscapedSIMD(buf, s, escapeTables[stringEscapeHTML], true)
		i = j
		limit = min(valLen, j+32)
	} else if valLen >= 8 {
		// the last word overlaps the one before it: the first byte which may need an escape is in the word at
		// k, or it is none if the mask of the last word is 0, and j is then valLen.
		k := 0
		m := maskNormalizedHTML(wordAt(s, 0))
		for m == 0 && k+16 <= valLen {
			k += 8
			m = maskNormalizedHTML(wordAt(s, k))
		}
		if m == 0 && k+8 < valLen {
			k = valLen - 8
			m = maskNormalizedHTML(wordAt(s, k))
		}
		j = k + bits.TrailingZeros64(m)/8
	}
	for {
		for j < limit {
			c := s[j]

			if !needEscapeHTMLNormalizeUTF8[c] {
				// fast path: most of the time, printable ascii characters are used
				j++
				continue
			}

			if seq := escapeSequences[c]; seq != 0 {
				// a byte of ASCII, escaped by its sequence ( see escapeSequences ): the bytes of the word after
				// its length are written over by what follows.
				buf = append(buf, s[i:j]...)
				l := len(buf)
				buf = binary.LittleEndian.AppendUint64(buf, seq)[:l+int(seq>>56)]
				i = j + 1
				j = j + 1
				continue
			}
			if c >= utf8.RuneSelf {
				if k := skipPlainRunes(s[:limit], j, &needEscapeHTMLNormalizeUTF8, true); k != j {
					j = k
					continue
				}
			}
			state, size := decodeRuneInString(s[j:])
			if state == lineSepState || state == paragraphSepState {
				// U+2028 or U+2029, E2 80 A8 or E2 80 A9, if the escaper escapes them ( see appendSpecial )
				if e.escapeJS {
					buf = append(buf, s[i:j]...)
					buf = append(buf, `\u202`...)
					buf = append(buf, hex[s[j+2]&0xf])
					j += 3
					i = j
					continue
				}
			} else if state == runeErrorState {
				buf, _ = e.appendSpecial(buf, s[i:j], s[j:], state)
				valid = false
				j++
				i = j
				continue
			}
			j += size
		}
		if j >= valLen {
			break
		}
		buf = append(buf, s[i:j]...)
		var n int
		buf, n = appendEscapedSIMD(buf, s[j:], escapeTables[stringEscapeHTML], true)
		j += n
		i = j
		limit = min(valLen, j+32)
	}

	return append(append(buf, s[i:]...), '"'), valid
}

func appendHTMLString(e *Escaper, buf []byte, s string) ([]byte, bool) {
	valLen := len(s)
	if valLen == 0 {
		return append(buf, `""`...), true
	}
	buf = append(buf, '"')
	var i int
	// the bytes up to the first one which may need an escape are skipped by SIMD, or by words.
	j := 0
	if valLen >= 32 && hasEscapeLoop {
		// a long string is escaped by SIMD, up to a byte which is left to this loop
		buf, j = appendEscapedSIMD(buf, s, escapeTables[stringEscapeHTML], false)
		i = j
	} else if valLen >= 8 {
		// the last word overlaps the one before it: the first byte which may need an escape is in the word at
		// k, or it is none if the mask of the last word is 0, and j is then valLen.
		k := 0
		m := maskNormalizedHTML(wordAt(s, 0))
		for m == 0 && k+16 <= valLen {
			k += 8
			m = maskNormalizedHTML(wordAt(s, k))
		}
		if m == 0 && k+8 < valLen {
			k = valLen - 8
			m = maskNormalizedHTML(wordAt(s, k))
		}
		j = k + bits.TrailingZeros64(m)/8
	}
	for j < valLen {
		c := s[j]

		if !needEscapeHTML[c] {
			// fast path: most of the time, printable ascii characters are used
			j++
			continue
		}

		// a byte of ASCII, escaped by its sequence ( see escapeSequences ): the bytes of the word after its
		// length are written over by what follows.
		seq := escapeSequences[c]
		buf = append(buf, s[i:j]...)
		l := len(buf)
		buf = binary.LittleEndian.AppendUint64(buf, seq)[:l+int(seq>>56)]
		i = j + 1
		j = j + 1
	}

	return append(append(buf, s[i:]...), '"'), true
}

func appendNormalizedString(e *Escaper, buf []byte, s string) ([]byte, bool) {
	valLen := len(s)
	if valLen == 0 {
		return append(buf, `""`...), true
	}
	if valLen >= minRunLength && !hasEscapeLoop {
		return appendNormalizedRuns(e, buf, s, &needEscapeNormalizeUTF8, false)
	}
	buf = append(buf, '"')
	valid := true
	var i int
	// the bytes up to the first one which may need an escape are skipped by SIMD, or by words.
	j := 0
	// limit is where the loop of the bytes gives the rest of a long string back to the loop of the escapes by
	// SIMD, which stops at a block of 32 bytes with invalid UTF-8, U+2028 or U+2029: the loop of the bytes
	// escapes that block, so that the loop by SIMD is called once for 32 bytes at most.
	limit := valLen
	if valLen >= 32 && hasEscapeLoop {
		// a long string is escaped by SIMD, up to a byte which is left to this loop
		buf, j = appendEscapedSIMD(buf, s, escapeTables[0], true)
		i = j
		limit = min(valLen, j+32)
	} else if valLen >= 8 {
		// the last word overlaps the one before it: the first byte which may need an escape is in the word at
		// k, or it is none if the mask of the last word is 0, and j is then valLen.
		k := 0
		m := maskNormalized(wordAt(s, 0))
		for m == 0 && k+16 <= valLen {
			k += 8
			m = maskNormalized(wordAt(s, k))
		}
		if m == 0 && k+8 < valLen {
			k = valLen - 8
			m = maskNormalized(wordAt(s, k))
		}
		j = k + bits.TrailingZeros64(m)/8
	}
	for {
		for j < limit {
			c := s[j]

			if !needEscapeNormalizeUTF8[c] {
				// fast path: most of the time, printable ascii characters are used
				j++
				continue
			}

			if seq := escapeSequences[c]; seq != 0 {
				// a byte of ASCII, escaped by its sequence ( see escapeSequences ): the bytes of the word after
				// its length are written over by what follows.
				buf = append(buf, s[i:j]...)
				l := len(buf)
				buf = binary.LittleEndian.AppendUint64(buf, seq)[:l+int(seq>>56)]
				i = j + 1
				j = j + 1
				continue
			}
			if c >= utf8.RuneSelf {
				if k := skipPlainRunes(s[:limit], j, &needEscapeNormalizeUTF8, false); k != j {
					j = k
					continue
				}
			}
			state, size := decodeRuneInString(s[j:])
			if state == lineSepState || state == paragraphSepState {
				// U+2028 or U+2029, E2 80 A8 or E2 80 A9, if the escaper escapes them ( see appendSpecial )
				if e.escapeJS {
					buf = append(buf, s[i:j]...)
					buf = append(buf, `\u202`...)
					buf = append(buf, hex[s[j+2]&0xf])
					j += 3
					i = j
					continue
				}
			} else if state == runeErrorState {
				buf, _ = e.appendSpecial(buf, s[i:j], s[j:], state)
				valid = false
				j++
				i = j
				continue
			}
			j += size
		}
		if j >= valLen {
			break
		}
		buf = append(buf, s[i:j]...)
		var n int
		buf, n = appendEscapedSIMD(buf, s[j:], escapeTables[0], true)
		j += n
		i = j
		limit = min(valLen, j+32)
	}

	return append(append(buf, s[i:]...), '"'), valid
}

func appendString(e *Escaper, buf []byte, s string) ([]byte, bool) {
	valLen := len(s)
	if valLen == 0 {
		return append(buf, `""`...), true
	}
	buf = append(buf, '"')
	var i int
	// the bytes up to the first one which may need an escape are skipped by SIMD, or by words.
	j := 0
	if valLen >= 32 && hasEscapeLoop {
		// a long string is escaped by SIMD, up to a byte which is left to this loop
		buf, j = appendEscapedSIMD(buf, s, escapeTables[0], false)
		i = j
	} else if valLen >= 8 {
		// the last word overlaps the one before it: the first byte which may need an escape is in the word at
		// k, or it is none if the mask of the last word is 0, and j is then valLen.
		k := 0
		m := maskPlain(wordAt(s, 0))
		for m == 0 && k+16 <= valLen {
			k += 8
			m = maskPlain(wordAt(s, k))
		}
		if m == 0 && k+8 < valLen {
			k = valLen - 8
			m = maskPlain(wordAt(s, k))
		}
		j = k + bits.TrailingZeros64(m)/8
	}
	for j < valLen {
		c := s[j]

		if !needEscape[c] {
			// fast path: most of the time, printable ascii characters are used
			j++
			continue
		}

		// a byte of ASCII, escaped by its sequence ( see escapeSequences ): the bytes of the word after its
		// length are written over by what follows.
		seq := escapeSequences[c]
		buf = append(buf, s[i:j]...)
		l := len(buf)
		buf = binary.LittleEndian.AppendUint64(buf, seq)[:l+int(seq>>56)]
		i = j + 1
		j = j + 1
	}

	return append(append(buf, s[i:]...), '"'), true
}

// The masks of a word of the options: the top bit of the first byte which may need an escape is set, and maybe
// the ones of the bytes after it ( see looseCommonMask ). They are small enough to be inlined. The masks of HTML
// are loose even if UTF-8 is not normalized: the exact one costs more for every word than it saves by skipping
// the bytes which are not ASCII, which the loop of appendHTMLString goes over one by one instead.

func maskNormalizedHTML(w uint64) uint64 {
	return (looseCommonMask(w) | ((w ^ (lsb * '<')) - lsb) | ((w ^ (lsb * '>')) - lsb) | ((w ^ (lsb * '&')) - lsb)) & msb
}

func maskNormalized(w uint64) uint64 {
	return looseCommonMask(w) & msb
}

func maskPlain(w uint64) uint64 {
	return exactCommonMask(w) & msb
}

// wordAt returns the eight bytes of the string at k as a little-endian word: the string has k+8 bytes or more.
func wordAt(s string, k int) uint64 {
	return binary.LittleEndian.Uint64(unsafe.Slice((*byte)(unsafe.Add(unsafe.Pointer(unsafe.StringData(s)), k)), 8))
}
