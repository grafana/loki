package decoder

import (
	"math/bits"
	"unsafe"
)

// The values which are not decoded, as the ones of the keys which match no field, are skipped by the grammar of
// JSON, as encoding/json checks the whole input: a value which is not valid is a syntax error wherever it is.

// skipState is where a skip of an object or an array is: what it looks for next.
type skipState uint8

const (
	// skipValueStart looks for a value.
	skipValueStart skipState = iota
	// skipAfterValue looks for what follows a value: a comma or the end of the object or the array it is in.
	skipAfterValue
)

// skipRestOfObject returns the position after the end of the object whose value ends at cursor, which it checks by
// the grammar of JSON: nesting is how deep the object is nested in the input.
func skipRestOfObject(buf []byte, cursor, nesting int64) (int64, error) {
	return skipGrammarFast(buf, cursor, nesting-1, 1, 1, resumeAfterValue)
}

// skipResume is where skipFast resumes: at a value, after a value, or after the key of an object.
type skipResume uint8

const (
	resumeValue skipResume = iota
	resumeAfterValue
	resumeAfterKey
	// resumeKey is where a key is waited for, which skipFast doesn't resume at: it tells the error of the byte.
	resumeKey
)

// skipEvent is why skipFast returns: the end of the value, or what it doesn't do itself, which its caller does
// before it resumes it.
type skipEvent uint8

const (
	skipDone skipEvent = iota
	// skipAString is a string which has an escape or a control character, or is at the end of the buffer.
	skipAString
	// skipANumber is a number which starts with a minus sign or is not an integer.
	skipANumber
	// skipDeep is an object or an array deeper than the levels which the bits of a word hold.
	skipDeep
	// skipInvalid is a byte which the grammar doesn't have where it is, for which the caller makes the error.
	skipInvalid
)

// skipGrammarFast skips the objects and the arrays which are open, level of them whose kinds are the bits of
// objects, from cursor, where it resumes, by skipFast, which returns for what it doesn't do: a string with an
// escape, a number which is not an integer, a byte which is not valid, which is done here, or an object or an array
// deeper than 64 levels, which the rest of the value is skipped by skipGrammar for.
func skipGrammarFast(buf []byte, cursor, nesting, level int64, objects uint64, resume skipResume) (int64, error) {
	var ev skipEvent
	cursor, level, objects, resume, ev = skipFast(buf, cursor, level, objects, resume, skipMaxLevel(nesting))
	if ev == skipDone {
		return cursor, nil
	}
	return skipGrammarEvent(buf, cursor, nesting, level, objects, resume, ev)
}

// skipMaxLevel returns the number of the levels of objects and arrays which skipFast opens at most, for a value
// nested nesting levels deep.
func skipMaxLevel(nesting int64) int64 {
	return min(maxDecodeNestingDepth-nesting, 64)
}

// skipGrammarEvent does what skipFast stopped for at cursor, ev, and resumes it, up to the end of the value.
func skipGrammarEvent(buf []byte, cursor, nesting, level int64, objects uint64, resume skipResume, ev skipEvent) (int64, error) {
	maxLevel := skipMaxLevel(nesting)
	for {
		switch ev {
		case skipDone:
			return cursor, nil
		case skipAString:
			end, err := skipString(buf, cursor)
			if err != nil {
				return 0, stringError(buf, cursor, err)
			}
			cursor = end
		case skipANumber:
			end, err := skipNumber(buf, cursor)
			if err != nil {
				return 0, err
			}
			cursor = end
		case skipDeep:
			// the value at cursor opens the level 64, or the limit of the nesting
			return skipGrammar(buf, cursor, nesting, level, objects, skipValueStart)
		default:
			return 0, skipFastError(buf, cursor, level, objects, resume)
		}
		cursor, level, objects, resume, ev = skipFast(buf, cursor, level, objects, resume, maxLevel)
	}
}

// skipFastError returns the syntax error of the byte at cursor, which skipFast stopped at, where it resumes.
func skipFastError(buf []byte, cursor, level int64, objects uint64, resume skipResume) error {
	switch resume {
	case resumeKey:
		return syntaxErrorAt(buf, cursor, whereKey)
	case resumeAfterKey:
		return syntaxErrorAt(buf, cursor, whereAfterKey)
	case resumeAfterValue:
		if objects&(1<<uint(level-1)) != 0 {
			return syntaxErrorAt(buf, cursor, whereAfterPair)
		}
		return syntaxErrorAt(buf, cursor, whereAfterElement)
	}
	switch buf[cursor] {
	case 't':
		return literalSyntaxError(buf, cursor, "true")
	case 'f':
		return literalSyntaxError(buf, cursor, "false")
	case 'n':
		return literalSyntaxError(buf, cursor, "null")
	}
	return syntaxErrorAt(buf, cursor, whereValue)
}

// skipFast skips from cursor, where it resumes, the objects and the arrays which are open, level of them whose
// kinds are the bits of objects, set for an object, up to the end of the outermost of them, and returns where it
// is and why it stops: at the end, or at what it doesn't do itself ( see skipEvent ), after which its caller
// resumes it. It calls nothing, so that its loop keeps its state in the registers.
//
// For skipInvalid, resume is the state at the byte: resumeValue for a value, resumeAfterValue after a value,
// resumeKey where a key is waited for and resumeAfterKey where its colon is.
//
//nolint:maintidx // one loop of gotos without calls, whose state stays in the registers
func skipFast(buf []byte, cursor, level int64, objects uint64, resume skipResume, maxLevel int64) (int64, int64, uint64, skipResume, skipEvent) {
	length := int64(len(buf))
	// the bytes are read without the checks of the bounds: every loop stops at the nul byte at the end of the
	// buffer, and a literal is compared byte by byte up to the first byte which differs
	b := (*sliceHeader)(unsafe.Pointer(&buf)).data
	// c is the byte at cursor after the white spaces are skipped: a byte above ' ' isn't one, so that the table
	// of the white spaces is read only for the bytes which may be
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
	// a string, which most values are, before the switch of the others
	if c == '"' {
		// a string of plain bytes up to its quote, eight bytes at a time
		for c := cursor + 1; c+8 <= length; c += 8 {
			if special := keyEndBytes(load64(buf, c)); special != 0 {
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
			// a number which is not an integer, which skipNumber checks from its start
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
	// a value here is in an object or an array: the value which skipFast starts at is one of them, and the end of
	// the outermost is returned at closed
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
	// a key of plain bytes, as the strings of the values
	for c := cursor + 1; c+8 <= length; c += 8 {
		if special := keyEndBytes(load64(buf, c)); special != 0 {
			if c += int64(bits.TrailingZeros64(special) / 8); char(b, c) == '"' {
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
	// the single space after the colon, which most objects have, is skipped before the loop over the others
	if char(b, cursor) == ' ' {
		cursor++
	}
	goto value
closed:
	// the end of an object or an array, which cursor is after
	if level--; level == 0 {
		return cursor, level, objects, resumeAfterValue, skipDone
	}
	goto afterValue
}

// skipGrammar skips from cursor, in state, the values of the objects and the arrays which are open, level of them
// the innermost of which is an object, up to the end of the outermost of them, or the value at cursor if none is
// open, and returns the position after it.
//
// The kinds of the open values are the bits of a word, set for an object, for the first 64 levels, and of the
// words of deeper, which are allocated as the nesting gets deeper: the loop keeps them in its registers.
//
//nolint:maintidx // one loop of gotos without calls, whose state stays in the registers
func skipGrammar(buf []byte, cursor, nesting, level int64, objects uint64, state skipState) (int64, error) {
	maxLevel := maxDecodeNestingDepth - nesting
	length := int64(len(buf))
	// the bytes are read without the checks of the bounds: every loop stops at the nul byte at the end of the
	// buffer, and a literal is compared byte by byte up to the first byte which differs
	b := (*sliceHeader)(unsafe.Pointer(&buf)).data
	var deeper []uint64
	if state == skipAfterValue {
		goto afterValue
	}
value:
	for isWhiteSpace[char(b, cursor)] {
		cursor++
	}
	switch c := char(b, cursor); c {
	case '{', '[':
		if level+1 > maxLevel {
			return 0, depthSyntaxError(buf, cursor)
		}
		if level < 64 {
			if c == '{' {
				objects |= 1 << uint(level)
			} else {
				objects &^= 1 << uint(level)
			}
		} else {
			deeper = pushDeeper(deeper, level, c == '{')
		}
		level++
		cursor++
		for isWhiteSpace[char(b, cursor)] {
			cursor++
		}
		if c == '{' {
			if char(b, cursor) == '}' {
				cursor++
				level--
				goto afterValue
			}
			goto key
		}
		if char(b, cursor) == ']' {
			cursor++
			level--
			goto afterValue
		}
		goto value
	case '"':
		// a string of plain bytes up to its quote without a call, eight bytes at a time, and any other by
		// skipString, which checks its escapes
		for c := cursor + 1; c+8 <= length; c += 8 {
			if special := keyEndBytes(load64(buf, c)); special != 0 {
				if c += int64(bits.TrailingZeros64(special) / 8); char(b, c) == '"' {
					cursor = c + 1
					goto afterValue
				}
				break
			}
		}
		end, err := skipString(buf, cursor)
		if err != nil {
			return 0, stringError(buf, cursor, err)
		}
		cursor = end
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		// an integer, which most numbers are, without a call
		start := cursor
		cursor++
		for char(b, cursor)-'0' <= 9 {
			cursor++
		}
		if c := char(b, cursor); c == '.' || c == 'e' || c == 'E' {
			end, err := skipNumberRest(buf, start, cursor)
			if err != nil {
				return 0, err
			}
			cursor = end
		}
	case '0':
		start := cursor
		cursor++
		if c := char(b, cursor); c == '.' || c == 'e' || c == 'E' {
			end, err := skipNumberRest(buf, start, cursor)
			if err != nil {
				return 0, err
			}
			cursor = end
		}
	case '-':
		end, err := skipNumber(buf, cursor)
		if err != nil {
			return 0, err
		}
		cursor = end
	case 't':
		if char(b, cursor+1) != 'r' || char(b, cursor+2) != 'u' || char(b, cursor+3) != 'e' {
			return 0, literalSyntaxError(buf, cursor, "true")
		}
		cursor += 4
	case 'f':
		if char(b, cursor+1) != 'a' || char(b, cursor+2) != 'l' || char(b, cursor+3) != 's' || char(b, cursor+4) != 'e' {
			return 0, literalSyntaxError(buf, cursor, "false")
		}
		cursor += 5
	case 'n':
		if char(b, cursor+1) != 'u' || char(b, cursor+2) != 'l' || char(b, cursor+3) != 'l' {
			return 0, literalSyntaxError(buf, cursor, "null")
		}
		cursor += 4
	default:
		return 0, syntaxErrorAt(buf, cursor, whereValue)
	}
afterValue:
	if level == 0 {
		return cursor, nil
	}
	for isWhiteSpace[char(b, cursor)] {
		cursor++
	}
	if inObject(objects, deeper, level-1) {
		switch char(b, cursor) {
		case ',':
			cursor++
			for isWhiteSpace[char(b, cursor)] {
				cursor++
			}
			goto key
		case '}':
			cursor++
			level--
			goto afterValue
		}
		return 0, syntaxErrorAt(buf, cursor, whereAfterPair)
	}
	switch char(b, cursor) {
	case ',':
		cursor++
		goto value
	case ']':
		cursor++
		level--
		goto afterValue
	}
	return 0, syntaxErrorAt(buf, cursor, whereAfterElement)
key:
	if char(b, cursor) != '"' {
		return 0, syntaxErrorAt(buf, cursor, whereKey)
	}
	// a key of plain bytes without a call, as the strings of the values
	for c := cursor + 1; c+8 <= length; c += 8 {
		if special := keyEndBytes(load64(buf, c)); special != 0 {
			if c += int64(bits.TrailingZeros64(special) / 8); char(b, c) == '"' {
				cursor = c + 1
				goto afterKey
			}
			break
		}
	}
	{
		end, err := skipString(buf, cursor)
		if err != nil {
			return 0, stringError(buf, cursor, err)
		}
		cursor = end
	}
afterKey:
	for isWhiteSpace[char(b, cursor)] {
		cursor++
	}
	if char(b, cursor) != ':' {
		return 0, syntaxErrorAt(buf, cursor, whereAfterKey)
	}
	cursor++
	goto value
}

// inObject reports whether the value open at level is an object.
func inObject(objects uint64, deeper []uint64, level int64) bool {
	if level < 64 {
		return objects&(1<<uint(level)) != 0
	}
	return deeper[(level-64)/64]&(1<<uint((level-64)%64)) != 0
}

// pushDeeper sets the kind of the value open at level, which is 64 or deeper, in deeper, which it returns.
func pushDeeper(deeper []uint64, level int64, object bool) []uint64 {
	i, bit := (level-64)/64, uint((level-64)%64)
	for int64(len(deeper)) <= i {
		deeper = append(deeper, 0)
	}
	if object {
		deeper[i] |= 1 << bit
	} else {
		deeper[i] &^= 1 << bit
	}
	return deeper
}

// skipNumber returns the position after the number at cursor, which it checks by the grammar of JSON.
func skipNumber(buf []byte, cursor int64) (int64, error) {
	c := cursor
	if buf[c] == '-' {
		c++
	}
	switch {
	case buf[c] == '0':
		c++
	case buf[c]-'1' <= 8:
		c = skipDigits(buf, c+1)
	default:
		return 0, numberSyntaxError(buf, c, cursor, whereNumber)
	}
	return skipNumberRest(buf, cursor, c)
}

// skipNumberRest returns the position after the fraction and the exponent of the number which starts at start and
// whose integer ends at c, which it checks by the grammar of JSON.
func skipNumberRest(buf []byte, start, c int64) (int64, error) {
	if buf[c] == '.' {
		c++
		if buf[c]-'0' > 9 {
			return 0, numberSyntaxError(buf, c, start, whereFraction)
		}
		c = skipDigits(buf, c)
	}
	if buf[c] == 'e' || buf[c] == 'E' {
		c++
		if buf[c] == '+' || buf[c] == '-' {
			c++
		}
		if buf[c]-'0' > 9 {
			return 0, numberSyntaxError(buf, c, start, whereExponent)
		}
		c = skipDigits(buf, c)
	}
	return c, nil
}

// stringError returns the syntax error of the string at cursor, which skipString failed with err, as encoding/json
// has it.
func stringError(buf []byte, cursor int64, err error) error {
	if serr := stringSyntaxError(buf, cursor); serr != nil {
		return serr
	}
	return err
}

// InputSyntaxError returns the first syntax error of the input of the decoding, as encoding/json reports it, or
// nil if the input is valid: the decoders report a syntax error as they meet it, which this reports again as
// encoding/json does, which checks the whole input first. The input is copied again, since the decoding writes
// its buffer, as the escapes of the keys are decoded in it.
func (ctx *RuntimeContext) InputSyntaxError() error {
	return inputSyntaxError(NewInput(ctx.origin))
}

// inputSyntaxError returns the first syntax error of buf, the input followed by the nul byte, or nil.
func inputSyntaxError(buf []byte) error {
	end, err := skipValue(buf, skipWhiteSpace(buf, 0), 0)
	if err != nil {
		return err
	}
	return ValidateEnd(buf, end)
}

// Valid reports whether buf, the input followed by the nul byte, is a JSON value, by the grammar which the values
// which are not decoded are skipped by.
func Valid(buf []byte) bool {
	end, err := skipValue(buf, skipWhiteSpace(buf, 0), 0)
	return err == nil && skipWhiteSpace(buf, end) == int64(len(buf))-1
}

// ValidateEnd returns the syntax error of what follows the value which ends at cursor in buf, the input followed by
// the nul byte: white spaces only may follow it, up to the end of the input. A nul byte in the input is a byte which
// the grammar doesn't have there, as encoding/json has it.
func ValidateEnd(buf []byte, cursor int64) error {
	cursor = skipWhiteSpace(buf, cursor)
	if cursor == int64(len(buf))-1 {
		return nil
	}
	return syntaxErrorAt(buf, cursor, whereAfterTop)
}
