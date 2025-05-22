package azure

import (
	"net/http"

	"github.com/go-kit/log"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/azure"
)

func NewBucketClient(cfg Config, name string, logger log.Logger, wrapRT func(http.RoundTripper) http.RoundTripper) (objstore.Bucket, error) {
	bucket, err := newBucketClient(cfg, name, logger, wrapRT, azure.NewBucketWithConfig)
	if err != nil {
		return nil, err
	}
	return &keyRewriteBucket{
		Bucket:    bucket,
		delimiter: cfg.ChunkDelimiter,
	}, nil
}

func newBucketClient(cfg Config, name string, logger log.Logger, wrapRT func(http.RoundTripper) http.RoundTripper, factory func(log.Logger, azure.Config, string, func(http.RoundTripper) http.RoundTripper) (*azure.Bucket, error)) (objstore.Bucket, error) {
	// Start with default config to make sure that all parameters are set to sensible values, especially
	// HTTP Config field.
	bucketConfig := azure.DefaultConfig
	bucketConfig.StorageAccountName = cfg.StorageAccountName
	bucketConfig.StorageAccountKey = cfg.StorageAccountKey.String()
	bucketConfig.StorageConnectionString = cfg.StorageConnectionString.String()
	bucketConfig.ContainerName = cfg.ContainerName
	bucketConfig.MaxRetries = cfg.MaxRetries
	bucketConfig.UserAssignedID = cfg.UserAssignedID
	bucketConfig.HTTPConfig.Transport = cfg.Transport

	if cfg.Endpoint != "" {
		// azure.DefaultConfig has the default Endpoint, overwrite it only if a different one was explicitly provided.
		bucketConfig.Endpoint = cfg.Endpoint
	}

	return factory(logger, bucketConfig, name, wrapRT)
}
