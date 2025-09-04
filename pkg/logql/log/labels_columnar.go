package log

import (
	"bytes"
	"slices"
)

// stringColumn is a columnar representation of a string slice. The bytes of all
// strings are stored in a single bytes slice `data`. The `offsets` slice points
// to the start of each string in `data`. The first entry will always be 0. All
// following entries are the accumulated length of all previous strings.
//
// Strings are always accessed via the `indices` slices. It points to the index
// of the string in the `offsets` slice. This technique allows deleting and
// sorting without copying the data. E.g. sorting just sorts the indices but not
// `data` not `offsets`.
type stringColumn struct {
	data    []byte
	offsets []int

	// indices is a selection vector.
	indices []int
}

func newStringColumn(capacity int) *stringColumn {
	return &stringColumn{
		data:    make([]byte, 0, capacity*16),
		offsets: make([]int, 0, capacity),
		indices: make([]int, 0, capacity),
	}
}

// add appends a new string
func (s *stringColumn) add(value []byte) {
	// The old values length is the offset of the new value
	s.offsets = append(s.offsets, len(s.data))

	s.data = append(s.data, value...)

	// Point to the last offset added
	s.indices = append(s.indices, len(s.offsets)-1)
}

// del remove the index from the selection vector. It does not remove the value.
// Use compact to also remove it from the data.
// TODO: implement compact
func (s *stringColumn) del(i int) {
	s.indices = append(s.indices[:i], s.indices[i+1:]...)
}

func (s *stringColumn) reset() {
	s.data = s.data[:0]
	s.offsets = s.offsets[:0]
	s.indices = s.indices[:0]
}

// get returns the string at the given index.
func (s *stringColumn) get(i int) []byte {
	index := s.indices[i]
	start := s.offsets[index]
	if index+1 >= len(s.offsets) {
		return s.data[start:]
	}

	end := s.offsets[index+1]
	return s.data[start:end]
}

// index returns the index of the string in the column.
func (s *stringColumn) index(value []byte) int {
	idx := bytes.Index(s.data, value)
	if idx == -1 {
		return -1
	}

	// Find index in offsets
	idx = slices.Index(s.offsets, idx)
	if idx == -1 {
		return -1
	}

	// Verify length
	var length int
	if idx+1 >= len(s.offsets) {
		length = len(s.data) - s.offsets[idx]
	} else {
		length = s.offsets[idx+1] - s.offsets[idx]
	}
	if length != len(value) {
		return -1
	}

	// Find index in indices
	return slices.Index(s.indices, idx)
}

func (s *stringColumn) len() int {
	return len(s.indices)
}

type columnarLabels struct {
	names  *stringColumn
	values *stringColumn
}

func (c *columnarLabels) add(name, value []byte) {
	c.names.add(name)
	c.values.add(value)
}

// override overrides the value of a label if it exists and returns true.
// If the label does not exist, it returns false and does nothing.
func (c *columnarLabels) override(name, value []byte) bool {
	for i := 0; i < len(c.names.indices); i++ {
		if bytes.Equal(c.names.get(i), name) {
			c.values.del(i)
			c.names.del(i)
			c.add(name, value)
			return true
		}
	}
	return false
}

func (c *columnarLabels) reset() {
	if c.names == nil {
		c.names = newStringColumn(0)
	}
	if c.values == nil {
		c.values = newStringColumn(0)
	}
	c.names.reset()
	c.values.reset()
}

func (c *columnarLabels) len() int {
	return c.names.len()
}

func (c *columnarLabels) get(key []byte) ([]byte, bool) {
	// Benchmarking showed that linear search is faster for small number of labels.
	if c.names.len() <= 50 {
		for i := 0; i < len(c.names.indices); i++ {
			if bytes.Equal(c.names.get(i), key) {
				return c.values.get(i), true
			}
		}
		return nil, false
	}

	idx := c.names.index(key)
	if idx == -1 {
		return nil, false
	}
	return c.values.get(idx), true
}

func (c *columnarLabels) getAt(i int) (name, value []byte) {
	return c.names.get(i), c.values.get(i)
}

func (c *columnarLabels) del(name []byte) {
	// TODO: to a string search on s.names.data
	for i := 0; i < len(c.names.indices); i++ {
		if bytes.Equal(c.names.get(i), name) {
			c.names.del(i)
			c.values.del(i)
		}
	}
}

func newColumnarLabels(capacity int) *columnarLabels {
	return &columnarLabels{
		names:  newStringColumn(capacity),
		values: newStringColumn(capacity),
	}
}

func newColumnarLabelsFromStrings(ss ...string) *columnarLabels {
	if len(ss)%2 != 0 {
		panic("invalid number of strings")
	}
	c := newColumnarLabels(len(ss) / 2)
	for i := 0; i < len(ss); i += 2 {
		c.add(unsafeGetBytes(ss[i]), unsafeGetBytes(ss[i+1]))
	}
	return c
}
