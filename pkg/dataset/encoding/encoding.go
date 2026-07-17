// Package encoding defines shared types for dataset serialization formats.
package encoding

import "github.com/grafana/loki/v3/pkg/dataset/buffer"

// BufferPosting describes a buffer's location within a contiguous byte range.
type BufferPosting struct {
	BufferID buffer.ID // Must be non-zero.
	Offset   int64     // Byte offset within the range. Must be non-negative.
	Length   int64     // Length in bytes. Must be non-negative.
}
