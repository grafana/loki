package gcs

import (
	"context"
	"net/http"

	"github.com/cristalhq/hedgedhttp"
	"github.com/go-kit/log"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/exthttp"
	"github.com/thanos-io/objstore/providers/gcs"

	"github.com/grafana/loki/v3/pkg/storage/bucket/instrumentation"
)

// NewBucketClient creates a new GCS bucket client
func NewBucketClient(ctx context.Context, cfg Config, name string, logger log.Logger, wrapRT func(http.RoundTripper) http.RoundTripper) (objstore.Bucket, error) {
	// start with default http configs
	bucketConfig := gcs.DefaultConfig
	bucketConfig.Bucket = cfg.BucketName
	bucketConfig.ServiceAccount = cfg.ServiceAccount.String()
	bucketConfig.ChunkSizeBytes = cfg.ChunkBufferSize
	bucketConfig.MaxRetries = cfg.MaxRetries

	if cfg.HedgeRequestsAt != 0 {
		var (
			err       error
			transport http.RoundTripper
		)

		if cfg.Transport != nil {
			transport = cfg.Transport
		} else {
			// Create default transport the same way that Thanos would create it
			transport, err = exthttp.DefaultTransport(bucketConfig.HTTPConfig)
			if err != nil {
				return nil, err
			}
		}

		// Wrap the transport into hedging transport
		var stats *hedgedhttp.Stats
		transport, stats, err = hedgedhttp.NewRoundTripperAndStats(cfg.HedgeRequestsAt, cfg.HedgeRequestsUpTo, transport)
		if err != nil {
			return nil, err
		}
		instrumentation.PublishHedgedMetrics(stats)

		bucketConfig.HTTPConfig.Transport = transport
	}

	return gcs.NewBucketWithConfig(ctx, logger, bucketConfig, name, wrapRT)
}
