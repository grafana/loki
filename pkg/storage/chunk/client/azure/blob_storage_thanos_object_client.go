package azure

import (
	"context"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/storage/bucket"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
)

// NewBlobStorageObjectClient makes a new BlobStorage-backed ObjectClient.
func NewBlobStorageThanosObjectClient(ctx context.Context, cfg bucket.Config, component string, logger log.Logger, hedgingCfg hedging.Config) (client.ObjectClient, error) {
	b, err := newBlobStorageThanosObjClient(ctx, cfg, component, logger, false, hedgingCfg)
	if err != nil {
		return nil, err
	}

	var hedged objstore.Bucket
	if hedgingCfg.At != 0 {
		hedged, err = newBlobStorageThanosObjClient(ctx, cfg, component, logger, true, hedgingCfg)
		if err != nil {
			return nil, err
		}
	}

	return bucket.NewObjectClientAdapter(b, hedged, logger), nil
}

func newBlobStorageThanosObjClient(ctx context.Context, cfg bucket.Config, component string, logger log.Logger, hedging bool, hedgingCfg hedging.Config) (objstore.Bucket, error) {
	if hedging {
		hedgedTrasport, err := hedgingCfg.RoundTripperWithRegisterer(nil, prometheus.WrapRegistererWithPrefix("loki_", prometheus.DefaultRegisterer))
		if err != nil {
			return nil, err
		}

		cfg.Azure.Transport = hedgedTrasport
	}

	return bucket.NewClient(ctx, bucket.Azure, cfg, component, logger)
}
