package client

import (
	"context"
	"errors"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
)

var (
	// ErrMethodNotImplemented when any of the storage clients do not implement a method
	ErrMethodNotImplemented = errors.New("method is not implemented")
	// ErrStorageObjectNotFound when object storage does not have requested object
	ErrStorageObjectNotFound = errors.New("object not found in storage")
)

// Client is for storing and retrieving chunks.
type Client interface {
	Stop()
	PutChunks(ctx context.Context, chunks []chunk.Chunk) error
	GetChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error)
	DeleteChunk(ctx context.Context, userID, chunkID string) error
	IsChunkNotFoundErr(err error) bool
	IsRetryableErr(err error) bool
}

// ObjectAndIndexClient allows optimisations where the same client handles both
// Only used by DynamoDB (dynamodbIndexReader and dynamoDBStorageClient)
type ObjectAndIndexClient interface {
	PutChunksAndIndex(ctx context.Context, chunks []chunk.Chunk, index index.WriteBatch) error
}

// Metrics holds metric related configuration for the objstore.
type Metrics struct {
	// Registerer is the prometheus registerer that will be used by the objstore
	// to register bucket metrics.
	Registerer prometheus.Registerer
	// BucketMetrics are objstore metrics that are will wrap the bucket.
	// This field is used to share metrics among buckets with from the same
	// component.
	// This should only be set by the function that interfaces with the bucket
	// package.
	BucketMetrics *objstore.Metrics
	// HedgingBucketMetrics are objstore metrics that are will wrap the
	// heding bucket.
	// This field is used to share metrics among buckets with from the same
	// component.
	// This should only be set by the function that interfaces with the bucket
	// package.
	HedgingBucketMetrics *objstore.Metrics
}
