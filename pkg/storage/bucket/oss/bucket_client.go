package oss

import (
	"github.com/go-kit/log"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/oss"
)

// NewBucketClient creates a new Alibaba Cloud OSS bucket client
func NewBucketClient(cfg Config, component string, logger log.Logger) (objstore.Bucket, error) {
	ossCfg := oss.Config{
		Endpoint:        cfg.Endpoint,
		Bucket:          cfg.Bucket,
		AccessKeyID:     cfg.AccessKeyID,
		AccessKeySecret: cfg.AccessKeySecret.String(),
	}
	return oss.NewBucketWithConfig(logger, ossCfg, component, nil)
}
