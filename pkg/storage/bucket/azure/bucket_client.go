package azure

import (
	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/azure"
	yaml "gopkg.in/yaml.v2"
)

func NewBucketClient(cfg Config, name string, logger log.Logger) (objstore.Bucket, error) {
	bucketConfig := azure.Config{
		StorageAccountName:      cfg.StorageAccountName,
		StorageAccountKey:       cfg.StorageAccountKey.String(),
		StorageConnectionString: cfg.ConnectionString.String(),
		ContainerName:           cfg.ContainerName,
		Endpoint:                cfg.EndpointSuffix,
		MaxRetries:              cfg.MaxRetries,
		HTTPConfig: azure.HTTPConfig{
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

	// Thanos currently doesn't support passing the config as is, but expects a YAML,
	// so we're going to serialize it.
	serialized, err := yaml.Marshal(bucketConfig)
	if err != nil {
		return nil, err
	}

	return azure.NewBucket(logger, serialized, name)
}
