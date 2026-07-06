package azure

import (
	"net/http"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/exthttp"
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
	bucketConfig.HTTPConfig.IdleConnTimeout       = model.Duration(cfg.HTTP.IdleConnTimeout)
	bucketConfig.HTTPConfig.ResponseHeaderTimeout = model.Duration(cfg.HTTP.ResponseHeaderTimeout)
	bucketConfig.HTTPConfig.InsecureSkipVerify    = cfg.HTTP.InsecureSkipVerify
	bucketConfig.HTTPConfig.TLSHandshakeTimeout   = model.Duration(cfg.HTTP.TLSHandshakeTimeout)
	bucketConfig.HTTPConfig.ExpectContinueTimeout = model.Duration(cfg.HTTP.ExpectContinueTimeout)
	bucketConfig.HTTPConfig.MaxIdleConns          = cfg.HTTP.MaxIdleConns
	bucketConfig.HTTPConfig.MaxIdleConnsPerHost   = cfg.HTTP.MaxIdleConnsPerHost
	bucketConfig.HTTPConfig.MaxConnsPerHost       = cfg.HTTP.MaxConnsPerHost
	bucketConfig.HTTPConfig.TLSConfig = exthttp.TLSConfig{
		CAFile:     cfg.HTTP.TLSConfig.CAPath,
		CertFile:   cfg.HTTP.TLSConfig.CertPath,
		KeyFile:    cfg.HTTP.TLSConfig.KeyPath,
		ServerName: cfg.HTTP.TLSConfig.ServerName,
	}

	if cfg.Endpoint != "" {
		// azure.DefaultConfig has the default Endpoint, overwrite it only if a different one was explicitly provided.
		bucketConfig.Endpoint = cfg.Endpoint
	}

	return factory(logger, bucketConfig, name, wrapRT)
}
