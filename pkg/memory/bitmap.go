package memory

import (
	"iter"
	"math/bits"
	"slices"
)

// bitmap represents a set of bits. This is a lightweight copy of
// [github.com/grafana/loki/v3/pkg/memory/bitmap], which we can't import to
// avoid cyclic dependencies.
type bitmap []uint64

// Set sets the bit at the specified index.
func (set bitmap) Set(index int, value bool) {
	if index < 0 || index >= set.Size() {
		panic("index out of range")
	}

	if value {
		set[index/64] |= 1 << (index % 64)
	} else {
		set[index/64] &^= 1 << (index % 64)
	}
}

// SetRange sets all the bits in the range [from, to). SetRange panics if from >
// to or if to > set.Size().
func (set bitmap) SetRange(from, to int, value bool) {
	if from > to || to > set.Size() {
		panic("invalid range")
	}

	if value {
		for i := from; i < to; i++ {
			set[i/64] |= 1 << (i % 64)
		}
	} else {
		for i := from; i < to; i++ {
			set[i/64] &^= 1 << (i % 64)
		}
	}
}

// Resize resizes the bitmap to hold at least the specified number of elements.
func (set *bitmap) Resize(elements int) {
	buf := *set

	words := (elements + 63) / 64

	if words > cap(buf) {
		buf = slices.Grow(buf, words)
		buf = buf[:words]
	} else {
		buf = buf[:words]
	}

	*set = buf
}

// Size returns the number of elements the bitmap can hold.
func (set bitmap) Size() int {
	return len(set) * 64
}

// IterValues returns an iterator over the set bits in the bitmap, up to (but not
// including) maxIndex.
func (set bitmap) IterValues(maxIndex int, value bool) iter.Seq[int] {
	return func(yield func(int) bool) {
		var start int

		for _, word := range set {
			rem := word
			if !value {
				rem = ^rem // Use a bitwise NOT to get unset bits.
			}

			for rem != 0 {
				firstSet := bits.TrailingZeros64(rem)
				index := start + firstSet
				if index >= maxIndex {
					return
				} else if !yield(index) {
					return
				}
				rem ^= 1 << firstSet
			}
			start += 64
		}
	}
}
