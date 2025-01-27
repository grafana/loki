// Package bufpool offers a pool of [*bytes.Buffer] objects that are placed
// into exponentially sized buckets.
//
// Bucketing prevents the memory cost of a pool from permanently increasing
// when a large buffer is placed into the pool.
package bufpool

import (
	"bytes"
)

// Get returns a buffer from the pool for the given size. Returned buffers are
// reset and ready for writes.
//
// The capacity of the returned buffer is guaranteed to be at least size.
func Get(size int) *bytes.Buffer {
	if size < 0 {
		size = 0
	}

	b := findBucket(uint64(size))

	buf := b.pool.Get().(*bytes.Buffer)
	buf.Reset()
	buf.Grow(size)
	return buf
}

// Put returns a buffer to the pool. The buffer is placed into an appropriate
// bucket based on its current capacity.
func Put(buf *bytes.Buffer) {
	if buf == nil {
		return
	}

	b := findBucket(uint64(buf.Cap()))
	if b == nil {
		return
	}
	b.pool.Put(buf)
}
