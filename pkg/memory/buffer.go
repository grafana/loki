package memory

import (
	"unsafe"

	"github.com/grafana/loki/v3/pkg/memory/internal/memalign"
	"github.com/grafana/loki/v3/pkg/memory/internal/unsafecast"
)

// Buffer is a low-level memory buffer for storing a set of elements
// contiguously in memory.
//
// Buffers must be created using [NewBuffer].
type Buffer[T any] struct {
	// To avoid double indirection (mem.data) for every operation, we cache the
	// casted representation of mem.

	alloc *Allocator
	mem   *Region
	data  []T
}

// NewBuffer creates a Buffer managed by the provided allocator. The returned Buffer
// will have an initial length of zero and a capacity of at least n (which may
// be 0).
//
// The lifetime of the returned Buffer must not exceed the lifetime of alloc.
func NewBuffer[T any](alloc *Allocator, n int) Buffer[T] {
	buf := Buffer[T]{alloc: alloc}
	if n > 0 {
		buf.Grow(n)
	}
	return buf
}

// Grow increases the capacity of buf, if necessary, to have space for at least
// another n elements. After Grow(n), at least n elements can be pushed to the
// slice without another allocation. If n is negative or too large to allocate
// the memory, Grow panics.
func (buf *Buffer[T]) Grow(n int) {
	if len(buf.data)+n <= cap(buf.data) {
		return
	}

	// Allocate new memory for our slice which is at least twice the capacity of
	// the current buffer. This also keeps the old memory alive in the
	// allocator, marked as used until [Allocator.Reclaim] is called.
	//
	// (We don't want to mark the old memory as free since there may still be
	// other references to it based on copies/slices of this buffer).
	newCap := max(len(buf.data)+n, 2*cap(buf.data))

	newBytes := newCap * int(unsafecast.Sizeof[T]())
	newMem := buf.alloc.Allocate(memalign.Align(newBytes))

	newData := castMemory[T](newMem)[:len(buf.data)]
	copy(newData, buf.data)

	buf.data = newData
	buf.mem = newMem
}

// Resize changes the length of buf to n, allowing to call [Buffer.Set] on any
// index up to n. Resize panics if n is bigger than the capacity of the buffer.
func (buf *Buffer[T]) Resize(n int) {
	buf.data = buf.data[:n]
}

// Push appends a single value to buf, adding 1 to buf's length. Push panics if
// there is not enough capacity for the value to be pushed.
func (buf *Buffer[T]) Push(value T) {
	i := len(buf.data)
	buf.data = buf.data[:i+1]
	buf.data[i] = value
}

// Append appends a set of values to buf, adding len(values) to buf's length.
// Append panics if there is not enough capacity for at least len(values) to be
// pushed.
func (buf *Buffer[T]) Append(values ...T) {
	from := len(buf.data)
	buf.data = buf.data[:from+len(values)]
	copy(buf.data[from:], values)
}

// AppendCount appends the value to buf n times, adding n to buf's length.
// AppendCount panics if there is not enough capacity for the value to be
// appended n times.
func (buf *Buffer[T]) AppendCount(value T, n int) {
	buf.data = buf.data[:len(buf.data)+n]
	for i := len(buf.data) - n; i < len(buf.data); i++ {
		buf.data[i] = value
	}
}

// Set sets the value at index i to value. Set panics if i is out of bounds.
func (buf *Buffer[T]) Set(i int, value T) { buf.data[i] = value }

// Get returns the value at index i. Get panics if i is out of bounds.
func (buf *Buffer[T]) Get(i int) T { return buf.data[i] }

// Len returns the length of buf.
func (buf *Buffer[T]) Len() int { return len(buf.data) }

// Cap returns the capacity of buf.
func (buf *Buffer[T]) Cap() int { return cap(buf.data) }

// Data returns the current data slice of buf so that elements can be read or
// modified directly.
func (buf *Buffer[T]) Data() []T { return buf.data }

// Clear zeroes out all memory in buf.
func (buf *Buffer[T]) Clear() {
	clear(buf.data)
}

// Serialize returns the serializable form of the underlying byte array
// representing buf, padded to 64-bytes. Padded bytes will be set to zero.
//
// The returned memory is shared with buf, not a copy.
func (buf *Buffer[T]) Serialize() []byte {
	if buf.mem == nil {
		return nil
	}

	// Arrow recommends padding array data to 64-byte boundaries.
	// Since bytes beyond where we've already written may have values from
	// previous instances of the Memory, we want to clear it out before
	// returning.
	out := unsafecast.Slice[T, byte](buf.data)
	alignedLen := memalign.Align(len(out))
	clear(out[len(out):alignedLen]) // May be a no-op if len is already aligned.
	return out[:alignedLen]
}

// castMemory converts a memory region to a slice of type To.
func castMemory[To any](mem *Region) []To {
	orig := mem.Data()

	var (
		toSize = int(unsafecast.Sizeof[To]())

		toLen = len(orig) / toSize
		toCap = cap(orig) / toSize
	)

	outPointer := (*To)(unsafe.Pointer(unsafe.SliceData(orig)))
	return unsafe.Slice(outPointer, toCap)[:toLen]
}
