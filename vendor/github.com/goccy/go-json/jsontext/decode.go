package jsontext

import (
	"bytes"
	"io"
	"slices"
	"unicode/utf8"

	"github.com/goccy/go-json/internal/jsonnum"
	"github.com/goccy/go-json/internal/textcoder"
)

// Decoder is a streaming decoder of raw JSON tokens and values. It reads a stream of top-level JSON values,
// separated by optional white space.
//
// Decoder.ReadToken and Decoder.ReadValue calls may be interleaved. For example, the JSON value
//
//	{"name":"value","array":[null,false,true,3.14159],"object":{"k":"v"}}
//
// can be read by these calls (ignoring errors):
//
//	d.ReadToken() // {
//	d.ReadToken() // "name"
//	d.ReadToken() // "value"
//	d.ReadValue() // "array"
//	d.ReadToken() // [
//	d.ReadToken() // null
//	d.ReadToken() // false
//	d.ReadValue() // true
//	d.ReadToken() // 3.14159
//	d.ReadToken() // ]
//	d.ReadValue() // "object"
//	d.ReadValue() // {"k":"v"}
//	d.ReadToken() // }
//
// which is one of many sequences of calls which read it.
type Decoder struct {
	d decoder
}

// decoder is the state of a Decoder.
type decoder struct {
	cfg config
	st  stack
	vs  valueScanner

	r      io.Reader
	bb     *bytes.Buffer // r, which is read in place
	buf    []byte        // the input which was read and is kept: from the last token or value on
	shared bool          // buf is the array of bb, which the decoder must not write to
	base   int64         // the offset of buf[0] in the input

	pos                int   // the read offset: the end of the last token or value
	prevStart, prevEnd int   // the last token or value, whose start may be before the buffer ( see endValue )
	rerr               error // the error of the reader in the current call
	peekErr            error // an error which PeekKind found, which the next read reports

	raw tokenSource // the source of its raw tokens
}

// NewDecoder constructs a streaming decoder which reads from r.
//
// If r is a bytes.Buffer, the decoder reads the buffer in place, without copying its contents. The buffer must
// not be written to while the decoder is used.
func NewDecoder(r io.Reader, opts ...Options) *Decoder {
	d := new(Decoder)
	d.Reset(r, opts...)
	return d
}

// Reset resets the decoder to read from r anew, with the options.
func (d *Decoder) Reset(r io.Reader, opts ...Options) {
	switch {
	case d == nil:
		panic("jsontext: invalid nil Decoder")
	case r == nil:
		panic("jsontext: invalid nil io.Reader")
	}
	d.d.reset(r, opts)
}

// ready reports whether the decoder is set up: the source of its tokens points to it.
func (d *decoder) ready() bool { return d.raw.dec == d }

// setUp sets up a decoder which is not ready: a zero value, as one whose input is empty, as
// encoding/json/jsontext takes it, or a copy of a decoder, which takes pointers and arrays of its own: it reads
// on its own as a copy of a decoder of encoding/json/jsontext does, and the decoder it was copied from keeps its
// state.
func (d *decoder) setUp() {
	if len(d.st.levels) == 0 {
		d.reset(nil, nil)
		return
	}
	d.raw.dec = d
	d.vs.st = &d.st
	d.st.own()
	if !d.shared {
		d.buf = slices.Clone(d.buf) // the input which the decoder moves and reads into
	}
	d.vs.scratch = nil
}

// reset resets the decoder to read from r, which is nil for no input, with the options.
func (d *decoder) reset(r io.Reader, opts []Options) {
	var c config // opts may hold the options of d, which Options returned
	c.Apply(opts)
	d.cfg = c
	if !d.ready() {
		// a zero value, or a copy, whose arrays are the ones of the decoder it was copied from
		d.st, d.vs.scratch = stack{}, nil
		if !d.shared {
			d.buf = nil
		}
	}
	d.st.reset()
	if d.st.outer != nil {
		d.st.outer, d.st.outerDepth, d.st.outerCount = nil, 0, 0 // of an attach by the v2 json package
	}
	if _, wasBuffer := d.r.(*bytes.Buffer); d.shared || wasBuffer {
		// the buffer of a bytes.Buffer which was read before is dropped, as encoding/json/jsontext drops it
		d.buf, d.shared = nil, false
	}
	d.r = r
	d.bb, _ = r.(*bytes.Buffer)
	d.buf = d.buf[:0]
	d.base, d.pos, d.prevStart, d.prevEnd = 0, 0, 0, 0
	d.rerr, d.peekErr = nil, nil
	d.raw = tokenSource{dec: d, form: formRaw}
	d.vs = valueScanner{
		st:                   &d.st,
		validUTF8:            !d.cfg.Has(allowInvalidUTF8),
		rejectDuplicateNames: !d.cfg.Has(allowDuplicateNames),
		decoding:             true,
		scratch:              d.vs.scratch[:0],
	}
}

// Options returns the options of the decoder: the ones it was constructed or last reset with, which follow a
// later Reset.
func (d *Decoder) Options() Options {
	return &d.d.cfg
}

func (d *decoder) prevOffset() int64  { return d.base + int64(d.prevStart) }
func (d *decoder) prevBuffer() []byte { return d.buf[d.prevStart:d.prevEnd] }

// minRead is the room which the buffer has for a read.
const minRead = 512

// fetch reads more input to the end of the buffer. It may move the buffer, and drop the input before the last
// token: it returns the number of bytes by which the positions in the buffer moved back. At the end of the
// input, it returns io.EOF, and at an error of the reader, the error in an ioError.
func (d *decoder) fetch() (int, error) {
	if d.rerr != nil {
		return 0, d.rerr
	}
	if d.r == nil {
		d.rerr = io.EOF // a zero value
		return 0, io.EOF
	}
	if d.bb != nil {
		switch {
		case d.bb.Len() == 0:
			d.rerr = io.EOF
			return 0, io.EOF
		case d.shared:
			// the buffer was written to after the decoder took its array, which the write may have changed: the
			// input is left in the buffer.
			d.rerr = &ioError{err: errBufferWriteAfterNext}
			return 0, d.rerr
		}
		b := d.bb.Next(d.bb.Len())
		if len(d.buf) == 0 {
			d.buf, d.shared = b, true
			return 0, nil
		}
		shift := d.makeRoom(len(b))
		d.buf = append(d.buf, b...)
		return shift, nil
	}
	shift := 0
	if cap(d.buf)-len(d.buf) < minRead+textcoder.InputRoom || d.shared {
		shift = d.makeRoom(minRead + textcoder.InputRoom)
	}
	for {
		// the room after the input is left to a caller which reads the buffer in place ( see textcoder.InputRoom )
		n, err := d.r.Read(d.buf[len(d.buf) : cap(d.buf)-textcoder.InputRoom])
		if n > 0 {
			// an error which comes with input is dropped, as encoding/json/jsontext drops it: the reader reports
			// it again at the next read, if it lasts.
			d.buf = d.buf[:len(d.buf)+n]
			return shift, nil
		}
		if err == nil {
			continue // no input, which encoding/json/jsontext reads again for, as long as it takes
		}
		if _, failed := d.r.(*failedInput); !failed && err != io.EOF {
			// but the error of a decoder which failed, which is returned as it is
			err = &ioError{err: err}
		}
		d.rerr = err
		return shift, err
	}
}

// makeRoom makes room for n more bytes at the end of the buffer, dropping the input before the last token only
// when the buffer is full, so that each byte is moved a few times at most. It returns by how much the
// positions moved back.
func (d *decoder) makeRoom(n int) int {
	if cap(d.buf)-len(d.buf) >= n && !d.shared {
		return 0
	}
	// the start of the last token, or of the buffer if it is before the buffer
	shift := max(d.prevStart, 0)
	keep := len(d.buf) - shift
	if keep+n <= cap(d.buf) && !d.shared {
		copy(d.buf, d.buf[shift:])
		d.buf = d.buf[:keep]
	} else {
		b := make([]byte, keep, max(2*cap(d.buf), keep+n, 4096))
		copy(b, d.buf[shift:])
		d.buf, d.shared = b, false
	}
	d.base += int64(shift)
	d.pos -= shift
	d.prevStart -= shift
	d.prevEnd -= shift
	return shift
}

// skip returns the position of the first byte from pos which is not white space, reading more input as it
// needs. At the end of the input, it returns the error of the reader.
func (d *decoder) skip(pos int) (int, error) {
	for {
		if pos = skipSpace(d.buf, pos); pos < len(d.buf) {
			return pos, nil
		}
		shift, err := d.fetch()
		pos -= shift
		if err != nil {
			return pos, err
		}
	}
}

// charError is the error of the invalid character at pos, which is not expected at the place. A
// character which the buffer cuts is read whole first, so that the error shows it as the input has it.
func (d *decoder) charError(pos int, place charPlace, where int) error {
	for d.rerr == nil && !utf8.FullRune(d.buf[pos:]) {
		shift, _ := d.fetch()
		pos -= shift
	}
	return d.failAt(invalidChar(d.buf[pos:], place), pos, where)
}

// failAt is a SyntacticError at the position pos of the buffer, in the value where points.
func (d *decoder) failAt(err error, pos int, where int) error {
	return &SyntacticError{ByteOffset: d.base + int64(pos), JSONPointer: d.st.pointer(where), Err: err}
}

// endError is the error at the end of the input at pos: io.EOF where a top-level value may start, or else an
// unexpected one. A reader error which is not io.EOF is returned as it is.
func (d *decoder) endError(pos int, err error, where int) error {
	if err != io.EOF {
		return err
	}
	if d.st.depth() == 0 && pos == skipSpace(d.buf, d.pos) && where != pointNext {
		return io.EOF
	}
	return d.failAt(io.ErrUnexpectedEOF, pos, where)
}

// PeekKind returns the kind of the next token, without reading it.
//
// It returns KindInvalid at an error, which it keeps for the next read call to report: a PeekKind call must
// be followed by a read call.
func (d *Decoder) PeekKind() Kind {
	s := &d.d
	if !s.ready() {
		s.setUp()
	}
	s.invalidate()
	pos, err := s.beforeToken(true)
	s.peekErr = nil // an error of an earlier PeekKind is replaced
	if err != nil {
		s.peekErr = err // even io.EOF, which the next read reports once
		return KindInvalid
	}
	return kindOf(s.buf[pos])
}

// beforeToken skips the white space and the delimiter before the next token, and returns its position. peek is
// set for PeekKind, which takes an end delimiter of any kind: the read which follows checks it.
//
// A comma or a colon is taken first, and then compared with the delimiter which the next token needs: one which
// is not needed, or not the one needed, is an invalid character itself.
func (d *decoder) beforeToken(peek bool) (int, error) {
	// an error of the reader, even io.EOF, ends only the call which met it: the next one reads again.
	d.rerr = nil
	// most tokens have the delimiter which they need, with some white space, in the buffer.
	b := d.buf
	if pos := skipWS(b, d.pos); pos < len(b) {
		c := b[pos]
		need := d.delimBefore(c, peek)
		if c != ',' && c != ':' {
			if need == 0 {
				return pos, nil
			}
		} else if c == need {
			if next := skipWS(b, pos+1); next < len(b) {
				if c := b[next]; c != ',' && c != ':' && (c != '}' && c != ']' || need == ':') && d.delimBefore(c, peek) == need {
					return next, nil
				}
			}
		}
	}
	pos, err := d.skip(d.pos)
	if err != nil {
		return pos, d.endError(pos, err, pointAt)
	}
	found, at := byte(0), pos
	need := d.delimBefore(d.buf[pos], peek)
	if c := d.buf[pos]; c == ',' || c == ':' {
		found = c
		base := d.base
		pos, err = d.skip(pos + 1)
		at -= int(d.base - base) // the input which a fetch dropped
		if err != nil {
			// at the end of the input, or an error of the reader, a delimiter which the next token can't need is
			// reported first
			if found == need {
				return pos, d.endError(pos, err, pointAt)
			}
		} else if c := d.buf[pos]; (c == '}' || c == ']') && !d.st.last().needValue() {
			need = 0 // before an end delimiter of any kind, a delimiter is the error
		} else {
			need = d.delimBefore(c, peek)
		}
	}
	switch {
	case found == need:
		return pos, nil
	case found == 0:
		return pos, d.missingDelim(pos)
	case need == 0:
		return at, d.charError(at, placeStartOfValue, pointAt)
	default:
		// the delimiter of the other kind is reported as the character where the needed one is
		return at, d.missingDelim(at)
	}
}

// delimBefore is the delimiter which the next token, which starts with c, needs before it: a comma, a colon, or
// none.
func (d *decoder) delimBefore(c byte, peek bool) byte {
	l := d.st.last()
	switch {
	case d.st.depth() == 0:
		return 0
	case l.needValue():
		return ':'
	case l.count == 0:
		return 0
	case c == '}' && (l.object || peek), c == ']' && (!l.object || peek):
		return 0 // the end
	}
	return ','
}

// missingDelim is the error of the character at pos, where a delimiter is needed.
func (d *decoder) missingDelim(pos int) error {
	l := d.st.last()
	switch {
	case l.needValue():
		return d.charError(pos, placeAfterObjectName, pointAt)
	case l.object:
		// the end of an array in an object is reported in the level which contains the object.
		where := pointAt
		if d.buf[pos] == ']' {
			where = pointOut
		}
		return d.charError(pos, placeAfterObjectValue, where)
	}
	return d.charError(pos, placeAfterArrayElement, pointAt)
}

// ReadToken reads the next Token and advances the read offset. The token is only valid until the next Peek,
// Read or Skip call. It returns io.EOF at the end of the input.
func (d *Decoder) ReadToken() (Token, error) {
	return d.d.readToken()
}

// readToken reads the next token in one loop where the token and the delimiter before it are valid and whole in
// the buffer, as most tokens are, and else by readTokenSlow, from the same state.
func (d *decoder) readToken() (Token, error) {
	if !d.ready() {
		d.setUp()
	}
	d.invalidate()
	b := d.buf
	pos := skipWS(b, d.pos)
	if pos >= len(b) || len(d.st.levels) == 1 || d.peekErr != nil {
		return d.readTokenSlow() // the top level, which reads a stream of values, among others
	}
	l := &d.st.levels[len(d.st.levels)-1]
	c := b[pos]
	switch {
	case c == '}' && l.object && l.count%2 == 0, c == ']' && !l.object:
		if len(d.st.levels)-1 == d.st.outerDepth {
			return d.readTokenSlow() // the level an attached decoder is in
		}
		d.st.pop()
		d.done(pos+1, pos+1)
		if c == '}' {
			return EndObject, nil
		}
		return EndArray, nil
	case l.count == 0:
	case c == ',' && (!l.object || l.count%2 == 0), c == ':' && l.object && l.count%2 == 1:
		if pos = skipWS(b, pos+1); pos >= len(b) || b[pos] == '}' || b[pos] == ']' {
			return d.readTokenSlow()
		}
		c = b[pos]
	default:
		return d.readTokenSlow()
	}
	name := l.object && l.count%2 == 0
	switch c {
	case '"':
		n, ok := simpleStringEnd(b[pos:])
		f, err := strFlags(0), error(nil)
		if !ok {
			n, f, err = scanStringFrom(b[pos:], n, 0, strModeOf(!d.cfg.Has(allowInvalidUTF8), true))
		}
		if err != nil || name && !d.st.insertName(unquotedName(&d.vs.scratch, b[pos:pos+n], f), !d.cfg.Has(allowDuplicateNames)) {
			return d.readTokenSlow()
		}
		l.count++
		d.done(pos, pos+n)
		return Token{src: &d.raw, num: uint64(d.base + int64(pos))}, nil
	case '{', '[':
		if name || d.st.push(c == '{') != nil {
			return d.readTokenSlow()
		}
		d.done(pos+1, pos+1)
		if c == '{' {
			return BeginObject, nil
		}
		return BeginArray, nil
	case 'n', 't', 'f':
		lit := literalOf(Kind(c))
		if name || len(b)-pos <= len(lit) || string(b[pos:pos+len(lit)]) != lit {
			return d.readTokenSlow()
		}
		l.count++
		d.done(pos+len(lit), pos+len(lit))
		return literalToken(Kind(c)), nil
	}
	n := jsonnum.End(b[pos:])
	if name || n < 0 {
		return d.readTokenSlow()
	}
	l.count++
	d.done(pos, pos+n)
	return Token{src: &d.raw, num: uint64(d.base + int64(pos))}, nil
}

func (d *decoder) readTokenSlow() (Token, error) {
	if err := d.peekErr; err != nil {
		d.peekErr = nil
		return Token{}, err
	}
	pos, err := d.beforeToken(false)
	if err != nil {
		return Token{}, err
	}
	l := d.st.last()
	c := d.buf[pos]
	switch c {
	case '{', '[':
		if l.needName() {
			return Token{}, d.failAt(ErrNonStringName, pos, pointAt)
		}
		if err := d.st.push(c == '{'); err != nil {
			return Token{}, d.failAt(err, pos, pointNext)
		}
		d.done(pos+1, pos+1)
		if c == '{' {
			return BeginObject, nil
		}
		return BeginArray, nil
	case '}', ']':
		if err := d.closeError(pos); err != nil {
			return Token{}, err
		}
		d.st.pop()
		d.done(pos+1, pos+1)
		if c == '}' {
			return EndObject, nil
		}
		return EndArray, nil
	}
	name := l.needName()
	where := pointNext
	if name {
		where = pointAt
	}
	start, end, f, err := d.scanToken(pos, where)
	if err != nil {
		return Token{}, err
	}
	if name {
		if c != '"' {
			return Token{}, d.failAt(ErrNonStringName, start, pointAt)
		}
		if err := d.insertName(start, end, f); err != nil {
			return Token{}, err
		}
	}
	d.st.last().count++
	if k := kindOf(c); k != KindString && k != KindNumber {
		d.done(end, end) // a literal isn't in the buffer after
		return literalToken(k), nil
	}
	d.done(start, end)
	return Token{src: &d.raw, num: uint64(d.base + int64(start))}, nil
}

// closeError is the error of the end delimiter at pos, or nil if it closes the innermost object or array.
func (d *decoder) closeError(pos int) error {
	l := d.st.last()
	c := d.buf[pos]
	switch {
	case d.st.depth() == 0:
		return d.charError(pos, placeStartOfValue, pointAt)
	case d.st.outer != nil && d.st.depth() == d.st.outerDepth:
		// the level a decoder attached at the place of a value is in, which is not a part of the value
		return d.failAt(errEndOfReadPlace, pos, pointAt)
	case l.object != (c == '}'):
		if l.needValue() {
			return d.charError(pos, placeAfterObjectValue, pointHere)
		}
		if l.object {
			return d.charError(pos, placeStartOfValue, pointAt)
		}
		return d.charError(pos, placeStartOfValue, pointNext)
	case l.needValue():
		return d.failAt(errMissingValue, pos, pointAt)
	}
	return nil
}

// done records the token or value at buf[start:end] as the last one read.
func (d *decoder) done(start, end int) {
	d.prevStart, d.prevEnd, d.pos = start, end, end
}

// invalidate voids the last token or value, as every call which reads does, even one which fails, as
// encoding/json/jsontext does to catch a use of it: a raw Token of it panics, and the first byte of a Value of it
// is overwritten. The input of a bytes.Buffer, whose array the decoder reads, and an input which the decoder
// doesn't own stay as they are.
func (d *decoder) invalidate() {
	if d.bb == nil && !d.shared && d.prevEnd > d.prevStart {
		if d.prevStart >= 0 {
			d.buf[d.prevStart] = '#'
		}
		d.prevStart = d.prevEnd
	}
}

// insertName adds the name of the string at buf[start:end] to the innermost object.
func (d *decoder) insertName(start, end int, f strFlags) error {
	name := unquotedName(&d.vs.scratch, d.buf[start:end], f)
	if !d.st.insertName(name, !d.cfg.Has(allowDuplicateNames)) {
		ptr := d.st.namePointer(name)
		return &SyntacticError{ByteOffset: d.base + int64(start), JSONPointer: ptr, Err: ErrDuplicateName}
	}
	return nil
}

// unquotedName is the value of the string s, which scanString took with the flags f: the bytes between its quotes,
// or, if it has escape sequences or invalid UTF-8, its value unquoted in scratch.
func unquotedName(scratch *[]byte, s []byte, f strFlags) []byte {
	if f&(strEscaped|strInvalidUTF8) != 0 {
		*scratch = appendUnquoted((*scratch)[:0], s, f)
		return *scratch
	}
	return s[1 : len(s)-1]
}

// scanToken scans the string, number or literal at pos, reading more input while the buffer cuts it. It
// returns the position of the token, which a read may have moved, and its end.
func (d *decoder) scanToken(pos int, where int) (int, int, strFlags, error) {
	var (
		n   int
		f   strFlags
		st  numState
		err error
	)
	c := d.buf[pos]
	validUTF8 := !d.cfg.Has(allowInvalidUTF8)
	// most tokens are whole in the buffer
	switch c {
	case '"':
		if n, f, err := scanString(d.buf[pos:], strModeOf(validUTF8, true)); err == nil {
			return pos, pos + n, f, nil
		}
	case 'n', 't', 'f':
	default:
		if kindOf(c) != KindNumber {
			if (c == ',' || c == ':') && d.st.last().count == 0 {
				where = pointAt // a delimiter before the first value
			}
			return pos, pos, 0, d.charError(pos, placeStartOfValue, where)
		}
		if n := jsonnum.End(d.buf[pos:]); n > 0 {
			return pos, pos + n, 0, nil
		}
	}
	for {
		b := d.buf[pos:]
		var cut bool     // the token may continue after the buffer
		var cutChar bool // an invalid character which the buffer cuts, read again to show it whole
		switch c {
		case '"':
			if n > 0 && plainEnd(b, n) == len(b) {
				// plain characters continue the string to the end of the buffer, as a slow reader gives them
				n, cut = len(b), true
				break
			}
			n, f, err = scanStringFrom(b, max(n, 1), f, strModeOf(validUTF8, d.rerr != io.EOF))
			cut = err == io.ErrUnexpectedEOF
		case 'n', 't', 'f':
			n, err = scanLiteral(b, literalOf(Kind(c)))
			cut = err == io.ErrUnexpectedEOF
		default:
			if st.inDigits() && n > 0 {
				if j := jsonnum.ScanDigits(b, n); j == len(b) {
					// digits continue the number to the end of the buffer, as a slow reader gives them
					n, st.done, cut = j, j, true
					break
				}
			}
			n, st, err = scanNumberFrom(b, n, st)
			cut = err == nil && n == len(b) || err == io.ErrUnexpectedEOF
		}
		if cut && d.rerr != io.EOF {
			// the token continues after the buffer
			if c == 'n' || c == 't' || c == 'f' {
				n = 0
			}
		} else if _, ok := err.(*textError); ok && d.rerr == nil && !utf8.FullRune(b[n:]) {
			// an invalid character which the buffer cuts is shown whole: the token is scanned again with more
			cutChar = true
			n, f, st = 0, 0, numState{}
		} else {
			break
		}
		shift, ferr := d.fetch()
		pos -= shift
		if ferr != nil && ferr != io.EOF {
			if cutChar {
				continue // the error which the input read shows, as far as it was read, which the scan finds again
			}
			return pos, pos, 0, ferr
		}
	}
	if err == nil {
		return pos, pos + n, f, nil
	}
	if err == io.ErrUnexpectedEOF && kindOf(c) == KindNumber {
		n = 0 // a number which the end of the input cuts is reported at its start
	}
	return pos, pos, 0, d.failAt(err, pos+n, where)
}

// ReadValue returns the next raw JSON value and advances the read offset. The value has no leading and
// trailing white space, and holds the exact bytes of the input, which may contain invalid UTF-8 if
// AllowInvalidUTF8 is set.
//
// The value is only valid until the next Peek, Read or Skip call, and must not be changed while the decoder is
// used. If the next token is the end of an object or array, it reports a SyntacticError and doesn't change the
// state of the decoder. It returns io.EOF at the end of the input.
func (d *Decoder) ReadValue() (Value, error) {
	s := &d.d
	start, end, err := s.readValue()
	if err != nil {
		return nil, err
	}
	return s.buf[start:end:end], nil // without the room after it, which is the input after the value
}

// SkipValue skips the next value. An object or array is read token by token, as ReadToken reads it, so that
// at an error the decoder is where the error is; another value is read as ReadValue reads it.
func (d *Decoder) SkipValue() error {
	s := &d.d
	if k := d.PeekKind(); k == KindBeginObject || k == KindBeginArray {
		depth := s.st.depth()
		for {
			if _, err := s.readToken(); err != nil {
				return err
			}
			if s.st.depth() == depth {
				return nil
			}
		}
	}
	_, _, err := s.readValue()
	return err
}

func (d *decoder) readValue() (int, int, error) {
	if !d.ready() {
		d.setUp()
	}
	d.invalidate()
	if err := d.peekErr; err != nil {
		d.peekErr = nil
		return 0, 0, err
	}
	pos, err := d.beforeToken(false)
	if err != nil {
		return 0, 0, err
	}
	l := d.st.last()
	switch c := d.buf[pos]; {
	case c == '}' || c == ']':
		// a value can't start with an end delimiter; an end delimiter which doesn't fit the innermost object or
		// array is reported as ReadToken reports it, but the end of an object which needs a value.
		if !(l.needValue() && c == '}') {
			if err := d.closeError(pos); err != nil {
				return 0, 0, err
			}
		}
		return 0, 0, d.charError(pos, placeStartOfValue, pointNext)
	case l.needName() && c == '"':
		start, end, f, err := d.scanToken(pos, pointAt)
		if err != nil {
			return 0, 0, err
		}
		if err := d.insertName(start, end, f); err != nil {
			return 0, 0, err
		}
		l.count++
		d.done(start, end)
		return start, end, nil
	}
	// a value where a name is must be a string, which is reported once the value is read.
	name := l.needName()
	count := l.count
	if d.vs.final = d.rerr == io.EOF; !name && (d.bb != nil || d.vs.final) {
		// the value is whole in the buffer, unless it is not valid: the buffer has all the input which there is.
		// From another reader, a buffer which cuts the value would be scanned twice.
		names := len(d.st.names.ends)
		if end, ok := d.vs.scanFast(d.buf, pos); ok {
			d.done(pos, end)
			return pos, end, nil
		}
		d.st.names.truncate(names)
	}
	rp := resumePoint{pos: pos, base: d.st.depth()}
	start := pos
	for {
		d.vs.final = d.rerr == io.EOF
		end, serr := d.vs.resume(d.buf, &rp)
		if serr == nil && name {
			d.vs.restore(rp.base, count)
			return 0, 0, d.failAt(ErrNonStringName, start, pointAt)
		}
		if serr == nil {
			d.done(start, end)
			return start, end, nil
		}
		cutChar := false // an invalid character which the buffer cuts, read again to show it whole
		if serr != errCut {
			d.vs.restore(rp.base, count)
			if _, ok := serr.err.(*textError); !ok || d.rerr != nil || utf8.FullRune(d.buf[serr.pos:]) {
				return 0, 0, &SyntacticError{ByteOffset: d.base + int64(serr.pos), JSONPointer: serr.ptr, Err: serr.err}
			}
			// the value is read again with more input
			rp = resumePoint{pos: start, base: rp.base}
			cutChar = true
		} else if d.rerr == io.EOF {
			// the end of the input ended a number at the end of the buffer: the reader is read again for the
			// rest of the value, as encoding/json/jsontext reads it.
			d.rerr = nil
		}
		for {
			shift, ferr := d.fetch()
			rp.pos -= shift
			start -= shift
			if ferr != nil {
				// at the end of the input, the value is scanned to its end; an error of the reader is the error
				// of the value, but after an invalid character, whose error the scan finds again as far as the
				// input was read.
				if ferr == io.EOF || cutChar {
					break
				}
				err := d.cutError(&rp, ferr)
				d.vs.restore(rp.base, count)
				return 0, 0, err
			}
			if rp.tok == 0 && rp.pos+1 == len(d.buf) && isSpace(d.buf[rp.pos]) {
				rp.pos++ // a byte of white space, as a slow reader gives them, without a call
				continue
			}
			if !d.stillCut(&rp) {
				break
			}
		}
	}
}

// cutError is the error of the reader, err, in a value which the buffer cuts at rp: where the levels which the
// value opened have a pointer, it is shown there, where the value ends as the end of the input would be: at the
// start of a number which is cut, and after the part of a literal.
func (d *decoder) cutError(rp *resumePoint, err error) error {
	if len(d.st.pointerBytes(int(rp.where), rp.base)) == 0 {
		return err
	}
	pos := rp.pos + rp.tok
	if rp.pos < len(d.buf) { // a token which is cut
		switch kindOf(d.buf[rp.pos]) {
		case KindNumber:
			pos = rp.pos
		case KindNull, KindTrue, KindFalse:
			pos = rp.pos + rp.lit
		}
	}
	return &SyntacticError{ByteOffset: d.base + int64(pos), JSONPointer: d.st.pointer(int(rp.where)), Err: err}
}

// stillCut reads the input which a fetch added to a value which the buffer cut, and reports whether the value is
// still cut at the end of the buffer: the white space, strings and numbers which a slow reader gives a byte at a
// time are read here, without going through the states of the scanner.
func (d *decoder) stillCut(rp *resumePoint) bool {
	b := d.buf
	if rp.tok == 0 {
		rp.pos = skipSpace(b, rp.pos)
		return rp.pos == len(b)
	}
	// a string or a number, which a cut token is
	if b[rp.pos] == '"' {
		if j := plainEnd(b, rp.pos+rp.tok); j == len(b) {
			rp.tok = j - rp.pos // plain characters continue the string, which costs one call
			return true
		}
		n, f, err := scanStringFrom(b[rp.pos:], rp.tok, rp.str, strModeOf(d.vs.validUTF8, true))
		if err == io.ErrUnexpectedEOF {
			rp.tok, rp.str = n, f
			return true
		}
		return false
	}
	n, st, err := scanNumberFrom(b[rp.pos:], rp.tok, rp.num)
	if rp.pos+n == len(b) && (err == nil || err == io.ErrUnexpectedEOF) {
		rp.tok, rp.num = n, st
		return true
	}
	return false
}

// InputOffset returns the offset in the input after the last token or value which was read. The decoder may
// have read more of the input into its buffer.
func (d *Decoder) InputOffset() int64 {
	return d.d.base + int64(d.d.pos)
}

// UnreadBuffer returns the input in the buffer of the decoder which was read from the reader but not by
// Decoder.ReadToken or Decoder.ReadValue: it may be any number of bytes, which may not be valid JSON. How much
// input the decoder buffers may change over time.
//
// The input after the last token or value is this buffer followed by the rest of the reader.
//
// The buffer must not be changed while the decoder is used, and is valid until the next Peek, Read or Skip
// call.
func (d *Decoder) UnreadBuffer() []byte {
	return d.d.buf[d.d.pos:]
}

// StackDepth returns the number of the objects and arrays which are open in the input which was read: 0 at the
// top level, before any token, after a top-level value and between top-level values, 1 in a top-level object
// or array, and so on.
func (d *Decoder) StackDepth() int {
	if !d.d.ready() {
		d.d.setUp()
	}
	return d.d.st.depth() + d.d.st.base // base: the levels around a decoder attached at the place of a value
}

// StackIndex returns the kind and the length of the level i of the stack, which is from 0 to
// Decoder.StackDepth: KindInvalid for the level 0, KindBeginObject for an object, KindBeginArray for an array.
// The length of an object counts its names and values: a complete object has an even length.
func (d *Decoder) StackIndex(i int) (Kind, int64) {
	if !d.d.ready() {
		d.d.setUp()
	}
	if d.d.st.outer != nil {
		return d.d.st.full().index(i) // a decoder attached at the place of a value
	}
	return d.d.st.index(i)
}

// StackPointer returns a JSON Pointer (RFC 6901) to the last value which was read.
func (d *Decoder) StackPointer() Pointer {
	if !d.d.ready() {
		d.d.setUp()
	}
	return d.d.st.pointer(pointLast)
}

// index is StackIndex of the stack.
func (s *stack) index(i int) (Kind, int64) {
	if i > s.depth() {
		_ = s.levels[1:][i] // the panic of encoding/json/jsontext, whose stack has the levels of the depth
	}
	l := s.levels[i]
	switch {
	case i == 0:
		return KindInvalid, l.count
	case l.object:
		return KindBeginObject, l.count
	}
	return KindBeginArray, l.count
}
