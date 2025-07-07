package gcs

import (
	"context"
	"net/http"

	"github.com/go-kit/log"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/gcs"
)

// NewBucketClient creates a new GCS bucket client
func NewBucketClient(ctx context.Context, cfg Config, name string, logger log.Logger, wrapRT func(http.RoundTripper) http.RoundTripper) (objstore.Bucket, error) {
	// start with default http configs
	bucketConfig := gcs.DefaultConfig
	bucketConfig.Bucket = cfg.BucketName
	bucketConfig.ServiceAccount = cfg.ServiceAccount.String()
	bucketConfig.ChunkSizeBytes = cfg.ChunkBufferSize
	bucketConfig.MaxRetries = cfg.MaxRetries
	bucketConfig.HTTPConfig.Transport = cfg.Transport

	return gcs.NewBucketWithConfig(ctx, logger, bucketConfig, name, wrapRT)
}
