package jsontext

import (
	"bytes"
	"errors"
	"io"
	"unsafe"

	"github.com/goccy/go-json/internal/options"
	"github.com/goccy/go-json/internal/textcoder"
)

func init() {
	textcoder.AttachDecoder = attachDecoder
	textcoder.DetachDecoder = detachDecoder
	textcoder.UnmarshalPlaceAt = unmarshalPlaceAt
	textcoder.DecoderPlace = decoderPlace
	textcoder.DecoderAt = func(dec unsafe.Pointer) (bool, textcoder.Outer, int) {
		d := &(*Decoder)(dec).d
		if !d.ready() {
			d.setUp()
		}
		return d.st.last().needName(), d.st.outer, d.st.depth() + d.st.base
	}
	textcoder.BeginValue = beginValue
	textcoder.DecoderInput = func(dec unsafe.Pointer, start int64) ([]byte, io.Reader) {
		d := &(*Decoder)(dec).d
		return d.buf[start-d.base:], d.r
	}
	textcoder.DecoderBuffer = func(dec unsafe.Pointer) ([]byte, int64, bool) {
		d := &(*Decoder)(dec).d
		return d.buf, d.base, !d.shared && d.bb == nil && d.r != nil && cap(d.buf)-len(d.buf) >= textcoder.InputRoom
	}
	textcoder.FetchInput = func(dec unsafe.Pointer, keep int64, n int) ([]byte, int64, int, error) {
		d := &(*Decoder)(dec).d
		// the bytes before keep are dropped as the ones before the last token are: the positions of the decoder
		// before them are the start of the buffer then, which the caller sets by EndValue or PutBack
		var shift int
		if cap(d.buf)-len(d.buf) <= textcoder.InputRoom {
			// the buffer is full, as a stream finds its buffer full before it reads ( see decoder.Stream.read )
			prev := d.prevStart
			d.prevStart = int(keep - d.base)
			shift = d.makeRoom(n + textcoder.InputRoom)
			d.prevStart = prev - shift
		}
		d.pos, d.prevStart, d.prevEnd = max(d.pos, 0), max(d.prevStart, 0), max(d.prevEnd, 0)
		m, err := d.r.Read(d.buf[len(d.buf):min(cap(d.buf)-textcoder.InputRoom, len(d.buf)+n)])
		d.buf = d.buf[:len(d.buf)+m]
		return d.buf, d.base, m, err
	}
	textcoder.EndValue = endValue
	textcoder.PutBack = func(dec unsafe.Pointer, start int64, in []byte) {
		d := &(*Decoder)(dec).d
		if d.shared {
			d.buf, d.shared = nil, false
		}
		d.buf = append(d.buf[:0], in...)
		d.base, d.pos, d.prevStart, d.prevEnd = start, 0, 0, 0
	}
	textcoder.SkipToEnd = skipToEnd
	textcoder.FailDecoder = func(dec unsafe.Pointer, err error) {
		d := &(*Decoder)(dec).d
		d.buf, d.shared = d.buf[:d.pos], true
		d.r, d.bb, d.rerr, d.peekErr = &failedInput{err: err}, nil, nil, nil
	}
	textcoder.DecoderFailed = func(dec unsafe.Pointer) bool {
		_, failed := (*Decoder)(dec).d.r.(*failedInput)
		return failed
	}
	textcoder.ConfigureDecoder = func(dec unsafe.Pointer, opts *options.Config) func() {
		d := &(*Decoder)(dec).d
		saved, validUTF8, rejectDuplicateNames := d.cfg, d.vs.validUTF8, d.vs.rejectDuplicateNames
		d.cfg = *opts
		d.vs.validUTF8, d.vs.rejectDuplicateNames = !d.cfg.Has(allowInvalidUTF8), !d.cfg.Has(allowDuplicateNames)
		return func() {
			d.cfg, d.vs.validUTF8, d.vs.rejectDuplicateNames = saved, validUTF8, rejectDuplicateNames
		}
	}
	textcoder.ReadError = func(err error) error { return &ioError{err: err} }
	textcoder.IsIOError = func(err error) bool {
		var e *ioError
		return errors.As(err, &e)
	}
	textcoder.AfterTopLevelError = func(in []byte, base int64, opts *options.Config) error {
		dec := NewDecoder(bytes.NewBuffer(in), opts)
		dec.d.base = base
		return dec.d.charError(0, placeAfterTopLevel, pointAt)
	}
	textcoder.ReleaseDecoder = func(dec unsafe.Pointer) {
		d := &(*Decoder)(dec).d
		d.cfg = config{}
		d.st.outer = nil
		if d.r != &attachedInput {
			d.r, d.bb = nil, nil // the reader of an input which is read on, which the decoder must not keep
		}
	}
	textcoder.DecoderPointer = func(dec unsafe.Pointer, where int) string {
		st := &(*Decoder)(dec).d.st
		switch where {
		case -1:
			return string(st.pointer(pointLast))
		case +1:
			return string(st.pointer(pointNext))
		}
		return string(st.pointer(pointAt))
	}
}

// attachedInput is the reader of an attached decoder, whose input is all in its buffer: an empty bytes.Buffer, which
// is never written to, which makes the decoder read its buffer in place, and nothing after it.
var attachedInput bytes.Buffer

// attachDecoder makes the Decoder dec read p.In from p.Pos, at the place whose innermost level is the one of p ( see
// textcoder.AttachDecoder ). The input is read in place: it is the array of a bytes.Buffer for the decoder, which
// never writes to it, and, if p has a reader, the buffer of the decoder, which it moves and reads into, once it
// needs more.
func attachDecoder(dec unsafe.Pointer, p *textcoder.DecoderAttachment) {
	d := &(*Decoder)(dec).d
	if !(p.Same && p.R == nil && d.ready() && d.r == &attachedInput) {
		// set up as reset sets up a decoder of a reader, by the options of the call, without the options of a
		// reset, which would be allocated.
		if !d.ready() {
			d.st, d.vs.scratch = stack{}, nil
		}
		d.cfg = *p.Opts
		d.r, d.bb = &attachedInput, &attachedInput
		if p.R != nil {
			d.r, d.bb = p.R, nil
		}
		d.raw = tokenSource{dec: d, form: formRaw}
		d.vs = valueScanner{
			st:                   &d.st,
			validUTF8:            !d.cfg.Has(allowInvalidUTF8),
			rejectDuplicateNames: !d.cfg.Has(allowDuplicateNames),
			decoding:             true,
			scratch:              d.vs.scratch[:0],
		}
	}
	st := &d.st
	st.reset()
	if !p.Top {
		_ = st.push(p.Object)
	}
	// the names of an object, which the value read at its place can't have, are not given.
	st.last().count = p.Count
	st.outer, st.outerDepth, st.outerCount = p.Outer, len(st.levels)-1, p.Count
	st.base = p.OuterDepth - st.outerDepth
	d.buf, d.shared = p.In, !p.Own
	d.base, d.pos, d.prevStart, d.prevEnd = p.Base, p.Pos, p.Pos, p.Pos
	d.rerr, d.peekErr = nil, nil
	d.vs.scratch = d.vs.scratch[:0]
	p.Depth = st.depth()
}

// detachDecoder sets where dec stopped reading in p, and drops its input ( see textcoder.DetachDecoder ).
func detachDecoder(dec unsafe.Pointer, p *textcoder.DecoderAttachment) {
	d := &(*Decoder)(dec).d
	start := d.prevStart
	if start == d.prevEnd && start > 0 {
		// a delimiter or a literal, which ends there: the buffer doesn't keep it as the last token.
		if c := d.buf[start-1]; c == '{' || c == '}' || c == '[' || c == ']' {
			start--
		} else {
			for start > 0 && 'a' <= d.buf[start-1] && d.buf[start-1] <= 'z' {
				start--
			}
		}
	}
	p.Depth, p.Count, p.Pos, p.End, p.PrevStart = d.st.depth(), d.st.last().count, d.pos, d.base+int64(d.pos), d.base+int64(start)
	p.Rest = nil
	if !d.shared {
		// the decoder read on into a buffer of its own, whose bytes after the place go back to the caller
		p.Rest = d.buf[d.pos:]
		if p.Own {
			p.In = d.buf
		}
	}
	d.buf, d.vs.in = nil, nil
	d.pos, d.prevStart, d.prevEnd = 0, 0, 0
	d.peekErr = nil
}

// unmarshalPlace is textcoder.UnmarshalPlace: the input is read by a Decoder, token by token, as encoding/json/v2
// reads it, which finds its syntax errors and the pointer of the value at stop as encoding/json/v2 does.
func unmarshalPlace(in []byte, opts *options.Config, stop int64, read textcoder.Read) (string, error) {
	dec := NewDecoder(bytes.NewBuffer(in), opts)
	d := &dec.d
	for {
		if stop >= 0 {
			if pos, err := d.beforeToken(true); err == nil && d.base+int64(pos) >= stop {
				return readPlace(dec, read)
			}
		}
		if _, err := dec.ReadToken(); err != nil {
			if err == io.EOF {
				// an input of white space only, which has no value
				return "", &SyntacticError{ByteOffset: int64(len(in)), Err: io.ErrUnexpectedEOF}
			}
			return "", err
		}
		if d.st.depth() == 0 {
			// the value is read: only white space may follow it
			pos, err := d.skip(d.pos)
			if err == nil {
				return "", d.charError(pos, placeAfterTopLevel, pointAt)
			}
			return "", nil
		}
	}
}

// decoderPlace is textcoder.DecoderPlace.
func decoderPlace(dec unsafe.Pointer, levels []textcoder.Level) []textcoder.Level {
	d := &(*Decoder)(dec).d
	if !d.ready() {
		d.setUp()
	}
	st := &d.st
	if st.outer != nil {
		st = st.full()
	}
	for i := range st.levels {
		l := &st.levels[i]
		n := len(levels)
		if n < cap(levels) {
			// the entry, whose name keeps its array, which the name is copied into
			levels = levels[:n+1]
		} else {
			levels = append(levels, textcoder.Level{})
		}
		tl := &levels[n]
		tl.Object, tl.Count = l.object, l.count
		if i < len(st.levels)-1 {
			tl.Count-- // the value of the next level, which is open, is not counted
		}
		if !l.object || l.last < l.first {
			tl.Name = nil
		} else if tl.Name = append(tl.Name[:0], st.names.get(l.last)...); tl.Name == nil {
			// the name "", which is a name, as a level without a name has none ( see textcoder.Level )
			tl.Name = []byte{}
		}
	}
	return levels
}

// beginValue is textcoder.BeginValue: the start of readValue, up to the value, which is not a name.
func beginValue(dec unsafe.Pointer) (int64, error) {
	d := &(*Decoder)(dec).d
	if !d.ready() {
		d.setUp()
	}
	d.invalidate()
	if err := d.peekErr; err != nil {
		d.peekErr = nil
		return 0, err
	}
	pos, err := d.beforeToken(false)
	if err != nil {
		return 0, err
	}
	if c := d.buf[pos]; c == '}' || c == ']' {
		// as readValue reports an end delimiter where a value is
		if l := d.st.last(); !(l.needValue() && c == '}') {
			if err := d.closeError(pos); err != nil {
				return 0, err
			}
		}
		return 0, d.charError(pos, placeStartOfValue, pointNext)
	}
	return d.base + int64(pos), nil
}

// endValue is textcoder.EndValue: the decoder is where done leaves it after a value, which its level counts.
func endValue(dec unsafe.Pointer, start, end int64, rest []byte) {
	d := &(*Decoder)(dec).d
	if rest == nil {
		// the start of the value may be before the buffer, which dropped it for the caller, which read more into
		// it ( see textcoder.FetchInput )
		d.done(int(start-d.base), int(end-d.base))
	} else {
		// the input after the value, which the reader gave the caller, in a buffer of the decoder, before which the
		// value starts
		if d.shared {
			d.buf, d.shared = nil, false
		}
		d.buf = append(d.buf[:0], rest...)
		d.base, d.pos, d.prevStart, d.prevEnd = end, 0, int(start-end), 0
	}
	d.st.last().count++
}

// skipToEnd is textcoder.SkipToEnd: the input is read as unmarshalPlaceAt reads it.
func skipToEnd(p *textcoder.DecoderAttachment, depth int, count int64) (int64, []byte, error) {
	dec := new(Decoder)
	attachDecoder(unsafe.Pointer(dec), p)
	d := &dec.d
	if d.st.outer != nil {
		d.st = *d.st.full()
	}
	for d.st.depth() != depth || d.st.levels[depth].count <= count {
		if _, err := dec.ReadToken(); err != nil {
			return 0, nil, err
		}
	}
	var rest []byte
	if !d.shared {
		rest = d.buf[d.pos:]
	}
	return d.base + int64(d.pos), rest, nil
}

// failedInput is the reader of a decoder which failed ( see textcoder.FailDecoder ): every read returns err, which
// the decoder returns as it is.
type failedInput struct{ err error }

func (r *failedInput) Read([]byte) (int, error) { return 0, r.err }

// unmarshalPlaceAt is textcoder.UnmarshalPlaceAt: the part of the input is read by a Decoder attached at its place,
// whose stack is made the one of the whole input, so that the levels of the place may end in the part.
func unmarshalPlaceAt(p *textcoder.DecoderAttachment, stop int64, read textcoder.Read) (string, error) {
	dec := new(Decoder)
	attachDecoder(unsafe.Pointer(dec), p)
	d := &dec.d
	if d.st.outer != nil {
		d.st = *d.st.full()
	}
	for {
		if stop >= 0 {
			if pos, err := d.beforeToken(true); err == nil && d.base+int64(pos) >= stop {
				return readPlace(dec, read)
			}
		}
		if _, err := dec.ReadToken(); err != nil {
			if err == io.EOF {
				return "", nil
			}
			return "", err
		}
		if d.st.depth() == 0 {
			return "", nil
		}
	}
}

// skipperOptions are the options of the skippers, by AllowDuplicateNames and AllowInvalidUTF8, made once so that a
// call makes no Options.
var skipperOptions = func() [2][2][]Options {
	var opts [2][2][]Options
	for i, dup := range []bool{false, true} {
		for j, utf8 := range []bool{false, true} {
			opts[i][j] = []Options{AllowDuplicateNames(dup), AllowInvalidUTF8(utf8)}
		}
	}
	return opts
}()

// takeSkipper is textcoder.TakeSkipper: an encoder of the pool, whose scanner doesn't write. Its options are set
// once: the fields of the scanner which they make are not set again by a call ( see encoder.init ).
func takeSkipper(allowDuplicateNames, allowInvalidUTF8 bool) unsafe.Pointer {
	var i, j int
	if allowDuplicateNames {
		i = 1
	}
	if allowInvalidUTF8 {
		j = 1
	}
	e := getEncoder(skipperOptions[i][j])
	e.vs.write, e.vs.spaced = false, false
	return unsafe.Pointer(e)
}

// releaseSkipper is textcoder.ReleaseSkipper.
func releaseSkipper(skipper unsafe.Pointer) { putEncoder((*encoder)(skipper)) }

// validValue is textcoder.ValidValue: v is checked as Value.IsValid checks it, by the scanner of the skipper, which
// doesn't write it and reports no place of an error: the count of the innermost level, which only the places tell,
// goes on.
func validValue(skipper unsafe.Pointer, v []byte) bool {
	end, err := (*encoder)(skipper).vs.scan(v, 0)
	return err == nil && end == len(v)
}

// readPlace reads the value at the start of the input of dec as read says, and returns the syntax error it meets,
// or else the pointer of the value.
func readPlace(dec *Decoder, read textcoder.Read) (string, error) {
	switch read {
	case textcoder.ReadNothing:
		return string(dec.d.st.pointer(pointNext)), nil
	case textcoder.ReadValue:
		if _, err := dec.ReadValue(); err != nil {
			return "", err
		}
	default:
		if _, err := dec.ReadToken(); err != nil {
			return "", err
		}
	}
	return string(dec.d.st.pointer(pointLast)), nil
}
