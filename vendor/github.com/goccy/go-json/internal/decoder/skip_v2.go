package decoder

import (
	"bytes"
	"math/bits"
	"unicode/utf8"
	"unsafe"

	"github.com/goccy/go-json/internal/textcoder"
)

// The values which v2 skips, as the ones of the names of no field, are skipped by the grammar of JSON as v1 skips
// them, and must have the text which v2 allows ( see textcoder.ValidValue ): valid UTF-8 and no escape of a surrogate
// which is not a pair, unless V2AllowInvalidUTF8, and no object with a name twice, unless V2AllowDuplicateNames. With
// both, a value is skipped as v1 skips it. Otherwise it is read once, by the loop of skipFast with the bytes which are
// not ASCII and the names of the objects too ( see skipFastV2 ).

// skipValueV2 skips the value at cursor, which v2 reads whole, nested in depth objects and arrays.
func (ctx *RuntimeContext) skipValueV2(cursor, depth int64) (int64, error) {
	buf := ctx.Buf
	if ctx.Option.V2&asV1Text == asV1Text {
		return skipValue(buf, cursor, depth)
	}
	start := skipWhiteSpace(buf, cursor)
	switch buf[start] {
	case '{', '[':
		return skipNestedV2(ctx, start, depth)
	case '"':
		if ctx.Option.V2&V2AllowInvalidUTF8 == 0 {
			return skipStringV2(buf, start)
		}
	}
	// a number and a literal, which are of ASCII, and a string with V2AllowInvalidUTF8, which has no name
	return skipValue(buf, start, depth)
}

// skipStringV2 returns the position after the string at cursor, which it checks by the grammar of JSON, and for the
// text which v2 allows, in one scan of the string: a string of no byte which is not ASCII and no escape has it. The
// words of such a string, which most are, are looked at by the loop here up to its quote, as skipString looks at
// them, where the buffer has room for them; any other string is scanned as a string is decoded.
func skipStringV2(buf []byte, cursor int64) (int64, error) {
	wordsEnd := int64(len(buf))
	if hasStringSIMD {
		wordsEnd = min(wordsEnd, cursor+1+64)
	}
	for c := cursor + 1; c+8 <= wordsEnd; c += 8 {
		w := load64(buf, c)
		if special := keyEndBytes(w) | w&msb; special != 0 {
			if c += int64(bits.TrailingZeros64(special) / 8); buf[c] == '"' {
				return c + 1, nil
			}
			break
		}
	}
	literal, next, info, err := skipStringDecoder.scanString(buf, cursor)
	if next < 0 {
		_, next, info, err = skipStringDecoder.scanStringRest(buf, literal, -next-1, info)
	}
	if err != nil {
		return 0, stringError(buf, cursor, err)
	}
	if (info.nonASCII || info.firstEscape >= 0) && !validV2Text(buf[cursor:next]) {
		return 0, errV2Text
	}
	return next, nil
}

// skipNestedV2 skips the object or the array at start, nested in depth objects and arrays, by skipFastV2, which
// returns for what it doesn't do: a string of an escape or of a byte which is not ASCII, which is done here, a number
// which is not an integer, a byte which is not valid, a name twice, or an object or an array deeper than skipLevels
// levels, for which the value is skipped again from start, as v1 skips it, and read by jsontext for its text.
func skipNestedV2(ctx *RuntimeContext, start, depth int64) (int64, error) {
	buf := ctx.Buf
	call := ctx.V2Call()
	// high is the top bit of the bytes of the strings, which stops the words of a string at a byte which is not ASCII
	// if the strings must be valid UTF-8, and names whether the names of the objects are kept
	var high uint64
	if ctx.Option.V2&V2AllowInvalidUTF8 == 0 {
		high = msb
	}
	names := ctx.Option.V2&V2AllowDuplicateNames == 0
	// the names of the call before the value, which the objects of the value which are open where it stops before its
	// end leave after them
	entries, copies := call.names.mark()
	maxLevel := skipMaxLevel(depth)
	cursor, level, objects, resume, ev := skipFastV2(buf, start, 0, 0, resumeValue, maxLevel, high, names, call)
	for {
		var err error
		switch ev {
		case skipDone:
			return cursor, nil
		case skipAString:
			var end int64
			if end, err = skipStringOfV2(buf, cursor, high); err == nil {
				if names && resume == resumeAfterKey && !call.skipNames[level-1].addName(call, buf, cursor, end) {
					err = errV2Text
				}
				cursor = end
			}
		case skipANumber:
			cursor, err = skipNumber(buf, cursor)
		case skipNameTwice:
			err = errV2Text
		case skipDeep:
			dropSkipNames(call, level, entries, copies)
			return skipDeepV2(ctx, start, depth)
		default:
			err = skipFastError(buf, cursor, level, objects, resume)
		}
		if err != nil {
			dropSkipNames(call, level, entries, copies)
			return 0, err
		}
		cursor, level, objects, resume, ev = skipFastV2(buf, cursor, level, objects, resume, maxLevel, high, names, call)
	}
}

// dropSkipNames drops the names of the objects which are open at the levels below level, where skipNestedV2 stops
// before the end of its value, and the maps of their sets, which the context of the call would keep: the names of the
// call are back to the entries and the copies before the value.
func dropSkipNames(call *V2Call, level int64, entries, copies int) {
	call.names.truncate(entries, copies)
	for i := range call.skipNames[:level] {
		call.skipNames[i].set = nil
	}
}

// skipStringOfV2 returns the position after the string at cursor, which it checks by the grammar of JSON, and for the
// text which v2 allows if high is set, the top bit of the bytes ( see skipNestedV2 ).
func skipStringOfV2(buf []byte, cursor int64, high uint64) (int64, error) {
	if high != 0 {
		return skipStringV2(buf, cursor)
	}
	end, err := skipString(buf, cursor)
	if err != nil {
		return 0, stringError(buf, cursor, err)
	}
	return end, nil
}

// addName adds the name of an object which is skipped, the string from start up to end in buf: as the input has it,
// or decoded, if it has an escape or invalid UTF-8, which is replaced, as jsontext compares names. It reports whether
// the set didn't have it.
func (s *nameSet) addName(call *V2Call, buf []byte, start, end int64) bool {
	if quoted := buf[start:end]; bytes.IndexByte(quoted, '\\') >= 0 || !utf8.Valid(quoted) {
		return s.addCopy(call, buf, unquotedName(quoted))
	}
	return s.add(call, buf, start+1, end-1)
}

// skipDeepV2 skips the value at start, nested in depth objects and arrays, which is deeper than the levels of
// skipFastV2, as v1 skips it, and reads it by jsontext for its text.
func skipDeepV2(ctx *RuntimeContext, start, depth int64) (int64, error) {
	buf := ctx.Buf
	end, err := skipValue(buf, start, depth)
	if err != nil {
		return 0, err
	}
	if !textcoder.ValidValue(skipperOf(ctx), buf[start:end]) {
		return 0, errV2Text
	}
	return end, nil
}

// skipNameTwice is the event of skipFastV2 for a name of an object which the object had before.
const skipNameTwice = skipInvalid + 1

// skipFastV2 is skipFast for v2: the words of a string stop at a byte which is not ASCII too, if high is the top bit
// of the bytes, and the names of the objects are kept in the sets of skipNames of call, by their levels, if names is
// set, which a name twice is the event skipNameTwice of. A name of an escape or of a byte which is not ASCII is left
// to the caller, as the strings of an escape are, which adds it to the set of the level: invalid UTF-8, which high
// may let a string have, is replaced in a name before it is compared.
//
//nolint:maintidx // one loop of gotos without calls but for the names, whose state stays in the registers
func skipFastV2(
	buf []byte, cursor, level int64, objects uint64, resume skipResume, maxLevel int64, high uint64, names bool, call *V2Call,
) (int64, int64, uint64, skipResume, skipEvent) {
	length := int64(len(buf))
	// the bytes are read without the checks of the bounds: every loop stops at the nul byte at the end of the
	// buffer, and a literal is compared byte by byte up to the first byte which differs
	b := (*sliceHeader)(unsafe.Pointer(&buf)).data
	// keyHigh is high for the words of the names, which stop at a byte which is not ASCII if they are kept
	keyHigh := high
	if names {
		keyHigh = msb
	}
	var c byte
	switch resume {
	case resumeAfterValue:
		goto afterValue
	case resumeAfterKey:
		goto afterKey
	}
value:
	if c = char(b, cursor); c <= ' ' {
		for isWhiteSpace[c] {
			cursor++
			c = char(b, cursor)
		}
	}
	if c == '"' {
		for c := cursor + 1; c+8 <= length; c += 8 {
			w := load64(buf, c)
			if special := keyEndBytes(w) | w&high; special != 0 {
				if c += int64(bits.TrailingZeros64(special) / 8); char(b, c) == '"' {
					cursor = c + 1
					goto afterValue
				}
				break
			}
		}
		return cursor, level, objects, resumeAfterValue, skipAString
	}
	switch c {
	case '{', '[':
		if level+1 > maxLevel {
			return cursor, level, objects, resumeValue, skipDeep
		}
		if c == '{' {
			objects |= 1 << uint(level)
			if names {
				call.skipNames[level] = nameSet{}
			}
		} else {
			objects &^= 1 << uint(level)
		}
		level++
		cursor++
		open := c
		if c = char(b, cursor); c <= ' ' {
			for isWhiteSpace[c] {
				cursor++
				c = char(b, cursor)
			}
		}
		if open == '{' {
			if c == '}' {
				cursor++
				goto closed
			}
			goto key
		}
		if c == ']' {
			cursor++
			goto closed
		}
		goto value
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		start := cursor
		cursor++
		for char(b, cursor)-'0' <= 9 {
			cursor++
		}
		if c := char(b, cursor); c == '.' || c == 'e' || c == 'E' {
			return start, level, objects, resumeAfterValue, skipANumber
		}
	case '0':
		if c := char(b, cursor+1); c == '.' || c == 'e' || c == 'E' {
			return cursor, level, objects, resumeAfterValue, skipANumber
		}
		cursor++
	case '-':
		return cursor, level, objects, resumeAfterValue, skipANumber
	case 't':
		if char(b, cursor+1) != 'r' || char(b, cursor+2) != 'u' || char(b, cursor+3) != 'e' {
			return cursor, level, objects, resumeValue, skipInvalid
		}
		cursor += 4
	case 'f':
		if char(b, cursor+1) != 'a' || char(b, cursor+2) != 'l' || char(b, cursor+3) != 's' || char(b, cursor+4) != 'e' {
			return cursor, level, objects, resumeValue, skipInvalid
		}
		cursor += 5
	case 'n':
		if char(b, cursor+1) != 'u' || char(b, cursor+2) != 'l' || char(b, cursor+3) != 'l' {
			return cursor, level, objects, resumeValue, skipInvalid
		}
		cursor += 4
	default:
		return cursor, level, objects, resumeValue, skipInvalid
	}
afterValue:
	if c = char(b, cursor); c <= ' ' {
		for isWhiteSpace[c] {
			cursor++
			c = char(b, cursor)
		}
	}
	if objects&(1<<uint(level-1)) != 0 {
		switch c {
		case ',':
			cursor++
			if c = char(b, cursor); c <= ' ' {
				for isWhiteSpace[c] {
					cursor++
					c = char(b, cursor)
				}
			}
			goto key
		case '}':
			cursor++
			goto closed
		}
		return cursor, level, objects, resumeAfterValue, skipInvalid
	}
	switch c {
	case ',':
		cursor++
		goto value
	case ']':
		cursor++
		goto closed
	}
	return cursor, level, objects, resumeAfterValue, skipInvalid
key:
	if c != '"' {
		return cursor, level, objects, resumeKey, skipInvalid
	}
	for c := cursor + 1; c+8 <= length; c += 8 {
		w := load64(buf, c)
		if special := keyEndBytes(w) | w&keyHigh; special != 0 {
			if c += int64(bits.TrailingZeros64(special) / 8); char(b, c) == '"' {
				if names {
					if s := &call.skipNames[level-1]; !s.addListed(call, buf, cursor+1, c) && !s.add(call, buf, cursor+1, c) {
						return cursor, level, objects, resumeAfterKey, skipNameTwice
					}
				}
				cursor = c + 1
				goto afterKey
			}
			break
		}
	}
	return cursor, level, objects, resumeAfterKey, skipAString
afterKey:
	if c = char(b, cursor); c <= ' ' {
		for isWhiteSpace[c] {
			cursor++
			c = char(b, cursor)
		}
	}
	if c != ':' {
		return cursor, level, objects, resumeAfterKey, skipInvalid
	}
	cursor++
	if char(b, cursor) == ' ' {
		cursor++
	}
	goto value
closed:
	// the end of an object or an array, which cursor is after
	if names && objects&(1<<uint(level-1)) != 0 {
		call.skipNames[level-1].end(call)
	}
	if level--; level == 0 {
		return cursor, level, objects, resumeAfterValue, skipDone
	}
	goto afterValue
}
