// Package bufpool offers a pool of [*bytes.Buffer] objects that are placed
// into exponentially sized buckets. This prevents rare spiky usages of big
// buffers from persisting in a pool forever.
package bufpool

import (
	"bytes"
	"sync"
)

// Get returns a buffer from the pool for the given size. Returned buffers are
// reset and ready for writes.
//
// The capacity of the returned buffer is guaranteed to be at least size.
func Get(size int) *bytes.Buffer {
	if size < 0 {
		size = 0
	}

	b := bucketMatch(uint64(size))

	buf := b.pool.Get().(*bytes.Buffer)
	buf.Reset()

	// Grow the buffer to the requested size if it's not already big enough. If
	// size is larger than the bucket's current capacity, it will immediately
	// double, causing it to flip to the next bucket boundary.
	buf.Grow(size)
	return buf
}

func bucketMatch(size uint64) *bucket {
	// Find the first bucket whose size is greater than or equal to the requested size.

	for _, b := range buckets {
		if b.size >= size {
			return b
		}
	}

	// If the requested size is larger than the largest bucket, just return the
	// largest.
	return buckets[len(buckets)-1]
}

// Put returns a buffer to the pool. The buffer is placed into an appropriate
// bucket based on its current capacity.
func Put(buf *bytes.Buffer) {
	if buf == nil {
		return
	}
	b := bucketMatch(uint64(buf.Cap()))
	b.pool.Put(buf)
}

type bucket struct {
	size uint64
	pool sync.Pool
}

var buckets []*bucket

const (
	bucketMin uint64 = 1024
	bucketMax uint64 = 1 << 36 /* 64 GiB */
)

func init() {

	// The first bucket is unsized.
	buckets = append(buckets, &bucket{
		size: 0,
		pool: sync.Pool{
			New: func() any {
				return bytes.NewBuffer(nil)
			},
		},
	})

	var bucketSize uint64 = 1024

	// Every bucket after the first bucket starts from 1024 bytes and doubles in
	// size up to 64 GiB.
	for {
		// Capture the size so New refers to the correct size per bucket.
		size := bucketSize

		buckets = append(buckets, &bucket{
			size: size,
			pool: sync.Pool{
				New: func() any {
					return bytes.NewBuffer(make([]byte, 0, size))
				},
			},
		})

		bucketSize *= 2
		if bucketSize > bucketMax {
			break
		}
	}
}
