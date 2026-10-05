package store

import (
	"crypto/rand"
	"fmt"
	"io"

	"github.com/oklog/ulid/v2"
	"github.com/zeebo/xxh3"
)

// NewStorageID returns a new opaque storage ID for an index object path.
// The returned value is a ULID: lexicographically sortable, encodes
// millisecond-precision creation time, and contains 80 bits of randomness.
func NewStorageID() (string, error) {
	id, err := ulid.New(ulid.Now(), rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate storage id: %w", err)
	}
	return id.String(), nil
}

// computeIndexHash hashes an index stream using xxh3.
func computeIndexHash(r io.Reader) (string, error) {
	hasher := xxh3.New()
	if _, err := io.Copy(hasher, r); err != nil {
		return "", fmt.Errorf("hash index stream: %w", err)
	}
	return fmt.Sprintf("%016x", hasher.Sum64()), nil
}

// hashCountingWriter tees writes to an inner io.Writer while feeding the
// bytes through xxh3 and counting them. Used by PutIndexStreaming so the
// store can populate Meta.Hash and Meta.SizeBytes without reading the
// uploaded object back.
type hashCountingWriter struct {
	w      io.Writer
	hasher *xxh3.Hasher
	n      int64
}

func newHashCountingWriter(w io.Writer) *hashCountingWriter {
	return &hashCountingWriter{w: w, hasher: xxh3.New()}
}
func (w *hashCountingWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	if n > 0 {
		// xxh3.Hasher.Write never returns an error.
		_, _ = w.hasher.Write(p[:n])
		w.n += int64(n)
	}
	return n, err
}

// Sum returns the xxh3 hash of all bytes written so far, formatted as 16 hex
// digits (matching computeIndexHash's output).
func (w *hashCountingWriter) Sum() string {
	return fmt.Sprintf("%016x", w.hasher.Sum64())
}

// Count returns the total number of bytes written.
func (w *hashCountingWriter) Count() int64 {
	return w.n
}
