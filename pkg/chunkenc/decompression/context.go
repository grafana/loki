package decompression

import (
	"context"
	"time"
)

type ctxKeyType string

const ctxKey ctxKeyType = "decompression"

// Stats is decompression statistic
type Stats struct {
	TimeDecompress    time.Duration // Time spent decompressing chunks
	TimeFiltering     time.Duration // Time spent filtering lines
	BytesDecompressed int64         // Total bytes decompressed data size
	BytesCompressed   int64         // Total bytes compressed read
}

// NewContext creates a new decompression context
func NewContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKey, &Stats{})
}

// GetStats returns decompression statistics from a context.
func GetStats(ctx context.Context) Stats {
	d, ok := ctx.Value(ctxKey).(*Stats)
	if !ok {
		return Stats{}
	}
	return *d
}

// Merge merges/adds new stats into the current context.
func Merge(ctx context.Context, m *Stats) {
	d, ok := ctx.Value(ctxKey).(*Stats)
	if !ok || m == nil {
		// Log this issue
		return
	}
	d.TimeDecompress += m.TimeDecompress
	d.TimeFiltering += m.TimeFiltering
	d.BytesDecompressed += m.BytesDecompressed
	d.BytesCompressed += m.BytesCompressed
}
