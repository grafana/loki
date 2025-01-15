// Package dataobj holds utilities for working with data objects.
package dataobj

import (
	"context"

	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// BuilderConfig configures a data object [Builder].
type BuilderConfig struct{}

// A Builder builds data objects from a set of incoming log data. Log data is
// appended to a builder by calling [Builder.Append]. Buffered log data is
// flushed manually by calling [Builder.Flush].
//
// Methods on Builder are not goroutine-safe; callers are responsible for
// synchronizing calls.
type Builder struct{}

// NewBuilder creates a new Builder which stores data objects for the specified
// tenant in a bucket.
func NewBuilder(cfg BuilderConfig, bucket objstore.Bucket, tenantID string) *Builder {
	// TODO(rfratto): implement
	_ = cfg
	_ = bucket
	_ = tenantID
	return &Builder{}
}

// Append buffers a stream to be written to a data object. If the Builder is
// full, Append returns false without appending the entry.
//
// Once a Builder is full, call [Builder.Flush] to flush the buffered data,
// then call Append again with the same entry.
func (b *Builder) Append(stream logproto.Stream) bool {
	// TODO(rfratto): implement
	_ = stream
	return true
}

// Flush flushes all buffered data to object storage. Calling Flush be result
// in a no-op if there is no buffered data to flush.
func (b *Builder) Flush(ctx context.Context) error {
	// TODO(rfratto): implement
	_ = ctx
	return nil
}
