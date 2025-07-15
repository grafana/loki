package bos

import (
	"github.com/go-kit/log"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/bos"
)

func NewBucketClient(cfg Config, name string, logger log.Logger) (objstore.Bucket, error) {
	bosCfg := bos.Config{
		Endpoint:  cfg.Endpoint,
		Bucket:    cfg.Bucket,
		SecretKey: cfg.SecretKey.String(),
		AccessKey: cfg.AccessKey,
	}
	return bos.NewBucketWithConfig(logger, bosCfg, name)
}
