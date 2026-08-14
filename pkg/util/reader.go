package util //nolint:revive

import (
	"io"

	"go.uber.org/atomic"
)

// A CountingReader counts the total number of bytes read from an [io.Reader].
// The count can be reset, useful when used with an [io.ReadSeeker].
type CountingReader struct {
	n atomic.Int64
	r io.Reader
}

// NewCountingReader returns a new CountingReader.
func NewCountingReader(r io.Reader) *CountingReader {
	return &CountingReader{r: r}
}

// N returns the total number of bytes read from the reader.
func (r *CountingReader) N() int64 {
	return r.n.Load()
}

// Read implements the [io.Reader] interface.
func (r *CountingReader) Read(p []byte) (n int, err error) {
	n, err = r.r.Read(p)
	r.n.Add(int64(n))
	return n, err
}

// Reset the count to zero.
func (r *CountingReader) Reset() {
	r.n.Store(0)
}
