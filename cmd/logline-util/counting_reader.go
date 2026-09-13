package main

import (
	"io"

	"go.uber.org/atomic"
)

// countingReaderAt wraps an io.ReaderAt and counts calls and bytes.
type countingReaderAt struct {
	r     io.ReaderAt
	count atomic.Int64
	bytes atomic.Int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	c.count.Add(1)
	c.bytes.Add(int64(n))
	return n, err
}

func (c *countingReaderAt) reset() {
	c.count.Store(0)
	c.bytes.Store(0)
}
