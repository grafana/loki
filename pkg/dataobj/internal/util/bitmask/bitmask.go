// Package bitmask provides an API for creating and manipulating
// bitmasks of arbitrary length.
package bitmask

// Mask is a bitmask of arbitrary length. The zero value is a Mask of length 0.
type Mask struct {
	len int
	b   []uint64
}

// New creates a new Mask with the given length. New panics if length is less
// than 1.
func New(length int) *Mask {
	var m Mask
	m.Reset(length)
	return &m
}

// Len returns the length of the mask.
func (m *Mask) Len() int { return m.len }

// Set sets the bit at the given index to 1. Set panics if index is out of
// range.
func (m *Mask) Set(index int) {
	if index < 0 || index >= m.len {
		panic("bitmask.Set: index out of range")
	}
	m.b[index/64] |= 1 << uint(index%64)
}

// Clear sets the bit at the given index to 0. Clear panics if index is out of
// range.
func (m *Mask) Clear(index int) {
	if index < 0 || index >= m.len {
		panic("bitmask.Clear: index out of range")
	}

	m.b[index/64] &^= 1 << uint(index%64)
}

// Test returns true if the bit at the given index is 1. Test panics if index
// is out of range.
func (m *Mask) Test(index int) bool {
	if index < 0 || index >= m.len {
		panic("bitmask.Has: index out of range")
	}

	return m.b[index/64]&(1<<uint(index%64)) != 0
}

// Reset zeroes out the mask and resets it to the given length. Reset panics if
// length is less than 1.
func (m *Mask) Reset(length int) {
	if length < 1 {
		panic("bitmask.Reset: length must be at least 1")
	}

	newLength := (length + 63) / 64
	if newLength > len(m.b) {
		newB := make([]uint64, newLength)
		copy(newB, m.b)
		m.b = newB
	} else {
		m.b = m.b[:newLength]
	}

	clear(m.b)
	m.len = length
}
