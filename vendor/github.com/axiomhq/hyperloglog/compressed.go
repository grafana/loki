package hyperloglog

import (
	"encoding/binary"
	"fmt"
	"slices"
)

// Original author of this file is github.com/clarkduvall/hyperloglog

type iterator struct {
	i    int
	last uint32
	v    *compressedList
}

func (iter *iterator) Next() uint32 {
	n, i := iter.v.decode(iter.i, iter.last)
	iter.last = n
	iter.i = i
	return n
}

func (iter *iterator) Peek() (uint32, int) {
	return iter.v.decode(iter.i, iter.last)
}

func (iter *iterator) Advance(last uint32, i int) {
	iter.last = last
	iter.i = i
}

func (iter iterator) HasNext() bool {
	return iter.i < iter.v.Len()
}

type compressedList struct {
	count uint32
	last  uint32
	b     variableLengthList
}

func (v *compressedList) Clone() *compressedList {
	if v == nil {
		return nil
	}

	newV := &compressedList{
		count: v.count,
		last:  v.last,
	}

	newV.b = make(variableLengthList, len(v.b))
	copy(newV.b, v.b)
	return newV
}

func (v *compressedList) AppendBinary(data []byte) ([]byte, error) {
	// At least 4 bytes for the two fixed sized values
	data = slices.Grow(data, 4+4)

	// Marshal the count and last values.
	data = append(data,
		// Number of items in the list.
		byte(v.count>>24),
		byte(v.count>>16),
		byte(v.count>>8),
		byte(v.count),
		// The last item in the list.
		byte(v.last>>24),
		byte(v.last>>16),
		byte(v.last>>8),
		byte(v.last),
	)

	// Append the variableLengthList
	return v.b.AppendBinary(data)
}

// unmarshal requires p to have passed checkPrecision, and data to be exactly
// the compressed list; trailing bytes are rejected.
func (v *compressedList) unmarshal(data []byte, p uint8) error {
	if len(data) < 12 {
		return fmt.Errorf("hyperloglog: compressed list header needs 12 bytes, have %d: %w", len(data), ErrorTooShort)
	}

	// Read the count.
	count, data := binary.BigEndian.Uint32(data[:4]), data[4:]

	// Read the last value.
	last, data := binary.BigEndian.Uint32(data[:4]), data[4:]

	// Read the size of the list.
	sz, data := binary.BigEndian.Uint32(data[:4]), data[4:]
	if err := exactLen("compressed list stream", uint64(len(data)), uint64(sz)); err != nil {
		return err
	}
	if count >= mp {
		return fmt.Errorf("hyperloglog: compressed list count %d >= %d: %w", count, mp, ErrorInvalidData)
	}
	if count == 0 {
		if sz != 0 || last != 0 {
			return fmt.Errorf("hyperloglog: empty compressed list has size %d and last %d: %w", sz, last, ErrorInvalidData)
		}
	} else if sz < count || sz > 5*count {
		return fmt.Errorf("hyperloglog: compressed list of %d entries has impossible stream size %d: %w", count, sz, ErrorInvalidData)
	}

	b := make(variableLengthList, sz)
	copy(b, data[:sz])

	// Walk the stream once, decoding each varint exactly once, so that count
	// and last cannot describe something the payload does not contain.
	var entries uint32
	var running uint32
	for i := 0; i < int(sz); {
		off := i
		x, end, ok := b.decode(off)
		if !ok {
			return fmt.Errorf("hyperloglog: compressed list varint at compressed-list offset %d is malformed: %w", off, ErrorInvalidData)
		}
		i = end
		// Every delta after the first strictly increases the running key, so
		// the decoded keys strictly increase, the sum cannot wrap, and count
		// cannot be inflated by duplicates.
		next := running + x
		if entries > 0 && next <= running {
			return fmt.Errorf("hyperloglog: compressed list delta %d at compressed-list offset %d does not increase the key past %d: %w", entries, off, running, ErrorInvalidData)
		}
		running = next
		if err := checkSparseKey(running, p); err != nil {
			return fmt.Errorf("hyperloglog: compressed-list offset %d: %w", off, err)
		}
		entries++
	}
	if count != entries {
		return fmt.Errorf("hyperloglog: compressed list count %d, decoded %d entries: %w", count, entries, ErrorInvalidData)
	}
	if last != running {
		return fmt.Errorf("hyperloglog: compressed list last %d, decoded %d: %w", last, running, ErrorInvalidData)
	}

	v.count, v.last, v.b = count, last, b
	return nil
}

func newCompressedList(capacity int) *compressedList {
	v := &compressedList{}
	v.b = make(variableLengthList, 0, capacity)
	return v
}

func (v *compressedList) Len() int {
	return len(v.b)
}

// decode ignores b.decode's ok: streams reach here only after unmarshal validated them or Append built them, and a malformed stream would still return next == len(v), terminating Iter.
func (v *compressedList) decode(i int, last uint32) (uint32, int) {
	n, i, _ := v.b.decode(i)
	return n + last, i
}

func (v *compressedList) clear() {
	v.count = 0
	v.last = 0
	v.b.clear()
}

func (v *compressedList) Append(x uint32) {
	v.count++
	v.b = v.b.Append(x - v.last)
	v.last = x
}

func (v *compressedList) Iter() iterator {
	return iterator{0, 0, v}
}

type variableLengthList []uint8

func (v variableLengthList) AppendBinary(data []byte) ([]byte, error) {
	// 4 bytes for the size of the list, and a byte for each element in the
	// list.
	data = slices.Grow(data, 4+len(v))

	// Length of the list. We only need 32 bits because the size of the set
	// couldn't exceed that on 32 bit architectures.
	sz := len(v)
	data = append(data,
		byte(sz>>24),
		byte(sz>>16),
		byte(sz>>8),
		byte(sz),
	)

	// Marshal each element in the list.
	data = append(data, v...)

	return data, nil
}

func (v *variableLengthList) clear() {
	*v = (*v)[:0]
}

// decode reads the varint at offset i and returns its value and the offset of
// the next one. ok is false when the varint runs past the end of v, spans more
// than 5 bytes, does not fit in 32 bits, or is not minimally encoded; next is
// then len(v), so a loop driven by next terminates on a malformed stream.
func (v variableLengthList) decode(i int) (x uint32, next int, ok bool) {
	j := i
	for ; j < len(v) && v[j]&0x80 != 0; j++ {
		x |= uint32(v[j]&0x7f) << (uint(j-i) * 7)
	}
	if j >= len(v) {
		return x, len(v), false
	}
	x |= uint32(v[j]) << (uint(j-i) * 7)
	n := j - i + 1
	if n > 5 || (n == 5 && v[j] > 0x0f) || (n > 1 && v[j] == 0) {
		return x, len(v), false
	}
	return x, j + 1, true
}

func (v variableLengthList) Append(x uint32) variableLengthList {
	for x&0xffffff80 != 0 {
		v = append(v, uint8((x&0x7f)|0x80))
		x >>= 7
	}
	return append(v, uint8(x&0x7f))
}
