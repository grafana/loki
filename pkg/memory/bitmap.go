package memory

import (
	"iter"
	"math/bits"

	"github.com/apache/arrow-go/v18/arrow/bitutil"

	"github.com/grafana/loki/v3/pkg/memory/internal/memalign"
)

// Bitmap is a bit-packed representation of a sequence of boolean values. Bits
// are ordered in LSB (Least Significant Bit) for compatibility with Apache
// Arrow.
//
// The zero value is ready for use, unassociated with a memory allocator. Use
// [NewBitmap] to create an allocator-associated bitmap.
type Bitmap struct {
	// alloc is an optional Allocator to use for retrieving memory. If nil,
	// memory is created using Go's built-in memory allocation.
	alloc *Allocator

	// The data slice below is always synchronized so that len(data) ==
	// cap(data). This avoids the need for re-slicing every 8 values, which is
	// a surprisingly expensive operation in a hot loop due to bounds checking.

	data []uint8 // Bitpacked data; always set to the full capacity to avoid re-slicing.
	len  int     // Number of bits in the bitmap.
}

// NewBitmap creates a Bitmap managed by the provided allocator. The returned
// Bitmap will have an initial length of zero and a capacity of at least n
// (which may be 0).
//
// If alloc is nil, memory is created using Go's built-in memory allocation.
// Otherwise, the lifetime of the returned Bitmap must not exceed the lifetime
// of alloc.
func NewBitmap(alloc *Allocator, n int) Bitmap {
	bmap := Bitmap{alloc: alloc}
	if n > 0 {
		bmap.Grow(n)
	}
	return bmap
}

// Append appends value to bmap.
func (bmap *Bitmap) Append(value bool) {
	if bmap.needGrow(1) {
		bmap.Grow(1)
	}
	bmap.AppendUnsafe(value)
}

func (bmap *Bitmap) needGrow(n int) bool { return bmap.len+n > bmap.capValues() }

// AppendUnsafe appends value to bmap without checking for capacity.
func (bmap *Bitmap) AppendUnsafe(value bool) {
	bitutil.SetBitTo(bmap.data, bmap.len, value)
	bmap.len++
}

// AppendCount appends value count times to bmap.
func (bmap *Bitmap) AppendCount(value bool, count int) {
	if count < 0 {
		panic("count must be non-negative")
	}
	if bmap.needGrow(count) {
		bmap.Grow(count)
	}
	bmap.AppendCountUnsafe(value, count)
}

// AppendBitmap appends the contents of another bitmap into bmap.
func (bmap *Bitmap) AppendBitmap(from Bitmap) {
	if bmap.needGrow(from.Len()) {
		bmap.Grow(from.Len())
	}
	bitutil.CopyBitmap(from.data, 0, from.len, bmap.data, bmap.len)
	bmap.len += from.Len()
}

// AppendCountUnsafe appends value count times to bmap without checking for
// capacity.
func (bmap *Bitmap) AppendCountUnsafe(value bool, count int) {
	bitutil.SetBitsTo(bmap.data, int64(bmap.len), int64(count), value)
	bmap.len += count
}

// Set sets the bit at index i to the given value. Set panics if i is out of
// range of the length.
func (bmap *Bitmap) Set(i int, value bool) { bitutil.SetBitTo(bmap.data, i, value) }

// SetRange sets all the bits in the range [from, to). SetRange panics if from >
// to or if to > bmap.Len().
func (bmap *Bitmap) SetRange(from, to int, value bool) {
	bitutil.SetBitsTo(bmap.data, int64(from), int64(to-from), value)
}

// Get returns the value at index i. Get panics if i is out of range.
func (bmap *Bitmap) Get(i int) bool { return bitutil.BitIsSet(bmap.data, i) }

// AppendValues adds a sequence of values to bmap.
func (bmap *Bitmap) AppendValues(values ...bool) {
	if bmap.needGrow(len(values)) {
		bmap.Grow(len(values))
	}

	for i, value := range values {
		bitutil.SetBitTo(bmap.data, bmap.len+i, value)
	}
	bmap.len += len(values)
}

// Grow increases bmap's capacity, if necessary, to guarantee space for another
// n values. After Grow(n), at least n values can be appended to bmap without
// another allocation. If n is negative or too large to allocate the memory,
// Grow panics.
func (bmap *Bitmap) Grow(n int) {
	if n < 0 {
		panic("negative length")
	}

	valuesCap := bmap.capValues()
	if bmap.len+n <= valuesCap {
		return
	}

	newValuesCap := max(bmap.len+n, 2*valuesCap)
	newData := bmap.getNewData(words(newValuesCap))

	copy(newData, bmap.data)
	bmap.data = newData
}

// getNewData gets a new data slice with the specified minimum size. The
// returned data is padded to 64 bytes for compatibility with Arrow buffer
// requirements.
func (bmap *Bitmap) getNewData(minSize int) []uint8 {
	size := memalign.Align(minSize)

	if bmap.alloc != nil {
		mem := bmap.alloc.Allocate(size)
		return mem.Data()
	}

	return make([]uint8, size)
}

// words returns the number of uint8 words needed to represent n bits.
func words(bits int) int {
	return (bits + 7) / 8
}

// capValues returns the capacity of bmap in terms of the number of values it
// can hold (one byte is 8 values).
func (bmap *Bitmap) capValues() int {
	return 8 * cap(bmap.data)
}

// Resize changes the length of bmap to n, allowing to set any index of bmap up
// to n. Resize will allocate additional memory if necessary.
func (bmap *Bitmap) Resize(n int) {
	if n < 0 {
		panic("negative length")
	} else if n == bmap.len {
		return
	}

	if n > bmap.capValues() {
		bmap.Grow(n - bmap.len)
	}

	bmap.len = n
}

// Len returns the length of bmap.
func (bmap *Bitmap) Len() int { return bmap.len }

// Cap returns how many values bmap can hold without needing a new allocation.
func (bmap *Bitmap) Cap() int { return bmap.capValues() }

// SetCount returns the number of bits set in the bitmap.
func (bmap *Bitmap) SetCount() int {
	return bitutil.CountSetBits(bmap.data, 0, bmap.len)
}

// ClearCount returns the number of bits unset in the bitmap.
func (bmap *Bitmap) ClearCount() int {
	return bmap.Len() - bmap.SetCount()
}

// Clone returns a copy of bmap. If bmap is associated with an allocator, the
// returned bitmap uses the same allocator.
func (bmap *Bitmap) Clone() *Bitmap {
	newData := bmap.getNewData(cap(bmap.data))
	copy(newData, bmap.data)

	return &Bitmap{
		alloc: bmap.alloc,

		data: newData,
		len:  bmap.len,
	}
}

// Bytes returns the raw representation of bmap, with bits stored in Least
// Significant Bit (LSB) order.
func (bmap *Bitmap) Bytes() []byte { return bmap.data }

// IterValues returns an iterator over bits, returning the index
// of each bit matching value.
func (bmap *Bitmap) IterValues(value bool) iter.Seq[int] {
	return func(yield func(int) bool) {
		var start int

		for _, word := range bmap.data {
			rem := word
			if !value {
				rem = ^rem // Use a NOT to get unset bits.
			}

			for rem != 0 {
				firstSet := bits.TrailingZeros8(rem)
				index := start + firstSet
				if index >= bmap.len {
					return
				} else if !yield(index) {
					return
				}
				rem ^= 1 << firstSet
			}

			start += 8
		}
	}
}
