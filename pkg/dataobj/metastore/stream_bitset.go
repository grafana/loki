package metastore

import "math/bits"

// streamBitset is a set of stream IDs of one logs section. Stream i is the bit i%8 of byte i/8. Two
// sets that differ only in trailing zero bytes are equal.
type streamBitset []byte

// union adds the streams of other to the set.
func (s *streamBitset) union(other []byte) {
	// A row bitmap ends at its last stream, so the bitmaps of two rows can have different lengths.
	if len(*s) < len(other) {
		grown := make(streamBitset, len(other))
		copy(grown, *s)
		*s = grown
	}
	for i, b := range other {
		(*s)[i] |= b
	}
}

// intersection returns the streams that are in both a and b.
func intersection(a, b streamBitset) streamBitset {
	out := make(streamBitset, min(len(a), len(b)))
	for i := range out {
		out[i] = a[i] & b[i]
	}
	return out
}

// difference returns the streams that are in a and not in b.
func difference(a, b streamBitset) streamBitset {
	out := make(streamBitset, len(a))
	for i := range a {
		out[i] = a[i]
		if i < len(b) {
			out[i] &^= b[i]
		}
	}
	return out
}

// count returns the number of streams in the set.
func (s streamBitset) count() int {
	var n int
	for _, b := range s {
		n += bits.OnesCount8(b)
	}
	return n
}

// ids returns the stream IDs of the set in ascending order. It returns nil for an empty set.
func (s streamBitset) ids() []int64 {
	var ids []int64
	for i, b := range s {
		for ; b != 0; b &= b - 1 {
			ids = append(ids, int64(i*8+bits.TrailingZeros8(b)))
		}
	}
	return ids
}

// streamBitsetOf returns the set of the given stream IDs.
func streamBitsetOf(ids []int64) streamBitset {
	if len(ids) == 0 {
		return nil
	}

	// Find the highest ID first, to allocate the set once.
	var highest int64
	for _, id := range ids {
		highest = max(highest, id)
	}
	set := make(streamBitset, highest/8+1)
	for _, id := range ids {
		set[id/8] |= 1 << (id % 8)
	}
	return set
}
