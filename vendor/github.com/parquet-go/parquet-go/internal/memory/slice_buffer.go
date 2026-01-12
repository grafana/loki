package memory

import (
	"unsafe"

	"github.com/parquet-go/parquet-go/internal/unsafecast"
)

// slice is a wrapper around a slice to enable pooling.
type slice[T Datum] struct{ data []T }

func newSlice[T Datum](cap int) *slice[T] {
	return &slice[T]{data: make([]T, 0, cap)}
}

// SliceBuffer is a buffer that stores data in a single contiguous slice.
// The slice grows by moving to larger size buckets from pools as needed.
// This design provides efficient sequential access and minimal overhead for small datasets.
type SliceBuffer[T Datum] struct {
	slice *slice[T] // non-nil if data came from pool (used to return to pool on Reset)
	data  []T       // the active slice (always used for access)
}

const (
	numBuckets          = 32
	minBucketSize       = 4096   // 4 KiB (smallest bucket)
	lastShortBucketSize = 262144 // 256 KiB (transition point for growth strategy)
)

var slicePools [numBuckets]Pool[slice[byte]]

// nextBucketSize computes the next bucket size using the hybrid growth strategy:
// - Below 256KB: grow by 2x (4K, 8K, 16K, 32K, 64K, 128K, 256K)
// - Above 256KB: grow by 1.5x (384K, 576K, 864K, ...)
func nextBucketSize(size int) int {
	if size < lastShortBucketSize {
		return size * 2
	} else {
		return size + (size / 2)
	}
}

// SliceBufferFrom creates a SliceBuffer that wraps an existing slice without copying.
// The buffer takes ownership of the slice and will not return it to any pool.
func SliceBufferFrom[T Datum](data []T) SliceBuffer[T] {
	return SliceBuffer[T]{data: data}
}

// SliceBufferFor creates a SliceBuffer with pre-allocated capacity for the given number of elements.
// The buffer will be backed by a pooled slice large enough to hold cap elements.
func SliceBufferFor[T Datum](cap int) SliceBuffer[T] {
	var buf SliceBuffer[T]
	buf.Grow(cap)
	return buf
}

// reserveMore ensures the buffer has capacity for at least one more element.
//
// This is moved to a separate function to keep the complexity cost of AppendValue
// within the inlining budget.
//
//go:noinline
func (b *SliceBuffer[T]) reserveMore() { b.reserve(1) }

// reserve ensures the buffer has capacity for at least count more elements.
// It handles transitioning from external data to pooled storage and growing when needed.
// Caller must check that len(b.data)+count > cap(b.data) before calling.
func (b *SliceBuffer[T]) reserve(count int) {
	elemSize := int(unsafe.Sizeof(*new(T)))
	requiredBytes := (len(b.data) + count) * elemSize
	bucketIndex := bucketIndexOfGet(requiredBytes)

	if bucketIndex < 0 {
		// Size exceeds all buckets, allocate directly without pooling
		newCap := len(b.data) + count
		newData := make([]T, len(b.data), newCap)
		copy(newData, b.data)
		if b.slice != nil {
			putSliceToPool(b.slice, elemSize)
			b.slice = nil
		}
		b.data = newData
		return
	}

	if b.slice == nil {
		// Either empty or using external data
		b.slice = getSliceFromPool[T](bucketIndex, elemSize)
		if b.data != nil {
			// Transition from external data to pooled storage
			b.slice.data = append(b.slice.data, b.data...)
		}
		b.data = b.slice.data
	} else {
		// Already using pooled storage, grow to new bucket
		oldSlice := b.slice
		b.slice = getSliceFromPool[T](bucketIndex, elemSize)
		b.slice.data = append(b.slice.data, b.data...)
		oldSlice.data = b.data // Sync before returning to pool
		putSliceToPool(oldSlice, elemSize)
		b.data = b.slice.data
	}
}

// Append adds data to the buffer, growing the slice as needed by promoting to
// larger pool buckets.
func (b *SliceBuffer[T]) Append(data ...T) {
	if len(b.data)+len(data) > cap(b.data) {
		b.reserve(len(data))
	}
	b.data = append(b.data, data...)
}

// AppendFunc calls fn with the internal buffer and updates the buffer with the
// returned slice. This is useful for functions that follow the append pattern
// (e.g., strconv.AppendInt, jsonlite.AppendQuote) where the function may
// reallocate the slice if capacity is insufficient.
//
// If fn reallocates the slice (returns a different backing array), the buffer
// transitions to using external data and releases any pooled storage.
func (b *SliceBuffer[T]) AppendFunc(fn func([]T) []T) {
	const reserveSize = 1024

	if (cap(b.data) - len(b.data)) < reserveSize {
		// Ensure there's some extra capacity to reduce reallocations
		// when fn appends small amounts of data.
		b.reserve(reserveSize)
	}

	oldData := b.data
	newData := fn(b.data)

	if unsafe.SliceData(oldData) != unsafe.SliceData(newData) {
		if b.slice != nil {
			// Release pooled storage since fn allocated new memory
			b.slice.data = oldData
			putSliceToPool(b.slice, int(unsafe.Sizeof(*new(T))))
			b.slice = nil
		}
		b.slice = &slice[T]{data: newData}
	}

	b.data = newData
}

// AppendValue appends a single value to the buffer.
func (b *SliceBuffer[T]) AppendValue(value T) {
	if len(b.data) == cap(b.data)-1 {
		b.reserveMore()
	}
	b.data = append(b.data, value)
}

// SliceWriter wraps a SliceBuffer[byte] to provide io.Writer and other
// byte-specific write methods without generics type parameter shadowing issues.
type SliceWriter struct {
	Buffer *SliceBuffer[byte]
}

// Write appends data to the buffer and returns the number of bytes written.
// Implements io.Writer.
func (w SliceWriter) Write(p []byte) (n int, err error) {
	w.Buffer.Append(p...)
	return len(p), nil
}

// WriteString appends a string to the buffer by copying its bytes.
func (w SliceWriter) WriteString(s string) (n int, err error) {
	if len(s) == 0 {
		return 0, nil
	}
	oldLen := len(w.Buffer.data)
	if oldLen+len(s) > cap(w.Buffer.data) {
		w.Buffer.reserve(len(s))
	}
	w.Buffer.data = w.Buffer.data[:oldLen+len(s)]
	copy(w.Buffer.data[oldLen:], s)
	return len(s), nil
}

// WriteByte appends a single byte to the buffer.
func (w SliceWriter) WriteByte(c byte) error {
	w.Buffer.AppendValue(c)
	return nil
}

// Reset returns the slice to its pool and resets the buffer to empty.
func (b *SliceBuffer[T]) Reset() {
	if b.slice != nil {
		b.slice.data = b.data // Sync before returning to pool
		elemSize := int(unsafe.Sizeof(*new(T)))
		putSliceToPool(b.slice, elemSize)
		b.slice = nil
	}
	b.data = nil
}

// Cap returns the current capacity.
func (b *SliceBuffer[T]) Cap() int { return cap(b.data) }

// Len returns the number of elements currently in the buffer.
func (b *SliceBuffer[T]) Len() int { return len(b.data) }

// Slice returns a view of the current data.
// The returned slice is only valid until the next call to Append or Reset.
func (b *SliceBuffer[T]) Slice() []T { return b.data }

// Swap swaps the elements at indices i and j.
func (b *SliceBuffer[T]) Swap(i, j int) {
	b.data[i], b.data[j] = b.data[j], b.data[i]
}

// Less reports whether the element at index i is less than the element at index j.
func (b *SliceBuffer[T]) Less(i, j int) bool {
	s := b.data
	return s[i] < s[j]
}

// Grow ensures the buffer has capacity for at least n more elements.
func (b *SliceBuffer[T]) Grow(n int) {
	if n > 0 && len(b.data)+n > cap(b.data) {
		b.reserve(n)
	}
}

// Clone creates a copy of the buffer with its own pooled allocation.
// The cloned buffer is allocated from the pool with exactly the right size.
func (b *SliceBuffer[T]) Clone() SliceBuffer[T] {
	if len(b.data) == 0 {
		return SliceBuffer[T]{}
	}

	elemSize := int(unsafe.Sizeof(*new(T)))
	requiredBytes := len(b.data) * elemSize
	bucketIndex := bucketIndexOfGet(requiredBytes)

	if bucketIndex < 0 {
		// Size exceeds all buckets, allocate directly without pooling
		newData := make([]T, len(b.data))
		copy(newData, b.data)
		return SliceBuffer[T]{data: newData}
	}

	cloned := SliceBuffer[T]{
		slice: getSliceFromPool[T](bucketIndex, elemSize),
	}
	cloned.slice.data = append(cloned.slice.data, b.data...)
	cloned.data = cloned.slice.data
	return cloned
}

// Resize changes the length of the buffer to size, growing capacity if needed.
// If size is larger than the current length, the new elements contain uninitialized data.
// If size is smaller, the buffer is truncated.
func (b *SliceBuffer[T]) Resize(size int) {
	if size <= len(b.data) {
		b.data = b.data[:size]
	} else {
		if size > cap(b.data) {
			b.reserve(size - len(b.data))
		}
		b.data = b.data[:size]
	}
}

func bucketIndexOfGet(requiredBytes int) int {
	if requiredBytes <= 0 {
		return 0
	}

	// Find the smallest bucket that can hold requiredBytes
	size := minBucketSize
	for i := range numBuckets {
		if requiredBytes <= size {
			return i
		}
		size = nextBucketSize(size)
	}

	// If requiredBytes exceeds all buckets, return -1 to indicate
	// the allocation should not use pooling
	return -1
}

func bucketIndexOfPut(capacityBytes int) int {
	// When releasing buffers, some may have a capacity that is not one of the
	// bucket sizes (due to the use of append for example). In this case, we
	// return the buffer to the highest bucket with a size less or equal
	// to the buffer capacity.
	if capacityBytes < minBucketSize {
		return -1
	}

	size := minBucketSize
	for i := range numBuckets {
		nextSize := nextBucketSize(size)
		if capacityBytes < nextSize {
			return i
		}
		size = nextSize
	}

	// If we've gone through all buckets, return the last bucket
	return numBuckets - 1
}

func bucketSize(bucketIndex int) int {
	if bucketIndex < 0 || bucketIndex >= numBuckets {
		return 0
	}

	size := minBucketSize
	for range bucketIndex {
		size = nextBucketSize(size)
	}
	return size
}

func getSliceFromPool[T Datum](bucketIndex int, elemSize int) *slice[T] {
	byteSlice := slicePools[bucketIndex].Get(
		func() *slice[byte] {
			return newSlice[byte](bucketSize(bucketIndex))
		},
		func(s *slice[byte]) {
			s.data = s.data[:0]
		},
	)

	typeSlice := (*slice[T])(unsafe.Pointer(byteSlice))
	typeSlice.data = unsafecast.Slice[T](byteSlice.data)
	return typeSlice
}

func putSliceToPool[T Datum](s *slice[T], elemSize int) {
	if s == nil || s.data == nil {
		return
	}

	byteLen := cap(s.data) * elemSize
	bucketIndex := bucketIndexOfPut(byteLen)

	// If bucket index is -1, the buffer is too small to pool
	if bucketIndex < 0 {
		return
	}

	byteSlice := (*slice[byte])(unsafe.Pointer(s))
	byteSlice.data = unsafecast.Slice[byte](s.data)
	slicePools[bucketIndex].Put(byteSlice)
}

var _ SliceBuffer[byte]
