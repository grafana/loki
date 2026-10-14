//go:build !go1.27 || !goexperiment.jsonv2

package decoder

import (
	"encoding/json"
	"reflect"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/runtime"
)

// The tokens of a stream as encoding/json before Go 1.27 reads them: a comma or a colon which the grammar doesn't
// have is an error of Token, which is not kept, and Decode reads a value where Token would.

// streamPeek is what More peeked at, which encoding/json before Go 1.27 doesn't keep: More and Token move the
// offset over the white spaces.
type streamPeek struct{}

// reset does nothing.
func (streamPeek) reset() {}

// valueStartMismatch returns nil: encoding/json before Go 1.27 tells the bracket where a value is waited for as
// the grammar does.
func (*Stream) valueStartMismatch(int64) error {
	return nil
}

// countsScanned is whether the offset of a syntax error of a value of Decode is counted from the bytes which the
// reads of the values read, as encoding/json before Go 1.27 counts it: the white spaces and the delimiters which
// Token or More read, and the comma or the colon before a value of Decode, are not counted.
const countsScanned = true

// prepareInTokens moves the cursor to the value which Decode reads between the tokens: after the comma or the
// colon which the state waits for. It is not inlined, so that Decode keeps the size it had.
//
//go:noinline
func (s *Stream) prepareInTokens() error {
	switch s.tokenState {
	case tokenFailed:
		return s.err
	case tokenArrayComma:
		// the comma and the colon are looked for without an error kept, as encoding/json does
		if !s.peek() {
			return s.endError()
		}
		if s.buf[s.cursor] != ',' {
			return errors.ErrSyntax("expected comma after array element", s.TotalOffset())
		}
		s.cursor++
		s.tokenState = tokenArrayValue
		// the value is read after the comma
		s.markPrevEnd()
		s.readStart = s.offset + s.cursor
	case tokenObjectColon:
		if !s.peek() {
			return s.endError()
		}
		if s.buf[s.cursor] != ':' {
			return errors.ErrSyntax("expected colon after object key", s.TotalOffset())
		}
		s.cursor++
		s.tokenState = tokenObjectValue
		// the value is read after the colon
		s.markPrevEnd()
		s.readStart = s.offset + s.cursor
	default:
		// the value is read from the cursor, white spaces before it too
		s.readStart = s.offset + s.cursor
	}
	if !s.tokenValueAllowed() {
		return errors.ErrSyntax("not at beginning of value", s.TotalOffset())
	}
	if !s.skipWhiteSpace() {
		return s.fail(s.endError())
	}
	return nil
}

// Token returns the next token of the stream: a delimiter, a string, a number, a bool or nil. The commas and the
// colons are consumed where the grammar has them.
func (s *Stream) Token() (any, error) {
	if s.tokenState == tokenFailed {
		return nil, s.err
	}
	for {
		if !s.peek() {
			return nil, s.endError()
		}
		c := s.buf[s.cursor]
		switch c {
		case '[', '{':
			if !s.tokenValueAllowed() {
				return nil, s.tokenError(c)
			}
			s.cursor++
			s.tokenStack = append(s.tokenStack, s.tokenState)
			if c == '[' {
				s.tokenState = tokenArrayStart
			} else {
				s.tokenState = tokenObjectStart
			}
			return json.Delim(c), nil
		case ']', '}':
			if c == ']' && s.tokenState != tokenArrayStart && s.tokenState != tokenArrayComma ||
				c == '}' && s.tokenState != tokenObjectStart && s.tokenState != tokenObjectComma {
				return nil, s.tokenError(c)
			}
			s.cursor++
			s.tokenState = s.tokenStack[len(s.tokenStack)-1]
			s.tokenStack = s.tokenStack[:len(s.tokenStack)-1]
			s.tokenValueEnd()
			return json.Delim(c), nil
		case ':':
			if s.tokenState != tokenObjectColon {
				return nil, s.tokenError(c)
			}
			s.cursor++
			s.tokenState = tokenObjectValue
			continue
		case ',':
			switch s.tokenState {
			case tokenArrayComma:
				s.tokenState = tokenArrayValue
			case tokenObjectComma:
				s.tokenState = tokenObjectKey
			default:
				return nil, s.tokenError(c)
			}
			s.cursor++
			continue
		case '"':
			if s.tokenState == tokenObjectStart || s.tokenState == tokenObjectKey {
				key, err := s.tokenString()
				if err != nil {
					return nil, err
				}
				s.tokenState = tokenObjectColon
				return key, nil
			}
		}
		if !s.tokenValueAllowed() {
			return nil, s.tokenError(c)
		}
		// a value is read as Decode reads it into an interface{}, as encoding/json before Go 1.27 reads it: its
		// errors, and the bytes which the reads of the values read ( see countsScanned ), are the ones of Decode
		dec, err := s.DecoderOf(anyPtrType)
		if err != nil {
			return nil, err
		}
		var v any
		if err := s.Decode(dec, anyPtrType, unsafe.Pointer(&v)); err != nil {
			return nil, err
		}
		return v, nil
	}
}

// anyPtrType is the type of *interface{}, which Token decodes a value into.
var anyPtrType = runtime.TypePtr(reflect.TypeOf((*any)(nil)))

// tokenError returns the syntax error of the byte c at the cursor, which the grammar doesn't have in the state, as
// encoding/json has it.
func (s *Stream) tokenError(c byte) error {
	var context string
	switch s.tokenState {
	case tokenTopValue, tokenArrayStart, tokenArrayValue, tokenObjectValue:
		context = " looking for beginning of value"
	case tokenArrayComma:
		context = " after array element"
	case tokenObjectKey:
		context = " looking for beginning of object key string"
	case tokenObjectColon:
		context = " after object key"
	case tokenObjectComma:
		context = " after object key:value pair"
	}
	return errors.ErrSyntax("invalid character "+quoteChar(c)+context, s.TotalOffset())
}

// InputOffset returns the offset of the stream which Decoder.InputOffset reports: the number of the bytes
// consumed from the reader, or, after a read failed, the end of the value before it.
func (s *Stream) InputOffset() int64 {
	if s.tokenState == tokenFailed {
		return s.errOffset
	}
	return s.TotalOffset()
}

// More reports whether the current array or object has another element.
func (s *Stream) More() bool {
	if !s.peek() {
		return false
	}
	switch s.buf[s.cursor] {
	case ']', '}':
		return false
	}
	return true
}

// tokenValueAllowed reports whether a value may be read in the state.
func (s *Stream) tokenValueAllowed() bool {
	switch s.tokenState {
	case tokenTopValue, tokenArrayStart, tokenArrayValue, tokenObjectValue:
		return true
	}
	return false
}
