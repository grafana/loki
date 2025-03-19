package bufpool

import (
	"bytes"
	"math"
	"sync"
)

type bucket struct {
	size uint64
	pool sync.Pool
}

var buckets []*bucket

// Bucket sizes are exponentially sized from 1KiB to 64GiB. The max boundary is
// picked arbitrarily.
const (
	bucketMin uint64 = 1024
	bucketMax uint64 = 1 << 36 /* 64 GiB */
)

func init() {
	nextBucket := bucketMin

	for {
		// Capture the size so New refers to the correct size per bucket.
		buckets = append(buckets, &bucket{
			size: nextBucket,
			pool: sync.Pool{
				New: func() any {
					// We don't preallocate the buffer here; this will help a bucket pool
					// to be filled with buffers of varying sizes within that bucket.
					//
					// If we *did* preallocate the buffer, then any call to
					// [bytes.Buffer.Grow] beyond the bucket size would immediately cause
					// it to double in size, placing it in the next bucket.
					return bytes.NewBuffer(nil)
				},
			},
		})

		// Exponentially grow the bucket size up to bucketMax.
		nextBucket *= 2
		if nextBucket > bucketMax {
			break
		}
	}

	// Catch-all for buffers bigger than bucketMax.
	buckets = append(buckets, &bucket{
		size: math.MaxUint64,
		pool: sync.Pool{
			New: func() any {
				return bytes.NewBuffer(nil)
			},
		},
	})
}

// findBucket returns the first bucket that is large enough to hold size.
func findBucket(size uint64) *bucket {
	for _, b := range buckets {
		if b.size >= size {
			return b
		}
	}

	// We shouldn't be able to reach this point; the final bucket is sized for
	// anything, but if we do reach this we'll return the last bucket anyway.
	return buckets[len(buckets)-1]
}
