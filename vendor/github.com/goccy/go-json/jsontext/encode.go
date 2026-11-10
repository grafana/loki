package jsontext

import (
	"bytes"
	"io"
	"math"
	"math/bits"
	"slices"
	"unsafe"

	"github.com/goccy/go-json/internal/floatfmt"
)

// Encoder is a streaming encoder of raw JSON tokens and values. It writes a stream of top-level JSON values,
// each followed by a line feed.
//
// Encoder.WriteToken and Encoder.WriteValue calls may be interleaved. For example, the JSON value
//
//	{"name":"value","array":[null,false,true,3.14159],"object":{"k":"v"}}
//
// can be written by these calls (ignoring errors):
//
//	e.WriteToken(BeginObject)        // {
//	e.WriteToken(String("name"))     // "name"
//	e.WriteToken(String("value"))    // "value"
//	e.WriteValue(Value(`"array"`))   // "array"
//	e.WriteToken(BeginArray)         // [
//	e.WriteToken(Null)               // null
//	e.WriteToken(False)              // false
//	e.WriteValue(Value("true"))      // true
//	e.WriteToken(Float(3.14159))     // 3.14159
//	e.WriteToken(EndArray)           // ]
//	e.WriteValue(Value(`"object"`))  // "object"
//	e.WriteValue(Value(`{"k":"v"}`)) // {"k":"v"}
//	e.WriteToken(EndObject)          // }
//
// which is one of many sequences of calls which write it.
type Encoder struct {
	e encoder
}

// encoder is the state of an Encoder.
type encoder struct {
	cfg config
	st  stack
	vs  valueScanner

	w    io.Writer
	bb   *bytes.Buffer // w, whose array the output is appended to
	buf  []byte        // the output which is not written to w yet
	base int64         // the offset of buf[0] in the output

	// derived is the cfg which the options of vs were set from, once derivedOK is set, so that an encoder which
	// is used again with the same options doesn't set them again.
	derived   config
	derivedOK bool

	avail    []byte // the buffer of AvailableBuffer, which the output doesn't share
	maxValue int    // the OR of the lengths of the values written since the last reset, which sizes avail

	// invalid is whether a value which MarshalEncode of the v2 json package was writing failed in an object or an
	// array which it opened: nothing can be written then, until a Reset. The encoder is kept not ready, so that the
	// writes, which check that first, go to the paths which report it.
	invalid bool
	// attached is whether the encoder was attached by the v2 json package after it was set up ( see
	// attachEncoder ), which attaches it again without setting it up.
	attached bool
	// closedPast is whether the attached encoder was asked to end the object or the array which it is attached
	// in, which is not its own: the value which it was given the place of fails ( see detachEncoder ).
	closedPast bool
	// failErr is the error of a call which failed at the attached encoder ( see textcoder.Fail ).
	failErr error
}

// NewEncoder constructs a streaming encoder which writes to w, with the options. It writes its buffer to w when
// the buffer is full enough, or when a top-level value is complete.
//
// If w is a bytes.Buffer, the encoder appends to the buffer directly, without copying the output from a buffer
// of its own.
func NewEncoder(w io.Writer, opts ...Options) *Encoder {
	e := new(Encoder)
	e.Reset(w, opts...)
	return e
}

// Reset resets the encoder to write to w anew, with the options.
func (e *Encoder) Reset(w io.Writer, opts ...Options) {
	switch {
	case e == nil:
		panic("jsontext: invalid nil Encoder")
	case w == nil:
		panic("jsontext: invalid nil io.Writer")
	}
	e.e.reset(w, opts)
}

func (e *encoder) reset(w io.Writer, opts []Options) {
	var c config // opts may hold the options of e, which Options returned
	c.Apply(opts)
	e.cfg = c
	e.init(w)
}

// ready reports whether the encoder is set up: its scanner points to its stack.
func (e *encoder) ready() bool { return e.vs.st == &e.st }

// setUp sets up an encoder which is not ready: a zero value, as one with no writer, which keeps its output, as
// encoding/json/jsontext takes it, or a copy of an encoder, which takes pointers and arrays of its own: it writes
// on its own as a copy of an encoder of encoding/json/jsontext does, and the encoder it was copied from keeps its
// state.
func (e *encoder) setUp() {
	if e.invalid {
		return
	}
	if len(e.st.levels) == 0 {
		e.init(nil)
		return
	}
	e.vs.st = &e.st
	e.st.own()
	e.buf = slices.Clone(e.buf) // the output which is not written yet, which a flush writes as any output
	e.vs.lines, e.vs.scratch, e.vs.ro = nil, nil, reorderer{}
	e.avail = make([]byte, 0, cap(e.avail)) // of the capacity it had, which a Reset keeps
}

// writeTokenSetUp is writeToken of an encoder which is not ready, which is set up first.
func (e *encoder) writeTokenSetUp(t Token) error {
	if e.invalid {
		return e.failAt(errInvalidNamespace, len(e.buf), pointNext)
	}
	e.setUp()
	return e.writeToken(t)
}

// writeValueSetUp is writeValue of an encoder which is not ready, which is set up first.
func (e *encoder) writeValueSetUp(v Value) error {
	if e.invalid {
		return e.failAt(errInvalidNamespace, len(e.buf), pointNext)
	}
	e.setUp()
	return e.writeValue(v)
}

// init resets the state of the encoder to write to w, which is nil for none, with the options of e.cfg.
func (e *encoder) init(w io.Writer) {
	if e.attached {
		e.derivedOK = false // the white space of an attached encoder, which its options don't tell ( see derive )
	}
	e.invalid, e.attached = false, false
	if !e.ready() {
		// a zero value, or a copy, whose arrays are the ones of the encoder it was copied from
		e.st, e.buf = stack{}, nil
		e.avail = make([]byte, 0, cap(e.avail)) // of the capacity it had, which a Reset keeps
		e.vs.st, e.vs.lines, e.vs.scratch, e.vs.ro = &e.st, nil, nil, reorderer{}
	}
	e.st.reset()
	if e.st.outer != nil {
		e.st.outer = nil // of an attach by the v2 json package
	}
	if _, wasBuffer := e.w.(*bytes.Buffer); wasBuffer {
		e.buf = nil // the array of the bytes.Buffer written before, which stays its own, or of a nil one
	}
	e.w = w
	e.bb, _ = w.(*bytes.Buffer)
	e.buf = e.buf[:0]
	if e.bb != nil {
		e.buf = e.bb.AvailableBuffer()
	}
	e.base, e.maxValue = 0, 0
	if !e.derivedOK || !e.cfg.Same(&e.derived) {
		e.derive()
	}
	// the fields are set one by one: a literal of the scanner would be built in a temporary and copied.
	vs := &e.vs
	vs.write, vs.keep = true, false
	vs.spaced = e.vs.ws.multiline || e.vs.ws.colon || e.vs.ws.comma
	vs.run = 0
	vs.wsFrom, vs.wsEnd = 0, 0
	if vs.in != nil || vs.out != nil {
		vs.in, vs.out = nil, nil
	}
}

// derive sets the fields which follow from the options, e.cfg.
func (e *encoder) derive() {
	vs := &e.vs
	vs.ws = whitespaceOf(&e.cfg)
	if e.attached {
		// the output of a method or a function of the v2 json package, which formats the whole output after it: the
		// encoder writes it compact, as the encoder of the package writes the rest, but reports its options.
		vs.ws = whitespace{}
	}
	vs.esc = escapesOf(&e.cfg)
	vs.validUTF8 = !e.cfg.Has(allowInvalidUTF8)
	vs.rejectDuplicateNames = !e.cfg.Has(allowDuplicateNames)
	vs.lazyNames = !vs.rejectDuplicateNames
	vs.decoding = false
	vs.final = true
	vs.preserve = e.cfg.Has(preserveRawStrings)
	vs.canonInts = e.cfg.Has(canonicalizeRawInts)
	vs.canonFlts = e.cfg.Has(canonicalizeRawFloats)
	vs.reorder = e.cfg.Has(reorderRawObjects)
	vs.lines = vs.lines[:0] // the lines of the white space of other options
	// whitespace may have set the defaults of Multiline in cfg: the options which were given differ from it, and
	// are derived again.
	e.derived, e.derivedOK = e.cfg, true
}

// Options returns the options of the encoder: the ones it was constructed or last reset with, and, for Multiline,
// the space after a colon and the indentation it takes if they are not set. They follow a later Reset.
func (e *Encoder) Options() Options {
	return &e.e.cfg
}

// OutputOffset returns the offset in the output after the last token or value which was written. The encoder
// may not have written all of it to the writer yet.
func (e *Encoder) OutputOffset() int64 {
	return e.e.base + int64(len(e.e.buf))
}

// AvailableBuffer returns an empty buffer which may have room, to build a Value for the next call of
// Encoder.WriteValue:
//
//	b := e.AvailableBuffer()
//	b = append(b, '"')
//	b = appendString(b, v) // append the string formatting of v
//	b = append(b, '"')
//	... := e.WriteValue(b)
//
// WriteValue takes a valid JSON value: a Value built as raw bytes takes more care than a Token of a
// constructor such as String.
func (e *Encoder) AvailableBuffer() []byte {
	// a buffer of its own: the room of the output would be written over by the value built in it. Its capacity
	// is the power of two above the values written, as the one of encoding/json/jsontext.
	if n := 1 << bits.Len(uint(e.e.maxValue|63)); cap(e.e.avail) < n {
		e.e.avail = make([]byte, 0, n)
	}
	return e.e.avail[:0]
}

// StackDepth returns the number of the objects and arrays which are open in the output: 0 at the top level,
// before any token, after a top-level value and between top-level values, 1 in a top-level object or array, and
// so on.
func (e *Encoder) StackDepth() int {
	if !e.e.ready() {
		e.e.setUp()
	}
	if e.e.st.outer != nil {
		return e.e.st.full().depth()
	}
	return e.e.st.depth()
}

// StackIndex returns the kind and the length of the level i of the stack, which is from 0 to
// Encoder.StackDepth: KindInvalid for the level 0, KindBeginObject for an object, KindBeginArray for an array.
// The length of an object counts its names and values: a complete object has an even length.
func (e *Encoder) StackIndex(i int) (Kind, int64) {
	if !e.e.ready() {
		e.e.setUp()
	}
	if e.e.st.outer != nil {
		return e.e.st.full().index(i)
	}
	return e.e.st.index(i)
}

// StackPointer returns a JSON Pointer (RFC 6901) to the last value which was written.
func (e *Encoder) StackPointer() Pointer {
	if !e.e.ready() {
		e.e.setUp()
	}
	return e.e.st.pointer(pointLast)
}

// failAt is a SyntacticError at the position pos of the output buffer.
func (e *encoder) failAt(err error, pos int, where int) error {
	return &SyntacticError{ByteOffset: e.base + int64(pos), JSONPointer: e.st.pointer(where), Err: err}
}

// appendDelim appends the comma or colon and the white space before a token of kind k, for which the grammar
// was checked.
func (e *encoder) appendDelim(b []byte, k Kind) []byte {
	l := e.st.last()
	if e.st.depth() == 0 {
		return b
	}
	if l.needValue() {
		// a colon, which is also the delimiter of the offset of an error of an end delimiter there
		b = append(b, ':')
		if e.vs.ws.colon {
			b = append(b, ' ')
		}
		return b
	}
	if k == KindEndObject || k == KindEndArray {
		if l.count > 0 && e.vs.ws.multiline {
			b = e.vs.ws.appendLine(b, e.st.depth()-1)
		}
		return b
	}
	if l.count > 0 {
		b = e.vs.ws.appendComma(b)
	}
	if e.vs.ws.multiline {
		b = e.vs.ws.appendLine(b, e.st.depth())
	}
	return b
}

// WriteToken writes the next token and advances the write offset.
//
// The token must fit the grammar: a number where the encoder needs an object name, which is always a string,
// or the end of an object where an array ends, is an error. At an invalid token, it reports a SyntacticError,
// and the state of the encoder doesn't change: the offset of the error is Encoder.OutputOffset plus the
// delimiters and white space which would precede the token.
func (e *Encoder) WriteToken(t Token) error {
	return e.e.writeToken(t)
}

func (e *encoder) writeToken(t Token) error {
	if !e.ready() {
		return e.writeTokenSetUp(t)
	}
	k := t.Kind()
	l := e.st.last()
	if misplaced(l, k, e.st.depth()) {
		return e.misplacedError(l, k)
	}
	b := e.buf
	switch {
	case e.vs.spaced:
		b = e.appendDelim(b, k)
	case len(e.st.levels) == 1 || l.count == 0 || k == KindEndObject || k == KindEndArray:
		// no delimiter in an output without white space: at the top level, before the first token of a level,
		// and before an end.
	case l.object && l.count&1 == 1:
		b = append(b, ':')
	default:
		b = append(b, ',')
	}
	pos := len(b)
	var err error
	switch k {
	case KindNull, KindTrue, KindFalse:
		b = append(b, literalOf(k)...)
	case KindBeginObject, KindBeginArray:
		if err := e.st.push(k == KindBeginObject); err != nil {
			return e.failAt(err, pos, pointNext)
		}
		if e.cfg.Value&stringTag != 0 {
			e.cfg.Value &^= stringTag
		}
		e.setBuf(append(b, byte(k)))
		return e.endValue()
	case KindEndObject, KindEndArray:
		if e.st.depth() == e.st.outerDepth {
			return e.closePastError() // the level an attached encoder is in, which misplaced lets through
		}
		e.st.pop()
		e.setBuf(append(b, byte(k)))
		return e.endValue()
	case KindString:
		b, err = e.appendString(b, t, l.needName())
	default:
		if t.src == &floatSource {
			b = floatfmt.AppendFloat(b, math.Float64frombits(t.num), 64) // a finite number, whose kind is KindNumber
		} else {
			b, err = e.appendNumberToken(b, t)
		}
	}
	if err != nil {
		return err
	}
	l.count++
	e.setBuf(b)
	return e.endValue()
}

// misplaced reports whether a token of kind k doesn't fit the grammar in the innermost level l at depth.
func misplaced(l *level, k Kind, depth int) bool {
	switch k {
	case KindEndObject, KindEndArray:
		return depth == 0 || l.object != (k == KindEndObject) || l.needValue()
	case KindInvalid:
		return true
	}
	return l.needName() && k != KindString
}

// closePastError is the error of the end of the object or the array which an attached encoder is attached in: the
// encoder writes nothing more, and the value which it was given the place of fails.
func (e *encoder) closePastError() error {
	err := e.failAt(errEndOfPlace, len(e.buf), pointNext)
	e.closedPast, e.invalid, e.vs.st = true, true, nil
	return err
}

// misplacedError is the error of a token of kind k which is misplaced in the innermost level l.
func (e *encoder) misplacedError(l *level, k Kind) error {
	switch {
	case k == KindInvalid:
		return e.failAt(errInvalidToken, len(e.buf)+e.delimLen(k), pointNext)
	case k != KindEndObject && k != KindEndArray:
		return e.failAt(ErrNonStringName, len(e.buf)+e.delimLen(k), pointAt)
	case e.st.depth() == 0 || l.object != (k == KindEndObject):
		return e.failAt(errMismatchDelim, len(e.buf)+e.delimLen(k), pointNext)
	}
	return e.failAt(errMissingValue, len(e.buf)+e.delimLen(k), pointAt)
}

// delimLen is the length of the delimiter and the white space which precede a token of kind k.
func (e *encoder) delimLen(k Kind) int {
	var buf [64]byte
	return len(e.appendDelim(buf[:0], k))
}

// appendString appends the string token t, a name of an object if name is set.
func (e *encoder) appendString(b []byte, t Token, name bool) ([]byte, error) {
	pos := len(b)
	switch t.source().form {
	case formString:
		var valid bool
		b, valid = appendQuoted(b, t.str, e.vs.esc)
		if !valid && e.vs.validUTF8 {
			return b, e.failAt(errInvalidUTF8, pos, pointNext)
		}
		if name {
			if valid && e.vs.rejectDuplicateNames && e.st.addNewName(readOnlyBytes(t.str)) {
				return b, nil // a new name of an object of few names, as most are, without the calls of insertName
			}
			return b, e.insertName(pos, readOnlyBytes(t.str), !valid) // the name is copied
		}
		return b, nil
	case formFloat, formFloat32:
		// NaN or an infinity
		b = append(b, '"')
		b = appendFloatText(b, math.Float64frombits(t.num), 64)
		b = append(b, '"')
		if name {
			return b, e.insertName(pos, b[pos+1:len(b)-1], false)
		}
		return b, nil
	}
	raw := t.raw()
	_, f, err := scanString(raw, strModeOf(e.vs.validUTF8, false))
	if err != nil {
		return b, e.failAt(err, pos, pointNext)
	}
	b = appendRawString(b, raw, f, e.vs.esc, e.cfg.Has(preserveRawStrings))
	if name {
		var buf [64]byte
		return b, e.insertName(pos, appendUnquoted(buf[:0], raw, f), false)
	}
	return b, nil
}

// insertName adds a name of the innermost object, written at pos.
func (e *encoder) insertName(pos int, name []byte, mangle bool) error {
	if mangle {
		name = appendValidUTF8(nil, name)
	}
	if !e.st.insertName(name, e.vs.rejectDuplicateNames) {
		ptr := e.st.namePointer(name)
		return &SyntacticError{ByteOffset: e.base + int64(pos), JSONPointer: ptr, Err: ErrDuplicateName}
	}
	return nil
}

// appendNumberToken appends the number token t.
func (e *encoder) appendNumberToken(b []byte, t Token) ([]byte, error) {
	if t.source().form != formRaw {
		return t.appendNumber(b), nil
	}
	raw := t.raw()
	if n, err := scanNumber(raw); err != nil || n < len(raw) {
		return b, e.failAt(invalidChar(raw[n:], placeInNumber), len(b)+n, pointNext)
	}
	if e.vs.numberKept(raw) {
		return append(b, raw...), nil
	}
	return appendCanonicalNumber(b, raw), nil
}

// flushSize is the size of the output from which it is written before a top-level value is complete: the
// buffer of a writer stays about as small as it, unless a token or value is larger.
const flushSize = 4 << 10

// endValue ends a token or value: after a complete top-level value, a line feed, and the output is written,
// as it is when the buffer is large.
// setBuf sets the output to b, which was appended to it: a slice of the output itself, if b is in its array, so
// that the pointer to the array, which most writes don't change, is not written again with a write barrier while
// the GC marks.
func (e *encoder) setBuf(b []byte) {
	if unsafe.SliceData(b) == unsafe.SliceData(e.buf) {
		e.buf = e.buf[:len(b)]
	} else {
		e.buf = b
	}
}

func (e *encoder) endValue() error {
	// it is inlined where the output is kept: in a value, or without a writer, which endOutput writes nothing to.
	if e.w == nil || len(e.st.levels) > 1 && len(e.buf) <= flushSize {
		return nil
	}
	return e.endOutput()
}

func (e *encoder) endOutput() error {
	if e.st.depth() > 0 {
		return e.flush()
	}
	if e.w == nil {
		return nil // a zero value, which keeps its output without the line feed of a stream
	}
	if !e.cfg.Has(omitTopLevelNewline) {
		e.buf = append(e.buf, '\n')
	}
	return e.flush()
}

// flush writes the buffer to the writer.
func (e *encoder) flush() error {
	if e.w == nil || len(e.buf) == 0 {
		return nil
	}
	n, err := e.w.Write(e.buf)
	e.base += int64(n)
	switch {
	case e.bb != nil:
		// the room of the buffer is kept at a quarter of its length at least, as encoding/json/jsontext keeps it,
		// so that the output after it doesn't make the encoder grow it.
		if avail := e.bb.Available(); avail < e.bb.Len()/4 {
			e.bb.Grow(avail + 1)
		}
		e.buf = e.bb.AvailableBuffer()
	case err == nil || n >= len(e.buf):
		// the output is written, as encoding/json/jsontext takes it where the writer reports no error, whatever n
		e.buf = e.buf[:0]
	case n > 0:
		e.buf = e.buf[:copy(e.buf, e.buf[n:])]
	}
	if err != nil {
		return &ioError{write: true, err: err}
	}
	return nil
}

// WriteValue writes the next raw value and advances the write offset. The encoder checks that the value is
// valid, and formats its white space and strings as the options say. With AllowInvalidUTF8, invalid UTF-8 is
// mangled as the Unicode replacement character, U+FFFD.
//
// The value must fit the grammar (see the examples of Encoder.WriteToken). At an invalid value, it reports a
// SyntacticError, and the state of the encoder doesn't change: the offset of the error is Encoder.OutputOffset
// plus the offset of the error in v.
func (e *Encoder) WriteValue(v Value) error {
	return e.e.writeValue(v)
}

func (e *encoder) writeValue(v Value) error {
	if !e.ready() {
		return e.writeValueSetUp(v)
	}
	e.maxValue |= len(v)
	if e.bb != nil && overlaps(v, e.buf[len(e.buf):]) {
		// a value in the free space of the bytes.Buffer, whose output is written over it before it is read
		v = bytes.Clone(v)
	}
	l := e.st.last()
	if len(v) > 1 && v[0] == '"' && !l.needName() && e.vs.plain() {
		// a string of ASCII which needs no escape and no check, as most strings which a method writes are, is
		// written as it is, without the scanner.
		if end, ok := simpleStringEnd(v); ok && end == len(v) {
			e.setBuf(append(e.appendDelim(e.buf, KindString), v...))
			l.count++
			return e.endValue()
		}
	}
	k := v.Kind()
	name := l.needName()
	b := e.appendDelim(e.buf, k)
	pos := len(b)
	e.vs.out = b
	end, serr := e.vs.scan(v, 0)
	b = e.vs.out
	e.vs.out = nil
	if serr != nil {
		return &SyntacticError{ByteOffset: e.base + int64(pos+serr.pos), JSONPointer: serr.ptr, Err: serr.err}
	}
	if end < len(v) {
		// white space after the value, or an error
		if end = skipSpace(v, end); end < len(v) {
			e.st.last().count--
			return e.failAt(invalidChar(v[end:], placeAfterTopLevel), pos+end, pointAt)
		}
	}
	if name {
		e.st.last().count--
		if k != KindString {
			return e.failAt(ErrNonStringName, pos, pointAt)
		}
		// the name, which the scanner wrote, is unquoted from the output
		var buf [64]byte
		if err := e.insertName(pos, appendUnquoted(buf[:0], b[pos:], strEscaped|strInvalidUTF8), false); err != nil {
			return err
		}
		e.st.last().count++
	}
	e.buf = b
	return e.endValue()
}
