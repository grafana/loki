package columnar

import (
	"fmt"
	"sort"

	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/memory"
)

// maxScannedChunks is the largest number of chunks for which a [ChunkedUTF8] finds the chunk of
// an element with a linear scan. A binary search over an index of chunk starts serves more chunks.
const maxScannedChunks = 8

// UTF8Reader is an [Array] of UTF-8 strings that supports reading an element by its index.
//
// A caller must not assume that the bytes of the values are contiguous.
type UTF8Reader interface {
	Array

	// Get returns the value at index i, or an empty slice for a null element. The slice shares
	// memory with the array, so the caller must not change it.
	//
	// Get can take more than constant time.
	//
	// Get panics if i is out of range.
	Get(i int) []byte

	// DataLen returns the total length of the values in bytes.
	DataLen() int
}

var (
	_ UTF8Reader = (*UTF8)(nil)
	_ UTF8Reader = (*ChunkedUTF8)(nil)
)

// ChunkedUTF8 is a view: an [Array] of UTF-8 strings that reads as one array, but holds several
// [UTF8] arrays, called chunks. It copies no value.
//
// A ChunkedUTF8 has no null element, and none of its chunks is empty. It is read-only. It is
// valid as long as every chunk is valid. The zero value is an empty array.
//
// The compute kernels read a [UTF8] only. A caller must join the chunks with [Flatten] before it
// passes a ChunkedUTF8 to a kernel.
type ChunkedUTF8 struct {
	chunks []*UTF8
	length int

	// starts[k] is the index of the first element of chunks[k]. It is nil when the view has at
	// most maxScannedChunks chunks.
	starts []int
}

// NewChunkedUTF8 returns a [ChunkedUTF8] over chunks, in order. It skips an empty chunk.
// It copies the slice, so the caller can change the slice afterwards. The caller must not change
// the chunks.
//
// NewChunkedUTF8 panics if a chunk is nil or has a null element.
func NewChunkedUTF8(chunks []*UTF8) *ChunkedUTF8 {
	kept := make([]*UTF8, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk == nil {
			panic("columnar: ChunkedUTF8 chunk is nil")
		}
		if chunk.Nulls() > 0 {
			panic("columnar: ChunkedUTF8 chunk has a null element")
		}
		if chunk.Len() > 0 {
			kept = append(kept, chunk)
		}
	}
	return newChunkedUTF8(kept)
}

// newChunkedUTF8 returns a [ChunkedUTF8] that owns chunks. Every chunk must be non-empty and
// have no null element.
func newChunkedUTF8(chunks []*UTF8) *ChunkedUTF8 {
	c := &ChunkedUTF8{chunks: chunks}
	if len(chunks) > maxScannedChunks {
		c.starts = make([]int, len(chunks))
	}
	for k, chunk := range chunks {
		if c.starts != nil {
			c.starts[k] = c.length
		}
		c.length += chunk.Len()
	}
	return c
}

// NumChunks returns the number of chunks.
func (c *ChunkedUTF8) NumChunks() int { return len(c.chunks) }

// Chunk returns the chunk at index k. The caller must not change it.
//
// Chunk panics if k is out of range.
func (c *ChunkedUTF8) Chunk(k int) *UTF8 { return c.chunks[k] }

func (c *ChunkedUTF8) isDatum() {}
func (c *ChunkedUTF8) isArray() {}

// Kind implements [Datum] and returns [types.KindUTF8].
func (c *ChunkedUTF8) Kind() types.Kind { return types.KindUTF8 }

// Type returns a [*types.UTF8].
func (c *ChunkedUTF8) Type() types.Type { return utf8Type }

// Len returns the total number of elements in the array.
func (c *ChunkedUTF8) Len() int { return c.length }

// Nulls returns 0, because a ChunkedUTF8 has no null element.
func (c *ChunkedUTF8) Nulls() int { return 0 }

// IsNull returns false, because a ChunkedUTF8 has no null element.
func (c *ChunkedUTF8) IsNull(int) bool { return false }

// Validity returns an empty bitmap, because a ChunkedUTF8 has no null element.
func (c *ChunkedUTF8) Validity() memory.Bitmap { return memory.Bitmap{} }

// DataLen returns the total length of the values in bytes.
func (c *ChunkedUTF8) DataLen() int {
	var n int
	for _, chunk := range c.chunks {
		n += chunk.DataLen()
	}
	return n
}

// Size returns the total size in bytes of the buffers of the chunks.
func (c *ChunkedUTF8) Size() int {
	var n int
	for _, chunk := range c.chunks {
		n += chunk.Size()
	}
	return n
}

// Get returns the value at index i. The slice shares memory with a chunk, so the caller must not
// change it.
//
// Get takes constant time for a small number of chunks, and logarithmic time for more.
//
// Get panics if i is out of range.
func (c *ChunkedUTF8) Get(i int) []byte {
	if i < 0 || i >= c.length {
		panic(fmt.Sprintf("columnar: index %d out of range [0, %d)", i, c.length))
	}

	if c.starts != nil {
		k := sort.Search(len(c.chunks), func(k int) bool { return c.starts[k] > i }) - 1
		return c.chunks[k].Get(i - c.starts[k])
	}
	for _, chunk := range c.chunks {
		if i < chunk.Len() {
			return chunk.Get(i)
		}
		i -= chunk.Len()
	}
	panic("columnar: ChunkedUTF8 length does not match its chunks")
}

// Slice returns a slice of c from i to j. The slice shares memory with c.
//
// When i equals j, the slice is a new empty [UTF8]. When the elements lie in one chunk, it is a
// UTF8. When they span chunks, it is a ChunkedUTF8.
func (c *ChunkedUTF8) Slice(i, j int) Array {
	if i < 0 || j < i || j > c.length {
		panic(errorSliceBounds{i, j, c.length})
	}

	var (
		out  []*UTF8
		base int
	)
	for _, chunk := range c.chunks {
		end := base + chunk.Len()
		if lo, hi := max(i, base), min(j, end); lo < hi {
			out = append(out, chunk.Slice(lo-base, hi-base).(*UTF8))
		}
		base = end
	}

	switch len(out) {
	case 0:
		return NewUTF8(nil, []int32{0}, memory.Bitmap{})
	case 1:
		return out[0]
	default:
		return newChunkedUTF8(out)
	}
}

// ConcatView is like [Concat], but it copies no value when it can return a [ChunkedUTF8].
//
// When every input is a [UTF8] with no null element or a ChunkedUTF8, ConcatView returns a
// ChunkedUTF8 over their chunks. An empty array adds no chunk. If the inputs hold one chunk
// only, ConcatView returns that chunk. In every other case, ConcatView returns the result of
// Concat, which uses alloc.
//
// A result that is not a copy shares memory with the inputs. It is valid as long as the inputs
// are valid.
func ConcatView(alloc *memory.Allocator, in []Array) (Array, error) {
	if len(in) == 0 {
		return Concat(alloc, in)
	}

	// Check the first input before allocating the chunk slice, so a call on arrays that are not
	// UTF8 skips the allocation.
	switch in[0].(type) {
	case *UTF8, *ChunkedUTF8:
	default:
		return Concat(alloc, in)
	}

	chunks := make([]*UTF8, 0, len(in))
	for _, arr := range in {
		switch arr := arr.(type) {
		case *UTF8:
			if arr.Nulls() > 0 {
				return Concat(alloc, in)
			}
			if arr.Len() > 0 {
				chunks = append(chunks, arr)
			}
		case *ChunkedUTF8:
			chunks = append(chunks, arr.chunks...)
		default:
			return Concat(alloc, in)
		}
	}

	switch len(chunks) {
	case 0:
		return Concat(alloc, in)
	case 1:
		return chunks[0], nil
	default:
		return newChunkedUTF8(chunks), nil
	}
}

// Flatten returns arr as one contiguous array. It copies the chunks of a [ChunkedUTF8] into
// alloc and returns a [UTF8]. It returns any other array unchanged.
func Flatten(alloc *memory.Allocator, arr Array) (Array, error) {
	view, ok := arr.(*ChunkedUTF8)
	if !ok {
		return arr, nil
	}
	return Concat(alloc, []Array{view})
}
