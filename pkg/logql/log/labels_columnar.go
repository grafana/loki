package log

import "bytes"

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

func (s *stringColumn) get(i int) []byte {
	// TODO: test this. It's tricky
	index := s.indices[i]
	start := s.offsets[index]
	if index+1 >= len(s.offsets) {
		return s.data[start:]
	}

	end := s.offsets[index+1]
	return s.data[start:end]
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

func (s *columnarLabels) len() int {
	return s.names.len()
}

func (s *columnarLabels) get(key []byte) ([]byte, bool) {
	// TODO: to a string search on s.names.data
	for i := 0; i < len(s.names.indices); i++ {
		if bytes.Equal(s.names.get(i), key) {
			return s.values.get(i), true
		}
	}
	return nil, false
}

func (s *columnarLabels) getAt(i int) (name, value []byte) {
	return s.names.get(i), s.values.get(i)
}

func (s *columnarLabels) del(name []byte) {
	// TODO: to a string search on s.names.data
	for i := 0; i < len(s.names.indices); i++ {
		if bytes.Equal(s.names.get(i), name) {
			s.names.del(i)
			s.values.del(i)
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
