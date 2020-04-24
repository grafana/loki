package querier

import (
	"context"
	"io"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/storage"
	"github.com/thanos-io/thanos/pkg/objstore"
	"github.com/thanos-io/thanos/pkg/runutil"
	"github.com/thanos-io/thanos/pkg/store/storepb"
	"github.com/weaveworks/common/logging"
	"google.golang.org/grpc/metadata"

	"github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/storegateway"
	"github.com/cortexproject/cortex/pkg/util/services"
)

// BucketStoresService wraps BucketStores into a service which triggers both the initial
// sync at startup and a periodic sync honoring configured the sync interval.
type BucketStoresService struct {
	services.Service

	cfg    tsdb.Config
	logger log.Logger
	stores *storegateway.BucketStores
}

func NewBucketStoresService(cfg tsdb.Config, bucketClient objstore.Bucket, logLevel logging.Level, logger log.Logger, registerer prometheus.Registerer) (*BucketStoresService, error) {
	var storesReg prometheus.Registerer
	if registerer != nil {
		storesReg = prometheus.WrapRegistererWithPrefix("cortex_querier_", registerer)
	}

	stores, err := storegateway.NewBucketStores(cfg, nil, bucketClient, logLevel, logger, storesReg)
	if err != nil {
		return nil, err
	}

	s := &BucketStoresService{
		cfg:    cfg,
		stores: stores,
		logger: logger,
	}

	s.Service = services.NewBasicService(s.starting, s.syncStoresLoop, nil)

	return s, nil
}

func (s *BucketStoresService) starting(ctx context.Context) error {
	if s.cfg.BucketStore.SyncInterval > 0 {
		// Run an initial blocks sync, required in order to be able to serve queries.
		if err := s.stores.InitialSync(ctx); err != nil {
			return err
		}
	}

	return nil
}

// syncStoresLoop periodically calls SyncBlocks() to synchronize the blocks for all tenants.
func (s *BucketStoresService) syncStoresLoop(ctx context.Context) error {
	// If the sync is disabled we never sync blocks, which means the bucket store
	// will be empty and no series will be returned once queried.
	if s.cfg.BucketStore.SyncInterval <= 0 {
		<-ctx.Done()
		return nil
	}

	syncInterval := s.cfg.BucketStore.SyncInterval

	// Since we've just run the initial sync, we should wait the next
	// sync interval before resynching.
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(syncInterval):
	}

	err := runutil.Repeat(syncInterval, ctx.Done(), func() error {
		level.Info(s.logger).Log("msg", "synchronizing TSDB blocks for all users")
		if err := s.stores.SyncBlocks(ctx); err != nil && err != io.EOF {
			level.Warn(s.logger).Log("msg", "failed to synchronize TSDB blocks", "err", err)
		} else {
			level.Info(s.logger).Log("msg", "successfully synchronized TSDB blocks for all users")
		}

		return nil
	})

	// This should never occur because the rununtil.Repeat() returns error
	// only if the callback function returns error (which doesn't), but since
	// we have to handle the error because of the linter, it's better to log it.
	return errors.Wrap(err, "blocks synchronization has been halted due to an unexpected error")
}

// Series makes a series request to the underlying user bucket store.
func (s *BucketStoresService) Series(ctx context.Context, userID string, req *storepb.SeriesRequest) ([]*storepb.Series, storage.Warnings, error) {
	// Inject the user ID into the context metadata, as expected by BucketStores.
	ctx = setUserIDToGRPCContext(ctx, userID)

	srv := storegateway.NewBucketStoreSeriesServer(ctx)
	err := s.stores.Series(req, srv)
	if err != nil {
		return nil, nil, err
	}

	return srv.SeriesSet, srv.Warnings, nil
}

func setUserIDToGRPCContext(ctx context.Context, userID string) context.Context {
	// We have to store it in the incoming metadata because we have to emulate the
	// case it's coming from a gRPC request, while here we're running everything in-memory.
	return metadata.NewIncomingContext(ctx, metadata.Pairs(tsdb.TenantIDExternalLabel, userID))
}
