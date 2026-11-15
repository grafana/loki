package decoder

import (
	stderrors "errors"
	"io"
	"math/bits"
	"reflect"
	"slices"
	"strconv"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/jsonnum"
	"github.com/goccy/go-json/internal/runtime"
)

// A value of a stream is decoded from the buffer by the decoders of a byte slice when the buffer holds it whole. An
// object or an array which the buffer doesn't hold whole is decoded element by element instead, without a read for
// it: each element is decoded the same way, from the buffer if it holds the element whole, and else element by element
// in turn, so that the reader is read only when the next token isn't whole in the buffer. So the decoding never reads
// past the token being read, which fails a value at the read of its first invalid byte, and the buffer holds a read
// and the longest token, whatever the size of a value: the bytes of the elements decoded are dropped when the buffer
// is filled again. The strings decoded then are copied out of the buffer, which they would keep alive, unless
// NoCopyStringOption is set.
//
// The objects and the arrays of structs, maps, slices, arrays, pointers to them and interface{} values are decoded
// so. A value of any other type, as one which has an UnmarshalJSON method, which takes the bytes of the whole value,
// or a value of another kind than its type, whose type error the decoder of its type reports, is read whole, checked
// element by element as it is read ( see wholeEnd ).

// errLongValue is what a scan of a value returns for an object or an array which doesn't end within the bytes it may
// read ( see scanValue ).
var errLongValue = stderrors.New("json: value too long to be read whole")

// scanFailed returns the error of Decode for the error err of the scan of the value at the cursor, which Decode decodes
// into p, of the pointer type typ, by dec: a value too long to be read whole is decoded element by element, as Decode
// does a value whole, or read whole and decoded if dec doesn't decode it so.
//
//go:noinline
func (s *Stream) scanFailed(err error, dec Decoder, typ, p unsafe.Pointer) error {
	if err != errLongValue {
		return s.fail(err)
	}
	ctx := s.ctx
	ctx.Option = s.Option
	start := s.offset + s.cursor
	if s.elements == nil {
		// the entries of the first steps of the lookups ( see indexFirstStep ), which a slice grown from nothing
		// would take a few allocations for
		s.elements = &bracketIndex{closes: make([]bracketClose, 0, indexFirstStep/4)}
	}
	if (s.Option.Flags & NoCopyStringOption) != 0 {
		// the strings of the value refer to the buffer, which is not reused in place then ( see grow )
		s.referred = true
	}
	*s.elements = bracketIndex{closes: s.elements.closes[:0], end: start}
	decoded, err := s.decodeElements(dec, p, 0)
	if !decoded {
		var end int64
		if end, err = s.wholeEnd(0); err == nil {
			err = s.decodeWhole(dec, p, 0, end)
		}
	}
	if err != nil {
		ctx.DiscardTypeError()
		return s.fail(err)
	}
	if countsScanned {
		s.scanned += s.offset + s.cursor - s.readStart
	}
	s.more.reset()
	if s.tokenState != tokenTopValue {
		s.tokenValueEnd()
	}
	if e := ctx.typeError; e != nil {
		ctx.typeError = nil
		if e.plain != nil {
			return e.plain
		}
		// the steps were added from the value of the error up to the root ( see addTypeErrorStep )
		slices.Reverse(e.path)
		return e.toError(e.path, runtime.TypeOfPtr(typ), start)
	}
	return nil
}

// restAfter returns the error of Decode for err, the error of a member or an element of the object, if object is set,
// or the array which has the cursor after it, nested in depth objects and arrays: a syntax error or the end of the
// input as it is, and any other error once the rest of the object or the array is read, as a value read whole is read
// before its error is returned, whose syntax error or end is the error then.
func (s *Stream) restAfter(err error, object bool, depth int64) error {
	if s.grammarFailure(err) {
		return err
	}
	for {
		var ok bool
		var rerr error
		if object {
			if _, _, _, ok, rerr = s.nextMember(false); ok {
				rerr = s.memberValue()
			}
		} else {
			ok, rerr = s.nextElement(false)
		}
		if rerr != nil {
			return rerr
		}
		if !ok {
			return err
		}
		if rerr = s.skipNested(depth); rerr != nil {
			return rerr
		}
	}
}

// grammarFailure reports whether err is a syntax error or the end of the input, which fail a value read whole before
// it is decoded: any other error is the one of an element which is decoded, as an unknown field.
func (s *Stream) grammarFailure(err error) bool {
	_, ok := err.(*errors.SyntaxError)
	return ok || err == io.ErrUnexpectedEOF || err == s.readErr
}

// skipAfter returns the error of the decoding of the value at the cursor, nested in depth objects and arrays, which is
// whole in the buffer, and moves the cursor after it: a syntax error of the value first, as for a value read whole.
func (s *Stream) skipAfter(err error, depth int64) error {
	next, serr := skipValue(s.buf[:s.length+1], s.cursor, depth)
	if serr != nil {
		return s.elementError(serr, depth)
	}
	s.cursor = next
	return s.totalOffsetError(err)
}

// decodeElements decodes the object or the array at the cursor, nested in depth objects and arrays, by dec into p
// element by element. It reports false, having read nothing, if dec doesn't decode it so.
func (s *Stream) decodeElements(dec Decoder, p unsafe.Pointer, depth int64) (bool, error) {
	c := s.buf[s.cursor]
	switch d := dec.(type) {
	case *structDecoder:
		if c == '{' {
			return true, s.decodeStructElements(d, p, depth)
		}
	case *mapDecoder:
		if c == '{' && !d.keyUnsupported {
			return true, s.decodeMapElements(d, p, depth)
		}
	case *sliceDecoder:
		if c == '[' {
			return true, s.decodeSliceElements(d, p, depth)
		}
	case *arrayDecoder:
		if c == '[' {
			return true, s.decodeArrayElements(d, p, depth)
		}
	case *ptrDecoder:
		prior := *(*unsafe.Pointer)(p)
		ptr := prior
		if ptr == nil {
			ptr = newValue(d.typ)
			*(*unsafe.Pointer)(p) = ptr
		}
		decoded, err := s.decodeElements(d.dec, ptr, depth)
		if err != nil || !decoded {
			// the pointer of a value read whole is set as ptrDecoder sets it, and the one of a value which fails is
			// left as it was, as the grammar of a value read whole fails it before it is decoded
			*(*unsafe.Pointer)(p) = prior
		}
		return decoded, err
	case *anonymousFieldDecoder:
		if *(*unsafe.Pointer)(p) == nil {
			*(*unsafe.Pointer)(p) = newValue(d.structType)
		}
		return s.decodeElements(d.dec, unsafe.Add(*(*unsafe.Pointer)(p), d.offset), depth)
	case *interfaceDecoder:
		return s.decodeInterfaceElements(d, p, depth)
	case *bytesDecoder:
		if c == '[' {
			return s.decodeElements(d.sliceDecoder, p, depth)
		}
	}
	return false, nil
}

// decodeNested decodes the value at the cursor, nested in depth objects and arrays, by dec into p: whole if the
// buffer holds it, and else element by element.
func (s *Stream) decodeNested(dec Decoder, p unsafe.Pointer, depth int64) error {
	if c := s.buf[s.cursor]; c != '{' && c != '[' && s.decodeScalar(dec, p, depth) {
		return nil
	}
	end, whole, err := s.elementEnd(depth)
	if err != nil {
		return err
	}
	if !whole {
		if decoded, err := s.decodeElements(dec, p, depth); decoded {
			return err
		}
		if end, err = s.wholeEnd(depth); err != nil {
			return err
		}
	}
	return s.decodeWhole(dec, p, depth, end)
}

// decodeScalar decodes the value at the cursor, which is not an object or an array, nested in depth objects and
// arrays, by dec into p, if dec decodes a string, a number or a boolean, or a nil interface{} of v2 which calls nothing
// of the caller, without the steps of decodeWhole: a string is read whole first, and a number or a literal is decoded
// from what has been read. It reports false if the value doesn't end before the end of what has been read, or has an
// error: decodeWhole then reads it whole and decodes it again, and reports the error.
func (s *Stream) decodeScalar(dec Decoder, p unsafe.Pointer, depth int64) bool {
	switch d := dec.(type) {
	case *stringDecoder, *intDecoder, *uintDecoder, *floatDecoder, *boolDecoder, *numberDecoder, v2StringDecoder:
	case *v2InterfaceDecoder:
		// the value of a nil interface{} without functions of unmarshaling, which the decoder of v1 decodes ( see
		// v2InterfaceDecoder.Decode ): a value which an interface value holds may be decoded by a method, and a
		// function may be called, which read the place of the value in the stream ( see V2Call.Value )
		if !d.empty || d.fromAny || *(*any)(p) != nil {
			return false
		}
	default:
		return false
	}
	// the value is decoded only when it is whole, so that the decoder writes nothing of a part of it
	switch c := s.buf[s.cursor]; {
	case c == '"':
		end, info, valid, err := s.scanStringInfo()
		if err != nil || !valid {
			return false
		}
		switch dec.(type) {
		case *stringDecoder, v2StringDecoder:
			if info.firstEscape < 0 && !info.nonASCII {
				// the bytes of a string without an escape, of ASCII, are its value, as decodeStringField and
				// decodeStringV2 make it: they are not scanned again
				ctx := s.ctx
				ctx.Buf = s.buf[:s.length+1]
				ctx.origin = ctx.Buf
				*(*string)(p) = ctx.makeString(s.buf[s.cursor+1 : end-1])
				ctx.Buf, ctx.origin = nil, nil
				s.cursor = end
				return true
			}
		}
	case c == '-' || c-'0' <= 9:
		// a number which a byte after it ends
		if n, place := jsonnum.Scan(s.buf[s.cursor:s.length]); place != jsonnum.Valid || s.cursor+int64(n) >= s.length {
			return false
		}
	default:
		if lit := literalOf(c); lit == "" || s.length-s.cursor < int64(len(lit)) {
			return false
		}
	}
	ctx := s.ctx
	had := ctx.typeError
	ctx.Buf = s.buf[:s.length+1]
	ctx.origin = ctx.Buf
	cursor, err := dec.Decode(ctx, s.cursor, depth, p)
	ctx.Buf, ctx.origin = nil, nil
	if err != nil {
		ctx.typeError = had
		return false
	}
	if had == nil && ctx.typeError != nil {
		s.keepTypeError()
	}
	s.cursor = cursor
	return true
}

// wholeEnd returns the position after the object or the array at the cursor, nested in depth objects and arrays,
// which is read whole: its grammar is checked as it is read, keeping its bytes ( see skipNested ), so that a value
// which is not valid fails at its error, without the input after it being read, as a value decoded element by element
// does.
func (s *Stream) wholeEnd(depth int64) (int64, error) {
	start := s.offset + s.cursor
	keeps, keepFrom := s.keeps, s.keepFrom
	if !s.keeps || s.keepFrom > start {
		s.keeps, s.keepFrom = true, start
	}
	err := s.skipNested(depth)
	s.keeps, s.keepFrom = keeps, keepFrom
	if err != nil {
		return 0, err
	}
	end := s.cursor
	s.cursor = start - s.offset
	return end, nil
}

// elementEnd returns the position after the value at the cursor, nested in depth objects and arrays. An object or an
// array is found by the index of the brackets in what has been read, and else whole is false; any other value is read
// whole.
func (s *Stream) elementEnd(depth int64) (int64, bool, error) {
	if c := s.buf[s.cursor]; c != '{' && c != '[' {
		end, err := s.scanValue()
		return end, true, err
	}
	var rel int
	// the index is extended by steps twice as large as the one before, so that it goes past the end of the element
	// by its size at most, and as many steps as the doublings of its size are taken
	for step := int64(indexFirstStep); ; step *= 2 {
		var end int64
		var ok bool
		s.indexFrom(depth)
		if end, ok, rel = s.elements.closeOf(s.offset+s.cursor, depth, rel); ok {
			return end + 1 - s.offset, true, nil
		}
		if !s.indexAhead(step) {
			return 0, false, nil
		}
	}
}

// bracketIndex is where the objects and the arrays of the value which a stream decodes element by element end, in
// what has been read: it is extended by every read, so that each byte is scanned once, and an element is found whole
// in the buffer by the index, without a scan of its own at every level it is nested in. Only the ones nested in at
// most bound objects and arrays have an entry: the elements looked up are the ones of the objects and the arrays
// decoded element by element, which are few levels, and the brackets of the blocks of bytes deeper in an element
// are not looked at one by one.
type bracketIndex struct {
	// closes are the brackets which close the objects and the arrays, in their order. An object or an array ends at
	// the first one after it which is as deep: the ones before are deeper, in it.
	closes []bracketClose
	// depth is the number of the objects and the arrays open after what has been read.
	depth int64
	// bound is the depth of the deepest brackets indexed: the depth of the closes ( see bracketClose ).
	bound int64
	// end is the total offset up to which what has been read is indexed.
	end int64
	// next is the first entry which closeOf looks at: the values are looked up in the order of the input.
	next int
	// inString and escaped are the state of the scan after what has been read ( see compoundScanner.scan ).
	inString, escaped uint64
}

// bracketClose is a bracket which closes an object or an array: its total offset, and the number of the objects and
// the arrays which the one it closes is nested in, plus one for itself.
type bracketClose struct {
	at, depth int64
}

// indexAt makes the index have what has been read from the cursor, where an element nested in depth objects and
// arrays is ( see indexFrom ).
func (s *Stream) indexAt(depth int64) {
	s.indexFrom(depth)
	x := s.elements
	x.extend(s.buf, x.end-s.offset, s.length, s.offset)
	x.end = s.offset + s.length
}

// indexFrom makes the index go on from the cursor, where an element nested in depth objects and arrays is: it is
// extended by the bytes read since it was looked at last, as long as the lookups of the elements went on from it, and
// else made from the cursor, which a skip of elements left it behind, or which is deeper than the index has its
// entries. A deeper index is four times as deep as what it is made for, so that a value nested deep, which is decoded
// element by element at every level, has its bytes indexed again a few times only.
func (s *Stream) indexFrom(depth int64) {
	x := s.elements
	if at := s.offset + s.cursor; x.end < at || x.bound < depth+1 {
		n := len(x.closes)
		for n > x.next && x.closes[n-1].at > at {
			n--
		}
		x.closes = x.closes[:n]
		if x.bound < depth+1 {
			x.bound = 4 * max(depth+1, x.bound)
		}
		x.depth, x.inString, x.escaped, x.end = depth, 0, 0, at
	}
}

// indexFirstStep is what a lookup of the end of an element indexes first ( see elementEnd ), so that the bytes after
// the element, which a skip of elements may pass without the index, are not indexed for it: four blocks of the scan.
const indexFirstStep = 4 * scanBlockSize

// indexAhead extends the index by step bytes of what has been read at most, and reports false if it has all.
func (s *Stream) indexAhead(step int64) bool {
	x := s.elements
	to := min(x.end-s.offset+step, s.length)
	if x.end-s.offset >= to {
		return false
	}
	x.extend(s.buf, x.end-s.offset, to, s.offset)
	x.end = s.offset + to
	return true
}

// extend indexes buf[from:to], whose first byte is at the total offset offset.
func (x *bracketIndex) extend(buf []byte, from, to, offset int64) {
	if x.next >= 1024 && x.next*2 >= len(x.closes) {
		// the entries which closeOf looked past are dropped
		x.closes = x.closes[:copy(x.closes, x.closes[x.next:])]
		x.next = 0
	}
	const evenBits = 0x5555555555555555
	base := (*sliceHeader)(unsafe.Pointer(&buf)).data
	closes, depth, bound, inString, prevEscaped := x.closes, x.depth, x.bound, x.inString, x.escaped
	for pos := from; pos < to; pos += scanBlockSize {
		// the brackets out of the strings in the block, as compoundScanner.scan finds them
		var m scanMasks
		scanBlock(unsafe.Add(base, pos), &m)
		n := to - pos
		if n < scanBlockSize {
			valid := uint64(1)<<uint(n) - 1
			m.quote &= valid
			m.backslash &= valid
			m.open &= valid
			m.closing &= valid
		}
		backslash := m.backslash &^ prevEscaped
		followsEscape := backslash<<1 | prevEscaped
		oddStarts := backslash &^ evenBits &^ followsEscape
		evenSequences, carry := bits.Add64(oddStarts, backslash, 0)
		escaped := (evenBits ^ (evenSequences << 1)) & followsEscape
		if n < scanBlockSize {
			prevEscaped = (escaped >> uint(n)) & 1
		} else {
			prevEscaped = carry
		}
		inside := m.quote &^ escaped
		inside ^= inside << 1
		inside ^= inside << 2
		inside ^= inside << 4
		inside ^= inside << 8
		inside ^= inside << 16
		inside ^= inside << 32
		inside ^= inString
		inString = uint64(int64(inside) >> 63)
		outside := ^(inside | escaped)
		open, closing := m.open&outside, m.closing&outside
		if closing == 0 || depth-int64(bits.OnesCount64(closing)) >= bound {
			// no bracket of the block closes an object or an array as shallow as the bound
			depth += int64(bits.OnesCount64(open)) - int64(bits.OnesCount64(closing))
			continue
		}
		for brackets := open | closing; brackets != 0; brackets &= brackets - 1 {
			i := bits.TrailingZeros64(brackets)
			if open&(1<<uint(i)) != 0 {
				depth++
				continue
			}
			if depth <= bound {
				closes = append(closes, bracketClose{at: offset + pos + int64(i), depth: depth})
			}
			depth--
		}
	}
	x.closes, x.depth, x.inString, x.escaped = closes, depth, inString, prevEscaped
}

// lastElementClose returns the total offset of the bracket which closes the last element which is an object or an
// array in what has been read of the elements from the total offset at, which are nested in depth objects and arrays.
func (x *bracketIndex) lastElementClose(at, depth int64) (int64, bool) {
	// the entries before are passed for good: the lookups are in the order of the input
	for x.next < len(x.closes) && x.closes[x.next].at < at {
		x.next++
	}
	var end int64
	found := false
	for i := x.next; i < len(x.closes); i++ {
		c := x.closes[i]
		if c.depth <= depth {
			// the end of the array
			break
		}
		if c.depth == depth+1 {
			end, found = c.at, true
		}
	}
	return end, found
}

// closeOf returns the total offset of the bracket which closes the object or the array whose bracket is at the total
// offset at, nested in depth objects and arrays, if it has been read. The entries are looked at from the one rel
// after the first one which may close the value, which a lookup returns, so that the lookups of a value which has not
// been read whole go on after each other.
func (x *bracketIndex) closeOf(at, depth int64, rel int) (int64, bool, int) {
	for x.next < len(x.closes) && x.closes[x.next].at < at {
		x.next++
	}
	for i := x.next + rel; i < len(x.closes); i++ {
		if x.closes[i].depth == depth+1 {
			return x.closes[i].at, true, 0
		}
	}
	return 0, false, len(x.closes) - x.next
}

// decodeWhole decodes the value at the cursor, which ends at end, nested in depth objects and arrays, by dec into p,
// as Decode does a value.
func (s *Stream) decodeWhole(dec Decoder, p unsafe.Pointer, depth, end int64) error {
	ctx := s.ctx
	had := ctx.typeError
	cursor, err := s.callDecoder(dec, p, depth, end)
	if err != nil {
		if _, ok := err.(*errors.SyntaxError); ok {
			return s.elementError(err, depth)
		}
		ctx.typeError = had
		return s.skipAfter(err, depth)
	}
	if had == nil && ctx.typeError != nil {
		s.keepTypeError()
	}
	s.cursor = cursor
	return nil
}

// elementError returns the error of the decoding of the value at the cursor, nested in depth objects and arrays, as
// decodeError does: a syntax error is the first one of the value by the grammar.
func (s *Stream) elementError(err error, depth int64) error {
	if _, ok := err.(*errors.SyntaxError); ok {
		if serr := s.valueSyntaxErrorAt(depth); serr != nil {
			return serr
		}
	}
	return s.totalOffsetError(err)
}

// keepTypeError makes the type error recorded in the value at the cursor one of the whole input: its path from the
// value is made while the buffer holds the value, and the objects and the arrays the value is in add theirs to it
// ( see addTypeErrorStep ).
func (s *Stream) keepTypeError() {
	e := s.ctx.typeError
	if e.plain == nil {
		e.path = typeErrorPath(s.buf[:s.length+1], s.cursor, e)
		slices.Reverse(e.path)
	}
	e.start += s.offset
	e.end += s.offset
}

// addTypeErrorStep adds the step to a member or an element, named name, to the path of the type error recorded in
// it: the decoding of the member or the element had none before, which it is called for. The path is kept from the
// value up to the root, so that a step is added without a copy of the path.
func (s *Stream) addTypeErrorStep(name string) {
	e := s.ctx.typeError
	e.path = append(e.path, name)
}

// syntaxError returns the syntax error of the byte at the cursor, which the grammar doesn't have where it is.
func (s *Stream) syntaxError(where syntaxWhere) error {
	return s.grammarError(syntaxErrorAt(s.buf[:s.length+1], s.cursor, where))
}

// nextElement moves the cursor to the next element of the array whose elements are read: first is set before the
// first one. It reports false after the end of the array.
func (s *Stream) nextElement(first bool) (bool, error) {
	if !s.skipWhiteSpace() {
		return false, s.unexpectedEndError()
	}
	switch c := s.buf[s.cursor]; {
	case c == ']':
		s.cursor++
		return false, nil
	case first:
		return true, nil
	case c != ',':
		return false, s.syntaxError(whereAfterElement)
	}
	s.cursor++
	if !s.skipWhiteSpace() {
		return false, s.unexpectedEndError()
	}
	return true, nil
}

// nextMember moves the cursor after the key of the next member of the object whose members are read: first is set
// before the first one. It returns the key as the buffer has it, which the next read of the stream may drop, what it
// has, and the position it starts at. It reports false after the end of the object.
func (s *Stream) nextMember(first bool) ([]byte, stringInfo, int64, bool, error) {
	if !s.skipWhiteSpace() {
		return nil, stringInfo{}, 0, false, s.unexpectedEndError()
	}
	switch c := s.buf[s.cursor]; {
	case c == '}':
		s.cursor++
		return nil, stringInfo{}, 0, false, nil
	case first:
	case c != ',':
		return nil, stringInfo{}, 0, false, s.syntaxError(whereAfterPair)
	default:
		s.cursor++
		if !s.skipWhiteSpace() {
			return nil, stringInfo{}, 0, false, s.unexpectedEndError()
		}
	}
	if s.buf[s.cursor] != '"' {
		return nil, stringInfo{}, 0, false, s.syntaxError(whereKey)
	}
	end, info, valid, err := s.scanStringInfo()
	if err != nil {
		return nil, stringInfo{}, 0, false, err
	}
	start := s.cursor
	if valid {
		s.cursor = end
		return s.buf[start+1 : end-1], info, start, true, nil
	}
	// the error of the key
	key, next, info, err := skipStringDecoder.scanString(s.buf[:s.length+1], start)
	if next < 0 {
		key, next, info, err = skipStringDecoder.scanStringRest(s.buf[:s.length+1], key, -next-1, info)
	}
	if err != nil {
		return nil, stringInfo{}, 0, false, s.elementError(err, 0)
	}
	s.cursor = next
	return key, info, start, true, nil
}

// openElements moves the cursor after the bracket which opens the object or the array whose elements are read, nested
// in depth objects and arrays, and returns the depth of its elements. A bracket nested too deep is the syntax error
// which the grammar of a value read whole reports ( see skipGrammar ), wherever the reads of the input end.
func (s *Stream) openElements(depth int64) (int64, error) {
	if depth >= maxDecodeNestingDepth {
		return 0, s.grammarError(depthSyntaxError(s.buf[:s.length+1], s.cursor))
	}
	s.cursor++
	return depth + 1, nil
}

// memberValue moves the cursor from after the key of a member to its value.
func (s *Stream) memberValue() error {
	if !s.skipWhiteSpace() {
		return s.unexpectedEndError()
	}
	if s.buf[s.cursor] != ':' {
		return s.syntaxError(whereAfterKey)
	}
	s.cursor++
	if !s.skipWhiteSpace() {
		return s.unexpectedEndError()
	}
	return nil
}

// skipNested skips the value at the cursor, nested in depth objects and arrays, which is checked by the grammar, as
// skipValue does, reading from the reader as the value goes on past what has been read: skipFast stops at the end of
// what has been read, and resumes after the read where it stopped, so that each byte is checked once, before the next
// read.
func (s *Stream) skipNested(depth int64) error {
	if c := s.buf[s.cursor]; c != '{' && c != '[' {
		return s.skipScalar()
	}
	if depth >= maxDecodeNestingDepth {
		return s.grammarError(depthSyntaxError(s.buf[:s.length+1], s.cursor))
	}
	maxLevel := skipMaxLevel(depth)
	cursor, level, objects, resume := s.cursor, int64(0), uint64(0), resumeValue
	for {
		var ev skipEvent
		cursor, level, objects, resume, ev = skipFast(s.buf[:s.length+1], cursor, level, objects, resume, maxLevel)
		s.cursor = cursor
		switch ev {
		case skipDone:
			return nil
		case skipAString, skipANumber:
			// a string or a number which skipFast leaves to its caller
			if err := s.skipScalar(); err != nil {
				return err
			}
		case skipDeep:
			if err := s.skipNested(depth + level); err != nil {
				return err
			}
			resume = resumeAfterValue
		default:
			var err error
			if level, resume, err = s.skipResume(level, objects, resume); err != nil || level == 0 {
				return err
			}
		}
		cursor = s.cursor
	}
}

// skipScalar skips the value at the cursor, which is not an object or an array, as skipValue does: one which is whole
// and valid in what has been read is skipped by one scan, and any other is read whole first.
func (s *Stream) skipScalar() error {
	switch c := s.buf[s.cursor]; {
	case c == '"':
		end, _, valid, err := s.scanStringInfo()
		if err != nil {
			return err
		}
		if valid {
			s.cursor = end
			return nil
		}
	case c == '-' || c-'0' <= 9:
		// a number which a byte after it ends
		if n, place := jsonnum.Scan(s.buf[s.cursor:s.length]); place == jsonnum.Valid && s.cursor+int64(n) < s.length {
			s.cursor += int64(n)
			return nil
		}
		if _, err := s.scanLiteral(); err != nil {
			return err
		}
	default:
		if lit := literalOf(c); lit != "" && s.length-s.cursor >= int64(len(lit)) && string(s.buf[s.cursor:s.cursor+int64(len(lit))]) == lit {
			s.cursor += int64(len(lit))
			return nil
		}
		if _, err := s.scanValue(); err != nil {
			return err
		}
	}
	next, err := skipValue(s.buf[:s.length+1], s.cursor, 0)
	if err != nil {
		return s.elementError(err, 0)
	}
	s.cursor = next
	return nil
}

// skipError returns the syntax error of the byte at the cursor, where skipFast stopped in the state resume, with level
// objects and arrays open whose kinds are the bits of objects.
func (s *Stream) skipError(level int64, objects uint64, resume skipResume) error {
	return s.grammarError(skipFastError(s.buf[:s.length+1], s.cursor, level, objects, resume))
}

// skipResume does what skipFast stopped at the cursor for, in the state resume, with level objects and arrays open
// whose kinds are the bits of objects: a byte which the grammar doesn't have there, whose syntax error it returns, or
// the end of what has been read, for which it reads, and returns the level and the state which skipFast resumes in at
// the cursor then. The level is 0 after the end of the value.
func (s *Stream) skipResume(level int64, objects uint64, resume skipResume) (int64, skipResume, error) {
	if s.cursor < s.length {
		lit := literalOf(s.buf[s.cursor])
		if n := s.length - s.cursor; resume != resumeValue || n >= int64(len(lit)) || string(s.buf[s.cursor:s.length]) != lit[:n] {
			return 0, 0, s.skipError(level, objects, resume)
		}
		// true, false or null which what has been read cuts
		if !s.read() {
			return 0, 0, s.unexpectedEndError()
		}
		return level, resume, nil
	}
	switch resume {
	case resumeAfterValue:
		if s.cursor == 0 || s.buf[s.cursor-1]-'0' > 9 {
			break
		}
		// an integer, which the next bytes may go on: it is read whole, up to a byte after it, or the end of the input,
		// which the read below reports
		for s.cursor > 0 && isNumberByte(s.buf[s.cursor-1]) {
			s.cursor--
		}
		if err := s.skipScalar(); err != nil || s.cursor < s.length {
			return level, resume, err
		}
	case resumeValue, resumeKey:
		// a value or a key, after the white space after a bracket, a comma or a colon: the end of the object or the
		// array may follow its bracket only, and the key is skipped here, as skipFast doesn't resume at it
		open := max(s.cursor-1, 0)
		for open > 0 && isWhiteSpace[s.buf[open]] {
			open--
		}
		closing := byte(0)
		switch s.buf[open] {
		case '[':
			closing = ']'
		case '{':
			closing = '}'
		}
		if !s.skipWhiteSpace() {
			return 0, 0, s.unexpectedEndError()
		}
		switch c := s.buf[s.cursor]; {
		case closing != 0 && c == closing:
			s.cursor++
			return level - 1, resumeAfterValue, nil
		case resume == resumeKey && c == '"':
			return level, resumeAfterKey, s.skipScalar()
		case resume == resumeKey:
			return 0, 0, s.skipError(level, objects, resume)
		}
		return level, resume, nil
	}
	if !s.read() {
		return 0, 0, s.unexpectedEndError()
	}
	return level, resume, nil
}

// isNumberByte reports whether c is a byte of a number.
func isNumberByte(c byte) bool {
	return c-'0' <= 9 || c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-'
}

// decodeStructElements decodes the object at the cursor into the struct at p, as structDecoder.Decode does.
func (s *Stream) decodeStructElements(d *structDecoder, p unsafe.Pointer, depth int64) error {
	depth, err := s.openElements(depth)
	if err != nil {
		return err
	}
	flags := s.ctx.Option.Flags
	disallowUnknownFields := flags&DisallowUnknownFieldsOption != 0
	// seen are the fields decoded under FirstWinOption, by their indexes, and seenNum their number: once every one
	// is decoded, the rest of the object is skipped, as decodeObjectFirstWin does.
	var seen []uint64
	if flags&FirstWinOption != 0 {
		seen = make([]uint64, (d.fieldUniqueNameNum+63)/64)
	}
	seenNum := 0
	var arr [64]byte
	for first := true; ; first = false {
		key, info, _, ok, err := s.nextMember(first)
		if !ok {
			return err
		}
		var field *structFieldSet
		var inputName []byte
		if seenNum == 0 || seenNum < d.fieldUniqueNameNum {
			var decodedKey []byte
			field, decodedKey = d.keys.lookup(key, info)
			if field == nil && disallowUnknownFields {
				err := unknownFieldError(decodedKey)
				if serr := s.memberValue(); serr != nil {
					return serr
				}
				if serr := s.skipNested(depth); serr != nil {
					return serr
				}
				return s.restAfter(err, true, depth)
			}
			if field != nil {
				// the name of the step of a type error of the value, which a read may overwrite in the buffer
				inputName = append(arr[:0], decodedKey...)
			}
		}
		unsettable := field != nil && field.unsettable(p)
		if seen != nil && field != nil && !unsettable {
			w, bit := &seen[field.fieldIdx/64], uint64(1)<<uint(field.fieldIdx%64)
			if *w&bit != 0 {
				field = nil
			} else {
				*w |= bit
				seenNum++
			}
		}
		if err := s.memberValue(); err != nil {
			return err
		}
		had := s.ctx.typeError
		switch {
		case field == nil:
			err = s.skipNested(depth)
		case unsettable:
			err = s.skipUnsettable(d, field.err, depth)
		default:
			err = s.decodeNested(field.dec, unsafe.Add(p, field.offset), depth)
		}
		if err != nil {
			return s.restAfter(err, true, depth)
		}
		if s.ctx.typeError != had {
			s.addTypeErrorStep(string(inputName))
		}
	}
}

// skipUnsettable skips the value at the cursor, nested in depth objects and arrays, of a field of the struct of d which
// can't be set ( see structFieldSet.unsettable ), and records err, its error, as structDecoder.Decode does.
func (s *Stream) skipUnsettable(d *structDecoder, err error, depth int64) error {
	start, kind := s.offset+s.cursor, kindOf(s.buf[s.cursor])
	if serr := s.skipNested(depth); serr != nil {
		return serr
	}
	s.ctx.unsettableValueError(start, s.offset+s.cursor, kind, d.typ, err)
	return nil
}

// decodeMapElements decodes the object at the cursor into the map at p, as mapDecoder.Decode does.
func (s *Stream) decodeMapElements(d *mapDecoder, p unsafe.Pointer, depth int64) error {
	depth, err := s.openElements(depth)
	if err != nil {
		return err
	}
	mapValue := *(*unsafe.Pointer)(p)
	if mapValue == nil {
		mapValue = reflect.MakeMap(d.mapType).UnsafePointer()
	}
	t := d.temps.Get().(*mapTemps)
	mv := d.mapValue(mapValue)
	var arr [64]byte
	for first := true; ; first = false {
		key, info, start, ok, err := s.nextMember(first)
		if err != nil {
			return err
		}
		if !ok {
			*(*unsafe.Pointer)(p) = mapValue
			d.temps.Put(t)
			return nil
		}
		// the name of the entry, which a type error in it reports
		name := append(arr[:0], key...)
		had := s.ctx.typeError
		// the key is decoded by the decoder of the keys, as a value which ends after it
		end := s.cursor
		s.cursor = start
		keyEnd, err := s.callDecoder(d.keyDecoder, t.k, depth, end)
		drop := err == errMapKeyType
		if _, ok := err.(*errors.SyntaxError); ok {
			return s.elementError(err, depth)
		}
		if err != nil && !drop {
			// the error of a key is returned once the rest of the object is read
			s.ctx.typeError = had
			s.cursor = end
			if serr := s.memberValue(); serr != nil {
				return serr
			}
			if serr := s.skipNested(depth); serr != nil {
				return serr
			}
			return s.restAfter(s.totalOffsetError(err), true, depth)
		}
		if had == nil && s.ctx.typeError != nil {
			s.keepTypeError()
		}
		s.cursor = keyEnd
		if err := s.memberValue(); err != nil {
			return err
		}
		if drop {
			// the value of an entry which is dropped is skipped, as encoding/json does
			err = s.skipNested(depth)
		} else if err = s.decodeNested(d.valueDecoder, t.v, depth); err == nil {
			mv.SetMapIndex(t.kv, t.vv)
		}
		if err != nil {
			return s.restAfter(err, true, depth)
		}
		t.kv.SetZero()
		t.vv.SetZero()
		if s.ctx.typeError != had {
			s.addTypeErrorStep(string(decodeLiteral(name, info)))
		}
	}
}

// decodeSliceElements decodes the array at the cursor into the slice at p, as sliceDecoder.Decode does.
func (s *Stream) decodeSliceElements(d *sliceDecoder, p unsafe.Pointer, depth int64) error {
	depth, err := s.openElements(depth)
	if err != nil {
		return err
	}
	dst := (*sliceHeader)(p)
	// the elements are decoded into the slice up to limit, and into a buffer after it, as Decode does
	t := sliceTarget{d: d, dst: dst, limit: max(dst.cap, inPlaceSliceLength)}
	for idx := 0; ; {
		ok, err := s.nextElement(idx == 0)
		if err != nil || !ok {
			t.finish(idx, idx, err)
			return err
		}
		if n, err := s.decodeRun(&t, idx, depth); err != nil || n > idx {
			if idx = n; err != nil {
				return s.sliceElementsError(&t, idx, err, depth)
			}
			continue
		}
		had := s.ctx.typeError
		if err := s.decodeNested(d.valueDecoder, t.elem(idx), depth); err != nil {
			return s.sliceElementsError(&t, idx, err, depth)
		}
		t.decoded(idx)
		if s.ctx.typeError != had {
			s.addTypeErrorStep(strconv.Itoa(idx))
		}
		idx++
	}
}

// sliceElementsError returns the error of Decode for err, the error of the element idx of the array which t stores,
// whose elements are nested in depth objects and arrays. The slice has the element when err is not one of the grammar,
// as the slice which sliceDecoder.Decode decodes it into, which has a decoded value whose error is returned after it.
func (s *Stream) sliceElementsError(t *sliceTarget, idx int, err error, depth int64) error {
	// the element idx is decoded into, whole or in part, whether the slice has it or not
	used := idx + 1
	if !s.grammarFailure(err) {
		t.decoded(idx)
		idx++
	}
	t.finish(idx, used, err)
	return s.restAfter(err, false, depth)
}

// sliceTarget is where the elements of an array decoded element by element are stored: into the slice up to limit,
// and into a buffer of the pool after it, as sliceDecoder.Decode does.
type sliceTarget struct {
	d     *sliceDecoder
	dst   *sliceHeader
	elems *sliceBuf
	limit int
}

// elem returns where the element at idx is decoded into.
func (t *sliceTarget) elem(idx int) unsafe.Pointer {
	switch {
	case idx < t.limit:
		t.d.grow(t.dst, idx, idx+1)
		return unsafe.Add(t.dst.data, uintptr(idx)*t.d.size)
	case t.elems == nil:
		t.elems = t.d.takeBuf(t.dst, idx)
	}
	return t.d.bufElem(t.elems, idx)
}

// decoded counts the element at idx, which is decoded, in the length of the slice.
func (t *sliceTarget) decoded(idx int) {
	if idx < t.limit {
		t.dst.len = idx + 1
	}
}

// finish makes the slice have the n elements decoded, after the end of the array or the error err: used elements are
// decoded into, whole or in part.
func (t *sliceTarget) finish(n, used int, err error) {
	switch {
	case t.elems != nil:
		t.d.storeBuf(t.dst, t.elems, n, used)
	case err == nil && n == 0 && t.dst.data == nil:
		t.dst.data = unsafe.Pointer(&zeroBase)
	default:
		t.dst.len = n
	}
}

// decodeRun decodes the elements of the array from the one at the cursor, the element idx, which the buffer holds
// whole, by one setting of the decoders' buffer: the ones up to the last object or array element whose end has been
// read. It returns the number of the elements decoded with idx, and leaves the cursor after the last one. The
// elements of the array are nested in depth objects and arrays.
func (s *Stream) decodeRun(t *sliceTarget, idx int, depth int64) (int, error) {
	s.indexAt(depth)
	end, ok := s.elements.lastElementClose(s.offset+s.cursor, depth)
	if !ok {
		return idx, nil
	}
	ctx := s.ctx
	stop := end - s.offset + 1
	saved := s.buf[stop]
	s.buf[stop] = nul
	buf := s.buf[:stop+1]
	ctx.Buf, ctx.origin = buf, buf
	cursor := s.cursor
	var err error
	for {
		start := cursor
		had := ctx.typeError
		next, derr := t.d.valueDecoder.Decode(ctx, cursor, depth, t.elem(idx))
		if derr != nil {
			ctx.typeError = had
			// bytes of the grammar of a number which are more than one value, as 01 is 0 and 1, are decoded one by one
			// ( see scanLiteral ): the decoding stops before them, after the comma before them
			if !moreThanNumber(buf, start) {
				s.cursor, err = start, derr
			}
			break
		}
		t.decoded(idx)
		if had == nil && ctx.typeError != nil {
			s.cursor = start
			s.keepTypeError()
			s.addTypeErrorStep(strconv.Itoa(idx))
		}
		idx++
		s.cursor = next
		// the next element is in the run if it starts before its end
		if c := skipWhiteSpace(buf, next); buf[c] == ',' {
			if c = skipWhiteSpace(buf, c+1); c < stop {
				cursor = c
				continue
			}
		}
		break
	}
	s.buf[stop] = saved
	ctx.Buf, ctx.origin = nil, nil
	if _, ok := err.(*errors.SyntaxError); ok {
		return idx, s.elementError(err, depth)
	}
	if err != nil {
		return idx, s.skipAfter(err, depth)
	}
	return idx, nil
}

// moreThanNumber reports whether the bytes of the grammar of a number at start in buf, which a nul byte ends, are more
// than a number, as 01 is 0 and 1.
func moreThanNumber(buf []byte, start int64) bool {
	end := start
	for isNumberByte(buf[end]) {
		end++
	}
	n, _ := jsonnum.Scan(buf[start:end])
	return end > start && start+int64(n) < end
}

// decodeArrayElements decodes the array at the cursor into the array at p, as arrayDecoder.Decode does.
func (s *Stream) decodeArrayElements(d *arrayDecoder, p unsafe.Pointer, depth int64) error {
	depth, err := s.openElements(depth)
	if err != nil {
		return err
	}
	for idx := 0; ; idx++ {
		ok, err := s.nextElement(idx == 0)
		if err != nil {
			return err
		}
		if !ok {
			d.zeroFrom(p, idx)
			return nil
		}
		if idx >= d.alen {
			if err := s.skipNested(depth); err != nil {
				return err
			}
			continue
		}
		had := s.ctx.typeError
		if err := s.decodeNested(d.valueDecoder, unsafe.Add(p, uintptr(idx)*d.size), depth); err != nil {
			return s.restAfter(err, false, depth)
		}
		if s.ctx.typeError != had {
			s.addTypeErrorStep(strconv.Itoa(idx))
		}
	}
}

// decodeInterfaceElements decodes the object or the array at the cursor into the interface value at p, as
// interfaceDecoder.Decode does. It reports false for a value of an interface type with methods which is kept, whose
// value is a type error, and, as decodeElements does, for a pointer which the interface value holds whose value is
// read whole.
func (s *Stream) decodeInterfaceElements(d *interfaceDecoder, p unsafe.Pointer, depth int64) (bool, error) {
	// the value is found by its type, or by its itab for an interface type with methods
	key, ptr := (*emptyInterface)(p).typ, (*emptyInterface)(p).ptr
	if d.hasMethods {
		key, ptr = (*nonEmptyInterface)(p).itab, (*nonEmptyInterface)(p).ptr
	}
	if ptr != nil {
		v := d.values.Load(uintptr(key))
		if v == nil {
			iface := reflect.NewAt(d.typ, p).Elem().Interface()
			var err error
			if v, err = d.valueDecoderOf(s.ctx, key, iface); err != nil {
				if serr := s.skipNested(depth); serr != nil {
					return true, serr
				}
				return true, err
			}
		}
		if v.kind == ifaceValuePointee {
			// what the pointer points to is decoded into, as a value of the pointer type
			return s.decodeElements(v.dec, ptr, depth)
		}
	}
	if d.hasMethods {
		return false, nil
	}
	var v any
	var err error
	if s.buf[s.cursor] == '{' {
		v, err = s.decodeAnyObject(d, depth)
	} else {
		v, err = s.decodeAnyArray(d, depth)
	}
	if err != nil {
		return true, err
	}
	*(*any)(p) = v
	return true, nil
}

// decodeAnyObject decodes the object at the cursor into a new map[string]interface{}, as decodeNewStringAnyMap does:
// the entries are pushed to the stack of entries of the context, and put into a map of their number at the end.
func (s *Stream) decodeAnyObject(d *interfaceDecoder, depth int64) (any, error) {
	depth, err := s.openElements(depth)
	if err != nil {
		return nil, err
	}
	ctx := s.ctx
	base := len(ctx.entryStack)
	for first := true; ; first = false {
		key, info, _, ok, err := s.nextMember(first)
		if !ok {
			if err != nil {
				ctx.popEntries(base)
				return nil, err
			}
			entries := ctx.entryStack[base:]
			m := make(map[string]any, len(entries))
			for i := range entries {
				m[entries[i].key] = entries[i].value
			}
			ctx.popEntries(base)
			return m, nil
		}
		name := string(decodeLiteral(key, info))
		if err := s.memberValue(); err != nil {
			ctx.popEntries(base)
			return nil, err
		}
		had := ctx.typeError
		if err := s.decodeNested(d, unsafe.Pointer(&ctx.slot), depth); err != nil {
			ctx.slot = nil
			ctx.popEntries(base)
			return nil, s.restAfter(err, true, depth)
		}
		ctx.entryStack = append(ctx.entryStack, anyEntry{key: name, value: ctx.slot})
		ctx.slot = nil
		if ctx.typeError != had {
			s.addTypeErrorStep(name)
		}
	}
}

// decodeAnyArray decodes the array at the cursor into a new []interface{}, as decodeAnySlice does: the elements are
// pushed to the stack of the context, and copied into a slice of their number at the end.
func (s *Stream) decodeAnyArray(d *interfaceDecoder, depth int64) (any, error) {
	depth, err := s.openElements(depth)
	if err != nil {
		return nil, err
	}
	ctx := s.ctx
	base := len(ctx.anyStack)
	for idx := 0; ; idx++ {
		ok, err := s.nextElement(idx == 0)
		if err != nil {
			ctx.popAny(base)
			return nil, err
		}
		if !ok {
			if idx == 0 {
				return emptyAnySlice, nil
			}
			elems := make([]any, idx)
			copy(elems, ctx.anyStack[base:])
			ctx.popAny(base)
			return elems, nil
		}
		had := ctx.typeError
		if err := s.decodeNested(d, unsafe.Pointer(&ctx.slot), depth); err != nil {
			ctx.slot = nil
			ctx.popAny(base)
			return nil, s.restAfter(err, false, depth)
		}
		ctx.anyStack = append(ctx.anyStack, ctx.slot)
		ctx.slot = nil
		if ctx.typeError != had {
			s.addTypeErrorStep(strconv.Itoa(idx))
		}
	}
}
