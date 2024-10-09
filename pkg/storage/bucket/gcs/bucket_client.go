package gcs

import (
	"context"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/exthttp"
	"github.com/thanos-io/objstore/providers/gcs"
)

// NewBucketClient creates a new GCS bucket client
func NewBucketClient(ctx context.Context, cfg Config, name string, logger log.Logger) (objstore.Bucket, error) {
	bucketConfig := gcs.Config{
		Bucket:         cfg.BucketName,
		ServiceAccount: cfg.ServiceAccount.String(),
		ChunkSizeBytes: cfg.ChunkBufferSize,
		HTTPConfig: exthttp.HTTPConfig{
			IdleConnTimeout:       model.Duration(cfg.HTTP.IdleConnTimeout),
			ResponseHeaderTimeout: model.Duration(cfg.HTTP.ResponseHeaderTimeout),
			InsecureSkipVerify:    cfg.HTTP.InsecureSkipVerify,
			TLSHandshakeTimeout:   model.Duration(cfg.HTTP.TLSHandshakeTimeout),
			ExpectContinueTimeout: model.Duration(cfg.HTTP.ExpectContinueTimeout),
			MaxIdleConns:          cfg.HTTP.MaxIdleConns,
			MaxIdleConnsPerHost:   cfg.HTTP.MaxIdleConnsPerHost,
			MaxConnsPerHost:       cfg.HTTP.MaxConnsPerHost,
			Transport:             cfg.HTTP.Transport,
		},
	}

	return gcs.NewBucketWithConfig(ctx, logger, bucketConfig, name)
}
