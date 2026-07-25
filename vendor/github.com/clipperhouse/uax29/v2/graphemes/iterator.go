package graphemes

// FromString returns an iterator for the grapheme clusters in the input string.
// Iterate while Next() is true, and access the grapheme via Value().
func FromString(s string) *Iterator[string] {
	return &Iterator[string]{
		split: splitFuncString,
		data:  s,
	}
}

// FromBytes returns an iterator for the grapheme clusters in the input bytes.
// Iterate while Next() is true, and access the grapheme via Value().
func FromBytes(b []byte) *Iterator[[]byte] {
	return &Iterator[[]byte]{
		split: splitFuncBytes,
		data:  b,
	}
}

// Iterator is a generic iterator for grapheme clusters in strings or byte slices,
// with an ASCII hot path optimization.
type Iterator[T ~string | ~[]byte] struct {
	split func(T, bool) (int, T, error)
	data  T
	pos   int
	start int
}

var (
	splitFuncString = splitFunc[string]
	splitFuncBytes  = splitFunc[[]byte]
)

// Next advances the iterator to the next grapheme cluster.
// Returns false when there are no more grapheme clusters.
func (iter *Iterator[T]) Next() bool {
	if iter.pos >= len(iter.data) {
		return false
	}
	iter.start = iter.pos

	// ASCII hot path: if current byte is printable ASCII and
	// next byte is also ASCII (or end of data), return single byte
	b := iter.data[iter.pos]
	if b >= 0x20 && b < 0x7F {
		// If next byte is non-ASCII, it could be a combining mark
		if iter.pos+1 >= len(iter.data) || iter.data[iter.pos+1] < 0x80 {
			iter.pos++
			return true
		}
	}

	// Fall back to actual grapheme parsing
	remaining := iter.data[iter.pos:]
	advance, _, err := iter.split(remaining, true)
	if err != nil {
		panic(err)
	}
	if advance <= 0 {
		panic("splitFunc returned a zero or negative advance")
	}
	iter.pos += advance
	if iter.pos > len(iter.data) {
		panic("splitFunc advanced beyond end of data")
	}
	return true
}

// Value returns the current grapheme cluster.
func (iter *Iterator[T]) Value() T {
	return iter.data[iter.start:iter.pos]
}

// Start returns the byte position of the current grapheme in the original data.
func (iter *Iterator[T]) Start() int {
	return iter.start
}

// End returns the byte position after the current grapheme in the original data.
func (iter *Iterator[T]) End() int {
	return iter.pos
}

// Reset resets the iterator to the beginning of the data.
func (iter *Iterator[T]) Reset() {
	iter.start = 0
	iter.pos = 0
}

// SetText sets the data for the iterator to operate on, and resets all state.
func (iter *Iterator[T]) SetText(data T) {
	iter.data = data
	iter.start = 0
	iter.pos = 0
}

// First returns the first grapheme cluster without advancing the iterator.
func (iter *Iterator[T]) First() T {
	if len(iter.data) == 0 {
		return iter.data
	}

	// Use a copy to leverage Next()'s ASCII optimization
	cp := *iter
	cp.pos = 0
	cp.start = 0
	cp.Next()
	return cp.Value()
}
