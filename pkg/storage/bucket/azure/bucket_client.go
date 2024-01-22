package azure

import (
	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/exthttp"
	"github.com/thanos-io/objstore/providers/azure"
)

func NewBucketClient(cfg Config, name string, logger log.Logger) (objstore.Bucket, error) {
	// retain default endpoint if it is not explicitly configured
	endpoint := azure.DefaultConfig.Endpoint
	if cfg.EndpointSuffix != "" {
		endpoint = cfg.EndpointSuffix
	}

	bucketConfig := azure.Config{
		StorageAccountName:      cfg.StorageAccountName,
		StorageAccountKey:       cfg.StorageAccountKey.String(),
		StorageConnectionString: cfg.ConnectionString.String(),
		ContainerName:           cfg.ContainerName,
		Endpoint:                endpoint,
		MaxRetries:              cfg.MaxRetries,
		HTTPConfig: exthttp.HTTPConfig{
			IdleConnTimeout:       model.Duration(cfg.IdleConnTimeout),
			ResponseHeaderTimeout: model.Duration(cfg.ResponseHeaderTimeout),
			InsecureSkipVerify:    cfg.InsecureSkipVerify,
			TLSHandshakeTimeout:   model.Duration(cfg.TLSHandshakeTimeout),
			ExpectContinueTimeout: model.Duration(cfg.ExpectContinueTimeout),
			MaxIdleConns:          cfg.MaxIdleConns,
			MaxIdleConnsPerHost:   cfg.MaxIdleConnsPerHost,
			MaxConnsPerHost:       cfg.MaxConnsPerHost,
		},
	}

	return azure.NewBucketWithConfig(logger, bucketConfig, name)
}
