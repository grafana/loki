package jsontext

import (
	"unsafe"

	"github.com/goccy/go-json/internal/options"
	"github.com/goccy/go-json/internal/textcoder"
)

func init() {
	textcoder.Attach = attachEncoder
	textcoder.Detach = detachEncoder
	textcoder.Position = encoderPosition
	textcoder.Place = encoderPlace
	textcoder.Configure = configureEncoder
	textcoder.Fail = func(enc any, err error) {
		if e := &enc.(*Encoder).e; e.attached && e.failErr == nil {
			e.failErr = err
		}
	}
	textcoder.Invalidate = func(enc any) {
		e := &enc.(*Encoder).e
		e.invalid = true
		e.vs.st = nil
	}
	textcoder.UnmarshalPlace = unmarshalPlace
	textcoder.TakeSkipper = takeSkipper
	textcoder.ReleaseSkipper = releaseSkipper
	textcoder.ValidValue = validValue
}

// encoderPosition returns the levels of enc and the offset of its next value.
func encoderPosition(enc any, levels []textcoder.Level) ([]textcoder.Level, int64) {
	e := &enc.(*Encoder).e
	if !e.ready() {
		e.setUp()
	}
	st := &e.st
	if st.outer != nil {
		st = st.full()
	}
	for i := range st.levels {
		l := &st.levels[i]
		tl := textcoder.Level{Object: l.object, Count: l.count}
		if i < len(st.levels)-1 {
			tl.Count-- // the value of the next level, which is open, is not counted
		}
		if l.object && l.last >= l.first {
			if tl.Name = st.names.get(l.last); tl.Name == nil {
				// the name "", which is a name, as a level without a name has none ( see textcoder.Level )
				tl.Name = []byte{}
			}
		}
		levels = append(levels, tl)
	}
	_, _, offset, _, _ := encoderPlace(enc)
	return levels, offset
}

// encoderPlace returns the innermost level of enc, whether it is the top level, the offset of its next value,
// without the levels around it, the Outer of its attach, and its depth in the whole output.
func encoderPlace(enc any) (textcoder.Level, bool, int64, textcoder.Outer, int) {
	e := &enc.(*Encoder).e
	if !e.ready() {
		e.setUp()
	}
	l := e.st.last()
	k := KindNull
	if l.needName() {
		k = KindString
	}
	inner := textcoder.Level{Object: l.object, Count: l.count}
	return inner, e.st.depth() == 0, e.base + int64(len(e.buf)) + int64(e.delimLen(k)), e.st.outer, e.st.depth() + e.st.base
}

// configureEncoder makes enc write by the options opts until the returned function is called, with the strings of
// the values as they are: they were escaped by the options of the call which wrote them.
func configureEncoder(enc any, opts options.Options) func() {
	e := &enc.(*Encoder).e
	saved := e.cfg
	e.cfg = config{}
	opts.ApplyTo(&e.cfg)
	e.cfg.Value = e.cfg.Value&^(escapeForHTML|escapeForJS) | preserveRawStrings
	e.derive()
	return func() {
		e.cfg = saved
		e.derive()
	}
}

// attachEncoder makes the Encoder enc write after out, at the place whose innermost level is inner ( see
// textcoder.Attach ).
func attachEncoder(enc unsafe.Pointer, p *textcoder.Attachment) {
	e := &(*Encoder)(enc).e
	st := &e.st
	e.closedPast, e.failErr = false, nil
	if p.Same && e.attached && e.ready() && cap(st.levels) >= 2 {
		// attached again with the same options, as most calls of the methods are: the state of the grammar and of
		// the scanner is reset, as init resets it, without the rest. The levels are written as reset and push
		// write them, the top level and the place, which no name of an object is given for.
		e.invalid = false
		if p.Top {
			st.levels = st.levels[:1]
			st.levels[0] = level{count: p.Count}
		} else {
			st.levels = st.levels[:2]
			st.levels[0] = level{count: 1}
			st.levels[1] = level{object: p.Object, count: p.Count, last: -1}
		}
		if n := &st.names; len(n.ends) != 0 || len(n.indexes) != 0 {
			n.reset()
		}
		vs := &e.vs
		vs.write, vs.keep = true, false
		vs.run = 0
		vs.wsFrom, vs.wsEnd = 0, 0
		if vs.in != nil || vs.out != nil {
			vs.in, vs.out = nil, nil
		}
	} else {
		e.cfg = *p.Opts
		e.w, e.attached = nil, false // which init doesn't derive again for
		e.init(nil)
		e.attached = true
		// compact, as derive makes it for an attached encoder: init derived it before the encoder was attached.
		e.vs.ws, e.vs.spaced = whitespace{}, false
		if !p.Top {
			_ = st.push(p.Object)
		}
		// the names of an object, which the value written at its place can't have, are not given.
		st.last().count = p.Count
		st.outer = p.Outer
	}
	st.outerDepth, st.outerCount = len(st.levels)-1, p.Count
	st.base = p.OuterDepth - st.outerDepth
	e.setOutput(p.Out, p.Base)
	p.Skip, p.Depth = 0, len(st.levels)-1
	if len(p.Out) == 0 && !p.Top {
		p.Skip = e.skipDelim()
	}
}

// setOutput sets the output of an attached encoder to out, at the offset base of the whole output. The pointers are
// written only if they change, as a write of a pointer costs a write barrier while the GC marks: the output is
// mostly the same array, whose length is set by a slice of itself.
func (e *encoder) setOutput(out []byte, base int64) {
	if unsafe.SliceData(e.buf) == unsafe.SliceData(out) && cap(e.buf) == cap(out) {
		e.buf = e.buf[:len(out)]
	} else {
		e.buf = out
	}
	e.base, e.maxValue = base, 0
}

// skipDelim returns the length of the delimiter which the attached encoder writes before the value at the start
// of its output, which isn't a part of it ( see textcoder.Attach ), and moves the offset of the output before it.
func (e *encoder) skipDelim() int {
	k := KindNull
	if e.st.last().needName() {
		k = KindString
	}
	skip := e.delimLen(k)
	e.base -= int64(skip)
	return skip
}

// detachEncoder sets the output of enc and the place where it is in p.
func detachEncoder(enc unsafe.Pointer, p *textcoder.Attachment) {
	e := &(*Encoder)(enc).e
	p.Out, p.Depth, p.Count, p.Failed = e.buf, e.st.depth(), e.st.last().count, e.failErr
	e.failErr = nil
	if e.closedPast {
		p.Depth-- // the level which the value was asked to end, as encoding/json/jsontext ends it
	}
}
