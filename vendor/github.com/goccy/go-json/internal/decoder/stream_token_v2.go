//go:build go1.27 && goexperiment.jsonv2

package decoder

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"unicode/utf8"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
)

// The tokens of a stream as encoding/json of Go 1.27 reads them, by encoding/json/jsontext: a token or a value is
// read after the comma or the colon which the grammar has before it, which is checked against the kind of the
// token, and a read which fails leaves the stream where it was. A value may be read where a key is waited for,
// as the key.

// streamPeek is what More peeked at: encoding/json of Go 1.27 reports by InputOffset the start of the next token
// after More, until a value or a token is read.
type streamPeek struct {
	// peeked is whether More was called after the last value or token which was read.
	peeked bool
	// offset is the total offset of the next token which More peeked at, or of the end of the value or the token
	// before it if the input has none.
	offset int64
}

// reset forgets what More peeked at, after a value or a token is read.
func (p *streamPeek) reset() {
	p.peeked = false
}

// countsScanned is whether the offset of a syntax error of a value of Decode is counted from the bytes which the
// reads of the values read: encoding/json of Go 1.27 reports the offset in the whole input.
const countsScanned = false

// invalidCharacter returns the syntax error of the character at the total offset, which the grammar doesn't
// have where it is: encoding/json of Go 1.27 reports the rune there. The bytes of the rune are read if the buffer
// ends in it, and the ones from at are kept for it.
func (s *Stream) invalidCharacter(at int64, where string) error {
	cursor := s.TotalOffset()
	s.cursor = min(cursor, at) - s.offset
	for s.length-(at-s.offset) < utf8.UTFMax && !utf8.FullRune(s.buf[at-s.offset:s.length]) && s.read() {
	}
	s.cursor = cursor - s.offset
	err := invalidCharacterError(s.buf[:s.length+1], at-s.offset, where)
	return s.totalOffsetError(err)
}

// tokenEnded is after Token read the end of the input: every read returns io.EOF.
const tokenEnded = tokenFailed + 1

// needDelim returns the comma or the colon which the grammar has before a token of the kind which starts with c,
// or 0 for none.
func (s *Stream) needDelim(c byte) byte {
	switch s.tokenState {
	case tokenObjectColon:
		return ':'
	case tokenArrayComma, tokenObjectComma:
		if c != '}' && c != ']' {
			return ','
		}
	}
	return 0
}

// readDelim reads, from start, the white spaces and the comma or the colon before the next token, which it checks
// against the token, and moves the state after it. It returns the byte which starts the token.
func (s *Stream) readDelim(start int64) (byte, error) {
	if !s.peek() {
		return 0, s.tokenEnd(start)
	}
	var delim byte
	at := s.TotalOffset()
	if c := s.buf[s.cursor]; c == ',' || c == ':' {
		delim = c
		s.cursor++
		if !s.skipWhiteSpace() {
			// the next token is not known: a string, which follows any comma or colon, is taken for it
			if s.needDelim('"') != delim {
				return 0, s.delimError('"', at)
			}
			return 0, s.tokenEnd(start)
		}
	}
	c := s.buf[s.cursor]
	if s.needDelim(c) != delim {
		// the byte after the white spaces: the comma or the colon, or else the token
		return 0, s.delimError(c, at)
	}
	switch {
	case delim == ',' && s.tokenState == tokenArrayComma:
		s.tokenState = tokenArrayValue
	case delim == ',':
		s.tokenState = tokenObjectKey
	case delim == ':':
		s.tokenState = tokenObjectValue
	}
	return c, nil
}

// delimError returns the error of the byte c at the total offset at, which is not the comma or the colon which the
// grammar has before a token which starts with next.
func (s *Stream) delimError(next byte, at int64) error {
	where := "looking for beginning of value"
	switch s.needDelim(next) {
	case ':':
		where = "after object key"
	case ',':
		if s.tokenState == tokenObjectComma {
			where = "after object key:value pair"
		} else {
			where = "after array element"
		}
	}
	return s.invalidCharacter(at, where)
}

// tokenEnd returns the error of the end of the input before a token: io.EOF if what is left is white spaces,
// commas and colons, which a value may follow, as encoding/json reports it.
func (s *Stream) tokenEnd(start int64) error {
	if err := s.endError(); err != io.EOF {
		return err
	}
	for i := start - s.offset; i < s.length; i++ {
		switch s.buf[i] {
		case ' ', '\t', '\n', '\r', ',', ':':
		default:
			return io.ErrUnexpectedEOF
		}
	}
	return io.EOF
}

// restore moves the stream back to the total offset start, where a read which failed started, which InputOffset
// reports, and returns err.
func (s *Stream) restore(start int64, err error) error {
	s.moveBack(start)
	return err
}

// moveBack moves the stream back to the total offset start, where a read which failed started, which InputOffset
// reports.
func (s *Stream) moveBack(start int64) {
	if start >= s.offset {
		s.cursor = start - s.offset
	}
	s.errOffset = start
}

// prepareInTokens moves the cursor to the value which Decode reads between the tokens: after the comma or the
// colon which the grammar has before it. A read which fails is kept, as encoding/json keeps it. It is not
// inlined, so that Decode keeps the size it had.
//
//go:noinline
func (s *Stream) prepareInTokens() error {
	if s.tokenState == tokenFailed || s.tokenState == tokenEnded {
		return s.err
	}
	start := s.TotalOffset()
	c, err := s.readDelim(start)
	if err == nil {
		err = s.valueStartError(c)
	}
	if err != nil {
		if err == io.EOF {
			// the end of the input in an object or an array
			err = io.ErrUnexpectedEOF
		}
		s.moveBack(start)
		s.err, s.tokenState = err, tokenFailed
		return err
	}
	return nil
}

// valueStartError returns the error of a value which starts with c where it can't be read: the end of an object or
// an array, or a value which is not a string where a key is waited for.
func (s *Stream) valueStartError(c byte) error {
	at := s.TotalOffset()
	switch {
	case c == '}' || c == ']':
		// a bracket where the value starts
		if err := s.valueStartMismatch(at - s.offset); err != nil {
			return err
		}
		return s.invalidCharacter(at, "looking for beginning of value")
	case kindOf(c) == noValue:
		// a byte which starts no token
		return s.invalidCharacter(at, "looking for beginning of value")
	case c != '"' && (s.tokenState == tokenObjectStart || s.tokenState == tokenObjectKey):
		return s.memberNameError(at)
	}
	return nil
}

// valueStartMismatch returns the error of the bracket at the position at in the buffer, where a value is waited for
// in the value which is read or where it starts, if it is the end of the other kind of the object or the array
// which Token opened last, which has a member or an element already: jsontext reports it as a mismatch of that
// one, whatever the value is in. It returns nil for any other one, which is not the start of a value.
func (s *Stream) valueStartMismatch(at int64) error {
	switch c := s.buf[at]; {
	case c == ']' && (s.tokenState == tokenObjectKey || s.tokenState == tokenObjectColon ||
		s.tokenState == tokenObjectValue || s.tokenState == tokenObjectComma):
		return s.invalidCharacter(s.offset+at, "after object key:value pair")
	case c == '}' && (s.tokenState == tokenArrayValue || s.tokenState == tokenArrayComma):
		return s.invalidCharacter(s.offset+at, "after array element")
	}
	return nil
}

// memberNameError returns the error of the value at the cursor, at the total offset at, where the name of a member
// of an object is waited for: the value is read first, as jsontext reads a value or a token, and its syntax error,
// or the end of the input in it, is the error; else the name is not a string.
func (s *Stream) memberNameError(at int64) error {
	if _, err := s.scanValue(); err != nil {
		return err
	}
	if err := s.valueSyntaxError(); err != nil {
		return err
	}
	return errors.ErrSyntax("object member name must be a string", at+1)
}

// mismatchError returns the error of the end of an object or an array c at the total offset at, which doesn't end
// the one which is open.
func (s *Stream) mismatchError(at int64) error {
	where := "looking for beginning of value"
	switch s.tokenState {
	case tokenArrayComma:
		where = "after array element"
	case tokenObjectComma, tokenObjectValue:
		// a bracket where a value of an object is waited for too, as jsontext reports it
		where = "after object key:value pair"
	}
	return s.invalidCharacter(at, where)
}

// Token returns the next token of the stream: a delimiter, a string, a number, a bool or nil. The commas and the
// colons are consumed where the grammar has them.
func (s *Stream) Token() (any, error) {
	// a read which fails leaves the stream where it was, in its state, with the bytes from where it was
	state := s.tokenState
	s.keeps, s.keepFrom = true, s.TotalOffset()
	v, err := s.token()
	s.keeps = false
	if _, ok := err.(*errors.UnmarshalTypeError); err == nil || ok {
		// a token is read: InputOffset reports its end again
		s.more.reset()
	} else if s.tokenState != tokenFailed && s.tokenState != tokenEnded {
		s.tokenState = state
	}
	return v, err
}

// token reads the next token for Token.
func (s *Stream) token() (any, error) {
	if s.tokenState == tokenFailed || s.tokenState == tokenEnded {
		return nil, s.err
	}
	start := s.TotalOffset()
	c, err := s.readDelim(start)
	if err != nil {
		if err == io.EOF {
			// InputOffset reports a comma or a colon before the end, with the white spaces before it, and not the
			// white spaces at the end
			s.moveBack(start)
			if rest := trimLeftSpaces(s.buf[s.cursor:s.length]); len(rest) > 0 && (rest[0] == ',' || rest[0] == ':') {
				s.errOffset = s.TotalOffset() + int64(len(s.buf[s.cursor:s.length])-len(rest)) + 1
			}
			s.tokenState, s.err = tokenEnded, err
			return nil, err
		}
		return nil, s.restore(start, err)
	}
	at := s.TotalOffset()
	switch c {
	case '[', '{':
		if s.tokenState == tokenObjectStart || s.tokenState == tokenObjectKey {
			return nil, s.restore(start, errors.ErrSyntax("object member name must be a string", at+1))
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
		open := tokenArrayStart
		if c == '}' {
			open = tokenObjectStart
		}
		if len(s.tokenStack) == 0 || !s.inOpen(open) {
			return nil, s.restore(start, s.mismatchError(at))
		}
		if s.tokenState == tokenObjectValue {
			return nil, s.restore(start, errors.ErrSyntax("missing value after object key", at+1))
		}
		s.cursor++
		s.tokenState = s.tokenStack[len(s.tokenStack)-1]
		s.tokenStack = s.tokenStack[:len(s.tokenStack)-1]
		s.tokenValueEnd()
		return json.Delim(c), nil
	case '"':
		if s.tokenState == tokenObjectStart || s.tokenState == tokenObjectKey {
			key, err := s.tokenString()
			if err != nil {
				return nil, s.restore(start, err)
			}
			s.tokenState = tokenObjectColon
			return key, nil
		}
	default:
		if kindOf(c) != noValue && (s.tokenState == tokenObjectStart || s.tokenState == tokenObjectKey) {
			return nil, s.restore(start, s.memberNameError(at))
		}
	}
	v, err := s.tokenValue(c)
	if err != nil {
		if _, ok := err.(*errors.UnmarshalTypeError); ok {
			// the value is read: the stream goes on after it
			s.tokenValueEnd()
			return nil, err
		}
		return nil, s.restore(start, err)
	}
	s.tokenValueEnd()
	return v, nil
}

// inOpen reports whether the innermost object or array which is open is of the kind of the state open.
func (s *Stream) inOpen(open tokenState) bool {
	switch s.tokenState {
	case tokenArrayStart, tokenArrayValue, tokenArrayComma:
		return open == tokenArrayStart
	}
	return open == tokenObjectStart
}

// tokenError returns the syntax error of the byte c at the cursor, which the grammar doesn't have in the state.
func (s *Stream) tokenError(c byte) error {
	return s.invalidCharacter(s.TotalOffset(), "looking for beginning of value")
}

// InputOffset returns the offset of the stream which Decoder.InputOffset reports: the end of the last token or
// value read, or where a read which failed started.
func (s *Stream) InputOffset() int64 {
	offset := s.TotalOffset()
	if s.tokenState == tokenFailed || s.tokenState == tokenEnded {
		offset = s.errOffset
	}
	if s.more.peeked {
		// after More, the start of the next token which it peeked at, after the white spaces before it
		return s.more.offset
	}
	return offset
}

// More reports whether the current array or object has another element: at the end of the input in one, it
// reports true, and the error of the end is returned by the next read, as encoding/json of Go 1.27 does.
func (s *Stream) More() bool {
	if s.tokenState == tokenFailed || s.tokenState == tokenEnded {
		return s.err != io.EOF
	}
	// the next token is peeked at, with the comma or the colon before it, as Token reads it: the cursor is kept,
	// so that the bytes from it stay in the buffer while more input is read
	start := s.TotalOffset()
	s.more.peeked, s.more.offset = true, start
	pos, ok := s.peekFrom(s.cursor)
	if !ok {
		return s.moreAtEnd(start)
	}
	s.more.offset = s.offset + pos
	at, c := s.offset+pos, s.buf[pos]
	var delim byte
	if c == ',' || c == ':' {
		delim = c
		if pos, ok = s.peekFrom(pos + 1); !ok {
			// the next token is not known: a string, which follows any comma or colon, is taken for it
			if s.needDelim('"') != delim {
				return s.moreError(s.delimError('"', at))
			}
			return s.moreAtEnd(start)
		}
		c = s.buf[pos]
	}
	if s.needDelim(c) != delim {
		return s.moreError(s.delimError(c, at))
	}
	return c != ']' && c != '}'
}

// peekFrom returns the position of the first byte from pos which is not a white space, reading more input as
// needed without moving the cursor, or false at the end of the input.
func (s *Stream) peekFrom(pos int64) (int64, bool) {
	// the position is kept relative to the cursor, because a read may move the data ( grow )
	rel := pos - s.cursor
	for {
		for pos = s.cursor + rel; pos < s.length; pos++ {
			if !isWhiteSpace[s.buf[pos]] {
				return pos, true
			}
		}
		rel = s.length - s.cursor
		if !s.read() {
			return 0, false
		}
	}
}

// moreAtEnd returns what More reports at the end of the input, which it peeked at from the total offset start: the
// end of a stream of values, or the unexpected end of an object or an array, which is kept as the error.
func (s *Stream) moreAtEnd(start int64) bool {
	if len(s.tokenStack) == 0 {
		return false
	}
	// the error is at the end of the input, after the white spaces, which InputOffset doesn't report
	s.err, s.errOffset, s.tokenState = errors.ErrSyntax("unexpected end of JSON input", s.offset+s.length), start, tokenFailed
	return true
}

// moreError keeps err, the error of the token which More peeked at, which encoding/json of Go 1.27 reads, and
// returns true.
func (s *Stream) moreError(err error) bool {
	s.err, s.errOffset, s.tokenState = err, s.TotalOffset(), tokenFailed
	return true
}

func trimLeftSpaces(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\n' || b[0] == '\r') {
		b = b[1:]
	}
	return b
}

// tokenValue returns the value at the cursor, which starts with c, as the token of a string, a number, a bool or
// null.
func (s *Stream) tokenValue(c byte) (any, error) {
	switch c {
	case '"':
		return s.tokenString()
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		end, err := s.scanValue()
		if err != nil {
			return nil, err
		}
		// the number ends where the grammar ends it, as 01 is 0 and 1, and is a number by the grammar
		end = streamNumberEnd(s.buf, s.cursor, end)
		literal := s.buf[s.cursor:end]
		if !isValidNumber(literal) {
			return nil, s.valueSyntaxError()
		}
		s.cursor = end
		if (s.Option.Flags & UseNumberOption) != 0 {
			return json.Number(literal), nil
		}
		f64, err := strconv.ParseFloat(*(*string)(unsafe.Pointer(&literal)), 64)
		if err != nil {
			// a number out of the range of float64, which is read: a type error of the value, which Token
			// returns after it
			return nil, &errors.UnmarshalTypeError{Value: "number " + string(literal), Type: float64Type, Offset: s.TotalOffset()}
		}
		return f64, nil
	case 't', 'f', 'n':
		end, err := s.scanValue()
		if err != nil {
			return nil, err
		}
		var v any
		lit := "null"
		switch c {
		case 't':
			v, lit = true, "true"
		case 'f':
			v, lit = false, "false"
		}
		// the literal ends after its bytes: what follows is the next token
		if !bytes.HasPrefix(s.buf[s.cursor:end], []byte(lit)) {
			return nil, s.valueSyntaxError()
		}
		s.cursor += int64(len(lit))
		return v, nil
	}
	return nil, s.tokenError(c)
}
