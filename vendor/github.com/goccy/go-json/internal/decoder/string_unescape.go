package decoder

import (
	"fmt"
	"math/bits"
	"unicode/utf8"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
)

// decodeStringRest is decodeStringValue for a long string, which scanString stopped in: it scans the rest of the
// string as scanStringRest does, and, at an escape, leaves the rest to unescapeRest, which decodes the string in
// the same pass. The loop of a string without an escape keeps nothing more than the one of scanStringRest.
//
//go:noinline
func (d *stringDecoder) decodeStringRest(ctx *RuntimeContext, literal []byte, cursor int64, info stringInfo) (string, int64, bool, error) {
	if info.firstEscape >= 0 {
		return d.unescapeRest(ctx, literal, cursor, info)
	}
	buf := ctx.Buf
	start := cursor - int64(len(literal))
	var high uint64
	if info.nonASCII {
		high = msb
	}
	b := (*sliceHeader)(unsafe.Pointer(&buf)).data
	buflen := int64(len(buf))
	for {
		if i, h, ok := indexStringSpecial(unsafe.Add(b, cursor), int(buflen-cursor)); ok {
			high |= h
			cursor += int64(i)
		} else {
			for cursor+8 <= buflen {
				w := load64(buf, cursor)
				if special := keyEndBytes(w); special != 0 {
					i := int64(bits.TrailingZeros64(special) / 8)
					high |= w & msb & (1<<(uint(i)*8&63) - 1)
					cursor += i
					break
				}
				high |= w & msb
				cursor += 8
			}
		}
		c := char(b, cursor)
		switch c {
		case '\\':
			return d.unescapeRest(ctx, buf[start:cursor], cursor, stringInfo{firstEscape: -1, nonASCII: high&msb != 0})
		case '"':
			literal := buf[start:cursor]
			if high&msb != 0 {
				literal = validLiteral(literal)
			}
			return ctx.makeString(literal), cursor + 1, true, nil
		case nul:
			return d.skipOtherValue(ctx, errors.ErrUnexpectedEndOfJSON("string", cursor))
		default:
			if c < 0x20 {
				return d.skipOtherValue(ctx, errors.ErrSyntax(fmt.Sprintf("invalid character %s in string literal", quoteChar(c)), cursor+1))
			}
			high |= uint64(c)
			cursor++
		}
	}
}

// unescapeRest continues decodeStringRest from the bytes of the string so far, literal, and the position after
// them, cursor, which is at an escape or after the first 64 bytes of a string with an escape: it scans the rest
// of the string and decodes it in the same pass, into the scratch bytes of the context. A run of plain bytes is
// scanned by SIMD, and moved at the byte which ends it.
//
//go:noinline
func (d *stringDecoder) unescapeRest(ctx *RuntimeContext, literal []byte, cursor int64, info stringInfo) (string, int64, bool, error) {
	buf := ctx.Buf
	start := cursor - int64(len(literal))
	var high uint64
	if info.nonASCII {
		high = msb
	}
	escaped := info.firstEscape >= 0
	out := ctx.unescaped[:0]
	if escaped {
		out = growBytes(out, len(literal)+64)
		out = out[:unescapeTo(unsafe.Pointer(unsafe.SliceData(out)), literal, info.firstEscape)]
	}
	// run is the position of the plain bytes which are not in out yet, once the string has an escape.
	run := cursor
	b := (*sliceHeader)(unsafe.Pointer(&buf)).data
	buflen := int64(len(buf))
	for {
		if i, h, ok := indexStringSpecial(unsafe.Add(b, cursor), int(buflen-cursor)); ok {
			high |= h
			cursor += int64(i)
		} else {
			for cursor+8 <= buflen {
				w := load64(buf, cursor)
				if special := keyEndBytes(w); special != 0 {
					i := int64(bits.TrailingZeros64(special) / 8)
					high |= w & msb & (1<<(uint(i)*8&63) - 1)
					cursor += i
					break
				}
				high |= w & msb
				cursor += 8
			}
		}
		c := char(b, cursor)
		switch c {
		case '\\':
			if escaped {
				out = append(out, buf[run:cursor]...)
			} else {
				escaped = true
				out = append(growBytes(out, int(cursor-start)+64), buf[start:cursor]...)
			}
			cursor++
			switch e := char(b, cursor); e {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				out = append(out, unescapeMap[e])
				cursor++
			case 'u':
				code, ok := hex4(b, cursor+1, buflen)
				if !ok {
					return d.skipOtherValue(ctx, d.hexError(buf, cursor))
				}
				cursor += 5
				if code >= 0xd800 && code < 0xdc00 && char(b, cursor) == '\\' && cursor+1 < buflen && char(b, cursor+1) == 'u' {
					if lo, ok := hex4(b, cursor+2, buflen); ok && lo >= 0xdc00 && lo < 0xe000 {
						code = (code-0xd800)<<10 | (lo - 0xdc00) + 0x10000
						cursor += 6
					}
				}
				out = utf8.AppendRune(out, code)
			default:
				return d.skipOtherValue(ctx, errors.ErrUnexpectedEndOfJSON("escaped string", cursor))
			}
			run = cursor
		case '"':
			next := cursor + 1
			if !escaped {
				literal := buf[start:cursor]
				if high&msb != 0 {
					literal = validLiteral(literal)
				}
				return ctx.makeString(literal), next, true, nil
			}
			out = append(out, buf[run:cursor]...)
			if cap(out) <= maxUnescapeScratchSize {
				ctx.unescaped = out[:0]
			}
			if high&msb != 0 && !utf8.Valid(out) {
				return string(coerceUTF8(out)), next, true, nil
			}
			return ctx.copyString(out), next, true, nil
		case nul:
			return d.skipOtherValue(ctx, errors.ErrUnexpectedEndOfJSON("string", cursor))
		default:
			if c < 0x20 {
				return d.skipOtherValue(ctx, errors.ErrSyntax(fmt.Sprintf("invalid character %s in string literal", quoteChar(c)), cursor+1))
			}
			high |= uint64(c)
			cursor++
		}
	}
}

// growBytes returns b with room for n bytes more.
func growBytes(b []byte, n int) []byte {
	if cap(b)-len(b) < n {
		b = append(b[:cap(b)], make([]byte, n)...)[:len(b)]
	}
	return b
}

// hex4 returns the code of the four hexadecimal digits at pos, and false if they are not four of them.
func hex4(b unsafe.Pointer, pos, buflen int64) (rune, bool) {
	if pos+4 >= buflen {
		return 0, false
	}
	var code rune
	for i := int64(0); i < 4; i++ {
		c := char(b, pos+i)
		if !(('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')) {
			return 0, false
		}
		code = code<<4 | rune(hexToInt[c])
	}
	return code, true
}

// hexError returns the error of the \u escape at pos, whose digits are not four hexadecimal ones, as scanString does.
func (d *stringDecoder) hexError(buf []byte, pos int64) error {
	if pos+5 >= int64(len(buf)) {
		return errors.ErrUnexpectedEndOfJSON("escaped string", pos)
	}
	for i := int64(1); i <= 4; i++ {
		c := buf[pos+i]
		if !(('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')) {
			return errors.ErrSyntax(fmt.Sprintf("json: invalid character %c in \\u hexadecimal character escape", c), pos+i)
		}
	}
	return errors.ErrUnexpectedEndOfJSON("escaped string", pos)
}
