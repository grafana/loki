package memory

import (
	"math/bits"
	"unsafe"
)

// ChunkBuffer is a buffer that stores data in fixed-size chunks.
// Chunks are allocated lazily on demand and reused via slice pools.
// The chunk size is rounded up to the nearest power of two to utilize slice buffers fully.
// This design minimizes memory fragmentation and provides predictable memory usage.
type ChunkBuffer[T Datum] struct {
	chunks    []*slice[byte]
	chunkSize int // in bytes, always a power of 2
	length    int
}

// ChunkBufferFor creates a new ChunkBuffer with the given chunk size (in bytes).
// The chunk size will be rounded up to the nearest power of two.
func ChunkBufferFor[T Datum](chunkSize int) ChunkBuffer[T] {
	size := nextPowerOfTwo(uint(chunkSize))
	// Clamp to bucket system range
	minSize := uint(minBucketSize)
	maxSize := uint(bucketSize(numBuckets - 1))
	size = max(minSize, min(size, maxSize))
	return ChunkBuffer[T]{chunkSize: int(size)}
}

func nextPowerOfTwo(n uint) uint {
	if n == 0 {
		return 1
	}
	// bits.Len(x) returns the minimum number of bits required to represent x
	return 1 << bits.Len(n-1)
}

// Append adds data to the buffer, allocating new chunks as needed.
func (b *ChunkBuffer[T]) Append(data ...T) {
	elemSize := int(unsafe.Sizeof(*new(T)))
	capacity := b.chunkSize / elemSize

	for len(data) > 0 {
		if len(b.chunks) == 0 || (b.length&(capacity-1)) == 0 {
			bucketIndex := bucketIndexOfGet(b.chunkSize)
			chunk := slicePools[bucketIndex].Get(
				func() *slice[byte] {
					return newSlice[byte](b.chunkSize)
				},
				func(s *slice[byte]) { s.data = s.data[:0] },
			)
			b.chunks = append(b.chunks, chunk)
		}

		currentChunk := b.chunks[len(b.chunks)-1]
		chunkData := (*T)(unsafe.Pointer(unsafe.SliceData(currentChunk.data)))
		typeChunk := unsafe.Slice(chunkData, capacity)
		offset := b.length & (capacity - 1)
		available := capacity - offset
		toWrite := min(len(data), available)

		copy(typeChunk[offset:], data[:toWrite])
		b.length += toWrite
		data = data[toWrite:]
	}
}

// Reset returns all chunks to the pool and resets the buffer to empty.
func (b *ChunkBuffer[T]) Reset() {
	bucketIndex := bucketIndexOfGet(b.chunkSize)
	for i := range b.chunks {
		slicePools[bucketIndex].Put(b.chunks[i])
		b.chunks[i] = nil
	}
	b.chunks = b.chunks[:0]
	b.length = 0
}

// Len returns the number of elements currently in the buffer.
func (b *ChunkBuffer[T]) Len() int { return b.length }

// NumChunks returns the number of chunks currently allocated.
func (b *ChunkBuffer[T]) NumChunks() int { return len(b.chunks) }

// Chunk returns the data for the chunk at the given index.
// The caller must ensure i < NumChunks().
func (b *ChunkBuffer[T]) Chunk(i int) []T {
	elemSize := int(unsafe.Sizeof(*new(T)))
	capacity := b.chunkSize / elemSize
	chunk := b.chunks[i]
	chunkData := (*T)(unsafe.Pointer(unsafe.SliceData(chunk.data)))
	// Return slice with valid length but full capacity
	remaining := b.length - i*capacity
	length := min(remaining, capacity)
	s := unsafe.Slice(chunkData, capacity)
	return s[:length]
}

// Chunks returns an iterator over the chunks in the buffer.
// Each chunk is yielded as a slice containing the valid data.
func (b *ChunkBuffer[T]) Chunks(yield func([]T) bool) {
	elemSize := int(unsafe.Sizeof(*new(T)))
	capacity := b.chunkSize / elemSize
	remaining := b.length
	for _, c := range b.chunks {
		if remaining == 0 {
			break
		}
		chunkData := (*T)(unsafe.Pointer(unsafe.SliceData(c.data)))
		chunkSize := min(remaining, capacity)
		typeChunk := unsafe.Slice(chunkData, chunkSize)
		if !yield(typeChunk) {
			return
		}
		remaining -= chunkSize
	}
}
