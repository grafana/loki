package jsontext

import (
	"hash/maphash"
	"slices"
	"strconv"

	"github.com/goccy/go-json/internal/jsonstring"
	"github.com/goccy/go-json/internal/textcoder"
)

// maxDepth is the deepest nesting of objects and arrays which an encoder or decoder takes.
const maxDepth = 10000

// level is a level of the nesting: the top level of the stream, or an open object or array. Its count is the
// number of tokens in it: the values of the top level or an array, and the names and values of an object.
type level struct {
	count  int64
	object bool
	first  int    // for an object, the index in names.ends of its first name
	last   int    // for an object, the index in names.ends of its last name, or -1, or, for valueScanner.lazyNames, -2 minus the position of the name in the input
	named  int64  // for an object, its count after its last name was added
	bits   uint64 // for an object, the bits of its names ( jsonstring.NameBit ), which the name of no bit is not one of
}

// stack is the state of the grammar: the levels of the nesting, the top level first, and the names of the open
// objects.
type stack struct {
	levels []level
	names  names
	// outer gives the levels around the level outerDepth, an encoder was attached at with the count outerCount,
	// which the stack doesn't have ( see textcoder.Attach ): the pointers and the levels of the stack are found
	// with them ( see full ).
	outer      textcoder.Outer
	outerDepth int
	outerCount int64
	// base is the number of the levels around the top level of the stack in the whole output, of an encoder attached
	// in them, which its depth is counted after for the deepest level ( see textcoder.Attachment.OuterDepth ).
	base int
}

// full returns the stack of the whole output: the levels which outer gives, in place of the levels up to
// outerDepth, followed by the levels in them. It is made for a pointer or for the levels which an encoder is asked
// for, which most writes don't need.
func (s *stack) full() *stack {
	t := &stack{}
	t.reset()
	t.applyLevels(s.outer.Levels())
	at, inner := &s.levels[s.outerDepth], t.last()
	delta := at.count - s.outerCount
	if len(s.levels) > s.outerDepth+1 {
		delta-- // the open value of the next level, which its push below counts again
	}
	inner.count += delta
	if at.object && at.last >= at.first {
		// the names written after the attach.
		for k := at.first; k <= at.last; k++ {
			t.insertName(s.names.get(k), false)
		}
		inner.named = at.named + delta
	}
	levels := s.levels[s.outerDepth+1:]
	for i, l := range levels {
		_ = t.push(l.object)
		if i > 0 {
			// the open value, which the push counted again in the level before
			t.levels[len(t.levels)-2].count = levels[i-1].count
		}
		c := t.last()
		if l.object && l.last >= l.first {
			for k := l.first; k <= l.last; k++ {
				t.insertName(s.names.get(k), false)
			}
		}
		c.count, c.named = l.count, l.named
	}
	return t
}

// applyLevels sets the stack, which has the top level only, to the levels, the top level first, of which an
// object has its last name only ( see textcoder.Level ).
func (s *stack) applyLevels(levels []textcoder.Level) {
	for i, l := range levels {
		if i > 0 {
			// the open value of the level which contains it is counted by the push.
			_ = s.push(l.Object)
		}
		cur := s.last()
		if l.Object && l.Name != nil {
			// the name of the value which is open or next, the place of the value: the names before it are not
			// given, which a value written at its place can't have.
			s.insertName(l.Name, false)
			cur.named = (l.Count - 1) | 1 // the count after the name
		}
		cur.count = l.Count
	}
}

// own gives the stack arrays of its own: the ones of a coder which was copied are shared with the coder it was
// copied from, whose state they hold too. The indexes of the names are made again where an object needs one.
// If the other coder changed the arrays since the copy, which interleaved use of both does, the names may not
// fit the levels: they are made again then, as empty names where the levels need one, so that the copy, whose
// reads or writes are not defined, keeps a state which doesn't panic.
func (s *stack) own() {
	s.levels = slices.Clone(s.levels)
	s.names.buf = slices.Clone(s.names.buf)
	s.names.ends = slices.Clone(s.names.ends)
	s.names.indexes = nil
	if !s.namesFit() {
		s.refit()
	}
}

// namesFit reports whether the levels and the names are in the state which reads and writes keep: an object
// which is not the deepest level has the name of the value which is open in it, its last one; the names of an
// object follow the ones of the object around it; and the deepest object has its last name, if it has one, at
// the end of the names.
func (s *stack) namesFit() bool {
	n := &s.names
	for k, end := range n.ends {
		if end > len(n.buf) || k > 0 && end < n.ends[k-1] {
			return false
		}
	}
	if len(s.levels) == 0 || s.levels[0].object {
		return false
	}
	next := 0 // the index of the first name of the next object
	deepest := len(s.levels) - 1
	for k, l := range s.levels {
		if !l.object {
			continue
		}
		if l.first != next {
			return false
		}
		if k < deepest {
			// its name and the value which is open in it are counted
			if l.count%2 == 1 || l.count == 0 || l.last < l.first || l.last >= len(n.ends) {
				return false
			}
			next = l.last + 1
			continue
		}
		// the deepest level, which has no name before its first member
		if l.last == -1 {
			return l.count == 0 && len(n.ends) == l.first
		}
		return l.last >= l.first && l.last == len(n.ends)-1
	}
	return len(n.ends) == next
}

// refit makes the names again for the levels, which namesFit takes: an object which is not the deepest level, or
// which has members, has an empty name.
func (s *stack) refit() {
	n := &s.names
	n.buf, n.ends = n.buf[:0], n.ends[:0]
	s.levels[0].object = false
	for k := range s.levels {
		l := &s.levels[k]
		if !l.object {
			continue
		}
		l.first, l.last, l.named, l.bits = len(n.ends), -1, 0, 0
		if k < len(s.levels)-1 {
			l.count = max(l.count&^1, 2) // its name and the value which is open in it
		}
		if l.count > 0 {
			n.ends = append(n.ends, len(n.buf))
			l.last, l.named = l.first, (l.count-1)|1 // the count after the name, before its value
		}
	}
}

func (s *stack) reset() {
	s.levels = append(s.levels[:0], level{})
	s.base = 0
	s.names.reset()
}

// depth is the number of open objects and arrays.
func (s *stack) depth() int { return len(s.levels) - 1 }

func (s *stack) last() *level { return &s.levels[len(s.levels)-1] }

// push opens an object or an array, whose begin token is a value of the level where it is.
func (s *stack) push(object bool) error {
	n := len(s.levels) // once, which keeps the function inlined into the reads and the writes of tokens
	if n+s.base > maxDepth {
		return errMaxDepth
	}
	s.levels[n-1].count++
	if n < cap(s.levels) {
		s.levels = s.levels[:n+1]
	} else {
		s.grow()
	}
	// the level is written in place: an append of it would build it in a temporary, whose copy would load it
	// by 16 bytes from narrower stores, which stalls the CPU.
	*s.last() = level{object: object, first: len(s.names.ends), last: -1}
	return nil
}

func (s *stack) grow() { s.levels = append(s.levels, level{}) }

// pop closes the innermost object or array.
func (s *stack) pop() {
	if l := s.last(); l.object {
		s.names.truncate(l.first)
	}
	s.levels = s.levels[:len(s.levels)-1]
}

// Where the innermost level is in its grammar.
func (l *level) needName() bool  { return l.object && l.count%2 == 0 }
func (l *level) needValue() bool { return l.object && l.count%2 == 1 }

// The pointers of a stack point to a value in the innermost level: pointLast to the last one which was read or
// written, pointNext to the one after it where a value is next, and pointAt to the level itself, or to the
// value of the last name in an object whose value is next.
const (
	pointLast = iota
	pointNext
	pointAt
	pointOut  // pointAt of the level which contains the innermost one, as if it was closed
	pointHere // the innermost object or array itself
)

// pointer is the JSON pointer of the stack at the value which where chooses.
func (s *stack) pointer(where int) Pointer {
	if s.outer != nil {
		return s.full().pointer(where)
	}
	if len(s.levels) == 1 {
		return ""
	}
	return Pointer(s.pointerBytes(where, 0))
}

// namePointer is the pointer to the name of a member of the innermost object, which is not inserted.
func (s *stack) namePointer(name []byte) Pointer {
	if s.outer != nil {
		return s.full().namePointer(name)
	}
	return Pointer(appendPointerToken(s.pointerBytes(pointAt, 0), name))
}

// pointerBytes is the pointer of the place where, of the levels after the level of the depth from.
func (s *stack) pointerBytes(where, from int) []byte {
	var b []byte
	n := len(s.levels)
	switch where {
	case pointOut:
		n--
		where = pointAt
	case pointHere:
		n--
		where = pointLast
	}
	for i := from + 1; i < n; i++ {
		l := &s.levels[i]
		innermost := i == n-1
		switch {
		case l.object:
			// the last name, while its value or a value in it is read, and after it was read if pointLast.
			// the name must be the one of the last member, which a value where a name is has not.
			if l.last >= 0 && l.named >= l.count-1 && (l.count%2 == 1 || !innermost || where == pointLast) {
				b = appendPointerToken(b, s.names.get(l.last))
			}
		case !innermost || where == pointLast:
			if l.count > 0 {
				b = strconv.AppendInt(append(b, '/'), l.count-1, 10)
			}
		case where == pointNext:
			b = strconv.AppendInt(append(b, '/'), l.count, 10)
		}
	}
	return b
}

// insertName adds the unquoted name to the innermost object, which must need a name, and reports false if
// rejectDuplicate is set and the object has it already. Without rejectDuplicate, the object keeps only its last
// name, which the pointers need.
func (s *stack) insertName(name []byte, rejectDuplicate bool) bool {
	l := s.last()
	n := &s.names
	if !rejectDuplicate {
		// the object keeps its last name only, for the pointers of errors: no object is indexed.
		n.buf = append(n.buf[:n.start(l.first)], name...)
		n.ends = append(n.ends[:l.first], len(n.buf))
		l.last = l.first
		l.named = l.count + 1
		return true
	}
	if bit := jsonstring.NameBit(name); l.bits&bit == 0 {
		l.bits |= bit // a name of a bit which no name has is new: it is not looked for
	} else if n.has(l.first, name) {
		return false
	}
	n.add(l.first, name)
	l.last = len(n.ends) - 1
	l.named = l.count + 1
	return true
}

// addNewName adds the unquoted name to the innermost object, which must need a name, as insertName with
// rejectDuplicate does, and reports true, if the name is of a bit which no name of the object has and the names of
// the objects are few, as most are: then it is new, and no object has an index of its names. Otherwise it adds
// nothing, and insertName is to add it.
func (s *stack) addNewName(name []byte) bool {
	l := &s.levels[len(s.levels)-1]
	n := &s.names
	bit := jsonstring.NameBit(name)
	if l.bits&bit != 0 || len(n.indexes) != 0 || len(n.ends)-l.first >= maxLinearNames || len(n.buf)+len(name) > maxLinearBytes {
		return false
	}
	l.bits |= bit
	n.buf = append(n.buf, name...)
	n.ends = append(n.ends, len(n.buf))
	l.last = len(n.ends) - 1
	l.named = l.count + 1
	return true
}

// names are the names of the open objects, back to back, an object after the objects which contain it.
type names struct {
	buf  []byte // the names, unquoted
	ends []int  // the end of each name in buf
	// the indexes of the objects of many names, the innermost last.
	indexes []nameIndex
}

// A name is looked for in the names of its object one by one while they are few, and over maxLinearNames or
// maxLinearBytes of them, through an index of their hashes: the limits over which encoding/json/jsontext takes
// a map.
const (
	maxLinearNames = 64
	maxLinearBytes = 1024
)

// nameIndex is an index of the names of an object: a table of open addressing, whose size is a power of two,
// of the positions of its names plus one, by their hashes.
type nameIndex struct {
	first  int // the index in ends of the first name of the object
	slots  []int32
	hashes []uint64 // the hash of each name of the object
}

// nameSeed is the seed of the hashes, chosen at random as the one of a Go map is, so that no input can have
// many names of the same hash.
var nameSeed = maphash.MakeSeed()

func (n *names) reset() {
	n.buf = n.buf[:0]
	n.ends = n.ends[:0]
	n.indexes = n.indexes[:0]
}

// truncate drops the names from the index first on: the names of an object which is closed, or of the objects
// which a scan that gave up added, whose indexes are dropped too.
func (n *names) truncate(first int) {
	k := len(n.indexes)
	for k > 0 && n.indexes[k-1].first >= first {
		k--
	}
	n.indexes = n.indexes[:k]
	n.buf = n.buf[:n.start(first)]
	n.ends = n.ends[:first]
}

// start is the position in buf of the name at the index i.
func (n *names) start(i int) int {
	if i == 0 {
		return 0
	}
	return n.ends[i-1]
}

func (n *names) get(i int) []byte { return n.buf[n.start(i):n.ends[i]] }

// index is the index of the object whose first name is at first, or nil.
func (n *names) index(first int) *nameIndex {
	if k := len(n.indexes); k > 0 && n.indexes[k-1].first == first {
		return &n.indexes[k-1]
	}
	return nil
}

// has reports whether the object whose first name is at first has the name.
func (n *names) has(first int, name []byte) bool {
	if x := n.index(first); x != nil {
		h := maphash.Bytes(nameSeed, name)
		mask := uint64(len(x.slots) - 1)
		for slot := h & mask; x.slots[slot] != 0; slot = (slot + 1) & mask {
			i := int(x.slots[slot]) - 1
			if x.hashes[i] == h && string(n.get(first+i)) == string(name) {
				return true
			}
		}
		return false
	}
	// the length and the first byte are compared first, which rejects most names without a call of the runtime.
	start := n.start(first)
	for _, end := range n.ends[first:] {
		if end-start == len(name) && (len(name) == 0 || n.buf[start] == name[0] && string(n.buf[start:end]) == string(name)) {
			return true
		}
		start = end
	}
	return false
}

// add appends a name to the innermost object, whose first name is at first.
func (n *names) add(first int, name []byte) {
	n.buf = append(n.buf, name...)
	n.ends = append(n.ends, len(n.buf))
	if len(n.indexes) == 0 && len(n.ends)-first <= maxLinearNames && len(n.buf) <= maxLinearBytes {
		return // no object has an index, nor needs one: the names of all of them are few
	}
	if x := n.index(first); x != nil {
		x.hashes = append(x.hashes, maphash.Bytes(nameSeed, name))
		if 2*len(x.hashes) > len(x.slots) {
			x.rebuild(2 * len(x.slots))
		} else {
			x.insert(len(x.hashes) - 1)
		}
		return
	}
	if len(n.ends)-first > maxLinearNames || len(n.buf)-n.start(first) > maxLinearBytes {
		n.indexObject(first)
	}
}

// indexObject makes the index of the names of the innermost object, whose first name is at first.
func (n *names) indexObject(first int) {
	// the arrays of an index of an object which was closed are used again.
	if k := len(n.indexes); k < cap(n.indexes) {
		n.indexes = n.indexes[:k+1]
	} else {
		n.indexes = append(n.indexes, nameIndex{})
	}
	x := &n.indexes[len(n.indexes)-1]
	x.first, x.hashes = first, x.hashes[:0]
	for i := first; i < len(n.ends); i++ {
		x.hashes = append(x.hashes, maphash.Bytes(nameSeed, n.get(i)))
	}
	// a table of a quarter of its slots used at most: an object of a copy, whose indexes were dropped, may have
	// many names already.
	size := 4 * maxLinearNames
	for 2*len(x.hashes) > size {
		size *= 2
	}
	x.rebuild(size)
}

// rebuild makes the table of the given size, a power of two, of all the names.
func (x *nameIndex) rebuild(size int) {
	if cap(x.slots) >= size {
		x.slots = x.slots[:size]
		clear(x.slots)
	} else {
		x.slots = make([]int32, size)
	}
	for i := range x.hashes {
		x.insert(i)
	}
}

// insert puts the name at the index i of the object in the table.
func (x *nameIndex) insert(i int) {
	mask := uint64(len(x.slots) - 1)
	slot := x.hashes[i] & mask
	for x.slots[slot] != 0 {
		slot = (slot + 1) & mask
	}
	x.slots[slot] = int32(i + 1)
}
