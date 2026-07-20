// Package encoding defines shared types for dataset serialization formats.
package encoding

import (
	"context"

	"github.com/grafana/loki/v3/pkg/dataset/buffer"
)

// RangeReader reads ranges of bytes from a resource. It is similar to
// io.ReaderAt but accepts a context and carries its own length.
type RangeReader interface {
	// ReadRange reads len(p) bytes starting at byte offset off.
	ReadRange(ctx context.Context, p []byte, off int64) (n int, err error)

	// Len returns the total size in bytes.
	Len() int64
}

// BufferPosting describes a buffer's location within a contiguous byte range.
type BufferPosting struct {
	BufferID buffer.ID // Must be non-zero.
	Offset   int64     // Byte offset within the range. Must be non-negative.
	Length   int64     // Length in bytes. Must be non-negative.
}
