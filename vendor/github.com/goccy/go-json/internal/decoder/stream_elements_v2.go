package decoder

import (
	"bytes"
	"io"
	"reflect"
	"sync"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
	"github.com/goccy/go-json/internal/jsonfields"
	"github.com/goccy/go-json/internal/textcoder"
)

// The values of a stream of the semantics of encoding/json/v2, which the v2 json package decodes by UnmarshalRead. A
// value which the buffer holds whole is decoded by the decoders of v2, as Unmarshal decodes it, and an object or an
// array which it doesn't is decoded element by element, as Stream.Decode does for v1 ( see stream_elements.go ), by
// the rules of v2: the decoding stops at the first error, the names of an object are matched and checked as the
// decoders of v2 do, a value of a type which a method or a function reads whole is read whole, as encoding/json/v2
// reads it, and a method or a function which reads a jsontext.Decoder is given one which reads on from the reader of
// the stream. The strings are copied out of the buffer, as Unmarshal copies them out of its input, so that the buffer
// is reused.
//
// The objects and the arrays which a value is decoded element by element in are the path of the call ( see
// V2Call.Path ), which the v2 json package finds the place of an error with: the innermost function records the part
// of the buffer to read again ( see StreamPlace ), from the end of the token before the member, the element or the
// value which failed. That token, a delimiter or the bracket which begins the level, is kept in the buffer until the
// value after it is decoded, or begins to be decoded element by element: a decoder is attached at it, and the place
// of an error found again from it.

// streamsV2 are the streams of the calls of the v2 json package, which keep their buffers and their contexts.
var streamsV2 = sync.Pool{New: func() any { return NewStreamV2() }}

// NewStreamV2 returns a new stream for the decodings of v2, which a pool keeps: its context has the decoders of the
// types decoded last, as a context of the pool of the contexts has them ( see RuntimeContext.recentDecoders ).
func NewStreamV2() *Stream {
	s := NewStream(nil)
	s.ctx.recentDecoders = &[recentDecoderSets]recentDecoderSet{}
	return s
}

// maxReusedStreamBuffer is the size of the largest buffer which a stream of the pool keeps: one which grew for a long
// token or a value read whole is dropped.
const maxReusedStreamBuffer = 4 * (initBufSize + bufPadding)

// TakeStreamV2 returns a stream which reads r, for a decoding of v2 by the state of the call of its context ( see
// RuntimeContext.V2Call ), whose path is the top level.
func TakeStreamV2(r io.Reader) *Stream {
	s := streamsV2.Get().(*Stream)
	s.BeginV2(r)
	return s
}

// BeginV2 makes the stream, which has no reader ( see EndV2 ), read r, for a decoding of v2 by the state of the
// call of its context, whose path is the top level.
func (s *Stream) BeginV2(r io.Reader) {
	s.r = r
	call := s.ctx.V2Call()
	call.Stream = s
	call.Path = append(call.Path[:0], textcoder.Level{})
	call.pathNames = append(call.pathNames[:0], namePlace{})
}

// BeginV2At is BeginV2 for a value at the place levels, whose names are its own, which are made the path of the
// call of the stream, without a copy ( see SetPath ). It returns the path the call had, whose arrays the caller
// keeps for its next levels.
func (s *Stream) BeginV2At(r io.Reader, levels []textcoder.Level) []textcoder.Level {
	s.r = r
	call := s.ctx.V2Call()
	call.Stream = s
	path := call.Path
	call.Path = levels
	call.clearPathNames()
	return path
}

// viewedInput is the source of a stream which reads in place an input which is whole ( see ViewV2 ).
var viewedInput byte

// ViewV2 makes the stream, which BeginV2At began, read in place in, a whole input, which has a nul byte after it, from
// the offset base of the whole input: it reads nothing more, and its buffer is its own again after EndV2. A caller
// which decodes a value of in whole, as decodeWholeV2 decodes the value of a stream, sets the stream at the value
// ( see LendAt ), so that a method or a function of the value finds its place by the stream.
func (s *Stream) ViewV2(in []byte, base int64) {
	s.src, s.own = unsafe.Pointer(&viewedInput), s.buf
	s.buf, s.offset, s.length = in, base, int64(len(in))-1
	s.readErr = io.EOF
}

// The room which a jsontext.Decoder leaves after its input for a stream which reads its buffer in place is the
// padding of the buffer of a stream ( see textcoder.InputRoom ): the conversion of their difference, a negative
// constant if they differ, doesn't compile then.
const _, _ = uint(textcoder.InputRoom - bufPadding), uint(bufPadding - textcoder.InputRoom)

// BorrowV2 makes the stream, which BeginV2 or BeginV2At began, read the input of dec, a jsontext.Decoder, from the
// offset from of the whole input, in the buffer of dec, in place, and read more into it through dec, initBufSize
// bytes at most at a time, as it reads its reader ( see readSource ): the bytes are not copied into its own buffer.
// It reports false, having done nothing, if the buffer can't be read so ( see textcoder.DecoderBuffer ). Once a
// decoder attached at a value reads on, the stream holds what it needs of the buffer in its own, and reads its reader
// ( see ReadOn ).
func (s *Stream) BorrowV2(dec unsafe.Pointer, from int64) bool {
	buf, base, ok := textcoder.DecoderBuffer(dec)
	if !ok || from < base {
		return false
	}
	s.src, s.own = dec, s.buf
	s.buf, s.offset, s.length, s.cursor = buf[:cap(buf)], base, int64(len(buf)), from-base
	s.buf[s.length] = nul
	return true
}

// readSource is read for a stream which reads the buffer of a jsontext.Decoder in place ( see BorrowV2 ): the
// decoder reads more into its buffer, keeping what the stream keeps, which it may move.
func (s *Stream) readSource() bool {
	if int64(len(s.buf))-s.length <= textcoder.InputRoom {
		// the names of the path which the levels have by their places in the buffer, which the decoder moves, as it
		// is full ( see textcoder.FetchInput )
		s.copyNames()
	}
	buf, base, n, err := textcoder.FetchInput(s.src, s.offset+s.keepStart(), initBufSize)
	s.cursor -= base - s.offset
	s.buf, s.offset, s.length = buf[:cap(buf)], base, int64(len(buf))
	s.buf[s.length] = nul
	if err != nil {
		s.readErr = err
	}
	return n > 0 || err == nil
}

// leaveSource makes the stream, which reads the buffer of a jsontext.Decoder in place, hold what a read keeps of the
// buffer in its own, and read its reader, as a stream of a reader does.
func (s *Stream) leaveSource() {
	s.copyNames()
	from := s.keepStart()
	n := s.length - from
	if int64(len(s.own)) <= n+bufPadding {
		// room for the bytes, the padding after them and a read of a byte at least, as read needs it
		s.own = make([]byte, n+bufPadding+1)
	}
	copy(s.own, s.buf[from:s.length])
	s.buf, s.own, s.src = s.own, nil, nil
	s.offset, s.cursor, s.length = s.offset+from, s.cursor-from, n
	s.buf[s.length] = nul
}

// leaveSourceFrom makes the stream, if it reads the buffer of a jsontext.Decoder in place, hold the bytes from the
// total offset from in its own buffer ( see leaveSource ), for a decoder attached at the value after from, which
// may read on: the input which it reads and its place are then the ones of a stream of a reader.
func (s *Stream) leaveSourceFrom(from int64) {
	if s.src != nil {
		keeps, keepFrom := s.keepFromHere(from)
		s.leaveSource()
		s.keeps, s.keepFrom = keeps, keepFrom
	}
}

// ReleaseStreamV2 puts the stream back, without its reader and the state of its call ( see EndV2 ).
func ReleaseStreamV2(s *Stream) {
	s.EndV2()
	streamsV2.Put(s)
}

// EndV2 drops the reader of the stream and the state of its call, which may then read another reader ( see
// BeginV2 ).
func (s *Stream) EndV2() {
	if s.src != nil {
		// the buffer of the stream, which read the one of a jsontext.Decoder in place
		s.buf, s.own, s.src = s.own, nil, nil
	}
	// the value of the call, which refers to nothing, is set by the next decoding before it reads it, but not the
	// place, which the caller reads after a failure before the decoding too ( see DecodeV2After )
	call := s.ctx.v2
	call.Stream, call.Place.Set = nil, false
	if len(call.names.entries) != 0 {
		call.names.truncate(0, 0)
	}
	if call.skippers != [3]unsafe.Pointer{} {
		releaseSkippers(call)
	}
	if len(s.buf) > maxReusedStreamBuffer {
		s.buf = make([]byte, initBufSize+bufPadding)
	}
	s.length, s.cursor, s.offset = 0, 0, 0
	s.buf[0] = nul
	s.r, s.readErr, s.err = nil, nil, nil
	s.keeps, s.keepFrom = false, 0
	s.ctx.Option.Flags, s.ctx.Option.V2 = 0, 0
}

// StartAt makes the stream, which has read nothing, start at the offset of the input, which its reader reads from.
func (s *Stream) StartAt(offset int64) {
	s.offset = offset
}

// Offset returns the offset in the input of the cursor of the stream.
func (s *Stream) Offset() int64 {
	return s.offset + s.cursor
}

// Drop empties the buffer of the stream, which a decoder attached at its value read on from and failed after: the
// stream has nothing to give back ( see Read ), as the decoding of the value fails.
func (s *Stream) Drop() {
	s.copyNames()
	s.offset += s.length
	s.cursor, s.length = 0, 0
	s.buf[0] = nul
}

// Unread returns the bytes of the buffer which the stream read after its cursor.
func (s *Stream) Unread() []byte {
	return s.buf[s.cursor:s.length]
}

// Context returns the context of the stream, which its values are decoded by.
func (s *Stream) Context() *RuntimeContext {
	return s.ctx
}

// Read returns what has been read and not dropped, and the offset of its first byte in the input.
func (s *Stream) Read() ([]byte, int64) {
	return s.buf[:s.length], s.offset
}

// ReadsInPlace reports whether the stream reads the buffer of a jsontext.Decoder in place ( see BorrowV2 ), which
// what it has read is then: it has not left it for its own buffer ( see leaveSource ).
func (s *Stream) ReadsInPlace() bool {
	return s.src != nil
}

// ReadError returns the error of the reader, which the stream reports at the end of what it read, or nil if the
// reader reached the end of the input, or has not failed.
func (s *Stream) ReadError() error {
	if s.readErr == io.EOF {
		return nil
	}
	return s.readErr
}

// ReadOn reads more of the input into p, for a decoder attached at the value of the stream ( see V2Call.Value ):
// initBufSize bytes at most, as the stream reads, and the error of the reader is kept, as the stream keeps it.
func (s *Stream) ReadOn(p []byte) (int, error) {
	if s.readErr != nil {
		return 0, s.readErr
	}
	if s.src != nil {
		s.leaveSource()
	}
	n, err := s.r.Read(p[:min(len(p), initBufSize)])
	if err != nil {
		s.readErr = err
	}
	return n, err
}

// Resume makes the stream go on at the offset end of the input, where a decoder attached at its value stopped: in the
// buffer, or, if the decoder read on, before rest, what it read and didn't use, which the buffer takes.
func (s *Stream) Resume(end int64, rest []byte) {
	s.copyNames()
	if rest == nil {
		s.cursor = end - s.offset
		return
	}
	if int64(len(rest)) > int64(len(s.buf))-bufPadding {
		s.buf = make([]byte, int64(len(rest))+bufPadding)
	}
	s.length = int64(copy(s.buf, rest))
	s.buf[s.length] = nul
	s.offset, s.cursor = end, 0
}

// DecodeV2 decodes the next value of the stream by dec, a decoder of v2, into p: whole if the buffer holds it, and
// else element by element. The value is nested in depth objects and arrays, the ones of the path but the top level.
// It returns the error of the reader, io.EOF at the end of the input, if no value is left, and the error of the
// decoding, whose place is recorded ( see V2Call.Place ) unless it is an error of encoding/json/v2 at its place.
func (s *Stream) DecodeV2(dec Decoder, p unsafe.Pointer, depth int64) error {
	s.ctx.v2.Place.Set = false
	// the end of the token before the value, before the white space, which is kept while the value is looked for
	from := s.offset + s.cursor
	keeps, keepFrom := s.keepFromHere(from)
	if !s.skipSpaceBefore() {
		s.keeps, s.keepFrom = keeps, keepFrom
		return s.endError()
	}
	return s.decodeTopV2(dec, p, depth, from, keeps, keepFrom)
}

// DecodeLongV2After is DecodeV2After for an object or an array at the offset start of what has been read, which the
// caller found not whole in it ( see WholeEndV2 ), by dec, which is not the decoder of a method, a function or a
// pointer ( see ReadsJSONText ): it is decoded element by element at once, as decodeTopV2 decodes it after its scan.
func (s *Stream) DecodeLongV2After(dec Decoder, p unsafe.Pointer, depth, from, start int64) error {
	call := s.ctx.v2
	// the names of the objects of the value which an error ended are dropped after it ( see DecodeV2After )
	entries, copies := call.names.mark()
	call.Place.Set = false
	s.cursor = start - s.offset
	s.beginElements()
	err := s.decodeLongV2(dec, p, depth, from)
	call.names.truncate(entries, copies)
	return err
}

// DecodeV2After is DecodeV2 for a value at the offset start of the input, after from, the offset of the end of the
// token before it, which the stream reads from: the white space and the delimiter between them, which were checked,
// are skipped. It never returns io.EOF.
func (s *Stream) DecodeV2After(dec Decoder, p unsafe.Pointer, depth, from, start int64) error {
	// the names of the objects of the value which an error ended are dropped after it: the value of a method, whose
	// error the method may ignore, leaves none to the objects around it ( see callNames )
	entries, copies := s.ctx.v2.names.mark()
	err := s.decodeV2After(dec, p, depth, from, start)
	s.ctx.v2.names.truncate(entries, copies)
	return err
}

func (s *Stream) decodeV2After(dec Decoder, p unsafe.Pointer, depth, from, start int64) error {
	s.ctx.v2.Place.Set = false
	keeps, keepFrom := s.keepFromHere(from)
	for s.offset+s.length <= start {
		if !s.read() {
			s.keeps, s.keepFrom = keeps, keepFrom
			return s.unexpectedEndError()
		}
	}
	s.cursor = start - s.offset
	return s.decodeTopV2(dec, p, depth, from, keeps, keepFrom)
}

// decodeTopV2 decodes the value at the cursor of DecodeV2, after from, the end of the token before it, which is kept
// while the value is looked for: keeps and keepFrom are what the stream kept before.
func (s *Stream) decodeTopV2(dec Decoder, p unsafe.Pointer, depth, from int64, keeps bool, keepFrom int64) error {
	switch dec.(type) {
	case *v2UnmarshalFromDecoder, *v2FuncDecoder, *ptrDecoder, *basicPtrDecoder, *v2InvalidDecoder:
		// a method or a function which reads a jsontext.Decoder, which may read on past the value, as the one of
		// encoding/json/v2 reads the values after it, also through a pointer; or a type of no representation, whose
		// error is before the value is read, whatever follows
		s.keeps, s.keepFrom = keeps, keepFrom
		s.beginElements()
		return s.decodeNestedV2(dec, p, depth, from)
	}
	end, err := s.scanValue()
	s.keeps, s.keepFrom = keeps, keepFrom
	switch {
	case err == errLongValue:
		// an object or an array, which what has been read doesn't hold whole: the decoder is not one of a pointer
		// or a method, which decodeNestedV2 looks through
		s.beginElements()
		return s.decodeLongV2(dec, p, depth, from)
	case err != nil:
		return s.placeAt(err, from)
	}
	return s.decodeWholeV2(dec, p, depth, end, from)
}

// beginElements makes the index of the objects and the arrays which the value at the cursor is decoded element by
// element in start at the value ( see bracketIndex ).
func (s *Stream) beginElements() {
	if s.elements == nil {
		// the entries of the first steps of the lookups ( see indexFirstStep ), which a slice grown from nothing
		// would take a few allocations for
		s.elements = &bracketIndex{closes: make([]bracketClose, 0, indexFirstStep/4)}
	}
	*s.elements = bracketIndex{closes: s.elements.closes[:0], end: s.offset + s.cursor}
}

// AfterTopLevel moves the stream to the end of the input after its top-level value, which only white space may
// follow, and reports whether it is there: if not, the place of the byte after the value is recorded ( see
// V2Call.Place ). It returns the error of the reader.
func (s *Stream) AfterTopLevel() (bool, error) {
	if s.skipWhiteSpace() {
		s.setPlace(s.offset+s.cursor, s.length)
		return false, nil
	}
	if s.readErr != io.EOF {
		return false, s.readErr
	}
	return true, nil
}

// setPlace records the place of an error of the decoding, which the buffer has from the total offset from up to end,
// unless an inner value recorded its own ( see V2Call.Place ).
func (s *Stream) setPlace(from, end int64) {
	// the names of the path, which the error is found again with
	s.copyNames()
	if pl := &s.ctx.v2.Place; !pl.Set {
		*pl = StreamPlace{From: from - s.offset, End: end, Set: true}
	}
}

// placeAt records the place of err, which the buffer has from the total offset from up to what has been read, and
// returns err.
func (s *Stream) placeAt(err error, from int64) error {
	s.setPlace(from, s.length)
	return err
}

// placeDelimiterError is placeAt for err, the error of the next member or element of an object or an array which is
// read token by token, after from, the end of the token before it: an error at a comma or a colon, which is not
// one where it is, is found again after the byte after it which is not white space has been read, as encoding/json/v2
// reads the token after a delimiter before it reports the delimiter, and its error tells that token.
func (s *Stream) placeDelimiterError(err error, from int64) error {
	if c := s.buf[s.cursor]; c == ',' || c == ':' {
		s.readPastDelimiter(from)
	}
	return s.placeAt(err, from)
}

// readPastDelimiter reads the input after the comma or the colon at the cursor, keeping the bytes from the total
// offset from, up to the first byte after it which is not white space, or the end of the input.
func (s *Stream) readPastDelimiter(from int64) {
	keeps, keepFrom := s.keepFromHere(from)
	at := s.offset + s.cursor + 1
	for {
		p := at - s.offset
		for p < s.length && isWhiteSpace[s.buf[p]] {
			p++
		}
		if at = s.offset + p; p < s.length || !s.read() {
			break
		}
	}
	s.keeps, s.keepFrom = keeps, keepFrom
}

// placeValue is placeAt for the value at the cursor, which ends at end.
func (s *Stream) placeValue(err error, from, end int64) error {
	s.setPlace(from, end)
	return err
}

// keepFromHere makes the buffer keep the bytes from the total offset from, which reads would drop, and returns what
// it kept before, which the caller sets back.
func (s *Stream) keepFromHere(from int64) (bool, int64) {
	keeps, keepFrom := s.keeps, s.keepFrom
	if !s.keeps || s.keepFrom > from {
		s.keeps, s.keepFrom = true, from
	}
	return keeps, keepFrom
}

// SetPath sets the path of the call of the stream to levels, the place of the next value which it decodes, copied
// into path, whose arrays it reuses, and returns the path it had, which the caller sets back by RestorePath.
func (s *Stream) SetPath(path, levels []textcoder.Level) []textcoder.Level {
	s.copyNames()
	call := s.ctx.v2
	saved := call.Path
	path = path[:0]
	for _, l := range levels {
		n := len(path)
		if n < cap(path) {
			// the entry, whose name keeps its array: the name of the level is copied, as the names of the path are
			// copied into ( see setName )
			path = path[:n+1]
			path[n].Object, path[n].Count = l.Object, l.Count
		} else {
			path = append(path, textcoder.Level{Object: l.Object, Count: l.Count})
		}
		name := path[n].Name[:0]
		if len(l.Name) != 0 {
			name = append(name, l.Name...)
		}
		if name == nil && l.Name != nil {
			name = []byte{} // the name "", which is a name
		}
		path[n].Name = name
	}
	call.Path = path
	call.clearPathNames()
	return saved
}

// clearPathNames makes the places of the names of the path of the call none, one for each level ( see setNameAt ).
func (call *V2Call) clearPathNames() {
	if n := len(call.Path); cap(call.pathNames) >= n {
		call.pathNames = call.pathNames[:n]
		clear(call.pathNames)
	} else {
		call.pathNames = make([]namePlace, n)
	}
}

// RestorePath sets the path of the call of the stream back to saved, which SetPath returned, and returns the path
// which it set, whose arrays the caller keeps for the next SetPath.
func (s *Stream) RestorePath(saved []textcoder.Level) []textcoder.Level {
	s.copyNames()
	call := s.ctx.v2
	path := call.Path
	call.Path = saved
	// the names of saved were copied when it was set aside ( see SetPath )
	call.clearPathNames()
	return path
}

// pushLevel adds the level of an object or an array which is decoded element by element to the path, and returns
// its index.
func (s *Stream) pushLevel(object bool) int {
	call := s.ctx.v2
	n := len(call.Path)
	call.pathNames = append(call.pathNames[:n], namePlace{})
	if n < cap(call.Path) {
		// the entry, whose name keeps its array for the names of the level
		call.Path = call.Path[:n+1]
		l := &call.Path[n]
		l.Object, l.Count, l.Name = object, 0, l.Name[:0]
		return n
	}
	call.Path = append(call.Path, textcoder.Level{Object: object})
	return n
}

// level returns the level i of the path.
func (s *Stream) level(i int) *textcoder.Level {
	return &s.ctx.v2.Path[i]
}

// popLevel removes the level i, which ended, from the path.
func (s *Stream) popLevel(i int) {
	call := s.ctx.v2
	call.Path, call.pathNames = call.Path[:i], call.pathNames[:i]
}

// decodeNestedV2 decodes the value at the cursor, nested in depth objects and arrays, by dec into p: whole if the
// buffer holds it, and else element by element. from is the total offset of the end of the token before the value,
// which the buffer has, and keeps while the value is looked for and read whole, but not while it is decoded element by
// element: a decoder attached at the value is attached there before anything more is read.
func (s *Stream) decodeNestedV2(dec Decoder, p unsafe.Pointer, depth, from int64) error {
	switch d := dec.(type) {
	case *ptrDecoder:
		if s.buf[s.cursor] != 'n' {
			// the value which the pointer points to, which is set to a new one if it is nil: its decoder may be one
			// of a method or a function which reads a jsontext.Decoder, and null sets the pointer to nil
			ptr := *(*unsafe.Pointer)(p)
			if ptr == nil {
				ptr = newValue(d.typ)
				*(*unsafe.Pointer)(p) = ptr
			}
			return s.decodeNestedV2(d.dec, ptr, depth, from)
		}
	case *basicPtrDecoder:
		if s.buf[s.cursor] != 'n' {
			// as a ptrDecoder, by the allocator of the type
			ptr := *(*unsafe.Pointer)(p)
			if ptr == nil {
				ptr = d.alloc()
				*(*unsafe.Pointer)(p) = ptr
			}
			return s.decodeNestedV2(d.dec, ptr, depth, from)
		}
	case *v2EmbeddedPtrDecoder:
		// a field through an embedded pointer, which is set to a new value if it is nil
		if *(*unsafe.Pointer)(p) == nil {
			if !d.settable {
				return s.placeAt(&errors.UnmarshalError{GoType: d.root, Start: s.cursor, NoKind: true, Err: errNilField}, from)
			}
			*(*unsafe.Pointer)(p) = newValue(d.elem)
		}
		return s.decodeNestedV2(d.dec, unsafe.Add(*(*unsafe.Pointer)(p), d.offset), depth, from)
	case *v2UnmarshalFromDecoder:
		if err := s.decodeAttached(d.typ, p, depth, from, nil); err != ErrUseDefault {
			return err
		}
		return s.decodeNestedV2(d.next, p, depth, from)
	case *v2FuncDecoder:
		return s.decodeFuncsV2(d, p, depth, from)
	case *v2InvalidDecoder:
		// the error before the value is read, as encoding/json/v2 reports it, whatever the value is
		return s.placeAt(&errors.UnmarshalError{GoType: d.typ, Start: s.cursor, NoKind: true, Err: d.err}, from)
	case *v2StringifiedPtrDecoder:
		// by the decoder of the pointer: an object or an array element by element, which is not the JSON string
		// "null", and any value but null of a pointer to a pointer, which fails before the value is read
		c := s.buf[s.cursor]
		if _, invalid := d.ptr.(*v2InvalidDecoder); c == '{' || c == '[' || invalid && c != 'n' {
			return s.decodeNestedV2(d.ptr, p, depth, from)
		}
	case *intDecoder, *uintDecoder, *floatDecoder, *boolDecoder, v2StringDecoder, *v2InterfaceDecoder:
		// a string, a number or a literal which the buffer has whole is decoded in it, as v1 decodes it, after from,
		// which a read for the rest of a string keeps ( see decodeScalar, which decodes a value of an interface only
		// into a nil interface{} )
		if c := s.buf[s.cursor]; c != '{' && c != '[' {
			keeps, keepFrom := s.keepFromHere(from)
			decoded := s.decodeScalar(dec, p, depth)
			s.keeps, s.keepFrom = keeps, keepFrom
			if decoded {
				return nil
			}
		}
	}
	keeps, keepFrom := s.keepFromHere(from)
	end, whole, err := s.elementEnd(depth)
	if err == nil && !whole {
		// a value decoded element by element doesn't keep the bytes before it
		s.keeps, s.keepFrom = keeps, keepFrom
		var decoded bool
		if decoded, err = s.decodeElementsV2(dec, p, depth, from); decoded {
			return err
		}
		start := s.offset + s.cursor
		s.keepFromHere(from)
		if end, err = s.wholeEnd(depth); err != nil {
			err = s.wholeValueError(err, start)
		}
	}
	if err == nil {
		err = s.decodeWholeV2(dec, p, depth, end, from)
	} else {
		err = s.placeAt(err, from)
	}
	s.keeps, s.keepFrom = keeps, keepFrom
	return err
}

// decodeLongV2 is decodeNestedV2 for the object or the array at the cursor which what has been read doesn't hold
// whole, found by scanValue: it is decoded element by element, or, if dec doesn't decode it so, read whole and
// decoded, without the lookup of its end by elementEnd, which looks at the same bytes again and indexes them.
func (s *Stream) decodeLongV2(dec Decoder, p unsafe.Pointer, depth, from int64) error {
	decoded, err := s.decodeElementsV2(dec, p, depth, from)
	if decoded {
		return err
	}
	start := s.offset + s.cursor
	keeps, keepFrom := s.keepFromHere(from)
	end, err := s.wholeEnd(depth)
	if err == nil {
		err = s.decodeWholeV2(dec, p, depth, end, from)
	} else {
		err = s.placeAt(s.wholeValueError(err, start), from)
	}
	s.keeps, s.keepFrom = keeps, keepFrom
	return err
}

// wholeValueError returns err, an error of wholeEnd, which read the value at the total offset start whole for its
// decoder: a syntax error is the one of the read of the whole value, which encoding/json/v2 reads whole for the
// decoder.
func (s *Stream) wholeValueError(err error, start int64) error {
	if s.grammarFailure(err) && err != s.readErr {
		return &errors.UnmarshalError{Start: start - s.offset, Err: err, Method: errors.UnmarshalWholeValue}
	}
	return err
}

// decodeWholeV2 decodes the value at the cursor, which ends at end, nested in depth objects and arrays, by dec into
// p, as Unmarshal decodes it: a type error which a decoder of v1 recorded is the error of the value. from is the
// total offset of the end of the token before the value.
func (s *Stream) decodeWholeV2(dec Decoder, p unsafe.Pointer, depth, end, from int64) error {
	ctx := s.ctx
	call := ctx.v2
	call.Value = StreamValue{From: from - s.offset, Start: s.cursor}
	stop := s.length
	if c := s.buf[s.cursor]; c != 't' && c != 'f' && c != 'n' {
		// the end of the value is known: a nul byte is put there while the value is decoded, as Decode puts it
		stop = end
	}
	saved := s.buf[stop]
	s.buf[stop] = nul
	ctx.Buf = s.buf[:stop+1]
	ctx.origin = ctx.Buf
	cursor, err := dec.Decode(ctx, s.cursor, depth, p)
	if err == nil && ctx.typeError != nil {
		err = ctx.v2Error()
	}
	s.buf[stop] = saved
	ctx.Buf, ctx.origin = nil, nil
	call.Value = StreamValue{}
	if err != nil {
		ctx.DiscardTypeError()
		return s.placeValue(err, from, end)
	}
	s.cursor = cursor
	return nil
}

// decodeAttached decodes the value at the cursor, nested in depth objects and arrays, which the buffer doesn't hold
// whole, into p, of typ, by its UnmarshalJSONFrom method, or by fn, a function of UnmarshalFromFunc, if it is set:
// they are given a jsontext.Decoder attached at its place, after from, the end of the token before it, which reads
// on from the reader of the stream. It returns ErrUseDefault if they declined the value.
func (s *Stream) decodeAttached(typ reflect.Type, p unsafe.Pointer, depth, from int64, fn *UnmarshalFunc) error {
	s.leaveSourceFrom(from)
	ctx := s.ctx
	call := ctx.v2
	call.Value = StreamValue{From: from - s.offset, Start: s.cursor, ReadsOn: true}
	start := s.offset + s.cursor
	s.copyNames()
	var err error
	if fn != nil {
		_, err = fn.Decode(ctx, s.cursor, depth, p, typ)
	} else {
		_, err = V2Hooks.UnmarshalFrom(ctx, s.cursor, depth, typ, p)
	}
	call.Value = StreamValue{}
	if err == ErrUseDefault {
		// the value, which the decoder read nothing of, is decoded from its start
		s.cursor = start - s.offset
	}
	return err
}

// decodeFuncsV2 decodes the value at the cursor, nested in depth objects and arrays, into p by the functions of d in
// their order, as v2FuncDecoder.Decode does: a function which reads a jsontext.Decoder is given one attached at its
// place, and any other the value read whole. from is the total offset of the end of the token before the value.
func (s *Stream) decodeFuncsV2(d *v2FuncDecoder, p unsafe.Pointer, depth, from int64) error {
	for i, fn := range d.funcs {
		if fn.FromDecoder {
			if err := s.decodeAttached(d.typ, p, depth, from, fn); err != ErrUseDefault {
				return err
			}
			continue
		}
		// a function which takes the value whole, which is read whole for it and the ones after it
		start := s.offset + s.cursor
		keeps, keepFrom := s.keepFromHere(from)
		end, whole, err := s.elementEnd(depth)
		if err == nil && !whole {
			if end, err = s.wholeEnd(depth); err != nil {
				err = s.wholeValueError(err, start)
			}
		}
		if err == nil {
			err = s.decodeWholeV2(&v2FuncDecoder{typ: d.typ, funcs: d.funcs[i:], next: d.next}, p, depth, end, from)
		} else {
			err = s.placeAt(err, from)
		}
		s.keeps, s.keepFrom = keeps, keepFrom
		return err
	}
	return s.decodeNestedV2(d.next, p, depth, from)
}

// decodeElementsV2 decodes the object or the array at the cursor, nested in depth objects and arrays, by dec into p
// element by element, after from, the total offset of the end of the token before it. It reports false, having read
// nothing, if dec doesn't decode it so: a value of a type which reads it whole, as encoding/json/v2 reads a value of
// a string, a number, bytes or a method whole before its error. A value of another kind than its type, which
// encoding/json/v2 reports at its first token, is the error.
func (s *Stream) decodeElementsV2(dec Decoder, p unsafe.Pointer, depth, from int64) (bool, error) {
	c := s.buf[s.cursor]
	var typ reflect.Type
	switch d := dec.(type) {
	case *v2StructDecoder:
		if c == '{' {
			if d.initErr != nil {
				return true, s.placeAt(&errors.UnmarshalError{GoType: d.initErr.GoType, Start: s.cursor, Err: d.initErr.Err}, from)
			}
			return true, s.decodeStructElementsV2(d, p, depth, from)
		}
		typ = d.typ
	case *v2MapDecoder:
		if c == '{' {
			return true, s.decodeMapElementsV2(d, p, depth, from)
		}
		typ = d.typ
	case *v2AnyMapDecoder:
		if c == '{' {
			m := *(*map[string]any)(p)
			if m == nil {
				m = map[string]any{}
			}
			err := s.decodeAnyObjectV2(d.iface, m, depth, from)
			*(*map[string]any)(p) = m
			return true, err
		}
		typ = interfaceMapType
	case *v2SliceDecoder:
		if c == '[' {
			return true, s.decodeSliceElementsV2(d, p, depth, from)
		}
		typ = d.typ
	case *v2ArrayDecoder:
		if c == '[' {
			return true, s.decodeArrayElementsV2(d, p, depth, from)
		}
		typ = d.typ
	case *v2FormatBytesWithLegacySemanticsDecoder:
		if c == '[' {
			return s.decodeElementsV2(d.elements, p, depth, from)
		}
		// any other value, which is read whole as bytes are
		return false, nil
	case *boolDecoder:
		typ = d.typ
	case *v2InvalidDecoder:
		// a type of no representation, which is the error before the value is read
		return true, s.placeAt(&errors.UnmarshalError{GoType: d.typ, Start: s.cursor, NoKind: true, Err: d.err}, from)
	case *v2InterfaceDecoder:
		return s.decodeInterfaceElementsV2(d, p, depth, from)
	default:
		return false, nil
	}
	// an object or an array of a type of another kind, whose error is at its first token
	return true, s.placeAt(&errors.UnmarshalError{GoType: typ, Start: s.cursor}, from)
}

// openElementsV2 moves the cursor after the bracket which begins the object or the array whose elements are read,
// nested in depth objects and arrays, after from, the total offset of the end of the token before it, and returns the
// depth of its elements and the index of its level in the path.
func (s *Stream) openElementsV2(object bool, depth, from int64) (int64, int, error) {
	depth, err := s.openElements(depth)
	if err != nil {
		return 0, 0, s.placeAt(err, from)
	}
	return depth, s.pushLevel(object), nil
}

// nextMemberV2 moves the cursor after the name of the next member of the object whose members are read, and returns
// the name, quoted as the buffer has it: first is set before the first one. It reports false after the end of the
// object.
func (s *Stream) nextMemberV2(first bool) ([]byte, bool, error) {
	_, _, start, ok, err := s.nextMember(first)
	if err != nil || !ok {
		return nil, ok, err
	}
	return s.buf[start:s.cursor], true, nil
}

// memberValueV2 moves the cursor from after the name of a member to its value, and returns the total offset of the
// end of the name, the token before the value.
func (s *Stream) memberValueV2() (int64, error) {
	nameEnd := s.offset + s.cursor
	if err := s.memberValue(); err != nil {
		return 0, err
	}
	return nameEnd, nil
}

// copyName copies name, the name of the next member of an object, before its colon is read, which a read may drop
// the name in the buffer for, into the spare name of the path ( see setName ).
func (s *Stream) copyName(name []byte) {
	call := s.ctx.v2
	if call.spareName = append(call.spareName[:0], name...); call.spareName == nil {
		// the name "", which is a name, as a level without a name has none ( see textcoder.Level )
		call.spareName = []byte{}
	}
}

// setName makes the name which copyName copied the last name of the level lv of the path, of its member k, whose
// colon is read: the level takes the spare name, and gives its former name, which the levels inside the value of
// the member and the next member copy theirs into.
func (s *Stream) setName(lv, k int) {
	call := s.ctx.v2
	l := &call.Path[lv]
	l.Count = countAfterName(int64(k))
	l.Name, call.spareName = call.spareName, l.Name[:0]
	call.pathNames[lv] = namePlace{}
}

// countAfterName and countAfterValue are the count of the level of an object of the path, which counts its names and
// its values ( see textcoder.Level ), once the name of its member k is read, and once the value is.
func countAfterName(k int64) int64  { return 2*k + 1 }
func countAfterValue(k int64) int64 { return 2*k + 2 }

// namesCounted is the number of the names of an object which the count of its level counts.
func namesCounted(count int64) int64 { return (count + 1) / 2 }

// namePlace is the place of a name of an object in the buffer of a stream: the total offsets of its first byte and of
// the byte after it, inside the quotes. The zero namePlace is no name.
type namePlace struct {
	from, to int64
}

// isName reports whether the place is the one of a name: a name is after a quote, so that its end is 1 at least.
func (np namePlace) isName() bool { return np.to != 0 }

// setNameAt makes the name of the buffer from the total offset from up to to, inside its quotes, which the buffer
// keeps, the last name of the level lv of the path, of its member k, whose colon is read, as setName does for a name
// which copyName copied: the level has the name by its place, until copyNames copies it, before the buffer drops it.
func (s *Stream) setNameAt(lv, k int, from, to int64) {
	call := s.ctx.v2
	call.Path[lv].Count = countAfterName(int64(k))
	call.pathNames[lv] = namePlace{from: from, to: to}
	call.namesFrom = min(call.namesFrom, lv)
}

// exactField returns the field of the name key of an object, which nextMember found, with what it found of it in
// info, and reports whether it is plain, as plainFieldV2 finds the field of a plain name in the buffer: a name of
// ASCII without an escape, which is the field whose name is the name as it is. The field of any other is found by
// memberOf.
func exactField(keys *structKeys, key []byte, info stringInfo) (*structFieldSet, bool) {
	if info.firstEscape >= 0 || info.nonASCII {
		return nil, false
	}
	if !keys.hasExactLength(len(key)) {
		return nil, true
	}
	w0, w1 := keyWords(key)
	return keys.findExact(key, w0, w1), true
}

// copyNames copies the names which the levels of the path have by their places in the buffer ( see setNameAt )
// into the levels: before the buffer drops them or moves its bytes, and before the path is read, by the error of
// the decoding or by a method or a function which reads a jsontext.Decoder.
func (s *Stream) copyNames() {
	call := s.ctx.v2
	for i := call.namesFrom; i < len(call.pathNames); i++ {
		np := call.pathNames[i]
		if !np.isName() {
			continue
		}
		l := &call.Path[i]
		if l.Name = append(l.Name[:0], s.buf[np.from-s.offset:np.to-s.offset]...); l.Name == nil {
			l.Name = []byte{} // the name "", which is a name
		}
		call.pathNames[i] = namePlace{}
	}
	call.namesFrom = len(call.pathNames)
	if call.skip.active {
		s.copySkipNames()
	}
}

// copyStreamNames copies the names of the path of the stream which ctx decodes, if it decodes one ( see
// Stream.copyNames ), for a method or a function which reads a jsontext.Decoder, which may read the path.
func copyStreamNames(ctx *RuntimeContext) {
	call := ctx.v2
	if call == nil {
		return
	}
	if call.Stream == nil && call.Lent != nil {
		// a value of UnmarshalDecode which is decoded in the buffer of its decoder, whose stream the method or the
		// function needs
		V2Hooks.Lend(ctx)
	}
	if call.Stream != nil {
		call.Stream.copyNames()
	}
}

// WholeEndV2 returns the end of the object or the array at cursor of buf, which has the input up to length, followed
// by the room of a decoder ( see textcoder.InputRoom ), and reports whether it ends there: false for any other value.
func WholeEndV2(buf []byte, cursor, length int64) (int64, bool) {
	if c := buf[cursor]; c != '{' && c != '[' {
		return 0, false
	}
	var sc compoundScanner
	return sc.scan(buf, cursor, length)
}

// DecodeWholeInV2 decodes the value of buf from cursor up to end, which buf holds whole, by dec into p, as
// decodeWholeV2 decodes the value of a stream which the buffer holds whole: nested in depth objects and arrays, in
// buf, whose byte at end is nul while the value is decoded. A type error which a decoder of v1 recorded is the error
// of the value, and the names of the objects of the value which an error ended are dropped after it, as
// DecodeV2After drops them.
func DecodeWholeInV2(ctx *RuntimeContext, buf []byte, cursor, end, depth int64, dec Decoder, p unsafe.Pointer) (int64, error) {
	call := ctx.v2
	entries, copies := call.names.mark()
	saved := buf[end]
	buf[end] = nul
	ctx.Buf = buf[:end+1]
	ctx.origin = ctx.Buf
	next, err := dec.Decode(ctx, cursor, depth, p)
	if err == nil && ctx.typeError != nil {
		err = ctx.v2Error()
	}
	buf[end] = saved
	ctx.Buf, ctx.origin = nil, nil
	if err != nil {
		ctx.DiscardTypeError()
	}
	call.names.truncate(entries, copies)
	return next, err
}

// LendAt makes the stream, which BorrowV2 made read the buffer of a jsontext.Decoder from the offset from, the end of
// the token before its next value, at that value, at the offset start, as decodeWholeV2 is while it decodes it: for
// a value which the caller decodes whole in the buffer ( see V2Hooks.Lend ).
func (s *Stream) LendAt(from, start int64) {
	call := s.ctx.v2
	call.Place.Set = false
	s.cursor = start - s.offset
	call.Value = StreamValue{From: from - s.offset, Start: s.cursor}
}

// PlaceWholeV2 records the place of err, the error of the value at the cursor, which the caller decoded whole in the
// buffer up to end ( see DecodeWholeInV2 ), after from, the total offset of the end of the token before it, as
// decodeWholeV2 records it. It returns err.
func (s *Stream) PlaceWholeV2(err error, from, end int64) error {
	s.ctx.v2.Value = StreamValue{}
	return s.placeValue(err, from, end)
}

// ReadsJSONText reports whether dec is the decoder of a method or a function which reads a jsontext.Decoder, which
// may read on past its value, or of a pointer, whose value may be one: decodeTopV2 decodes them by decodeNestedV2.
func ReadsJSONText(dec Decoder) bool {
	switch dec.(type) {
	case *v2UnmarshalFromDecoder, *v2FuncDecoder, *ptrDecoder, *basicPtrDecoder:
		return true
	}
	return false
}

// decodeStructElementsV2 decodes the object at the cursor into the struct at p, as v2StructDecoder.Decode does, after
// from, the total offset of the end of the token before it.
func (s *Stream) decodeStructElementsV2(d *v2StructDecoder, p unsafe.Pointer, depth, from int64) error {
	depth, lv, err := s.openElementsV2(true, depth, from)
	if err != nil {
		return err
	}
	ctx := s.ctx
	// the loop of the options, which are looked at once for the object, as v2StructDecoder.Decode chooses it
	if !d.foldAlways && d.fallback == nil &&
		ctx.Option.V2&(V2MatchCaseInsensitiveNames|V2RejectUnknownMembers|V2AllowDuplicateNames) == V2AllowDuplicateNames {
		return s.decodeStructMembersV2(d, p, depth, lv)
	}
	rejectDuplicateNames := ctx.Option.V2&V2AllowDuplicateNames == 0
	fold := d.foldAlways || ctx.Option.V2&V2MatchCaseInsensitiveNames != 0
	var seen uint64
	var more []uint64
	var unknown nameSet
	for k := 0; ; k++ {
		// the end of the token before the member, which the place of its errors is read again from, kept until the
		// value of the member
		step := s.offset + s.cursor
		keeps, keepFrom := s.keepFromHere(step)
		quoted, ok, err := s.nextMemberV2(k == 0)
		if err != nil {
			return s.placeDelimiterError(err, step)
		}
		if !ok {
			unknown.end(ctx.v2)
			s.keeps, s.keepFrom = keeps, keepFrom
			s.popLevel(lv)
			return nil
		}
		start := s.cursor - int64(len(quoted))
		var field *structFieldSet
		var plain bool
		if fold {
			field, _, plain = d.foldedFieldV2(ctx, s.buf[:s.length+1], start)
		} else {
			field, _, plain = plainFieldV2(d.keyDecoder.keys, s.buf[:s.length+1], start)
		}
		name := quoted[1 : len(quoted)-1]
		if !plain {
			// a name which v2StructDecoder.Decode finds by memberOf
			candidate, _, err := d.keyDecoder.decodeKey(s.buf[:s.length+1], start, false)
			if err != nil {
				return s.placeAt(err, step)
			}
			if field, name, err = d.memberOf(ctx, quoted, candidate); err != nil {
				if e, ok := err.(*errors.UnmarshalError); ok {
					e.Start = start
				}
				return s.placeAt(err, step)
			}
		}
		switch {
		case field != nil:
			if !rejectDuplicateNames {
				break
			}
			if idx := field.fieldIdx; idx < seenFields {
				bit := uint64(1) << uint(idx)
				if seen&bit != 0 {
					return s.placeAt(&errors.UnmarshalError{Start: start, Err: errors.ErrDuplicateName}, step)
				}
				seen |= bit
			} else if fieldSeen(idx, &more, len(d.fields)) {
				return s.placeAt(&errors.UnmarshalError{Start: start, Err: errors.ErrDuplicateName}, step)
			}
		case ctx.Option.V2&V2RejectUnknownMembers != 0 && d.fallback == nil:
			return s.placeAt(&errors.UnmarshalError{GoType: d.typ, Start: start, Err: errors.ErrUnknownName}, step)
		case rejectDuplicateNames:
			if !unknown.addCopy(ctx.V2Call(), nil, name) {
				return s.placeAt(&errors.UnmarshalError{Start: start, Err: errors.ErrDuplicateName}, step)
			}
		}
		var raw []byte
		var rawValue *[]byte
		if field == nil && d.fallback != nil && d.fallback.mapType == nil {
			// a member which a jsontext.Value takes, checked before its value is read, with its name as the input
			// has it
			if rawValue, err = d.fallback.openRaw(p); err != nil {
				return s.placeAt(&errors.UnmarshalError{GoType: jsonfields.RawValueType, Start: start, Err: err}, step)
			}
			raw = bytes.Clone(quoted)
		}
		s.copyName(name)
		nameEnd, err := s.memberValueV2()
		if err != nil {
			return s.placeAt(err, step)
		}
		s.setName(lv, k)
		s.keeps, s.keepFrom = keeps, keepFrom
		switch {
		case field != nil:
			err = s.decodeNestedV2(field.dec, unsafe.Add(p, field.offset), depth, nameEnd)
		case rawValue != nil:
			err = s.appendRawV2(rawValue, raw, depth, nameEnd)
		case d.fallback != nil:
			err = s.decodeFallbackV2(d.fallback, p, depth, nameEnd, s.level(lv).Name)
		default:
			err = s.skipValueV2(depth, nameEnd)
		}
		if err != nil {
			return err
		}
		s.level(lv).Count = countAfterValue(int64(k))
	}
}

// decodeStructMembersV2 is the loop of decodeStructElementsV2 with V2AllowDuplicateNames, for a struct whose names
// are matched as they are, which has no fallback, without V2RejectUnknownMembers: the members of the object whose
// level of the path is lv, after its bracket, nested in depth objects and arrays, are decoded as decodeStructElements
// decodes them for v1, and the ones of no field skipped. The bytes from the end of the token before a member, the
// place of its errors, are kept while its name and a string, a number or a literal of its value are read, which the
// buffer has whole: as decodeStructElementsV2 keeps them for the name and decodeNestedV2 for the value.
func (s *Stream) decodeStructMembersV2(d *v2StructDecoder, p unsafe.Pointer, depth int64, lv int) error {
	ctx := s.ctx
	// what the caller keeps: the bytes of a member are kept from the end of the token before it, unless the caller
	// keeps them, from before the object
	keeps, keepFrom := s.keeps, s.keepFrom
	for k := 0; ; k++ {
		step := s.offset + s.cursor
		if !keeps {
			s.keeps, s.keepFrom = true, step
		}
		key, info, start, ok, err := s.nextMember(k == 0)
		if err != nil {
			return s.placeDelimiterError(err, step)
		}
		if !ok {
			s.keeps, s.keepFrom = keeps, keepFrom
			s.popLevel(lv)
			return nil
		}
		field, plain := exactField(d.keyDecoder.keys, key, info)
		if !plain {
			// a name which v2StructDecoder.Decode finds by memberOf, which is copied decoded
			candidate, _, err := d.keyDecoder.decodeKey(s.buf[:s.length+1], start, false)
			if err != nil {
				return s.placeAt(err, step)
			}
			var name []byte
			if field, name, err = d.memberOf(ctx, s.buf[start:s.cursor], candidate); err != nil {
				if e, ok := err.(*errors.UnmarshalError); ok {
					e.Start = start
				}
				return s.placeAt(err, step)
			}
			s.copyName(name)
		}
		// the name as the buffer has it, inside its quotes, which is kept up to the colon
		nameFrom, nameEnd := s.offset+start+1, s.offset+s.cursor
		if err := s.memberValue(); err != nil {
			return s.placeAt(err, step)
		}
		if plain {
			s.setNameAt(lv, k, nameFrom, nameEnd-1)
		} else {
			s.setName(lv, k)
		}
		switch {
		case field == nil:
			s.keeps, s.keepFrom = keeps, keepFrom
			err = s.skipValueV2(depth, nameEnd)
		case s.buf[s.cursor] != '{' && s.buf[s.cursor] != '[' && s.decodeScalar(field.dec, unsafe.Add(p, field.offset), depth):
			// a string, a number or a literal which the buffer has whole, as decodeNestedV2 decodes it: the decoders
			// of the values of v2 which decodeScalar decodes are the ones which decodeNestedV2 gives it
			s.keeps, s.keepFrom = keeps, keepFrom
		default:
			s.keeps, s.keepFrom = keeps, keepFrom
			err = s.decodeNestedV2(field.dec, unsafe.Add(p, field.offset), depth, nameEnd)
		}
		if err != nil {
			return err
		}
		s.level(lv).Count = countAfterValue(int64(k))
	}
}

// appendRawV2 appends the member of the name raw, as the input has it, whose value is at the cursor, to b, a
// jsontext.Value fallback which openRaw made ready, as v2Fallback.decodeMember does. The value, which encoding/json/v2
// reads whole, is nested in depth objects and arrays, after from, the total offset of the end of the name before it.
func (s *Stream) appendRawV2(b *[]byte, raw []byte, depth, from int64) error {
	keeps, keepFrom := s.keepFromHere(from)
	start := s.offset + s.cursor
	end, whole, err := s.elementEnd(depth)
	if err == nil && !whole {
		if end, err = s.wholeEnd(depth); err != nil {
			err = s.wholeValueError(err, start)
		}
	}
	// the value is read: nothing more is read for it
	s.keeps, s.keepFrom = keeps, keepFrom
	if err != nil {
		return s.placeAt(err, from)
	}
	ctx := s.ctx
	ctx.Buf = s.buf[:s.length+1]
	_, err = ctx.skipValueV2(s.cursor, depth)
	ctx.Buf = nil
	if err != nil {
		return s.placeValue(&errors.UnmarshalError{GoType: jsonfields.RawValueType, Start: s.cursor, Err: err}, from, end)
	}
	*b = append(*b, raw...)
	*b = append(*b, ':')
	*b = append(*b, s.buf[s.cursor:end]...)
	*b = append(*b, '}')
	s.cursor = end
	return nil
}

// decodeFallbackV2 decodes the value at the cursor, of the member of the name, which is of no field of the struct at
// p, into its map fallback, as v2Fallback.decodeMember does. from is the total offset of the end of the name before
// the value, nested in depth objects and arrays.
func (s *Stream) decodeFallbackV2(fb *v2Fallback, p unsafe.Pointer, depth, from int64, name []byte) error {
	m := valueAt(ptrTypeOf(fb.mapType), fb.target(p))
	if m.IsNil() {
		m.Set(reflect.MakeMap(fb.mapType))
	}
	key := reflect.ValueOf(string(name)).Convert(fb.mapType.Key())
	v := reflect.New(fb.mapType.Elem())
	if old := m.MapIndex(key); old.IsValid() {
		v.Elem().Set(old)
	}
	err := s.decodeNestedV2(fb.value, v.UnsafePointer(), depth, from)
	m.SetMapIndex(key, v.Elem())
	return err
}

// skipValueV2 skips the value at the cursor, nested in depth objects and arrays, after from, the total offset of the
// end of the token before it, as encoding/json/v2 skips it: checked for the text which v2 rejects, by the decoder if
// the buffer holds it whole, and else by a jsontext.Decoder attached at its place, which reads on from the reader of
// the stream.
func (s *Stream) skipValueV2(depth, from int64) error {
	keeps, keepFrom := s.keepFromHere(from)
	end, whole, err := s.elementEnd(depth)
	// the value is found, or the decoder is attached before anything more is read
	s.keeps, s.keepFrom = keeps, keepFrom
	if err != nil {
		return s.placeAt(err, from)
	}
	if !whole {
		if s.ctx.Option.V2&asV1Text == asV1Text {
			// a value of the grammar only, as v1 skips it
			return s.skipLongV2(depth)
		}
		s.leaveSourceFrom(from)
		call := s.ctx.v2
		call.Value = StreamValue{From: from - s.offset, Start: s.cursor, ReadsOn: true}
		s.copyNames()
		err := V2Hooks.SkipStreamValue(s, depth)
		call.Value = StreamValue{}
		return err
	}
	ctx := s.ctx
	ctx.Buf = s.buf[:s.length+1]
	_, err = ctx.skipValueV2(s.cursor, depth)
	ctx.Buf = nil
	if err != nil {
		return s.placeValue(err, from, end)
	}
	s.cursor = end
	return nil
}

// decodeMapElementsV2 decodes the object at the cursor into the map at p, as v2MapDecoder.Decode does, after from, the
// total offset of the end of the token before it.
func (s *Stream) decodeMapElementsV2(d *v2MapDecoder, p unsafe.Pointer, depth, from int64) error {
	depth, lv, err := s.openElementsV2(true, depth, from)
	if err != nil {
		return err
	}
	ctx := s.ctx
	m := valueAt(d.ptrType, p)
	if m.IsNil() {
		m.Set(reflect.MakeMap(d.typ))
	}
	rejectDuplicateNames := ctx.Option.V2&V2AllowDuplicateNames == 0
	var seen reflect.Value
	if rejectDuplicateNames && m.Len() > 0 {
		seen = reflect.MakeMap(reflect.MapOf(d.typ.Key(), emptyStructType))
	}
	// names are the names of the keys, if two keys may have the same name
	var names nameSet
	keyNames := rejectDuplicateNames && !uniqueKeyNames(d.keyDecoder, d.typ.Key().Kind(), ctx.Option.V2&V2AllowInvalidUTF8 != 0)
	k, v := reflect.New(d.typ.Key()), reflect.New(d.typ.Elem())
	for i := 0; ; i++ {
		// the end of the token before the entry, which the place of its errors is read again from, kept until the
		// value of the entry
		step := s.offset + s.cursor
		keeps, keepFrom := s.keepFromHere(step)
		quoted, ok, err := s.nextMemberV2(i == 0)
		if err != nil {
			return s.placeDelimiterError(err, step)
		}
		if !ok {
			names.end(ctx.v2)
			s.keeps, s.keepFrom = keeps, keepFrom
			s.popLevel(lv)
			return nil
		}
		start := s.cursor - int64(len(quoted))
		// the name is decoded as a value of the key type, which the buffer holds whole
		k.Elem().SetZero()
		ctx.Buf = s.buf[:s.length+1]
		ctx.origin = ctx.Buf
		call := ctx.v2
		call.Value = StreamValue{From: step - s.offset, Start: start}
		_, err = d.keyDecoder.Decode(ctx, start, depth, k.UnsafePointer())
		call.Value = StreamValue{}
		ctx.Buf, ctx.origin = nil, nil
		if err == nil && ctx.typeError != nil {
			ctx.Buf = s.buf[:s.length+1]
			err = ctx.v2Error()
			ctx.Buf = nil
		}
		if err != nil {
			ctx.DiscardTypeError()
			return s.placeAt(err, step)
		}
		name := unquotedName(quoted)
		if keyNames && !names.addCopy(ctx.V2Call(), nil, name) {
			return s.placeAt(&errors.UnmarshalError{Start: start, Err: errors.ErrDuplicateName}, step)
		}
		key := k.Elem()
		if old := m.MapIndex(key); old.IsValid() {
			if rejectDuplicateNames && (!seen.IsValid() || seen.MapIndex(key).IsValid()) {
				return s.placeAt(&errors.UnmarshalError{Start: start, Err: errors.ErrDuplicateName}, step)
			}
			// the value of the name is decoded into, but replaced with MergeWithLegacySemantics
			if s.Option.V2&V2MergeWithLegacySemantics == 0 {
				v.Elem().Set(old)
			} else {
				v.Elem().SetZero()
			}
		} else {
			v.Elem().SetZero()
		}
		s.copyName(name)
		nameEnd, err := s.memberValueV2()
		if err != nil {
			return s.placeAt(err, step)
		}
		s.setName(lv, i)
		s.keeps, s.keepFrom = keeps, keepFrom
		err = s.decodeNestedV2(d.valueDecoder, v.UnsafePointer(), depth, nameEnd)
		m.SetMapIndex(key, v.Elem())
		if seen.IsValid() {
			seen.SetMapIndex(key, reflect.Zero(emptyStructType))
		}
		if err != nil {
			return err
		}
		s.level(lv).Count = countAfterValue(int64(i))
	}
}

// nextElementV2 moves the cursor to the next element of the array whose elements are read, which has idx elements
// before it, and returns the total offset of the end of the token before it, which the place of its errors is read
// again from, as the place of the element. It reports false after the end of the array.
func (s *Stream) nextElementV2(idx int) (int64, bool, error) {
	step := s.offset + s.cursor
	keeps, keepFrom := s.keepFromHere(step)
	ok, err := s.nextElement(idx == 0)
	s.keeps, s.keepFrom = keeps, keepFrom
	return step, ok, err
}

// decodeSliceElementsV2 decodes the array at the cursor into the slice at p, as v2SliceDecoder.Decode does, after
// from, the total offset of the end of the token before it.
func (s *Stream) decodeSliceElementsV2(d *v2SliceDecoder, p unsafe.Pointer, depth, from int64) error {
	if d.zero && (*sliceHeader)(p).cap > 0 && s.Option.V2&V2MergeWithLegacySemantics == 0 {
		sv := valueAt(d.slicePtrType, p)
		sv.Slice(0, sv.Cap()).Clear()
	}
	depth, lv, err := s.openElementsV2(false, depth, from)
	if err != nil {
		return err
	}
	dst := (*sliceHeader)(p)
	// the elements are decoded into the slice up to limit, and into a buffer after it, as sliceDecoder.Decode does
	t := sliceTarget{d: d.sliceDecoder, dst: dst, limit: max(dst.cap, inPlaceSliceLength)}
	runs := readsWhole(d.valueDecoder)
	for idx := 0; ; {
		step, ok, err := s.nextElementV2(idx)
		if err != nil || !ok {
			t.finish(idx, idx, err)
			if err != nil {
				return s.placeDelimiterError(err, step)
			}
			s.popLevel(lv)
			return nil
		}
		if runs {
			n, err := s.decodeRunV2(&t, lv, idx, depth, step)
			if err != nil {
				t.finish(n, n+1, err)
				return err
			}
			if n > idx {
				idx = n
				continue
			}
		}
		if err := s.decodeNestedV2(d.valueDecoder, t.elem(idx), depth, step); err != nil {
			t.finish(idx, idx+1, err)
			return err
		}
		t.decoded(idx)
		idx++
		s.level(lv).Count = int64(idx)
	}
}

// decodeRunV2 decodes the elements of the array whose elements are read into t, from the element idx at the cursor,
// after step, the total offset of the end of the token before it: the ones which the buffer holds whole, up to the
// last one which is an object or an array that ends in what has been read, as decodeRun does, each as decodeWholeV2
// decodes it, without the lookup of its end. It returns the number of the elements of the array decoded, which the
// level lv of the path counts: idx if the buffer holds no such element. The decoder of the elements reads a value
// whole ( see readsWhole ).
func (s *Stream) decodeRunV2(t *sliceTarget, lv, idx int, depth, step int64) (int, error) {
	s.indexAt(depth)
	end, ok := s.elements.lastElementClose(s.offset+s.cursor, depth)
	if !ok {
		return idx, nil
	}
	ctx := s.ctx
	call := ctx.v2
	dec := t.d.valueDecoder
	// a nul byte is put after the last element while the run is decoded, as decodeRun puts it
	stop := end - s.offset + 1
	saved := s.buf[stop]
	s.buf[stop] = nul
	buf := s.buf[:stop+1]
	ctx.Buf, ctx.origin = buf, buf
	var err error
	for {
		call.Value = StreamValue{From: step - s.offset, Start: s.cursor}
		next, derr := dec.Decode(ctx, s.cursor, depth, t.elem(idx))
		if derr == nil && ctx.typeError != nil {
			derr = ctx.v2Error()
		}
		if derr != nil {
			ctx.DiscardTypeError()
			err = s.placeValue(derr, step, stop)
			break
		}
		t.decoded(idx)
		idx++
		s.level(lv).Count = int64(idx)
		s.cursor, step = next, s.offset+next
		// the next element is in the run if it starts before its end
		c := skipWhiteSpace(buf, next)
		if buf[c] != ',' {
			break
		}
		if c = skipWhiteSpace(buf, c+1); c >= stop {
			break
		}
		s.cursor = c
	}
	s.buf[stop] = saved
	ctx.Buf, ctx.origin = nil, nil
	call.Value = StreamValue{}
	return idx, err
}

// readsWhole reports whether decodeNestedV2 decodes a value which the buffer holds whole by dec as decodeWholeV2
// decodes it: unless dec, or the decoder of the value which dec decodes through pointers, is one of a method or a
// function which reads a jsontext.Decoder, which is given one attached at the value. The pointers of a type which
// points to itself, through other types or not, lead to no other decoder: slow, which follows them at half the
// pace, meets dec in them.
func readsWhole(dec Decoder) bool {
	slow := dec
	for odd := false; ; odd = !odd {
		switch d := dec.(type) {
		case *v2UnmarshalFromDecoder, *v2FuncDecoder:
			return false
		case *ptrDecoder:
			dec = d.dec
		case *basicPtrDecoder:
			dec = d.dec
		default:
			return true
		}
		if odd {
			slow = pointeeDecoder(slow)
		}
		if dec == slow {
			return true
		}
	}
}

// pointeeDecoder returns the decoder of the value which dec, a decoder of a pointer, decodes through it.
func pointeeDecoder(dec Decoder) Decoder {
	switch d := dec.(type) {
	case *ptrDecoder:
		return d.dec
	case *basicPtrDecoder:
		return d.dec
	}
	return dec
}

// decodeArrayElementsV2 decodes the array at the cursor into the array at p, as v2ArrayDecoder.Decode does, after
// from, the total offset of the end of the token before it.
func (s *Stream) decodeArrayElementsV2(d *v2ArrayDecoder, p unsafe.Pointer, depth, from int64) error {
	depth, lv, err := s.openElementsV2(false, depth, from)
	if err != nil {
		return err
	}
	var lengthErr error
	// an element is decoded into its zero value, but into the element with MergeWithLegacySemantics
	zeroElems := s.Option.V2&V2MergeWithLegacySemantics == 0
	for idx := 0; ; idx++ {
		step, ok, err := s.nextElementV2(idx)
		if err != nil {
			return s.placeDelimiterError(err, step)
		}
		if !ok {
			for ; idx < d.alen; idx++ {
				d.elem.zero(unsafe.Add(p, uintptr(idx)*d.elem.size))
				lengthErr = errArrayUnderflow
			}
			if lengthErr != nil && s.Option.V2&V2UnmarshalArrayFromAnyLength == 0 {
				// the error of the length is at the end of the array, which tells it
				return s.placeAt(&errors.UnmarshalError{GoType: d.typ, Start: s.cursor - 1, Err: lengthErr}, step)
			}
			s.popLevel(lv)
			return nil
		}
		if idx < d.alen {
			ep := unsafe.Add(p, uintptr(idx)*d.elem.size)
			if zeroElems {
				d.elem.zero(ep)
			}
			err = s.decodeNestedV2(d.valueDecoder, ep, depth, step)
		} else {
			err = s.skipValueV2(depth, step)
			lengthErr = errArrayOverflow
		}
		if err != nil {
			return err
		}
		s.level(lv).Count = int64(idx + 1)
	}
}

// decodeInterfaceElementsV2 decodes the object or the array at the cursor into the interface value at p, as
// v2InterfaceDecoder.Decode does, after from, the total offset of the end of the token before it. It reports false,
// having read nothing, for a value which is read whole.
func (s *Stream) decodeInterfaceElementsV2(d *v2InterfaceDecoder, p unsafe.Pointer, depth, from int64) (bool, error) {
	ctx := s.ctx
	if (*[2]unsafe.Pointer)(p)[0] != nil && s.Option.V2&V2MergeWithLegacySemantics != 0 && !d.holdsPointer(p) {
		// MergeWithLegacySemantics decodes into a pointer which the interface value holds only
		*(*[2]unsafe.Pointer)(p) = [2]unsafe.Pointer{}
	}
	if (*[2]unsafe.Pointer)(p)[0] != nil {
		// a value which the interface value holds is decoded into
		iface := valueAt(d.ptrType, p)
		held := iface.Elem()
		if !d.fromAny && (held.Type() == interfaceMapType || held.Type() == anySliceType) {
			return true, s.decodeMergingV2(d, p, depth, from)
		}
		v := reflect.New(held.Type())
		v.Elem().Set(held)
		dec, err := ctx.DecoderOfV2(ptrTypeOf(held.Type()))
		if err != nil {
			return true, err
		}
		err = s.decodeNestedV2(dec, v.UnsafePointer(), depth, from)
		iface.Set(v.Elem())
		return true, err
	}
	switch {
	case !d.empty:
		return true, s.placeAt(&errors.UnmarshalError{GoType: d.typ, Start: s.cursor, NoKind: true, Err: errNilInterface}, from)
	case d.fromAny:
		// the value of the type of its kind, which the functions of unmarshaling decode
		v := reflect.New(anyValueTypes[s.buf[s.cursor]])
		dec, err := ctx.DecoderOfV2(ptrTypeOf(v.Elem().Type()))
		if err != nil {
			return true, err
		}
		err = s.decodeNestedV2(dec, v.UnsafePointer(), depth, from)
		valueAt(d.ptrType, p).Set(v.Elem())
		return true, err
	}
	return true, s.decodeMergingV2(d, p, depth, from)
}

// decodeMergingV2 decodes the object or the array at the cursor into the interface{} at p, as decodeMerging does:
// into the map[string]interface{} or the []interface{} which it holds, or a new one.
func (s *Stream) decodeMergingV2(d *v2InterfaceDecoder, p unsafe.Pointer, depth, from int64) error {
	held := *(*any)(p)
	if s.buf[s.cursor] == '{' {
		m, ok := held.(map[string]any)
		if held != nil && !ok {
			return s.placeAt(&errors.UnmarshalError{GoType: reflect.TypeOf(held), Start: s.cursor}, from)
		}
		if m == nil {
			// a new map, of the entries of the object, which are decoded first
			m, err := s.decodeNewAnyObjectV2(d, depth, from)
			if m != nil {
				*(*any)(p) = m
			}
			return err
		}
		return s.decodeAnyObjectV2(d, m, depth, from)
	}
	a, ok := held.([]any)
	if held != nil && !ok {
		return s.placeAt(&errors.UnmarshalError{GoType: reflect.TypeOf(held), Start: s.cursor}, from)
	}
	if a == nil {
		// a new slice, of the elements of the array, which are decoded first
		a, err := s.decodeNewAnyArrayV2(d, depth, from)
		if a != nil {
			*(*any)(p) = a
		}
		return err
	}
	err := s.decodeAnyArrayV2(d, &a, depth, from)
	*(*any)(p) = a
	return err
}

// decodeNewAnyObjectV2 decodes the object at the cursor into a new map[string]interface{}, as decodeObjectInto does,
// after from, the total offset of the end of the token before it: the entries are pushed to the stack of the
// context, as decodeAnyObject pushes them for v1, and put into a map of their number at the end, which it returns,
// with the entries before an error. The value of a name which the object had is decoded into its first value.
func (s *Stream) decodeNewAnyObjectV2(d *v2InterfaceDecoder, depth, from int64) (map[string]any, error) {
	depth, lv, err := s.openElementsV2(true, depth, from)
	if err != nil {
		return nil, err
	}
	ctx := s.ctx
	base := len(ctx.entryStack)
	var names anyNames
	for k := 0; ; k++ {
		// the end of the token before the member, which the place of its errors is read again from, kept until the
		// value of the member
		step := s.offset + s.cursor
		keeps, keepFrom := s.keepFromHere(step)
		key, info, start, ok, err := s.nextMember(k == 0)
		if err != nil {
			return ctx.anyEntries(base), s.placeDelimiterError(err, step)
		}
		if !ok {
			s.keeps, s.keepFrom = keeps, keepFrom
			s.popLevel(lv)
			return ctx.anyEntries(base), nil
		}
		quoted := s.buf[start:s.cursor]
		if ctx.Option.V2&V2AllowInvalidUTF8 == 0 && hasNotASCIIOrEscape(quoted) && !validV2Text(quoted) {
			return ctx.anyEntries(base), s.placeAt(errV2Text, step)
		}
		name := string(decodeLiteral(key, info))
		at := names.index(ctx, base, name)
		if at >= 0 && ctx.Option.V2&V2AllowDuplicateNames == 0 {
			return ctx.anyEntries(base), s.placeAt(&errors.UnmarshalError{Start: start, Err: errors.ErrDuplicateName}, step)
		}
		plain := info.firstEscape < 0 && !info.nonASCII
		if !plain {
			s.copyName(unsafeBytes(name))
		}
		nameEnd := s.offset + s.cursor
		if err := s.memberValue(); err != nil {
			return ctx.anyEntries(base), s.placeAt(err, step)
		}
		if plain {
			s.setNameAt(lv, k, nameEnd-int64(len(name))-1, nameEnd-1)
		} else {
			s.setName(lv, k)
		}
		s.keeps, s.keepFrom = keeps, keepFrom
		if at >= 0 {
			// the value of a name which the object had, which is decoded into the first one: out of ctx.slot, which
			// the values in it are decoded into, from nil
			value := ctx.entryStack[at].value
			err = s.decodeNestedV2(d, unsafe.Pointer(&value), depth, nameEnd)
			ctx.entryStack[at].value = value
		} else {
			err = s.decodeNestedV2(d, unsafe.Pointer(&ctx.slot), depth, nameEnd)
			ctx.entryStack = append(ctx.entryStack, anyEntry{key: name, value: ctx.slot})
			ctx.slot = nil
		}
		if err != nil {
			return ctx.anyEntries(base), err
		}
		s.level(lv).Count = countAfterValue(int64(k))
	}
}

// anyNames finds the names of the entries of an object which decodeNewAnyObjectV2 pushed: by the bits of their
// hashes in mask, and a comparison with the names of the entries of its bit, up to maxListedNames names, and by the
// keys of a map of their entries past them, so that an object of many names is not quadratic in its names.
type anyNames struct {
	mask nameMask
	set  map[string]int
}

// index returns the index in the stack of the entry of the name, which the entries of the stack from base are, or -1
// if they don't have it, and adds the name, which the caller pushes the entry of then.
func (n *anyNames) index(ctx *RuntimeContext, base int, name string) int {
	entries := ctx.entryStack[base:]
	if n.set != nil {
		if at, ok := n.set[name]; ok {
			return at
		}
		n.set[name] = len(ctx.entryStack)
		return -1
	}
	w, bit := n.mask.bit(nameHash(unsafeBytes(name)))
	if *w&bit != 0 {
		for i := range entries {
			if entries[i].key == name {
				return base + i
			}
		}
	}
	*w |= bit
	if len(entries) == maxListedNames {
		// the names, past which they are found by the map, which has room for as many again
		n.set = make(map[string]int, 2*maxListedNames)
		for i := range entries {
			n.set[entries[i].key] = base + i
		}
		n.set[name] = len(ctx.entryStack)
	}
	return -1
}

// anyEntries returns a new map of the entries of the stack of the context from base, which it removes.
func (ctx *RuntimeContext) anyEntries(base int) map[string]any {
	entries := ctx.entryStack[base:]
	m := make(map[string]any, len(entries))
	for i := range entries {
		m[entries[i].key] = entries[i].value
	}
	ctx.popEntries(base)
	return m
}

// decodeNewAnyArrayV2 decodes the array at the cursor into a new []interface{}, as decodeArrayInto does, after from,
// the total offset of the end of the token before it: the elements are pushed to the stack of the context, as
// decodeAnyArray pushes them for v1, and copied into a slice of their number at the end, which it returns, with the
// elements before an error.
func (s *Stream) decodeNewAnyArrayV2(d *v2InterfaceDecoder, depth, from int64) ([]any, error) {
	depth, lv, err := s.openElementsV2(false, depth, from)
	if err != nil {
		return nil, err
	}
	ctx := s.ctx
	base := len(ctx.anyStack)
	for idx := 0; ; idx++ {
		step, ok, err := s.nextElementV2(idx)
		if err != nil {
			return ctx.anyElements(base), s.placeDelimiterError(err, step)
		}
		if !ok {
			s.popLevel(lv)
			return ctx.anyElements(base), nil
		}
		err = s.decodeNestedV2(d, unsafe.Pointer(&ctx.slot), depth, step)
		ctx.anyStack = append(ctx.anyStack, ctx.slot)
		ctx.slot = nil
		if err != nil {
			return ctx.anyElements(base), err
		}
		s.level(lv).Count = int64(idx + 1)
	}
}

// anyElements returns a new slice of the elements of the stack of the context from base, which it removes: an empty
// one, not nil, if there are none.
func (ctx *RuntimeContext) anyElements(base int) []any {
	elems := make([]any, len(ctx.anyStack)-base)
	copy(elems, ctx.anyStack[base:])
	ctx.popAny(base)
	return elems
}

// decodeAnyObjectV2 decodes the object at the cursor into m, as decodeObjectInto does, after from, the total offset
// of the end of the token before it.
func (s *Stream) decodeAnyObjectV2(d *v2InterfaceDecoder, m map[string]any, depth, from int64) error {
	depth, lv, err := s.openElementsV2(true, depth, from)
	if err != nil {
		return err
	}
	ctx := s.ctx
	var names nameSet
	for k := 0; ; k++ {
		// the end of the token before the member, which the place of its errors is read again from, kept until the
		// value of the member
		step := s.offset + s.cursor
		keeps, keepFrom := s.keepFromHere(step)
		quoted, ok, err := s.nextMemberV2(k == 0)
		if err != nil {
			return s.placeDelimiterError(err, step)
		}
		if !ok {
			names.end(ctx.v2)
			s.keeps, s.keepFrom = keeps, keepFrom
			s.popLevel(lv)
			return nil
		}
		if ctx.Option.V2&V2AllowInvalidUTF8 == 0 && hasNotASCIIOrEscape(quoted) && !validV2Text(quoted) {
			return s.placeAt(errV2Text, step)
		}
		name := string(unquotedName(quoted))
		if ctx.Option.V2&V2AllowDuplicateNames == 0 && !names.addCopy(ctx.V2Call(), nil, unsafeBytes(name)) {
			start := s.cursor - int64(len(quoted))
			return s.placeAt(&errors.UnmarshalError{Start: start, Err: errors.ErrDuplicateName}, step)
		}
		s.copyName(unsafeBytes(name))
		nameEnd, err := s.memberValueV2()
		if err != nil {
			return s.placeAt(err, step)
		}
		s.setName(lv, k)
		s.keeps, s.keepFrom = keeps, keepFrom
		// the value of the name in the map is decoded into, as decodeElem does: the decoder of the interface{}
		// decodes a name of an object twice into its first value ( see v2InterfaceDecoder.Decode ), but with
		// MergeWithLegacySemantics, which replaces it
		var value any
		if s.Option.V2&V2MergeWithLegacySemantics == 0 {
			value = m[name]
		}
		err = s.decodeNestedV2(d, unsafe.Pointer(&value), depth, nameEnd)
		m[name] = value
		if err != nil {
			return err
		}
		s.level(lv).Count = countAfterValue(int64(k))
	}
}

// decodeAnyArrayV2 decodes the array at the cursor into *a from its first element, as decodeArrayInto does, after
// from, the total offset of the end of the token before it.
func (s *Stream) decodeAnyArrayV2(d *v2InterfaceDecoder, a *[]any, depth, from int64) error {
	depth, lv, err := s.openElementsV2(false, depth, from)
	if err != nil {
		return err
	}
	*a = (*a)[:0]
	if *a == nil {
		*a = []any{}
	}
	for idx := 0; ; idx++ {
		step, ok, err := s.nextElementV2(idx)
		if err != nil {
			return s.placeDelimiterError(err, step)
		}
		if !ok {
			s.popLevel(lv)
			return nil
		}
		*a = append(*a, nil)
		if err := s.decodeNestedV2(d, unsafe.Pointer(&(*a)[len(*a)-1]), depth, step); err != nil {
			return err
		}
		s.level(lv).Count = int64(idx + 1)
	}
}
