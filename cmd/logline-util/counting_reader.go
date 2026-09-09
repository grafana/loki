package main

import (
	"io"
	"sync/atomic"
)

// countingReaderAt wraps an io.ReaderAt and counts calls and bytes.
type countingReaderAt struct {
	r     io.ReaderAt
	count int64
	bytes int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	atomic.AddInt64(&c.count, 1)
	atomic.AddInt64(&c.bytes, int64(n))
	return n, err
}

func (c *countingReaderAt) reset() {
	atomic.StoreInt64(&c.count, 0)
	atomic.StoreInt64(&c.bytes, 0)
}
