package decoder

import (
	"bytes"
	"io"
	"math/bits"
	"slices"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/jsonnum"
	"github.com/goccy/go-json/internal/runtime"
)

const (
	// initBufSize is the size of the buffer of a new stream, and the most bytes a read reads: the buffer grows only for
	// a token longer than it, or a value which is kept whole ( see grow ).
	initBufSize = 512
	// bufPadding is the number of the bytes kept free after the data in the buffer of a stream:
	// one for the nul byte which ends the value being decoded, the rest so that a scan reads
	// a whole block from any byte of the value.
	bufPadding = scanBlockSize
)

// Stream reads the values of a JSON text from an io.Reader one by one.
//
// A value is decoded by the same decoders as a byte slice is: a value which the buffer holds whole is decoded from
// it, with a nul byte put after the value, and an object or an array which it doesn't is decoded element by element
// as it is read ( see scanFailed ), so that the buffer keeps its size. The decoded strings are copied out of the
// buffer, so that a value keeps alive no memory but its own, and the buffer is reused in place when it is full: the
// bytes which are not consumed yet are moved to its start. Under NoCopyStringOption, the strings refer to the
// buffer, which is then never written again: the bytes are copied to a new one instead ( see referred ).
type Stream struct {
	// buf[:length] is what has been read, buf[length] is nul, and bufPadding bytes are free at the end.
	buf    []byte
	length int64
	// cursor is the first byte which is not consumed yet.
	cursor int64
	// offset is the number of the bytes discarded before buf[0]: the total offset is offset + cursor.
	offset int64
	r      io.Reader
	// readErr is the error the reader returned. Once set, the reader is never read again:
	// the data read before the error is consumed first, then the error is reported.
	readErr error
	ctx     *RuntimeContext
	Option  *Option
	// prevEnd is the total offset of the end of the previous value, which InputOffset reports after a read which
	// fails.
	prevEnd int64
	// tokenState is where the stream is between the tokens, and tokenStack the states of the objects and the
	// arrays which Token opened, the innermost last ( see Token ).
	tokenState tokenState
	tokenStack []tokenState
	// err is the error which a read failed with, which every read returns after it, as encoding/json does, and
	// errOffset the total offset which InputOffset reports after it: the end of the previous value.
	err       error
	errOffset int64
	// readStart is the total offset where the read of the value of Decode, or of the value of Token, started, and
	// scanned the number of the bytes which these reads read before it, by which encoding/json before Go 1.27
	// reports the offset of a syntax error of the value ( see scannedOffset ).
	readStart int64
	scanned   int64
	// more is what More peeked at, which InputOffset reports ( see streamPeek ).
	more streamPeek
	// keeps is set while the bytes from the total offset keepFrom are kept in the buffer, before the cursor too:
	// a read of a token which fails leaves the stream where it was ( see Token ).
	keeps    bool
	keepFrom int64
	// src is the jsontext.Decoder whose buffer the stream reads in place, for a decoding of v2 ( see BorrowV2 ):
	// a read reads more into that buffer, which is buf, and own is the buffer of the stream then.
	src unsafe.Pointer
	own []byte
	// elements is where the objects and the arrays of the value which is decoded element by element end, which the
	// lookups extend ( see indexAt ), made for the first such value and kept for the next ones, which allocate its
	// entries no more. It is nil until a value is decoded so.
	elements *bracketIndex
	// referred is whether a decoded value may refer to the buffer, which grow doesn't reuse in place then: the
	// decoded strings are copied out of it, unless NoCopyStringOption is set, under which a value refers to the
	// buffer it is decoded from ( see Stream.Decode and scanFailed ). It is about the buffer, not about the values:
	// a new buffer is referred to only by the value being decoded ( see grow ). The bytes given to UnmarshalText, of
	// a value or a map key, are a part of the buffer, which may be written again then: encoding.TextUnmarshaler has
	// the method copy them to keep them, and the Decoder of encoding/json gives a part of its buffer too.
	referred bool
}

// ReadAllV2 returns the whole input of the stream, which has read nothing yet, up to the end of its reader, followed
// by a nul byte and the padding which the decoders may read after it ( see SetInput ), or the error of the reader if it
// fails.
func (s *Stream) ReadAllV2() ([]byte, error) {
	in, err := io.ReadAll(s.r)
	if err != nil {
		return nil, err
	}
	return append(slices.Grow(in, 1+inputPadding), nul), nil
}

func NewStream(r io.Reader) *Stream {
	opt := &Option{}
	return &Stream{
		r:      r,
		buf:    make([]byte, initBufSize+bufPadding),
		ctx:    &RuntimeContext{Option: opt},
		Option: opt,
	}
}

// markPrevEnd keeps the end of the previous value of the stream, which InputOffset reports after a read which fails.
func (s *Stream) markPrevEnd() {
	s.prevEnd = s.offset + s.cursor
}

// fail keeps err, the error which a read failed with, which every read returns after it, as encoding/json does.
//
//go:noinline
func (s *Stream) fail(err error) error {
	s.err, s.errOffset, s.tokenState = err, s.prevEnd, tokenFailed
	return err
}

// TotalOffset returns the number of the bytes consumed from the reader.
func (s *Stream) TotalOffset() int64 {
	return s.offset + s.cursor
}

// Buffered returns a reader of the data read from the reader but not consumed yet.
func (s *Stream) Buffered() io.Reader {
	return bytes.NewReader(s.buf[s.cursor:s.length])
}

// read reads more bytes from the reader into the buffer, initBufSize bytes at most, whatever the size of the buffer.
// It returns false when nothing more can be read: the reader ended or failed ( readErr ).
func (s *Stream) read() bool {
	if s.readErr != nil {
		return false
	}
	if s.src != nil {
		return s.readSource()
	}
	if int64(len(s.buf))-s.length <= bufPadding {
		s.grow()
	}
	n, err := s.r.Read(s.buf[s.length:min(int64(len(s.buf))-bufPadding, s.length+initBufSize)])
	s.length += int64(n)
	s.buf[s.length] = nul
	if err != nil {
		s.readErr = err
	}
	return n > 0 || err == nil
}

// grow moves the bytes which are not consumed yet to the start of a buffer, so that the reader has room to read into:
// the same one, if nothing refers to it ( see referred ) and it has room for the bytes, or else a new one, as the
// old buffer is left to the strings which refer to it.
func (s *Stream) grow() {
	if call := s.ctx.v2; call != nil && len(call.pathNames) != 0 {
		// the names of the path which the levels have by their places in the buffer, which the bytes are moved from
		s.copyNames()
	}
	from := s.keepStart()
	remain := s.length - from
	size := int64(len(s.buf)) - bufPadding
	if remain >= size {
		// What is not consumed yet fills the buffer: a token longer than it, or a value which is kept whole ( see
		// wholeEnd ). The buffer grows only for it, twice as large.
		size *= 2
	}
	buf := s.buf
	if s.referred || size > int64(len(buf))-bufPadding {
		buf = make([]byte, size+bufPadding)
		// nothing refers to the new buffer yet, unless the value being decoded refers to it under NoCopyStringOption:
		// an option may clear the flag before the next value
		s.referred = (s.Option.Flags & NoCopyStringOption) != 0
	}
	copy(buf, s.buf[from:s.length])
	s.offset += from
	s.buf = buf
	s.length = remain
	s.cursor -= from
}

// keepStart returns the position of the first byte of the buffer which a read keeps: the cursor, or the first byte
// which is kept before it, or the end of the token before the value which a skip of v2 reads, where an error is found
// again from ( see skipLongV2 ).
func (s *Stream) keepStart() int64 {
	from := s.cursor
	if keep := s.keepFrom - s.offset; s.keeps && keep >= 0 && keep < from {
		from = keep
	}
	if call := s.ctx.v2; call != nil && call.skip.active {
		from = min(from, s.skipKeepFrom())
	}
	return from
}

// fill makes sure that the buffer holds the byte at the cursor.
func (s *Stream) fill() bool {
	for s.cursor >= s.length {
		if !s.read() {
			return false
		}
	}
	return true
}

// endError is the error reported when the reader has nothing more: io.EOF or what the reader returned.
func (s *Stream) endError() error {
	if s.readErr != nil {
		return s.readErr
	}
	return io.EOF
}

// truncatedValueError is the error reported when the reader ends in the middle of the value at the cursor: the first
// syntax error of what has been read, as encoding/json reads a value byte by byte, or the unexpected end.
//
//go:noinline
func (s *Stream) truncatedValueError() error {
	if err := s.valueSyntaxError(); err != nil {
		return err
	}
	return s.unexpectedEndError()
}

// unexpectedEndError is the error reported when the reader ends in the middle of a value.
func (s *Stream) unexpectedEndError() error {
	if s.readErr != nil && s.readErr != io.EOF {
		return s.readErr
	}
	return io.ErrUnexpectedEOF
}

// skipWhiteSpace moves the cursor to the next byte which is not white space.
// It returns false when the reader has nothing more.
func (s *Stream) skipWhiteSpace() bool {
	// a byte which is not white space, as the tokens of most values are not separated by any, without a call
	if s.cursor < s.length && !isWhiteSpace[s.buf[s.cursor]] {
		return true
	}
	return s.skipWhiteSpaceRest()
}

// skipWhiteSpaceRest is skipWhiteSpace without its check of the byte at the cursor first.
func (s *Stream) skipWhiteSpaceRest() bool {
	for {
		// the nul byte at the end of what has been read stops the skip
		if s.cursor = skipWhiteSpace(s.buf, s.cursor); s.cursor < s.length {
			return true
		}
		if !s.read() {
			return false
		}
	}
}

// peek moves the cursor to the next byte which is not a white space, as skipWhiteSpace does, but leaves it where it
// is when the input ends with white spaces only: the offset of the input doesn't count them, as encoding/json
// doesn't count what it peeks at. It returns false when the reader has nothing more.
func (s *Stream) peek() bool {
	var rel int64
	for {
		// the position is kept relative to the cursor, because a read may move the data ( grow )
		for pos := s.cursor + rel; pos < s.length; pos++ {
			if !isWhiteSpace[s.buf[pos]] {
				s.cursor = pos
				return true
			}
		}
		rel = s.length - s.cursor
		if !s.read() {
			return false
		}
	}
}

// prepare moves the cursor to the beginning of the next value: the white space is skipped, and, between the
// tokens of an object or an array, the comma or the colon which the grammar has before the value.
func (s *Stream) prepare() error {
	if s.tokenState != tokenTopValue {
		return s.prepareInTokens()
	}
	s.readStart = s.offset + s.cursor
	if !s.skipSpaceBefore() {
		return s.fail(s.endError())
	}
	return nil
}

// skipSpaceBefore is skipWhiteSpace for the white space before a value of the stream, which is little.
func (s *Stream) skipSpaceBefore() bool {
	for {
		if !s.fill() {
			return false
		}
		switch s.buf[s.cursor] {
		case ' ', '\n', '\t', '\r':
			s.cursor++
		default:
			return true
		}
	}
}

// isLiteralChar is true for the bytes of a number, true, false and null.
var isLiteralChar = [256]bool{
	'0': true, '1': true, '2': true, '3': true, '4': true, '5': true, '6': true, '7': true, '8': true, '9': true,
	'-': true, '+': true, '.': true, 'e': true, 'E': true,
	't': true, 'r': true, 'u': true, 'f': true, 'a': true, 'l': true, 's': true, 'n': true,
}

// scanValue finds the end of the value which begins at the cursor, reading from the reader as needed, and returns the
// position after the value. The value is not validated: the decoder does it. The cursor is not moved: the value is in
// buf[cursor:end], but the buffer may have been replaced. A string, a number, true, false and null are read whole; an
// object or an array which doesn't end in what has been read is not read on: errLongValue is returned, and it is
// decoded element by element ( see scanFailed ).
func (s *Stream) scanValue() (int64, error) {
	if !s.fill() {
		return 0, s.endError()
	}
	switch c := s.buf[s.cursor]; {
	case c == '{' || c == '[':
		return s.scanCompound()
	case c == '"':
		return s.scanString()
	case isLiteralChar[c]:
		return s.scanLiteral()
	}
	// The decoder reports the invalid character.
	return s.cursor + 1, nil
}

// scanCompound finds the end of the object or the array at the cursor in what has been read: the position after the
// bracket which closes it, or errLongValue if it doesn't end there.
func (s *Stream) scanCompound() (int64, error) {
	var sc compoundScanner
	pos, found := sc.scan(s.buf, s.cursor, s.length)
	if !found {
		return 0, errLongValue
	}
	return pos, nil
}

// scanString finds the end of the string at the cursor: the position after the quote which closes it, or, for a string
// which goes on past what has been read, after a control character or the byte of an escape which is not one, which a
// string can't have: the decoder reports it, and the input after it is not read ( see scanStringInfo ).
func (s *Stream) scanString() (int64, error) {
	var (
		rel     int64 = 1 // after the opening quote
		escaped bool
	)
	for {
		buf := s.buf
		pos := s.cursor + rel
		end := s.length
		for pos+8 <= end {
			w := load64(buf, pos)
			if byteMask(w, '\\') != 0 || escaped {
				break
			}
			if quote := byteMask(w, '"'); quote != 0 {
				return pos + int64(bits.TrailingZeros64(quote)/8) + 1, nil
			}
			pos += 8
		}
		lim := min(pos+8, end)
		for pos < lim {
			c := buf[pos]
			pos++
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				return pos, nil
			}
		}
		rel = pos - s.cursor
		if pos < end {
			continue
		}
		// a string which goes on past what has been read is checked before a read
		end, _, _, err := s.scanStringInfo()
		return end, err
	}
}

// scanStringInfo is scanString which also returns what the string has, as stringDecoder.scanString does, and whether
// it ends at its quote, and is valid then.
func (s *Stream) scanStringInfo() (int64, stringInfo, bool, error) {
	var (
		rel     int64 = 1 // after the opening quote
		escaped bool
		// hex is the number of the hexadecimal digits of a \u escape which are not read yet
		hex int
		// high has the high bits of the bytes, and firstEscape is the offset of the first backslash in the string
		high        uint64
		firstEscape int64 = -1
	)
	for {
		buf := s.buf
		pos := s.cursor + rel
		end := s.length
		// the words of the bytes up to a quote, a backslash or a control character, which the nul byte at the end of
		// what has been read is
		if !escaped && hex == 0 {
			for n := int64(len(buf)); pos+8 <= n; pos += 8 {
				w := load64(buf, pos)
				if stop := keyEndBytes(w); stop != 0 {
					// a quote, a backslash or a control character
					i := bits.TrailingZeros64(stop) / 8
					high |= w & (1<<(uint(i)*8&63) - 1)
					pos += int64(i)
					break
				}
				high |= w
			}
			if pos < end && buf[pos] == '"' {
				return pos + 1, stringInfo{firstEscape: int(firstEscape), nonASCII: high&msb != 0}, true, nil
			}
		}
		lim := min(pos+8, end)
		for pos < lim {
			c := buf[pos]
			high |= uint64(c)
			pos++
			switch {
			case c < ' ':
				return pos, stringInfo{}, false, nil
			case hex > 0:
				// an escape which is not one ends the string, so that the decoder reports it
				if !isHexDigit(c) {
					return pos, stringInfo{}, false, nil
				}
				hex--
			case escaped:
				escaped = false
				switch c {
				case 'u':
					hex = 4
				case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				default:
					return pos, stringInfo{}, false, nil
				}
			case c == '\\':
				escaped = true
				if firstEscape < 0 {
					firstEscape = pos - 1 - (s.cursor + 1)
				}
			case c == '"':
				return pos, stringInfo{firstEscape: int(firstEscape), nonASCII: high&msb != 0}, true, nil
			}
		}
		rel = pos - s.cursor
		if pos < end {
			continue
		}
		if !s.read() {
			return 0, stringInfo{}, false, s.truncatedValueError()
		}
	}
}

// scanLiteral finds the end of the number, true, false or null at the cursor: the position after true, false or null,
// or of the first byte which differs from it, and the position of the first byte which can't belong to the number by
// its grammar, or the end of the input. A number is read until a byte ends it, as encoding/json reads it, but a byte
// which makes it invalid ends it without a read more. A number ends where its grammar ends it, so that 01 is 0 and then
// 1, as encoding/json reads them.
func (s *Stream) scanLiteral() (int64, error) {
	if lit := literalOf(s.buf[s.cursor]); lit != "" {
		for i := 1; i < len(lit); i++ {
			// a read may bring nothing, and the next one more
			for s.cursor+int64(i) >= s.length {
				if !s.read() {
					return s.scanLiteralEnd()
				}
			}
			if s.buf[s.cursor+int64(i)] != lit[i] {
				return s.cursor + int64(i) + 1, nil
			}
		}
		return s.cursor + int64(len(lit)), nil
	}
	// an integer which doesn't start with 0, which most numbers are, ended by a byte in what has been read, without a
	// call: the nul byte at the end of what has been read stops the digits
	c := s.cursor
	if s.buf[c] == '-' {
		c++
	}
	if s.buf[c]-'1' <= 8 {
		c++
		for s.buf[c]-'0' <= 9 {
			c++
		}
		if b := s.buf[c]; c < s.length && b != '.' && b != 'e' && b != 'E' {
			return c, nil
		}
	}
	// rel is the end of the digits which end what has been read and continue the number, relative to the cursor, as a
	// read may move the data, or 0: the digits read after them are skipped, and the number is scanned again from its
	// start only at another byte, which a number has a few of, so that a long number is scanned in linear time
	var rel int64
	for {
		if rel > 0 {
			if rel = int64(jsonnum.ScanDigits(s.buf[s.cursor:s.length], int(rel))); s.cursor+rel == s.length {
				if !s.read() {
					return s.scanLiteralEnd()
				}
				continue
			}
		}
		n, place := jsonnum.Scan(s.buf[s.cursor:s.length])
		if end := s.cursor + int64(n); end < s.length {
			if place == jsonnum.Valid {
				return end, nil
			}
			// the byte where the number is invalid
			return end + 1, nil
		}
		rel = 0
		if place == jsonnum.Valid && digitsContinue(s.buf[s.cursor:s.length]) {
			rel = int64(n)
		}
		if !s.read() {
			return s.scanLiteralEnd()
		}
	}
}

// scanLiteralEnd returns what scanLiteral returns at the end of the input in a literal: the end of what has been read,
// or the error of the reader.
func (s *Stream) scanLiteralEnd() (int64, error) {
	if s.readErr != io.EOF {
		return 0, s.readErr
	}
	return s.length, nil
}

// digitsContinue reports whether a digit after num, a valid number, belongs to it: after a digit of any number but 0
// and -0, whose zero is the whole of the integer part.
func digitsContinue(num []byte) bool {
	last := num[len(num)-1]
	return last-'0' <= 9 && !(last == '0' && (len(num) == 1 || len(num) == 2 && num[0] == '-'))
}

// literalOf returns true, false or null, which c starts, or "".
func literalOf(c byte) string {
	switch c {
	case 't':
		return "true"
	case 'f':
		return "false"
	case 'n':
		return "null"
	}
	return ""
}

// DecoderOf returns the decoder of the type, from the recent decoders of the context of the stream.
func (s *Stream) DecoderOf(typ unsafe.Pointer) (Decoder, error) {
	return s.ctx.DecoderOf(typ)
}

// Decode decodes the next value of the stream into p, of the pointer type typ, by dec. If the value has a type
// error, the stream goes on after the value, as the one of encoding/json does.
func (s *Stream) Decode(dec Decoder, typ unsafe.Pointer, p unsafe.Pointer) error {
	s.markPrevEnd()
	if err := s.prepare(); err != nil {
		return err
	}
	// a value which the buffer doesn't hold whole is decoded element by element, without a read for it ( see
	// scanCompound )
	end, err := s.scanValue()
	if err != nil {
		return s.scanFailed(err, dec, typ, p)
	}
	ctx := s.ctx
	ctx.Option = s.Option
	if (s.Option.Flags & NoCopyStringOption) != 0 {
		// the strings of the value refer to the buffer, which is not reused in place then ( see grow )
		s.referred = true
	}
	var cursor int64
	if c := s.buf[s.cursor]; c != 't' && c != 'f' && c != 'n' {
		// The end of the value is known exactly: a nul byte is put there while the value is decoded,
		// so that the decoder never reads the next value, whatever it makes of a malformed one.
		// A number needs it too, because the decoder validates the byte which ends it.
		saved := s.buf[end]
		s.buf[end] = nul
		ctx.Buf = s.buf[:end+1]
		ctx.origin = ctx.Buf
		cursor, err = dec.Decode(ctx, s.cursor, 0, p)
		s.buf[end] = saved
	} else {
		// true, false and null end at the first byte which can't belong to them, and the decoder
		// reports that byte when it is wrong: it sees the whole buffer, ended by the nul byte.
		ctx.Buf = s.buf[:s.length+1]
		ctx.origin = ctx.Buf
		cursor, err = dec.Decode(ctx, s.cursor, 0, p)
	}
	ctx.Buf, ctx.origin = nil, nil
	if err != nil {
		return s.decodeError(err)
	}
	if ctx.HasTypeError() {
		return s.typeError(typ, cursor)
	}
	s.cursor = cursor
	if countsScanned {
		s.scanned += s.offset + cursor - s.readStart
	}
	s.more.reset()
	if s.tokenState != tokenTopValue {
		s.tokenValueEnd()
	}
	return nil
}

// callDecoder decodes the value at the cursor, which ends at end, by dec into p, nested in depth objects and arrays,
// and returns the position after it. The decoded strings are copied out of the buffer, which they would keep alive,
// unless NoCopyStringOption is set.
func (s *Stream) callDecoder(dec Decoder, p unsafe.Pointer, depth, end int64) (int64, error) {
	ctx := s.ctx
	var cursor int64
	var err error
	if c := s.buf[s.cursor]; c != 't' && c != 'f' && c != 'n' {
		// The end of the value is known exactly: a nul byte is put there while the value is decoded,
		// so that the decoder never reads the next value, whatever it makes of a malformed one.
		// A number needs it too, because the decoder validates the byte which ends it.
		saved := s.buf[end]
		s.buf[end] = nul
		ctx.Buf = s.buf[:end+1]
		ctx.origin = ctx.Buf
		cursor, err = dec.Decode(ctx, s.cursor, depth, p)
		s.buf[end] = saved
	} else {
		// true, false and null end at the first byte which can't belong to them, and the decoder
		// reports that byte when it is wrong: it sees the whole buffer, ended by the nul byte.
		ctx.Buf = s.buf[:s.length+1]
		ctx.origin = ctx.Buf
		cursor, err = dec.Decode(ctx, s.cursor, depth, p)
	}
	ctx.Buf, ctx.origin = nil, nil
	return cursor, err
}

// decodeError returns the error of the decoder, whose offset is made the one in the whole input, of the value at
// the cursor. A type error before it is not kept for the next value.
//
// A syntax error is the first one of the value by the grammar, as encoding/json reports it: it checks the value
// before it decodes it. The decoder never writes the buffer, which keeps the input.
//
//go:noinline
func (s *Stream) decodeError(err error) error {
	s.ctx.DiscardTypeError()
	// a syntax error of the value is the error, whatever the decoder met before it, as encoding/json checks the value
	// before it decodes it
	if serr := s.valueSyntaxError(); serr != nil {
		return s.fail(serr)
	}
	return s.fail(s.totalOffsetError(err))
}

// valueSyntaxError returns the first syntax error of the value at the cursor by the grammar, in what has been read:
// the error of a byte, with its offset in the whole input, or the unexpected end of the input, where encoding/json
// reads more. A number, true, false or null is followed by the byte after it, or by the end of the input: the scan
// of the value has read either. It is nil if what has been read has none.
//
//go:noinline
func (s *Stream) valueSyntaxError() error {
	return s.valueSyntaxErrorAt(0)
}

// valueSyntaxErrorAt is valueSyntaxError for a value nested in depth objects and arrays.
func (s *Stream) valueSyntaxErrorAt(depth int64) error {
	_, err := skipValue(s.buf[:s.length+1], s.cursor, depth)
	if err == nil {
		return nil
	}
	return s.grammarError(err)
}

// grammarError returns the syntax error err of a scan of the grammar in what has been read, as valueSyntaxError
// reports it.
func (s *Stream) grammarError(err error) error {
	if errors.IsAtEnd(err) {
		return s.unexpectedEndError()
	}
	if at, ok := errors.ValueStartAt(err); ok {
		if merr := s.valueStartMismatch(at); merr != nil {
			return merr
		}
	}
	return s.scannedOffset(s.totalOffsetError(err))
}

// scannedOffset makes the offset of the syntax error of the value read from readStart the one which encoding/json
// of the Go version reports ( see countsScanned ).
func (s *Stream) scannedOffset(err error) error {
	if e, ok := err.(*errors.SyntaxError); ok && countsScanned {
		e.Offset += s.scanned - s.readStart
	}
	return err
}

// typeError returns the type error of the value of the pointer type typ which Decode decoded from the cursor up
// to end, and moves the cursor after the value: the stream goes on after it, as the one of encoding/json does.
// Its offset is relative to the start of the value, as encoding/json reports it.
//
//go:noinline
func (s *Stream) typeError(typ unsafe.Pointer, end int64) error {
	start := s.cursor
	s.cursor = end
	if countsScanned {
		s.scanned += s.offset + end - s.readStart
	}
	s.more.reset()
	if s.tokenState != tokenTopValue {
		s.tokenValueEnd()
	}
	ctx := s.ctx
	// the path of the error is walked in the value ended by the nul byte, as it was decoded
	saved := s.buf[end]
	s.buf[end] = nul
	ctx.Buf = s.buf[:end+1]
	err := ctx.TypeError(runtime.TypeOfPtr(typ), start, start, nil)
	ctx.Buf = nil
	s.buf[end] = saved
	return err
}

// totalOffsetError makes the offset of an error of the decoder, which is relative to the buffer,
// the offset in the whole input.
func (s *Stream) totalOffsetError(err error) error {
	switch e := err.(type) {
	case *errors.SyntaxError:
		e.Offset += s.offset
	case *errors.UnmarshalTypeError:
		e.Offset += s.offset
	}
	return err
}

// tokenString decodes the string at the cursor by the string decoder, as Decode does.
func (s *Stream) tokenString() (any, error) {
	s.readStart = s.offset + s.cursor
	end, err := s.scanValue()
	if err != nil {
		return nil, err
	}
	saved := s.buf[end]
	s.buf[end] = nul
	literal, _, err := tokenStringDecoder.decodeByte(s.buf[:end+1], s.cursor)
	s.buf[end] = saved
	if err != nil {
		if serr := s.valueSyntaxError(); serr != nil {
			return nil, serr
		}
		return nil, s.totalOffsetError(err)
	}
	s.cursor = end
	if countsScanned {
		s.scanned += s.offset + end - s.readStart
	}
	if len(literal) != 0 && !isPartOf(literal, s.buf) {
		// the bytes of the escaped string, which decodeLiteral made: they are the string's own
		return unsafe.String(unsafe.SliceData(literal), len(literal)), nil
	}
	return string(literal), nil
}

var tokenStringDecoder = newStringDecoder("", "")
