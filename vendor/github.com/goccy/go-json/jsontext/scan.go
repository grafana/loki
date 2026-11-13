package jsontext

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"slices"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/goccy/go-json/internal/jsonnum"
	"github.com/goccy/go-json/internal/jsonstring"
)

// valueScanner reads a JSON value, and writes it formatted if write is set. The levels of the value are pushed
// on the stack of the encoder or decoder while the value is read, so that the pointers of the errors are the
// ones of the stream.
//
// A decoder which reads its input by parts scans a value which the end of its buffer cuts up to there, and
// continues the scan from a resumePoint after it read more.
type valueScanner struct {
	st *stack
	// the options
	validUTF8            bool
	rejectDuplicateNames bool
	// lazyNames is set where the names of the objects aren't checked and the input, in, is the whole value: an
	// object keeps the position of its last name in the input, as -2 minus it in level.last, and the names are
	// added to the stack at an error only, for its pointer.
	lazyNames bool
	// base is the depth of the stack before the value being scanned, whose levels are the ones of the value after
	// it ( see addNames ).
	base     int
	in       []byte
	decoding bool // the messages and the offsets of the errors of a decoder
	final    bool // the end of the input is the end of the buffer
	// numEnd is 1 plus the end of the buffer, where the end of the input ended a number in the current resume of
	// a decoder, or 0: as encoding/json/jsontext, the decoder reads again for the rest of the value.
	numEnd int

	// the output, when the value is written
	write     bool
	out       []byte
	ws        whitespace
	esc       escapeFlags
	preserve  bool
	canonInts bool
	canonFlts bool
	reorder   bool

	run           int    // the start of the input which is not appended to out yet, which out has as it is
	spaced        bool   // the output has white space, which is compared with the input
	wsFrom, wsEnd int    // the white space of the input which skipSpace left pending
	lines         []byte // a line feed, the prefix and the indentation of the deepest line so far

	ro      reorderer // the members of the objects which are reordered, by level
	scratch []byte
	// open, if it is set, keeps the open objects of scanObjects, which scanFast otherwise keeps in its frame: a
	// scanner which is used again, as the ones of the functions of values are, doesn't clear them at every value.
	open *[64]openObject

	// the single spaces which scanObjects dropped from a written value and didn't append the run before yet: the
	// output is appended by them when the scan ends, without a call at each one
	ngaps int
	gaps  [32]int32 // the positions, which scanObjects takes for buffers below 2 GiB

	// keep is set by the functions which format a value: a written value whose output is all of its input, which
	// no part of the scan appended yet, isn't appended at its end, and kept reports it.
	keep, kept bool
}

// The places in a value where a scan continues.
const (
	atValue  uint8 = iota
	atNext         // after a value: a comma or an end delimiter
	atName         // an object name
	atColon        // the colon after an object name
	atOpened       // after a begin delimiter: a value, or the end delimiter
)

// resumePoint is where a scan which the end of the buffer cut continues: the place, and the progress in a
// string or number which was cut.
type resumePoint struct {
	at    uint8
	where uint8 // where the pointer of an error at the cut points
	pos   int   // the start of the token which was cut, or the position of the place
	tok   int   // the length of the part of the token which was taken, 0 if none
	lit   int   // the length of the part of a literal which the buffer cuts, for the position of an error there
	str   strFlags
	num   numState
	base  int // the depth of the stack before the value
}

// scanError is an error of the value at an offset, with the pointer where it was found.
type scanError struct {
	err error
	pos int
	ptr Pointer
}

func (s *valueScanner) fail(err error, pos int, where int) *scanError {
	if s.lazyNames {
		s.addNames()
	}
	return &scanError{err: err, pos: pos, ptr: s.st.pointer(where)}
}

// addNames adds the names which the objects of the value have as their positions in the input, outer objects
// first, for the pointer of an error: not of the levels around the value, whose names are the stack's own or, at
// the level an attached coder is in, of the coder around it.
func (s *valueScanner) addNames() {
	n := &s.st.names
	for k := s.base + 1; k < len(s.st.levels); k++ {
		l := &s.st.levels[k]
		if !l.object || l.last >= 0 {
			continue
		}
		l.first = len(n.ends) // after the names added to the objects around it
		if l.last < -1 {
			p := -2 - l.last
			size, f, _ := scanString(s.in[p:], strModeOf(s.validUTF8, false))
			l.last = l.first
			n.add(l.first, unquotedName(&s.scratch, s.in[p:p+size], f))
		}
	}
}

// errCut reports a scan which the end of the buffer cut, which continues from the resume point.
var errCut = &scanError{err: io.ErrUnexpectedEOF}

// scan reads the value at b[i:], and returns the position after it, and counts it in the innermost level of the
// stack. At an error, the stack is left as it was.
func (s *valueScanner) scan(b []byte, i int) (int, *scanError) {
	s.run, s.wsEnd, s.in, s.kept, s.base = i, -1, b, false, s.st.depth()
	if s.plain() {
		out, names := len(s.out), len(s.st.names.ends)
		var end int
		var ok bool
		if s.open != nil {
			end, ok = s.scanObjects(b, i, s.open)
		} else {
			end, ok = s.scanFast(b, i)
		}
		if ok {
			s.in = nil
			if s.write {
				s.finish(b, i, end)
			}
			return end, nil
		}
		s.out, s.run, s.ngaps = s.out[:out], i, 0
		s.st.names.truncate(names)
	}
	count := s.st.last().count
	rp := resumePoint{pos: i, base: s.st.depth()}
	end, err := s.resume(b, &rp)
	s.in = nil
	if err != nil {
		s.restore(rp.base, count)
	} else if s.write {
		s.finish(b, i, end)
	}
	return end, err
}

// finish appends the rest of the output of the written value b[i:end], or, if keep is set and the output is all of
// b, which the scan didn't append any part of, leaves it out and sets kept.
func (s *valueScanner) finish(b []byte, i, end int) {
	if s.ngaps > 0 {
		s.flushGaps(b)
	} else if s.keep && s.run == i && i == 0 && end == len(b) {
		s.kept = true
		return
	}
	s.flush(b, end)
}

// plain reports whether scanFast may read a value: its output, if it is written, is the input without white
// space, whose strings and numbers are kept as they are unless they are not canonical.
func (s *valueScanner) plain() bool {
	return !s.spaced && !s.reorder && !s.canonInts && !s.canonFlts && s.esc == 0
}

// scanFast reads the value at b[i:] as resume does, in one loop which keeps the open objects and arrays as the
// bits of a word, without the levels of the stack or a resume point. It gives up, and reports false, where the
// value is not valid, where a string must be rewritten, deeper than 64 levels, and where the buffer may cut
// the value: resume reads it again from the start, and reports the errors. The names of the objects are checked
// as resume checks them.
func (s *valueScanner) scanFast(b []byte, i int) (int, bool) {
	if i < len(b) && b[i] != '{' && b[i] != '[' && b[i] > ' ' {
		// a string, a number or a literal, which opens no object: without the array of the open objects, which
		// would be cleared for it.
		return s.scanObjects(b, i, nil)
	}
	// the open objects are in this frame, not in the one of scanObjects: the spilled registers of the loop are
	// then near its stack pointer, where the CPUs of AMD Zen 4 read them without a stall.
	var open [64]openObject
	return s.scanObjects(b, i, &open)
}

// openObject is an open object of valueScanner.scanFast: the index in names.ends of its first name, and the
// bits of its names ( jsonstring.NameBit ).
type openObject struct {
	first int
	bits  uint64
}

// scanObjects is scanFast, which keeps the open objects in open by their depth in the value.
//
//nolint:maintidx // one loop of gotos over the grammar, whose tokens are read in it without calls
func (s *valueScanner) scanObjects(b []byte, i int, open *[64]openObject) (int, bool) {
	if len(b) > math.MaxInt32 {
		return i, false // resume reads a buffer whose positions don't fit the gaps
	}
	var objects uint64 // the bit of each open level which is an object, the innermost the lowest
	ngaps := s.ngaps   // the single spaces in gaps, in a register while the value is scanned
	var ok bool
	depth := 0
	names := &s.st.names
	limit := min(64, maxDepth-s.st.depth()-s.st.base) // the levels which the value may open
	mode := strModeOf(s.validUTF8, false)
	var n int
	var f strFlags
	var err error

value:
	if i < len(b) && b[i] <= ' ' {
		goto space1
	}
space1Back:
	if i == len(b) {
		return i, false
	}
	switch c := b[i]; c {
	case '{', '[':
		if depth == limit {
			return i, false
		}
		objects <<= 1
		if c == '{' {
			objects |= 1
			open[depth] = openObject{first: len(names.ends)}
		}
		depth++
		i++
		goto opened
	case '"':
		// a string of plain characters costs one call; scanStringFrom takes the others from where it stops
		if n = plainEnd(b, i+1) - i; i+n < len(b) && b[i+n] == '"' {
			n++
		} else if n, f, err = scanStringFrom(b[i:], n, 0, mode); err != nil || s.write && f&strNonCanonical != 0 && !s.preserve {
			return i, false
		}
		i += n
	case 'n':
		// the literals are compared in the loop, without a call
		if len(b)-i < 4 || string(b[i:i+4]) != "null" {
			return i, false
		}
		i += 4
	case 't':
		if len(b)-i < 4 || string(b[i:i+4]) != "true" {
			return i, false
		}
		i += 4
	case 'f':
		if len(b)-i < 5 || string(b[i:i+5]) != "false" {
			return i, false
		}
		i += 5
	default:
		// a number of the common forms, an integer and its fraction, is read in the loop: the digits of the
		// integer by bytes, and the ones of the fraction 8 bytes at a time. numberEnd reads the others. A number
		// which the buffer ends may continue after it.
		j := i
		if c == '-' {
			j++
		}
		if j < len(b) && '1' <= b[j] && b[j] <= '9' {
			for j++; j < len(b) && '0' <= b[j] && b[j] <= '9'; {
				j++
			}
		} else if j < len(b) && b[j] == '0' {
			j++
		} else {
			j = i
		}
		if j > i && j+1 < len(b) && b[j] == '.' && '0' <= b[j+1] && b[j+1] <= '9' {
			j = jsonnum.ScanDigits(b, j+2)
		}
		if j > i && j < len(b) && b[j] != '.' && b[j] != 'e' && b[j] != 'E' {
			i = j
		} else if n = jsonnum.End(b[i:]); n < 0 {
			return i, false
		} else {
			i += n
		}
	}

next:
	if depth == 0 {
		s.st.last().count++
		s.ngaps = ngaps
		return i, true
	}
	if i < len(b) && b[i] <= ' ' {
		goto space2
	}
space2Back:
	if i == len(b) {
		return i, false
	}
	switch c := b[i]; {
	case c == ',':
		i++
		if objects&1 != 0 {
			goto name
		}
		goto value
	case c == '}' && objects&1 != 0, c == ']' && objects&1 == 0:
		i++
		goto closed
	}
	return i, false

closed:
	depth--
	if objects&1 != 0 {
		names.truncate(open[depth].first)
	}
	objects >>= 1
	goto next

opened:
	if i < len(b) && b[i] <= ' ' {
		goto space3
	}
space3Back:
	if i == len(b) {
		return i, false
	}
	if c := b[i]; c == '}' && objects&1 != 0 || c == ']' && objects&1 == 0 {
		i++
		goto closed
	}
	if objects&1 != 0 {
		goto name
	}
	goto value

name:
	if i < len(b) && b[i] <= ' ' {
		goto space4
	}
space4Back:
	if i == len(b) || b[i] != '"' {
		return i, false
	}
	f = 0
	if n = plainEnd(b, i+1) - i; i+n < len(b) && b[i+n] == '"' {
		n++
	} else if n, f, err = scanStringFrom(b[i:], n, 0, mode); err != nil || s.write && f&strNonCanonical != 0 && !s.preserve {
		return i, false
	}
	if s.rejectDuplicateNames {
		name := b[i+1 : i+n-1] // a name without escape sequences is the bytes between its quotes, without a call
		if f&(strEscaped|strInvalidUTF8) != 0 {
			name = unquotedName(&s.scratch, b[i:i+n], f)
		}
		k := depth - 1
		o := &open[k]
		if bit := jsonstring.NameBit(name); o.bits&bit == 0 {
			o.bits |= bit
		} else if names.has(o.first, name) {
			return i, false
		}
		names.add(o.first, name)
	}
	i += n
	if i < len(b) && b[i] <= ' ' {
		goto space5
	}
space5Back:
	if i == len(b) || b[i] != ':' {
		return i, false
	}
	i++
	goto value

	// the white space before the tokens, which the loop jumps out to, so that it doesn't lie between them
space1:
	if ngaps, ok = s.oneSpace(b, i, ngaps); ok {
		i++
	} else {
		s.ngaps = ngaps
		i = s.skipSpace(b, i)
		ngaps = s.ngaps
	}
	goto space1Back
space2:
	if ngaps, ok = s.oneSpace(b, i, ngaps); ok {
		i++
	} else {
		s.ngaps = ngaps
		i = s.skipSpace(b, i)
		ngaps = s.ngaps
	}
	goto space2Back
space3:
	if ngaps, ok = s.oneSpace(b, i, ngaps); ok {
		i++
	} else {
		s.ngaps = ngaps
		i = s.skipSpace(b, i)
		ngaps = s.ngaps
	}
	goto space3Back
space4:
	if ngaps, ok = s.oneSpace(b, i, ngaps); ok {
		i++
	} else {
		s.ngaps = ngaps
		i = s.skipSpace(b, i)
		ngaps = s.ngaps
	}
	goto space4Back
space5:
	if ngaps, ok = s.oneSpace(b, i, ngaps); ok {
		i++
	} else {
		s.ngaps = ngaps
		i = s.skipSpace(b, i)
		ngaps = s.ngaps
	}
	goto space5Back
}

// restore pops the levels of a value which was not read to its end.
func (s *valueScanner) restore(base int, count int64) {
	for s.st.depth() > base {
		s.st.pop()
	}
	s.st.last().count = count
	s.ro.reset()
}

// resume scans the value from the resume point. If the end of the buffer cuts it and the input continues, it
// returns errCut, with the resume point where the scan continues.
//
//nolint:maintidx // one loop of gotos over the grammar, whose state stays in its variables
func (s *valueScanner) resume(b []byte, rp *resumePoint) (int, *scanError) {
	var err error
	var n int
	i := rp.pos
	base := rp.base
	s.numEnd = 0
	ws := wsNone     // the white space which a written value has before the next token
	l := s.st.last() // the innermost level, which a push and a close change
	switch rp.at {
	case atNext:
		goto next
	case atName:
		goto name
	case atColon:
		goto colon
	case atOpened:
		goto opened
	}

value:
	if i = s.skip(b, i); i == len(b) {
		if s.st.depth() == base {
			return s.cut(atValue, i, pointNext, rp) // an empty value, which the encoder counts as the next one
		}
		return s.cut(atValue, i, pointAt, rp)
	}
	if s.spaced {
		s.space(b, i, ws)
	}
	switch c := b[i]; c {
	case '{', '[':
		if err := s.st.push(c == '{'); err != nil {
			return i, s.fail(err, i, pointNext)
		}
		l = s.st.last()
		if s.write && c == '{' && s.reorder {
			s.flush(b, i+1)
			s.ro.open(len(s.out))
		}
		i++
		goto opened
	case '"':
		var f strFlags
		if rp.tok > 0 && !s.final && plainEnd(b, i+rp.tok) == len(b) {
			// plain characters continue the string to the end of the buffer, as a slow reader gives them
			rp.tok = len(b) - i
			return s.cut(atValue, i, pointNext, rp)
		} else if rp.tok > 0 {
			n, f, err = scanStringFrom(b[i:], rp.tok, rp.str, strModeOf(s.validUTF8, !s.final))
			rp.tok = 0
		} else if n = plainEnd(b, i+1) - i; i+n < len(b) && b[i+n] == '"' {
			n++ // a string of plain characters costs one call
		} else {
			n, f, err = scanStringFrom(b[i:], n, 0, strModeOf(s.validUTF8, !s.final))
		}
		if err != nil {
			if err == io.ErrUnexpectedEOF && !s.final {
				rp.tok, rp.str = n, f
				return s.cut(atValue, i, pointNext, rp)
			}
			return i + n, s.fail(err, i+n, pointNext)
		}
		if s.write {
			s.writeString(b, i, n, f)
		}
		i += n
	case 'n', 't', 'f':
		lit := literalOf(Kind(c))
		n, err = scanLiteral(b[i:], lit)
		if err != nil {
			if err == io.ErrUnexpectedEOF && !s.final {
				rp.lit = n
				return s.cut(atValue, i, pointNext, rp)
			}
			return i + n, s.fail(err, i+n, pointNext)
		}
		i += n
	default:
		if kindOf(c) != KindNumber {
			if l := &s.st.levels[base]; s.decoding && base > 0 && l.count > 1 && (c == ']' && l.object || c == '}' && !l.object) {
				// encoding/json/jsontext reports an end delimiter where a value is, in a value which a decoder
				// reads in an object or array, as the error of the object or array where the value is, when it
				// is the end of the other kind.
				if l.object {
					return i, s.fail(invalidChar(b[i:], placeAfterObjectValue), i, pointHere)
				}
				return i, s.fail(invalidChar(b[i:], placeAfterArrayElement), i, pointHere)
			}
			return i, s.fail(invalidChar(b[i:], placeStartOfValue), i, pointNext)
		}
		var st numState
		if rp.tok > 0 {
			n, st, err = scanNumberFrom(b[i:], rp.tok, rp.num)
			rp.tok = 0
		} else {
			// a number of the common forms is read here as scanObjects reads it, and the others by numberEnd
			j := i
			if c == '-' {
				j++
			}
			if j < len(b) && '1' <= b[j] && b[j] <= '9' {
				for j++; j < len(b) && '0' <= b[j] && b[j] <= '9'; {
					j++
				}
			} else if j < len(b) && b[j] == '0' {
				j++
			} else {
				j = i
			}
			if j > i && j+1 < len(b) && b[j] == '.' && '0' <= b[j+1] && b[j+1] <= '9' {
				j = jsonnum.ScanDigits(b, j+2)
			}
			if j > i && j < len(b) && b[j] != '.' && b[j] != 'e' && b[j] != 'E' {
				n = j - i
			} else if n = jsonnum.End(b[i:]); n < 0 {
				n, st, err = scanNumberFrom(b[i:], 0, numState{})
			}
		}
		if i+n == len(b) && (err == nil || err == io.ErrUnexpectedEOF) {
			if !s.final {
				rp.tok, rp.num = n, st
				return s.cut(atValue, i, pointNext, rp)
			}
			if err == nil && s.decoding {
				s.numEnd = len(b) + 1
			}
		}
		if err != nil {
			// a decoder reports a number which the input cuts at its start.
			if err != io.ErrUnexpectedEOF {
				// the position of the invalid character
			} else if s.decoding {
				n = 0
			} else {
				n = st.done
			}
			return i + n, s.fail(err, i+n, pointNext)
		}
		if s.write && !s.numberKept(b[i:i+n]) {
			s.flush(b, i)
			s.out = appendCanonicalNumber(s.out, b[i:i+n])
			s.run = i + n
		}
		i += n
	}
	l.count++

next:
	if s.st.depth() == base {
		return i, nil
	}
	if i = s.skip(b, i); i == len(b) {
		return s.cut(atNext, i, pointAt, rp)
	}
	if l.object {
		switch b[i] {
		case ',':
			if s.spaced && s.wsEnd == i {
				s.space(b, i, wsNone)
			}
			if s.write && s.reorder {
				s.flush(b, i)
				s.ro.endMember(len(s.out))
			}
			i++
			ws = wsElem
			goto name
		case '}':
			s.closeLevel(b, i, false)
			l = s.st.last()
			i++
			goto next
		}
		return i, s.fail(invalidChar(b[i:], placeAfterObjectValue), i, pointAt)
	}
	switch b[i] {
	case ',':
		if s.spaced && s.wsEnd == i {
			s.space(b, i, wsNone)
		}
		i++
		ws = wsElem
		goto value
	case ']':
		s.closeLevel(b, i, false)
		l = s.st.last()
		i++
		goto next
	}
	if s.decoding {
		return i, s.fail(invalidChar(b[i:], placeAfterArrayElement), i, pointAt)
	}
	return i, s.fail(invalidChar(b[i:], placeAfterArrayValue), i, pointAt)

opened:
	if i = s.skip(b, i); i == len(b) {
		return s.cut(atOpened, i, pointAt, rp)
	}
	if b[i] == '}' && l.object || b[i] == ']' && !l.object {
		s.closeLevel(b, i, true)
		l = s.st.last()
		i++
		goto next
	} else if ws = wsOpen; l.object {
		goto name
	}
	goto value

name:
	if i = s.skip(b, i); i == len(b) {
		return s.cut(atName, i, pointAt, rp)
	}
	if s.spaced {
		s.space(b, i, ws)
	}
	if b[i] != '"' {
		return i, s.fail(invalidChar(b[i:], placeStartOfString), i, pointAt)
	}
	{
		var f strFlags
		if rp.tok > 0 && !s.final && plainEnd(b, i+rp.tok) == len(b) {
			// plain characters continue the string to the end of the buffer, as a slow reader gives them
			rp.tok = len(b) - i
			return s.cut(atName, i, pointAt, rp)
		} else if rp.tok > 0 {
			n, f, err = scanStringFrom(b[i:], rp.tok, rp.str, strModeOf(s.validUTF8, !s.final))
			rp.tok = 0
		} else if n = plainEnd(b, i+1) - i; i+n < len(b) && b[i+n] == '"' {
			n++ // a string of plain characters costs one call
		} else {
			n, f, err = scanStringFrom(b[i:], n, 0, strModeOf(s.validUTF8, !s.final))
		}
		if err != nil {
			if err == io.ErrUnexpectedEOF && !s.final {
				rp.tok, rp.str = n, f
				return s.cut(atName, i, pointAt, rp)
			}
			return i + n, s.fail(err, i+n, pointAt)
		}
		name := b[i : i+n]
		if s.lazyNames {
			l.last, l.named = -2-i, l.count+1
		} else {
			unquoted := name[1 : len(name)-1] // a name without escape sequences, without a call
			if f&(strEscaped|strInvalidUTF8) != 0 {
				unquoted = unquotedName(&s.scratch, name, f)
			}
			if !s.st.insertName(unquoted, s.rejectDuplicateNames) {
				return i, &scanError{err: ErrDuplicateName, pos: i, ptr: s.st.namePointer(unquoted)}
			}
		}
		l.count++
		if s.write {
			if s.reorder {
				s.flush(b, i)
				s.ro.startMember(len(s.out), unquotedName(&s.scratch, name, f))
			}
			s.writeString(b, i, n, f)
		}
		i += n
	}

colon:
	if i = s.skip(b, i); i == len(b) {
		return s.cut(atColon, i, pointAt, rp)
	}
	if b[i] != ':' {
		return i, s.fail(invalidChar(b[i:], placeAfterObjectName), i, pointAt)
	}
	if s.spaced && s.wsEnd == i {
		s.space(b, i, wsNone)
	}
	i++
	ws = wsColon
	goto value
}

// cut is the end of the scan at the end of the buffer, at the place at, where the token or the place starts at
// i: the input continues, or the value is not complete.
func (s *valueScanner) cut(at uint8, i int, where int, rp *resumePoint) (int, *scanError) {
	if s.final && i+1 != s.numEnd {
		pos := i + rp.tok
		if rp.tok == 0 {
			pos = i
		}
		return pos, s.fail(io.ErrUnexpectedEOF, pos, where)
	}
	rp.at, rp.pos, rp.where = at, i, uint8(where)
	return i, errCut
}

// The output is the input where they are the same: the run of the input from s.run, which is not appended yet,
// is appended where the output differs, before what the output has instead.

// flush appends the run of the input up to i.
func (s *valueScanner) flush(b []byte, i int) {
	if s.run < i {
		s.out = append(s.out, b[s.run:i]...)
	}
	s.run = i
}

// flushGaps appends the runs of the input before the single spaces which scanObjects dropped, short ones by a word.
func (s *valueScanner) flushGaps(b []byte) {
	out, run := s.out, s.run // in registers, not in the scanner, while the runs are appended
	for _, g32 := range s.gaps[:s.ngaps] {
		g := int(g32)
		if n := g - run; n <= 8 && run+8 <= len(b) && cap(out)-len(out) >= 8 {
			k := len(out)
			binary.LittleEndian.PutUint64(out[k:k+8], binary.LittleEndian.Uint64(b[run:]))
			out = out[:k+n]
		} else {
			out = append(out, b[run:g]...)
		}
		run = g + 1
	}
	s.out, s.run, s.ngaps = out, run, 0
}

// oneSpace reports whether b[i] is a single space before a token, the most common white space, which scanObjects
// skips without a call: a written value keeps it in gaps, and so does one which isn't, without counting it, as
// long as there is room. k is the number of the gaps, which scanObjects keeps in a register while it scans, and
// it returns the new one. It is inlined.
func (s *valueScanner) oneSpace(b []byte, i, k int) (int, bool) {
	if k < len(s.gaps) && i+1 < len(b) && b[i] == ' ' && b[i+1] > ' ' {
		s.gaps[k] = int32(i)
		return k + int(bit(s.write)), true
	}
	return k, false
}

// bit is 1 for true and 0 for false.
func bit(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

// skip skips the white space at b[i:], which a written value doesn't have. It is inlined where there is none.
func (s *valueScanner) skip(b []byte, i int) int {
	if i < len(b) && b[i] <= ' ' {
		return s.skipSpace(b, i)
	}
	return i
}

// skipSpace skips the white space at b[i:]. A value written without white space drops it; one written with white
// space keeps it pending, for space to compare with the white space which the output has there.
func (s *valueScanner) skipSpace(b []byte, i int) int {
	// the loop of skipSpace, which is not called: it runs at every token of a value with white space
	end := i
	for end < len(b) && isSpace(b[end]) {
		end++
		if b[end-1] == '\n' && end < len(b) && (b[end] == ' ' || b[end] == '\t') {
			end = skipIndent(b, end)
		}
	}
	switch {
	case !s.write:
	case s.spaced:
		s.wsFrom, s.wsEnd = i, end
	default:
		if s.ngaps > 0 {
			s.flushGaps(b)
		}
		// a short run, as a token between spaces is, is copied by a word without a call of memmove.
		if n := i - s.run; n <= 8 && s.run+8 <= len(b) && cap(s.out)-len(s.out) >= 8 {
			k := len(s.out)
			binary.LittleEndian.PutUint64(s.out[k:k+8], binary.LittleEndian.Uint64(b[s.run:]))
			s.out = s.out[:k+n]
		} else {
			s.flush(b, i)
		}
		s.run = end
	}
	return end
}

// The white space which a value written with white space has before a token.
const (
	wsNone  = iota // none
	wsColon        // after a colon
	wsElem         // before an element or a member after the first one, after its comma
	wsOpen         // before the first element or member
	wsClose        // before the end of an object or array which is not empty
)

// space writes the white space of the kind ws before the token at b[i]: the white space of the input before it,
// which skipSpace left pending, is kept if it is the same.
func (s *valueScanner) space(b []byte, i int, ws int) {
	from := i
	if s.wsEnd == i {
		from = s.wsFrom
	}
	s.wsEnd = -1 // the pending white space is taken

	var sp bool
	var line []byte
	switch ws {
	case wsColon:
		sp = s.ws.colon
	case wsElem, wsOpen, wsClose:
		sp = ws == wsElem && s.ws.comma
		if s.ws.multiline {
			depth := s.st.depth()
			if ws == wsClose {
				depth--
			}
			line = s.line(depth)
		}
	}
	got := b[from:i]
	same := true
	if sp {
		same = len(got) > 0 && got[0] == ' '
		if same {
			got = got[1:]
		}
	}
	if !same || string(got) != string(line) {
		s.flush(b, from)
		if sp {
			s.out = append(s.out, ' ')
		}
		s.out = append(s.out, line...)
		s.run = i
	}
}

// line returns a line feed, the prefix and the indentation of the depth, sliced from a buffer which holds the
// deepest line so far.
func (s *valueScanner) line(depth int) []byte {
	if len(s.lines) == 0 {
		s.lines = append(append(s.lines, '\n'), s.ws.prefix...)
	}
	n := 1 + len(s.ws.prefix) + depth*len(s.ws.indent)
	for len(s.lines) < n {
		s.lines = append(s.lines, s.ws.indent...)
	}
	return s.lines[:n]
}

// writeString writes the string b[i:i+n], which scanString took with the flags f. It is inlined where the
// string is written as it is, without the options which escape more.
func (s *valueScanner) writeString(b []byte, i, n int, f strFlags) {
	if f&strNonCanonical != 0 && !s.preserve || s.esc != 0 {
		s.rewriteString(b, i, n, f)
	}
}

func (s *valueScanner) rewriteString(b []byte, i, n int, f strFlags) {
	if !rawStringKept(b[i:i+n], f, s.esc, s.preserve) {
		s.flush(b, i)
		s.out = appendRawString(s.out, b[i:i+n], f, s.esc, s.preserve)
		s.run = i + n
	}
}

// closeLevel pops the innermost object or array, whose end delimiter is at b[i].
func (s *valueScanner) closeLevel(b []byte, i int, empty bool) {
	if s.write {
		if b[i] == '}' && s.reorder {
			// the members end before the white space which is pending
			end := i
			if s.spaced && s.wsEnd == i {
				end = s.wsFrom
			}
			s.flush(b, end)
			s.ro.endMember(len(s.out))
			s.out = s.ro.close(s.out)
		}
		if s.spaced {
			if empty {
				if s.wsEnd == i {
					s.space(b, i, wsNone)
				}
			} else {
				s.space(b, i, wsClose)
			}
		}
	}
	s.st.pop()
}

// appendComma appends a comma, and the space after it if the options want it.
func (w *whitespace) appendComma(b []byte) []byte {
	if w.comma {
		return append(b, ',', ' ')
	}
	return append(b, ',')
}

// appendLine appends a line feed, the prefix and the indentation of the depth.
func (w *whitespace) appendLine(b []byte, depth int) []byte {
	b = append(b, '\n')
	b = append(b, w.prefix...)
	for range depth {
		b = append(b, w.indent...)
	}
	return b
}

// numberKept reports whether the options keep the number b as it is. -0 is 0 under either option, and an
// integer of at most 15 digits is exact as a double, which is formatted by its digits.
func (s *valueScanner) numberKept(b []byte) bool {
	if !s.canonInts && !s.canonFlts {
		return true
	}
	if string(b) == "-0" {
		return false
	}
	if isInteger(b) {
		return !s.canonInts || len(b) <= 15
	}
	return !s.canonFlts
}

// reorderer holds the members of the objects which ReorderRawObjects reorders, the innermost object last: the
// output of each member, from the start of its name to the end of its value, and its unquoted name. Its arrays
// are used again by the objects after.
type reorderer struct {
	objects []object // the open objects
	members []member // their members, back to back
	names   []byte   // the names of the members, back to back
	sorted  []member
	buf     []byte
}

type object struct {
	start   int // the start of the output of its members
	members int // the index of its first member
}

type member struct {
	name, nameEnd int // the name, in names
	start, end    int // the output
}

func (r *reorderer) reset() {
	r.objects, r.members, r.names = r.objects[:0], r.members[:0], r.names[:0]
}

func (r *reorderer) open(pos int) {
	r.objects = append(r.objects, object{start: pos, members: len(r.members)})
}

func (r *reorderer) startMember(pos int, name []byte) {
	r.names = append(r.names, name...)
	r.members = append(r.members, member{name: len(r.names) - len(name), nameEnd: len(r.names), start: pos, end: -1})
}

func (r *reorderer) endMember(pos int) {
	if k := len(r.members); k > r.objects[len(r.objects)-1].members && r.members[k-1].end < 0 {
		r.members[k-1].end = pos
	}
}

// close sorts the members of the innermost object in out, which are separated by commas and white space, by
// their names, and drops the object.
func (r *reorderer) close(out []byte) []byte {
	o := r.objects[len(r.objects)-1]
	members := r.members[o.members:]
	defer func() {
		r.objects = r.objects[:len(r.objects)-1]
		if len(members) > 0 {
			r.names = r.names[:members[0].name]
		}
		r.members = r.members[:o.members]
	}()
	// members of the same name are ordered by their output, from the name on, in the same order.
	compare := func(a, b member) int {
		if c := compareNames(r.names[a.name:a.nameEnd], r.names[b.name:b.nameEnd]); c != 0 {
			return c
		}
		return compareNames(bytes.TrimLeft(out[a.start:a.end], ", \n\r\t"), bytes.TrimLeft(out[b.start:b.end], ", \n\r\t"))
	}
	if len(members) < 2 || slices.IsSortedFunc(members, compare) {
		return out
	}
	r.sorted = append(r.sorted[:0], members...)
	slices.SortFunc(r.sorted, compare)
	// the separator of the members, from the end of the first one to the start of the second one.
	sep := out[members[0].end:members[1].start]
	buf := append(r.buf[:0], out[o.start:members[0].start]...)
	for i, x := range r.sorted {
		if i > 0 {
			buf = append(buf, sep...)
		}
		buf = append(buf, out[x.start:x.end]...)
	}
	buf = append(buf, out[members[len(members)-1].end:]...)
	r.buf = buf
	return append(out[:o.start], buf...)
}

// compareNames orders names by their UTF-16 code units, as RFC 8785, section 3.2.3 does. A byte of invalid UTF-8
// is U+FFFD, which is ordered by its bytes where the other is U+FFFD too, as encoding/json/jsontext orders it.
func compareNames(x, y []byte) int {
	for len(x) > 0 && len(y) > 0 {
		rx, nx := utf8.DecodeRune(x)
		ry, ny := utf8.DecodeRune(y)
		if rx != ry {
			ux, uy := utf16Units(rx), utf16Units(ry)
			if ux[0] != uy[0] {
				return int(ux[0]) - int(uy[0])
			}
			return int(ux[1]) - int(uy[1])
		}
		if rx == utf8.RuneError && (nx == 1 || ny == 1) && x[0] != y[0] {
			return int(x[0]) - int(y[0])
		}
		x, y = x[nx:], y[ny:]
	}
	return len(x) - len(y)
}

// utf16Units are the UTF-16 code units of r, the second one 0 if it has one.
func utf16Units(r rune) [2]uint16 {
	if r1, r2 := utf16.EncodeRune(r); r1 != utf8.RuneError {
		return [2]uint16{uint16(r1), uint16(r2)}
	}
	return [2]uint16{uint16(r), 0}
}
