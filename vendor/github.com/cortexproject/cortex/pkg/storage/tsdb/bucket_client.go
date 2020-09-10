package tsdb

import (
	"context"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/thanos/pkg/objstore"

	"github.com/cortexproject/cortex/pkg/storage/backend/azure"
	"github.com/cortexproject/cortex/pkg/storage/backend/filesystem"
	"github.com/cortexproject/cortex/pkg/storage/backend/gcs"
	"github.com/cortexproject/cortex/pkg/storage/backend/s3"
)

// NewBucketClient creates a new bucket client based on the configured backend
func NewBucketClient(ctx context.Context, cfg BucketConfig, name string, logger log.Logger, reg prometheus.Registerer) (client objstore.Bucket, err error) {
	switch cfg.Backend {
	case BackendS3:
		client, err = s3.NewBucketClient(cfg.S3, name, logger)
	case BackendGCS:
		client, err = gcs.NewBucketClient(ctx, cfg.GCS, name, logger)
	case BackendAzure:
		client, err = azure.NewBucketClient(cfg.Azure, name, logger)
	case BackendFilesystem:
		client, err = filesystem.NewBucketClient(cfg.Filesystem)
	default:
		return nil, errUnsupportedStorageBackend
	}

	if err != nil {
		return nil, err
	}

	return objstore.NewTracingBucket(bucketWithMetrics(client, name, reg)), nil
}

func bucketWithMetrics(bucketClient objstore.Bucket, name string, reg prometheus.Registerer) objstore.Bucket {
	if reg == nil {
		return bucketClient
	}

	return objstore.BucketWithMetrics(
		"", // bucket label value
		bucketClient,
		prometheus.WrapRegistererWith(prometheus.Labels{"component": name}, reg))
}
