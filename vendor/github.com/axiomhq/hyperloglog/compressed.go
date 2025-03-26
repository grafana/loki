package hyperloglog

import (
	"encoding/binary"
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

func (v *compressedList) UnmarshalBinary(data []byte) error {
	if len(data) < 12 {
		return ErrorTooShort
	}

	// Set the count.
	v.count, data = binary.BigEndian.Uint32(data[:4]), data[4:]

	// Set the last value.
	v.last, data = binary.BigEndian.Uint32(data[:4]), data[4:]

	// Set the list.
	sz, data := binary.BigEndian.Uint32(data[:4]), data[4:]
	v.b = make([]uint8, sz)
	if uint32(len(data)) < sz {
		return ErrorTooShort
	}
	for i := uint32(0); i < sz; i++ {
		v.b[i] = data[i]
	}
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

func (v *compressedList) decode(i int, last uint32) (uint32, int) {
	n, i := v.b.decode(i)
	return n + last, i
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
	for i := 0; i < sz; i++ {
		data = append(data, v[i])
	}

	return data, nil
}

func (v variableLengthList) decode(i int) (uint32, int) {
	var x uint32
	j := i
	for ; v[j]&0x80 != 0; j++ {
		x |= uint32(v[j]&0x7f) << (uint(j-i) * 7)
	}
	x |= uint32(v[j]) << (uint(j-i) * 7)
	return x, j + 1
}

func (v variableLengthList) Append(x uint32) variableLengthList {
	for x&0xffffff80 != 0 {
		v = append(v, uint8((x&0x7f)|0x80))
		x >>= 7
	}
	return append(v, uint8(x&0x7f))
}
