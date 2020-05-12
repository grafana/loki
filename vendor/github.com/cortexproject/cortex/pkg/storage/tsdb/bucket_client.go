package tsdb

import (
	"context"

	"github.com/go-kit/kit/log"
	"github.com/thanos-io/thanos/pkg/objstore"

	"github.com/cortexproject/cortex/pkg/storage/backend/azure"
	"github.com/cortexproject/cortex/pkg/storage/backend/filesystem"
	"github.com/cortexproject/cortex/pkg/storage/backend/gcs"
	"github.com/cortexproject/cortex/pkg/storage/backend/s3"
)

// NewBucketClient creates a new bucket client based on the configured backend
func NewBucketClient(ctx context.Context, cfg Config, name string, logger log.Logger) (objstore.Bucket, error) {
	switch cfg.Backend {
	case BackendS3:
		return s3.NewBucketClient(cfg.S3, name, logger)
	case BackendGCS:
		return gcs.NewBucketClient(ctx, cfg.GCS, name, logger)
	case BackendAzure:
		return azure.NewBucketClient(cfg.Azure, name, logger)
	case BackendFilesystem:
		return filesystem.NewBucketClient(cfg.Filesystem)
	default:
		return nil, errUnsupportedStorageBackend
	}
}
