package decoder

import (
	"math/bits"
	"unicode/utf8"
	"unsafe"
)

// A value of a stream which v2 skips, as the value of a name of no field, and which the buffer doesn't hold whole, is
// skipped with V2AllowDuplicateNames and V2AllowInvalidUTF8 as v1 skips it ( see Stream.skipNested ): by the grammar,
// read as it goes past what has been read, each byte once, by skipFastPath, which is skipFast with the levels of the
// value followed. An error of encoding/json/v2 has its place, the path to it ( see V2Call.Path ), which a decoder of
// v1 doesn't know: the levels of the value which are open where the skip stops are added to the path for it, with
// their counts and their last names, which are copied when a read moves the buffer ( see Stream.copyNames ), and the
// error is found again from the end of the token before it, which the buffer keeps while the skip reads.

// skipPath is what skipFastPath follows of the levels of the value which it skips, the levels 0 up to the one where
// it stops, for the place of an error: for an array, the commas after its elements, and for an object, the number
// of its names, and the total offset of the quote of the last one in the buffer, or copiedName once the name was
// copied into names, when a read moved the buffer. level and objects are the levels which are open while the stream
// reads, whose names are copied then; active is set while a value is skipped.
type skipPath struct {
	counts, quotes [skipLevels]int64
	names          [skipLevels][]byte
	level          int64
	objects        uint64
	active         bool
}

// copiedName is the place of the quote of the last name of a level of a skipPath whose name is copied into names.
const copiedName = -1

// skipFastPath is skipFast for a value of a stream whose levels tr follows: an open level has no comma and no name, a
// comma of an array and a name which is read are counted, and the place of the name, whose quote is at the total
// offset base in buf, kept. A name which skipFast leaves to its caller is counted by the caller. The numbers which
// are not integers are read here, as most numbers of a value are valid and whole in what has been read.
//
//nolint:maintidx // one loop of gotos without calls, whose state stays in the registers
func skipFastPath(
	buf []byte, cursor, level int64, objects uint64, resume skipResume, maxLevel int64, tr *skipPath, base int64,
) (int64, int64, uint64, skipResume, skipEvent) {
	length := int64(len(buf))
	// the bytes are read without the checks of the bounds: every loop stops at the nul byte at the end of the
	// buffer, and a literal is compared byte by byte up to the first byte which differs
	b := (*sliceHeader)(unsafe.Pointer(&buf)).data
	// c is the byte at cursor after the white spaces are skipped: a byte above ' ' isn't one, so that the table
	// of the white spaces is read only for the bytes which may be
	var c byte
	switch resume {
	case resumeAfterValue:
		goto afterValue
	case resumeAfterKey:
		goto afterKey
	}
value:
	if c = char(b, cursor); c <= ' ' {
		for isWhiteSpace[c] {
			cursor++
			c = char(b, cursor)
		}
	}
	// a string, which most values are, before the switch of the others
	if c == '"' {
		// a string of plain bytes up to its quote, eight bytes at a time
		for c := cursor + 1; c+8 <= length; c += 8 {
			if special := keyEndBytes(load64(buf, c)); special != 0 {
				if c += int64(bits.TrailingZeros64(special) / 8); char(b, c) == '"' {
					cursor = c + 1
					goto afterValue
				}
				break
			}
		}
		return cursor, level, objects, resumeAfterValue, skipAString
	}
	switch c {
	case '{', '[':
		if level+1 > maxLevel {
			return cursor, level, objects, resumeValue, skipDeep
		}
		if c == '{' {
			objects |= 1 << uint(level)
		} else {
			objects &^= 1 << uint(level)
		}
		// the mask of the index, below skipLevels, tells the compiler that it is in the bounds
		tr.counts[level&(skipLevels-1)] = 0
		level++
		cursor++
		open := c
		if c = char(b, cursor); c <= ' ' {
			for isWhiteSpace[c] {
				cursor++
				c = char(b, cursor)
			}
		}
		if open == '{' {
			if c == '}' {
				cursor++
				goto closed
			}
			goto key
		}
		if c == ']' {
			cursor++
			goto closed
		}
		goto value
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		// a number, read here by the grammar of the numbers: one which is not valid is left to the caller from its
		// start, which reports its error, as is one which the end of what has been read cuts after its sign, its
		// point or its exponent, which the caller reads whole. One which it cuts after a digit ends there: the nul
		// byte is not a byte after a value, where the caller reads on ( see Stream.skipResume ).
		start := cursor
		if c == '-' {
			cursor++
		}
		// a byte minus '0' is 9 at most for a digit only, and minus '1' 8 at most for a digit but 0, as the bytes
		// below them wrap around
		switch d := char(b, cursor); {
		case d == '0':
			cursor++
		case d-'1' <= 8:
			cursor++
			for char(b, cursor)-'0' <= 9 {
				cursor++
			}
		default:
			return start, level, objects, resumeAfterValue, skipANumber
		}
		if char(b, cursor) == '.' {
			if cursor++; char(b, cursor)-'0' > 9 {
				return start, level, objects, resumeAfterValue, skipANumber
			}
			for char(b, cursor)-'0' <= 9 {
				cursor++
			}
		}
		if d := char(b, cursor); d == 'e' || d == 'E' {
			if cursor++; char(b, cursor) == '+' || char(b, cursor) == '-' {
				cursor++
			}
			if char(b, cursor)-'0' > 9 {
				return start, level, objects, resumeAfterValue, skipANumber
			}
			for char(b, cursor)-'0' <= 9 {
				cursor++
			}
		}
	case 't':
		if char(b, cursor+1) != 'r' || char(b, cursor+2) != 'u' || char(b, cursor+3) != 'e' {
			return cursor, level, objects, resumeValue, skipInvalid
		}
		cursor += 4
	case 'f':
		if char(b, cursor+1) != 'a' || char(b, cursor+2) != 'l' || char(b, cursor+3) != 's' || char(b, cursor+4) != 'e' {
			return cursor, level, objects, resumeValue, skipInvalid
		}
		cursor += 5
	case 'n':
		if char(b, cursor+1) != 'u' || char(b, cursor+2) != 'l' || char(b, cursor+3) != 'l' {
			return cursor, level, objects, resumeValue, skipInvalid
		}
		cursor += 4
	default:
		return cursor, level, objects, resumeValue, skipInvalid
	}
afterValue:
	// a value here is in an object or an array: the value which skipFast starts at is one of them, and the end of
	// the outermost is returned at closed
	if c = char(b, cursor); c <= ' ' {
		for isWhiteSpace[c] {
			cursor++
			c = char(b, cursor)
		}
	}
	if objects&(1<<uint(level-1)) != 0 {
		switch c {
		case ',':
			cursor++
			if c = char(b, cursor); c <= ' ' {
				for isWhiteSpace[c] {
					cursor++
					c = char(b, cursor)
				}
			}
			goto key
		case '}':
			cursor++
			goto closed
		}
		return cursor, level, objects, resumeAfterValue, skipInvalid
	}
	switch c {
	case ',':
		cursor++
		tr.counts[(level-1)&(skipLevels-1)]++
		goto value
	case ']':
		cursor++
		goto closed
	}
	return cursor, level, objects, resumeAfterValue, skipInvalid
key:
	if c != '"' {
		return cursor, level, objects, resumeKey, skipInvalid
	}
	// a key of plain bytes, as the strings of the values
	for c := cursor + 1; c+8 <= length; c += 8 {
		if special := keyEndBytes(load64(buf, c)); special != 0 {
			if c += int64(bits.TrailingZeros64(special) / 8); char(b, c) == '"' {
				tr.counts[(level-1)&(skipLevels-1)]++
				tr.quotes[(level-1)&(skipLevels-1)] = base + cursor
				cursor = c + 1
				goto afterKey
			}
			break
		}
	}
	return cursor, level, objects, resumeAfterKey, skipAString
afterKey:
	if c = char(b, cursor); c <= ' ' {
		for isWhiteSpace[c] {
			cursor++
			c = char(b, cursor)
		}
	}
	if c != ':' {
		return cursor, level, objects, resumeAfterKey, skipInvalid
	}
	cursor++
	// the single space after the colon, which most objects have, is skipped before the loop over the others
	if char(b, cursor) == ' ' {
		cursor++
	}
	goto value
closed:
	// the end of an object or an array, which cursor is after
	if level--; level == 0 {
		return cursor, level, objects, resumeAfterValue, skipDone
	}
	goto afterValue
}

// skipLongV2 skips the object or the array at the cursor, nested in depth objects and arrays, which the buffer
// doesn't hold whole, with V2AllowDuplicateNames and V2AllowInvalidUTF8, as skipNested skips it, its levels followed
// for the place of an error ( see skipPathError ): the levels which are open when the stream reads are set in its
// state, which a read moves the buffer for ( see Stream.grow ).
func (s *Stream) skipLongV2(depth int64) error {
	tr := &s.ctx.v2.skip
	tr.active = true
	maxLevel := skipMaxLevel(depth)
	cursor, level, objects, resume := s.cursor, int64(0), uint64(0), resumeValue
	for {
		var ev skipEvent
		cursor, level, objects, resume, ev = skipFastPath(s.buf[:s.length+1], cursor, level, objects, resume, maxLevel, tr, s.offset)
		s.cursor = cursor
		switch ev {
		case skipDone:
			tr.active = false
			return nil
		case skipAString, skipANumber:
			// a string or a number which skipFastPath leaves to its caller, read whole if what has been read cuts it
			at := s.offset + s.cursor
			tr.level, tr.objects = level, objects
			err := s.skipScalar()
			if err != nil {
				return s.skipPathError(err, level, objects, resume, at)
			}
			if ev == skipAString && resume == resumeAfterKey {
				// a name, which is counted once it is read
				tr.counts[level-1]++
				tr.quotes[level-1] = at
			}
		case skipDeep:
			if err := s.skipDeepV2(depth, level, objects); err != nil {
				return err
			}
			resume = resumeAfterValue
		default:
			tr.level, tr.objects = level, objects
			next, nextResume, err := s.skipResume(level, objects, resume)
			if err != nil {
				// the error of the byte where skipResume stopped, or of the integer which it read whole from its
				// start
				return s.skipPathError(err, level, objects, resume, s.offset+s.cursor)
			}
			if resume == resumeKey && nextResume == resumeAfterKey {
				// a name which skipResume read, which is counted
				tr.counts[level-1]++
				tr.quotes[level-1] = s.offset + s.quoteBefore(s.cursor)
			}
			if level, resume = next, nextResume; level == 0 {
				tr.active = false
				return nil
			}
		}
		cursor = s.cursor
	}
}

// skipKeepFrom returns the position in the buffer from which a read keeps the bytes while skipLongV2 reads: the last
// one before the cursor which is not white space, before a number which ends at the cursor, which skipResume reads
// whole from its start, as a read may go on with it. It is the end of the token before the value or the name which
// is read, or the delimiter after which it is, which an error is found again from ( see skipPathError ).
func (s *Stream) skipKeepFrom() int64 {
	at := s.cursor
	for at > 0 && isNumberByte(s.buf[at-1]) {
		at--
	}
	return s.tokenBefore(at)
}

// tokenBefore returns the position of the last byte before at in the buffer which is not white space, or 0 if there
// is none: the end of a token, or a delimiter.
func (s *Stream) tokenBefore(at int64) int64 {
	p := at - 1
	for p > 0 && isWhiteSpace[s.buf[p]] {
		p--
	}
	return max(p, 0)
}

// quoteBefore returns the position of the quote which opens the valid string of the buffer which ends at end, after
// its closing quote: the one before it which no backslash escapes, an even number of backslashes before it.
func (s *Stream) quoteBefore(end int64) int64 {
	// the search starts before the closing quote, which is at end-1
	i := end - 2
	for {
		if s.buf[i] == '"' {
			n := int64(0)
			for i-n-1 >= 0 && s.buf[i-n-1] == '\\' {
				n++
			}
			if n%2 == 0 {
				return i
			}
		}
		i--
	}
}

// skipPathError returns err, the error of skipLongV2 at the total offset at, where it stopped with the levels level
// open, the kinds of which are the bits of objects, in the state resume, and records its place ( see V2Call.Place ):
// the levels are added to the path, and the error is found again from the end of the token before at.
func (s *Stream) skipPathError(err error, level int64, objects uint64, resume skipResume, at int64) error {
	from := s.pushSkipLevels(level, objects, resume, at)
	s.ctx.v2.skip.active = false
	return s.placeDelimiterError(err, from)
}

// pushSkipLevels adds the levels of the value which skipLongV2 skips, level of them open at the total offset at, the
// kinds of which are the bits of objects, in the state resume, to the path, and returns the total offset of the end of
// the token before at, which the innermost level is counted up to: a level around it is in the value after its
// last name, or its last comma.
func (s *Stream) pushSkipLevels(level int64, objects uint64, resume skipResume, at int64) int64 {
	tr := &s.ctx.v2.skip
	p := s.tokenBefore(at - s.offset)
	from := s.offset + p + 1
	for i := int64(0); i < level; i++ {
		object := objects&(1<<uint(i)) != 0
		l := s.level(s.pushLevel(object))
		n := tr.counts[i]
		switch c := s.buf[p]; {
		case i < level-1 && object:
			// in the value of the last name
			l.Count = countAfterName(n - 1)
		case i < level-1:
			// in the element after the last comma
			l.Count = n
		case c == '[' || c == '{':
			// the first element or name of the innermost level
			l.Count = 0
		case c == ',' || c == ':':
			// a value or a name after the delimiter, which is read again
			from--
			switch {
			case !object:
				l.Count = n
			case c == ':':
				l.Count = countAfterName(n - 1)
			default:
				l.Count = countAfterValue(n - 1)
			}
		case object && resume != resumeAfterKey:
			// after a value
			l.Count = countAfterValue(n - 1)
		case object:
			// after a name, before its colon
			l.Count = countAfterName(n - 1)
		default:
			// after an element
			l.Count = n + 1
		}
		if object && n > 0 {
			l.Name = append(l.Name[:0], s.skipName(i)...)
			if l.Name == nil {
				l.Name = []byte{} // the name "", which is a name
			}
		}
	}
	return from
}

// skipName returns the last name of the object open at the level i of the value which skipLongV2 skips: the one of
// the buffer, or the one which was copied before a read moved the buffer.
func (s *Stream) skipName(i int64) []byte {
	tr := &s.ctx.v2.skip
	if tr.quotes[i] == copiedName {
		return tr.names[i]
	}
	q := tr.quotes[i] - s.offset
	// a name of ASCII without an escape, up to its quote: any other is decoded, with the replacement character for
	// invalid UTF-8, as encoding/json/v2 compares and gives the names
	for n := q + 1; n < s.length; n++ {
		if c := s.buf[n]; c == '"' {
			return s.buf[q+1 : n]
		} else if c == '\\' || c >= utf8.RuneSelf {
			break
		}
	}
	end, err := skipString(s.buf[:s.length+1], q)
	if err != nil {
		// a name which was read whole before: it is valid
		return nil
	}
	return unquotedName(s.buf[q:end])
}

// copySkipNames copies the last names of the objects open in the value which skipLongV2 skips, which the buffer has,
// before a read moves it ( see Stream.copyNames ).
func (s *Stream) copySkipNames() {
	tr := &s.ctx.v2.skip
	for i := int64(0); i < tr.level; i++ {
		if tr.objects&(1<<uint(i)) != 0 && tr.counts[i] > 0 && tr.quotes[i] != copiedName {
			tr.names[i] = append(tr.names[i][:0], s.skipName(i)...)
			tr.quotes[i] = copiedName
		}
	}
}

// skipDeepV2 skips the object or the array at the cursor, which opens the level after the level levels of the value
// which skipLongV2 skips, which follows skipLevels levels at most, nested in depth objects and arrays: its levels are
// added to the path, and the value is skipped by skipLongV2, which follows its own levels, after which the levels are
// followed again from the path. A value nested too deep is the error of its bracket.
func (s *Stream) skipDeepV2(depth, level int64, objects uint64) error {
	at := s.offset + s.cursor
	if depth+level >= maxDecodeNestingDepth {
		return s.skipPathError(s.grammarError(depthSyntaxError(s.buf[:s.length+1], s.cursor)), level, objects, resumeValue, at)
	}
	call := s.ctx.v2
	tr := &call.skip
	n := len(call.Path)
	s.pushSkipLevels(level, objects, resumeValue, at)
	if err := s.skipLongV2(depth + level); err != nil {
		return err
	}
	for i := int64(0); i < level; i++ {
		l := &call.Path[n+int(i)]
		if tr.counts[i] = l.Count; l.Object {
			tr.counts[i] = namesCounted(l.Count)
			tr.names[i] = append(tr.names[i][:0], l.Name...)
		}
		tr.quotes[i] = copiedName
	}
	s.popLevel(n)
	tr.active = true
	return nil
}
