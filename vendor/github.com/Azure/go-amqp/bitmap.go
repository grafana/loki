package amqp

import (
	"math/bits"
)

// bitmap is a lazily initialized bitmap
type bitmap struct {
	max  uint32
	bits []uint64
}

// add sets n in the bitmap.
//
// bits will be expanded as needed.
//
// If n is greater than max, the call has no effect.
func (b *bitmap) add(n uint32) {
	if n > b.max {
		return
	}

	var (
		idx    = n / 64
		offset = n % 64
	)

	if l := len(b.bits); int(idx) >= l {
		b.bits = append(b.bits, make([]uint64, int(idx)-l+1)...)
	}

	b.bits[idx] |= 1 << offset
}

// remove clears n from the bitmap.
//
// If n is not set or greater than max the call has not effect.
func (b *bitmap) remove(n uint32) {
	var (
		idx    = n / 64
		offset = n % 64
	)

	if int(idx) >= len(b.bits) {
		return
	}

	b.bits[idx] &= ^uint64(1 << offset)
}

// next sets and returns the lowest unset bit in the bitmap.
//
// bits will be expanded if necessary.
//
// If there are no unset bits below max, the second return
// value will be false.
func (b *bitmap) next() (uint32, bool) {
	// find the first unset bit
	for i, v := range b.bits {
		// skip if all bits are set
		if v == ^uint64(0) {
			continue
		}

		var (
			offset = bits.TrailingZeros64(^v) // invert and count zeroes
			next   = uint32(i*64 + offset)
		)

		// check if in bounds
		if next > b.max {
			return next, false
		}

		// set bit
		b.bits[i] |= 1 << uint32(offset)
		return next, true
	}

	// no unset bits in the current slice,
	// check if the full range has been allocated
	if uint64(len(b.bits)*64) > uint64(b.max) {
		return 0, false
	}

	// full range not allocated, append entry with first
	// bit set
	b.bits = append(b.bits, 1)

	// return the value of the first bit
	return uint32(len(b.bits)-1) * 64, true
}
