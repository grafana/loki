// Package dataobj holds utilities for working with data objects.
package dataobj

import (
	"context"
	"errors"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/loki/pkg/push"
	"github.com/thanos-io/objstore"
)

// BuilderConfig configures a data object [Builder].
type BuilderConfig struct {
	// MaxPageSize sets a maximum size for encoded pages within the data object.
	// MaxPageSize accounts for encoding, but not for compression.
	//
	// Pages may can go above this size if a single entry within a column exceeds
	// MaxPageSize.
	MaxPageSize flagext.Bytes

	// MaxMetadataSize sets a maximum size for metadata within the data object.
	//
	// Metadata may go above this size if there is user-provided data (e.g.,
	// labels) which exceeds MaxMetadataSize.
	MaxMetadataSize flagext.Bytes

	// MaxObjectSizeBytes sets a maximum size for each data object. Once buffered
	// data in memory exceeds this size, a flush will be triggered.
	//
	// Only buffered data that fits within MaxObjectSizeBytes is flushed.
	// Remaining data can be flushed manually by calling [Builder.Flush].
	MaxObjectSizeBytes flagext.Bytes
}

// A Builder builds data objects from a set of incoming log data. Log data is
// appended to a builder by calling [Builder.Append]. Builders occasionally
// flush appended data to object storage after an Append based on its
// configuration. A flush can be manually triggered by calling [Builder.Flush].
//
// Once a builder is no longer needed, call [Builder.Close] to trigger a final
// flush and release resources.
type Builder struct {
	cfg    BuilderConfig
	bucket objstore.Bucket
}

// NewBuilder creates a new builder which stores data objects in the provided
// bucket.
func NewBuilder(cfg BuilderConfig, bucket objstore.Bucket) *Builder {
	return &Builder{
		cfg:    cfg,
		bucket: bucket,
	}
}

// Append buffers entries to be written as a data object. If enough data has
// been accumulated, Append will trigger a flush to object storage.
func (b *Builder) Append(ctx context.Context, tenantID string, entries push.PushRequest) error {
	return errors.New("not implemented")
}

// Flush triggers a flush of any appended data to object storage. Calling flush
// may result in a no-op if there is no buffered data to flush.
func (b *Builder) Flush(ctx context.Context) error {
	return errors.New("not implemented")
}

// Close triggers a final [Builder.Flush] before releasing resources. New data
// may not be appended to a closed Builder.
func (b *Builder) Close(ctx context.Context) error {
	return errors.New("not implemented")
}
