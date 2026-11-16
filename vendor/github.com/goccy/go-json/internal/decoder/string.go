package decoder

import (
	"fmt"
	"math/bits"
	"reflect"
	"strconv"
	"unicode/utf8"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/jsonstring"
)

type stringDecoder struct {
	structName string
	fieldName  string
	// typ is the type of the string, which the type errors report.
	typ reflect.Type
}

func newStringDecoder(structName, fieldName string) *stringDecoder {
	return &stringDecoder{
		structName: structName,
		fieldName:  fieldName,
		typ:        reflect.TypeOf(""),
	}
}

func (d *stringDecoder) errUnmarshalType(typeName string, offset int64) *errors.UnmarshalTypeError {
	return &errors.UnmarshalTypeError{
		Value:  typeName,
		Type:   reflect.TypeOf(""),
		Offset: offset,
		Struct: d.structName,
		Field:  d.fieldName,
	}
}

func (d *stringDecoder) Decode(ctx *RuntimeContext, cursor, depth int64, p unsafe.Pointer) (int64, error) {
	var (
		s   string
		c   int64
		ok  bool
		err error
	)
	buf := ctx.Buf
	if start := skipWhiteSpace(buf, cursor); buf[start] == '"' {
		s, c, ok, err = d.decodeStringField(ctx, start+1)
	} else {
		s, c, ok, err = d.decodeStringValue(ctx, cursor)
	}
	if err != nil {
		return 0, err
	}
	if ok {
		**(**string)(unsafe.Pointer(&p)) = s
	}
	return c, nil
}

func (d *stringDecoder) DecodePath(ctx *RuntimeContext, cursor, depth int64) ([][]byte, int64, error) {
	bytes, c, err := d.decodeByte(ctx.Buf, cursor)
	if err != nil {
		return nil, 0, err
	}
	if bytes == nil {
		return [][]byte{nullbytes}, c, nil
	}
	return [][]byte{bytes}, c, nil
}

// stringInfo is what scanString found in a string.
type stringInfo struct {
	// firstEscape is the offset of the first backslash in the bytes of the string, or -1 if it has no escape:
	// its bytes are the ones of the value then.
	firstEscape int
	// nonASCII is whether a byte of the string is not ASCII: the string may be invalid UTF-8.
	nonASCII bool
}

// decodeByte returns the bytes of the string at cursor, or nil for null: a part of buf, or a slice of their own if
// the string has an escape or invalid UTF-8 ( see decodeLiteral ).
func (d *stringDecoder) decodeByte(buf []byte, cursor int64) ([]byte, int64, error) {
	literal, next, info, err := d.scanString(buf, cursor)
	if next < 0 {
		literal, next, info, err = d.scanStringRest(buf, literal, -next-1, info)
	}
	if err != nil || literal == nil {
		return literal, next, err
	}
	return decodeLiteral(literal, info), next, nil
}

// unescapeString returns the string of the literal, which has an escape: it is decoded into the scratch bytes of the
// context, and copied, so that the string has the bytes of its length only, which are not zeroed before they are
// written, and the buffer is not written. A literal too long for the scratch bytes is decoded into bytes of its own,
// which the string is.
func (ctx *RuntimeContext) unescapeString(literal []byte, info stringInfo) string {
	if len(literal) > maxUnescapeScratchSize {
		decoded := decodeLiteral(literal, info)
		return unsafe.String(unsafe.SliceData(decoded), len(decoded))
	}
	if cap(ctx.unescaped) < len(literal) {
		// twice as large as before, so that strings of growing lengths make few of them
		ctx.unescaped = make([]byte, min(max(len(literal), 2*cap(ctx.unescaped)), maxUnescapeScratchSize))
	}
	n := jsonstring.UnescapeTo(unsafe.Pointer(unsafe.SliceData(ctx.unescaped)), literal, info.firstEscape)
	decoded := ctx.unescaped[:n]
	if info.nonASCII && !utf8.Valid(decoded) {
		return string(coerceUTF8(decoded))
	}
	return string(decoded)
}

// decodeLiteral decodes the escapes of the literal and replaces its invalid UTF-8, into a slice of its own if it
// has any: the buffer is never written, so that it keeps the input, which is checked again for a syntax error
// ( see Stream.decodeError ).
func decodeLiteral(literal []byte, info stringInfo) []byte {
	if info.firstEscape >= 0 {
		out := make([]byte, len(literal))
		literal = out[:jsonstring.UnescapeTo(unsafe.Pointer(unsafe.SliceData(out)), literal, info.firstEscape)]
	}
	if info.nonASCII && !utf8.Valid(literal) {
		literal = coerceUTF8(literal)
	}
	return literal
}

// validLiteral returns the literal of a string without an escape, whose bytes are not all ASCII, with its invalid
// UTF-8 replaced ( see decodeLiteral ).
func validLiteral(literal []byte) []byte {
	if !utf8.Valid(literal) {
		return coerceUTF8(literal)
	}
	return literal
}

// decodeStringValue is decodeString for the value of a string: a value of another kind is a type error, which is
// recorded, and skipped ( see skipOtherValue ). It is a function of its own, so that the decoding of the other
// strings, as the keys, is the one it was, and that Decode keeps nothing across its call.
func (d *stringDecoder) decodeStringValue(ctx *RuntimeContext, cursor int64) (string, int64, bool, error) {
	literal, next, info, err := d.scanString(ctx.Buf, cursor)
	if next < 0 {
		return d.decodeStringRest(ctx, literal, -next-1, info)
	}
	if err != nil {
		return d.skipOtherValue(ctx, err)
	}
	if literal == nil {
		return "", next, false, nil
	}
	if info.firstEscape >= 0 {
		return ctx.unescapeString(literal, info), next, true, nil
	}
	if info.nonASCII {
		literal = validLiteral(literal)
	}
	return ctx.makeString(literal), next, true, nil
}

// decodeStringField is decodeStringValue for the string whose bytes start at start, after its quote, which the decoder
// of a string field decodes. Its words are looked at for their end, an escape and a control byte, and, until the
// first one is met, a byte which is not ASCII: the words of a plain ASCII string are looked at for nothing more, and
// its bytes are the string. An escape is validated where it is met, and a byte which is not ASCII makes the words
// look past such bytes, so that any string is scanned once. The string is then decoded as decodeStringValue decodes
// it: an escaped string through the scratch bytes ( see unescapeString ), a long one from where the words stop ( see
// decodeStringRest ). Where the CPU has a SIMD scan, the words are looked at in the first 64 bytes only, after which
// a long string is scanned by SIMD. An error in the string is a syntax error of the string.
func (d *stringDecoder) decodeStringField(ctx *RuntimeContext, start int64) (string, int64, bool, error) {
	buf := ctx.Buf
	b := (*sliceHeader)(unsafe.Pointer(&buf)).data
	buflen := int64(len(buf))
	cursor := start
	wordsEnd := buflen
	if hasStringSIMD {
		wordsEnd = min(buflen, start+64)
	}
	// The words of a plain ASCII string, which most are, are looked at by this loop, which keeps nothing else: the
	// loop below goes on from the byte it stops at, which is not read again.
	for cursor+8 <= wordsEnd {
		w := load64(buf, cursor)
		if stop := keyEndBytes(w) | w&msb; stop != 0 {
			cursor += int64(bits.TrailingZeros64(stop) / 8)
			if char(b, cursor) == '"' {
				return ctx.makeString(buf[start:cursor]), cursor + 1, true, nil
			}
			break
		}
		cursor += 8
	}
	firstEscape := int64(-1)
	nonASCII := false
	// ascii are the top bits which stop the words at a byte which is not ASCII, until the first one is met.
	ascii := uint64(msb)
	for {
		c := char(b, cursor)
		switch {
		case c == '"':
			literal, next := buf[start:cursor], cursor+1
			if firstEscape < 0 {
				if nonASCII {
					literal = validLiteral(literal)
				}
				return ctx.makeString(literal), next, true, nil
			}
			return ctx.unescapeString(literal, stringInfo{firstEscape: int(firstEscape), nonASCII: nonASCII}), next, true, nil
		case c == '\\':
			if firstEscape < 0 {
				firstEscape = cursor - start
			}
			cursor++
			switch char(b, cursor) {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				cursor++
			case 'u':
				if cursor+5 >= buflen {
					return "", 0, false, errors.ErrUnexpectedEndOfJSON("escaped string", cursor)
				}
				for i := int64(1); i <= 4; i++ {
					c := char(b, cursor+i)
					if !(('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')) {
						return "", 0, false, errors.ErrSyntax(fmt.Sprintf("json: invalid character %c in \\u hexadecimal character escape", c), cursor+i)
					}
				}
				cursor += 5
			default:
				return "", 0, false, errors.ErrUnexpectedEndOfJSON("escaped string", cursor)
			}
		case c >= 0x80:
			nonASCII, ascii = true, 0
			cursor++
		case c == nul:
			return "", 0, false, errors.ErrUnexpectedEndOfJSON("string", cursor)
		case c < 0x20:
			return "", 0, false, errors.ErrSyntax(fmt.Sprintf("invalid character %s in string literal", quoteChar(c)), cursor+1)
		default:
			// the words stopped where they may not be read, not at a byte to look at
			if wordsEnd != buflen {
				// the words stopped at wordsEnd, not at a byte to look at: the rest is scanned by SIMD
				return d.decodeStringRest(ctx, buf[start:cursor], cursor, stringInfo{firstEscape: int(firstEscape), nonASCII: nonASCII})
			}
			cursor++
		}
		for cursor+8 <= wordsEnd {
			w := load64(buf, cursor)
			if stop := keyEndBytes(w) | w&ascii; stop != 0 {
				cursor += int64(bits.TrailingZeros64(stop) / 8)
				break
			}
			cursor += 8
		}
	}
}

// skipOtherValue returns the error of scanString for a value which is not a string, or, for a value of another
// kind, which scanString reports by an error at its position, records its type error and skips it. The skipped
// value is not nested in the ones before it: its depth is counted from 0.
//
//go:noinline
func (d *stringDecoder) skipOtherValue(ctx *RuntimeContext, err error) (string, int64, bool, error) {
	var cursor int64
	switch e := err.(type) {
	case *errors.UnmarshalTypeError:
		// a number, an array or an object
		cursor = e.Offset
	case *errors.SyntaxError:
		// true and false start no string
		if c := ctx.Buf[e.Offset]; c != 't' && c != 'f' {
			return "", 0, false, err
		}
		cursor = e.Offset
	default:
		return "", 0, false, err
	}
	next, err := ctx.skipTypeError(cursor, 0, d.typ)
	return "", next, false, err
}

// decodeString returns the string at cursor as a value to store, and false for null.
// An escaped string is decoded out of the buffer directly.
func (d *stringDecoder) decodeString(ctx *RuntimeContext, cursor int64) (string, int64, bool, error) {
	literal, next, info, err := d.scanString(ctx.Buf, cursor)
	if next < 0 {
		literal, next, info, err = d.scanStringRest(ctx.Buf, literal, -next-1, info)
	}
	if err != nil || literal == nil {
		return "", next, false, err
	}
	if info.firstEscape >= 0 {
		return ctx.unescapeString(literal, info), next, true, nil
	}
	if info.nonASCII {
		literal = validLiteral(literal)
	}
	return ctx.makeString(literal), next, true, nil
}

// plainStringEnd returns the position of the quote which ends the string whose bytes start at start, if its bytes
// are all ASCII and none of them is escaped, and -1 for any other string, which the caller decodes by decodeString.
// Most of the keys and many of the values are such strings: this is their scan alone, eight bytes at a time, with
// nothing to keep about the bytes it passes. A word is read only where the buffer has room for it, so that a string
// at the end of the buffer is left to decodeString too.
func plainStringEnd(buf []byte, start int64) int64 {
	cursor := start
	buflen := int64(len(buf))
	for cursor+8 <= buflen {
		w := load64(buf, cursor)
		// keyEndBytes has no false bit before its first true one, and none at a byte which is not ASCII, which the top
		// bits add: the first bit is the first byte which ends the plain bytes.
		if stop := keyEndBytes(w) | w&msb; stop != 0 {
			cursor += int64(bits.TrailingZeros64(stop) / 8)
			if buf[cursor] == '"' {
				return cursor
			}
			return -1
		}
		cursor += 8
	}
	return -1
}

// scanString finds the string at cursor, and returns its bytes as they are in buf, the position after it and
// what it found in it, or nil for null.
//
// Most strings are short, and scanned by its loop, which calls nothing so that it keeps its values in the
// registers. After 64 bytes of plain bytes, it stops and returns the bytes of the string so far, -1 minus the
// position it stopped at, and what it found so far: the caller continues the scan by scanStringRest, which scans
// the runs of plain bytes of a long string by SIMD where the CPU has it. The position is negative so that the
// result has no field more, whose return every string would pay for.
func (d *stringDecoder) scanString(buf []byte, cursor int64) ([]byte, int64, stringInfo, error) {
	for {
		switch buf[cursor] {
		case ' ', '\n', '\t', '\r':
			cursor++
		case '[':
			return nil, 0, stringInfo{}, d.errUnmarshalType("array", cursor)
		case '{':
			return nil, 0, stringInfo{}, d.errUnmarshalType("object", cursor)
		case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			return nil, 0, stringInfo{}, d.errUnmarshalType("number", cursor)
		case '"':
			cursor++
			start := cursor
			b := (*sliceHeader)(unsafe.Pointer(&buf)).data
			buflen := int64(len(buf))
			firstEscape := int64(-1)
			// high accumulates the bytes of the string: it is not zero if one of them is not ASCII.
			var high uint64
			// The words are read up to wordsEnd: the end of the buffer, or, where the CPU has a SIMD scan, 64
			// bytes after the start, after which the rest of a long string is left to scanStringRest ( see
			// scanString ). It is looked at once for a string, not for every word.
			wordsEnd := buflen
			if hasStringSIMD {
				wordsEnd = min(buflen, start+64)
			}
			for {
				// The words with nothing to look at are skipped eight bytes at a time: a word is read only where
				// the buffer has room for it, so that its end is scanned byte by byte.
				for cursor+8 <= wordsEnd {
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
				c := char(b, cursor)
				switch c {
				case '\\':
					if firstEscape < 0 {
						firstEscape = cursor - start
					}
					cursor++
					switch char(b, cursor) {
					case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
						cursor++
					case 'u':
						if cursor+5 >= buflen {
							return nil, 0, stringInfo{}, errors.ErrUnexpectedEndOfJSON("escaped string", cursor)
						}
						for i := int64(1); i <= 4; i++ {
							c := char(b, cursor+i)
							if !(('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')) {
								return nil, 0, stringInfo{}, errors.ErrSyntax(fmt.Sprintf("json: invalid character %c in \\u hexadecimal character escape", c), cursor+i)
							}
						}
						cursor += 5
					default:
						return nil, 0, stringInfo{}, errors.ErrUnexpectedEndOfJSON("escaped string", cursor)
					}
				case '"':
					return buf[start:cursor], cursor + 1, stringInfo{firstEscape: int(firstEscape), nonASCII: high&msb != 0}, nil
				case nul:
					return nil, 0, stringInfo{}, errors.ErrUnexpectedEndOfJSON("string", cursor)
				default:
					if c < 0x20 {
						return nil, 0, stringInfo{}, errors.ErrSyntax(fmt.Sprintf("invalid character %s in string literal", quoteChar(c)), cursor+1)
					}
					if wordsEnd != buflen {
						// the words stopped at wordsEnd, not at a byte to look at
						return buf[start:cursor], -cursor - 1, stringInfo{firstEscape: int(firstEscape), nonASCII: high&msb != 0}, nil
					}
					high |= uint64(c)
					cursor++
				}
			}
		case 'n':
			if err := validateNull(buf, cursor); err != nil {
				return nil, 0, stringInfo{}, err
			}
			cursor += 4
			return nil, cursor, stringInfo{}, nil
		default:
			return nil, 0, stringInfo{}, errors.ErrInvalidBeginningOfValue(buf[cursor], cursor)
		}
	}
}

// scanStringRest continues the scan of a long string from where scanString stopped, which returned the bytes
// of the string so far, the position it stopped at and what it found so far: the runs of plain bytes are scanned
// by SIMD ( see indexStringSpecial ), and a run near the end of the buffer by words. The escapes are validated as
// scanString does.
func (d *stringDecoder) scanStringRest(buf, literal []byte, cursor int64, info stringInfo) ([]byte, int64, stringInfo, error) {
	start := cursor - int64(len(literal))
	firstEscape := int64(info.firstEscape)
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
			if firstEscape < 0 {
				firstEscape = cursor - start
			}
			cursor++
			switch char(b, cursor) {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				cursor++
			case 'u':
				if cursor+5 >= buflen {
					return nil, 0, stringInfo{}, errors.ErrUnexpectedEndOfJSON("escaped string", cursor)
				}
				for i := int64(1); i <= 4; i++ {
					c := char(b, cursor+i)
					if !(('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')) {
						return nil, 0, stringInfo{}, errors.ErrSyntax(fmt.Sprintf("json: invalid character %c in \\u hexadecimal character escape", c), cursor+i)
					}
				}
				cursor += 5
			default:
				return nil, 0, stringInfo{}, errors.ErrUnexpectedEndOfJSON("escaped string", cursor)
			}
		case '"':
			return buf[start:cursor], cursor + 1, stringInfo{firstEscape: int(firstEscape), nonASCII: high&msb != 0}, nil
		case nul:
			return nil, 0, stringInfo{}, errors.ErrUnexpectedEndOfJSON("string", cursor)
		default:
			if c < 0x20 {
				return nil, 0, stringInfo{}, errors.ErrSyntax(fmt.Sprintf("invalid character %s in string literal", quoteChar(c)), cursor+1)
			}
			high |= uint64(c)
			cursor++
		}
	}
}

// coerceUTF8 returns a copy of the literal in which every byte of an invalid UTF-8 sequence
// is replaced by utf8.RuneError, as encoding/json does.
func coerceUTF8(literal []byte) []byte {
	return jsonstring.AppendValidUTF8(make([]byte, 0, len(literal)+2*utf8.UTFMax), literal)
}

// quoteChar formats c as a quoted character literal, as encoding/json does in its errors.
func quoteChar(c byte) string {
	if c == '\'' {
		return `'\''`
	}
	if c == '"' {
		return `'"'`
	}
	s := strconv.Quote(string(c))
	return "'" + s[1:len(s)-1] + "'"
}
