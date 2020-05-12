package storegateway

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/objstore"
	"github.com/thanos-io/thanos/pkg/store"
	storecache "github.com/thanos-io/thanos/pkg/store/cache"
	"github.com/thanos-io/thanos/pkg/store/storepb"
	"github.com/weaveworks/common/logging"
	"google.golang.org/grpc/metadata"

	"github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
)

// BucketStores is a multi-tenant wrapper of Thanos BucketStore.
type BucketStores struct {
	logger             log.Logger
	cfg                tsdb.Config
	bucket             objstore.Bucket
	logLevel           logging.Level
	bucketStoreMetrics *BucketStoreMetrics
	metaFetcherMetrics *MetadataFetcherMetrics
	indexCacheMetrics  prometheus.Collector
	filters            []block.MetadataFilter

	// Index cache shared across all tenants.
	indexCache storecache.IndexCache

	// Keeps a bucket store for each tenant.
	storesMu sync.RWMutex
	stores   map[string]*store.BucketStore

	// Metrics.
	syncTimes prometheus.Histogram
}

// NewBucketStores makes a new BucketStores.
func NewBucketStores(cfg tsdb.Config, filters []block.MetadataFilter, bucketClient objstore.Bucket, logLevel logging.Level, logger log.Logger, reg prometheus.Registerer) (*BucketStores, error) {
	indexCacheRegistry := prometheus.NewRegistry()

	u := &BucketStores{
		logger:             logger,
		cfg:                cfg,
		bucket:             bucketClient,
		filters:            filters,
		stores:             map[string]*store.BucketStore{},
		logLevel:           logLevel,
		bucketStoreMetrics: NewBucketStoreMetrics(),
		metaFetcherMetrics: NewMetadataFetcherMetrics(),
		indexCacheMetrics:  tsdb.MustNewIndexCacheMetrics(cfg.BucketStore.IndexCache.Backend, indexCacheRegistry),
		syncTimes: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "blocks_sync_seconds",
			Help:    "The total time it takes to perform a sync stores",
			Buckets: []float64{0.1, 1, 10, 30, 60, 120, 300, 600, 900},
		}),
	}

	// Init the index cache.
	var err error
	if u.indexCache, err = tsdb.NewIndexCache(cfg.BucketStore.IndexCache, logger, indexCacheRegistry); err != nil {
		return nil, errors.Wrap(err, "create index cache")
	}

	if reg != nil {
		reg.MustRegister(u.bucketStoreMetrics, u.metaFetcherMetrics, u.indexCacheMetrics)
	}

	return u, nil
}

// InitialSync does an initial synchronization of blocks for all users.
func (u *BucketStores) InitialSync(ctx context.Context) error {
	level.Info(u.logger).Log("msg", "synchronizing TSDB blocks for all users")

	if err := u.syncUsersBlocks(ctx, func(ctx context.Context, s *store.BucketStore) error {
		return s.InitialSync(ctx)
	}); err != nil {
		level.Warn(u.logger).Log("msg", "failed to synchronize TSDB blocks", "err", err)
		return err
	}

	level.Info(u.logger).Log("msg", "successfully synchronized TSDB blocks for all users")
	return nil
}

// SyncBlocks synchronizes the stores state with the Bucket store for every user.
func (u *BucketStores) SyncBlocks(ctx context.Context) error {
	if err := u.syncUsersBlocks(ctx, func(ctx context.Context, s *store.BucketStore) error {
		return s.SyncBlocks(ctx)
	}); err != nil {
		return err
	}

	return nil
}

func (u *BucketStores) syncUsersBlocks(ctx context.Context, f func(context.Context, *store.BucketStore) error) error {
	defer func(start time.Time) {
		u.syncTimes.Observe(time.Since(start).Seconds())
	}(time.Now())

	type job struct {
		userID string
		store  *store.BucketStore
	}

	wg := &sync.WaitGroup{}
	jobs := make(chan job)

	// Create a pool of workers which will synchronize blocks. The pool size
	// is limited in order to avoid to concurrently sync a lot of tenants in
	// a large cluster.
	for i := 0; i < u.cfg.BucketStore.TenantSyncConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for job := range jobs {
				if err := f(ctx, job.store); err != nil {
					level.Warn(u.logger).Log("msg", "failed to synchronize TSDB blocks for user", "user", job.userID, "err", err)
				}
			}
		}()
	}

	// Iterate the bucket, lazily create a bucket store for each new user found
	// and submit a sync job for each user.
	err := u.bucket.Iter(ctx, "", func(s string) error {
		user := strings.TrimSuffix(s, "/")

		bs, err := u.getOrCreateStore(user)
		if err != nil {
			return err
		}

		select {
		case jobs <- job{userID: user, store: bs}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	// Wait until all workers completed.
	close(jobs)
	wg.Wait()

	return err
}

// Series makes a series request to the underlying user bucket store.
func (u *BucketStores) Series(req *storepb.SeriesRequest, srv storepb.Store_SeriesServer) error {
	log, ctx := spanlogger.New(srv.Context(), "BucketStores.Series")
	defer log.Span.Finish()

	userID := getUserIDFromGRPCContext(ctx)
	if userID == "" {
		return fmt.Errorf("no userID")
	}

	store := u.getStore(userID)
	if store == nil {
		return nil
	}

	return store.Series(req, srv)
}

func (u *BucketStores) getStore(userID string) *store.BucketStore {
	u.storesMu.RLock()
	store := u.stores[userID]
	u.storesMu.RUnlock()

	return store
}

func (u *BucketStores) getOrCreateStore(userID string) (*store.BucketStore, error) {
	// Check if the store already exists.
	bs := u.getStore(userID)
	if bs != nil {
		return bs, nil
	}

	u.storesMu.Lock()
	defer u.storesMu.Unlock()

	// Check again for the store in the event it was created in-between locks.
	bs = u.stores[userID]
	if bs != nil {
		return bs, nil
	}

	userLogger := util.WithUserID(userID, u.logger)

	level.Info(userLogger).Log("msg", "creating user bucket store")

	userBkt := tsdb.NewUserBucketClient(userID, u.bucket)

	fetcherReg := prometheus.NewRegistry()
	fetcher, err := block.NewMetaFetcher(
		userLogger,
		u.cfg.BucketStore.MetaSyncConcurrency,
		userBkt,
		filepath.Join(u.cfg.BucketStore.SyncDir, userID), // The fetcher stores cached metas in the "meta-syncer/" sub directory
		fetcherReg,
		// The input filters MUST be before the ones we create here (order matters).
		append(u.filters, []block.MetadataFilter{
			block.NewConsistencyDelayMetaFilter(userLogger, u.cfg.BucketStore.ConsistencyDelay, fetcherReg),
			block.NewIgnoreDeletionMarkFilter(userLogger, userBkt, u.cfg.BucketStore.IgnoreDeletionMarksDelay),
			// Filters out duplicate blocks that can be formed from two or more overlapping
			// blocks that fully submatches the source blocks of the older blocks.
			// TODO(pracucci) can this cause troubles with the upcoming blocks sharding in the store-gateway?
			block.NewDeduplicateFilter(),
		}...),
		nil,
	)
	if err != nil {
		return nil, err
	}

	bucketStoreReg := prometheus.NewRegistry()
	bs, err = store.NewBucketStore(
		userLogger,
		bucketStoreReg,
		userBkt,
		fetcher,
		filepath.Join(u.cfg.BucketStore.SyncDir, userID),
		u.indexCache,
		uint64(u.cfg.BucketStore.MaxChunkPoolBytes),
		u.cfg.BucketStore.MaxSampleCount,
		u.cfg.BucketStore.MaxConcurrent,
		u.logLevel.String() == "debug", // Turn on debug logging, if the log level is set to debug
		u.cfg.BucketStore.BlockSyncConcurrency,
		nil,   // Do not limit timerange.
		false, // No need to enable backward compatibility with Thanos pre 0.8.0 queriers
		u.cfg.BucketStore.BinaryIndexHeader,
		u.cfg.BucketStore.IndexCache.PostingsCompression,
		u.cfg.BucketStore.PostingOffsetsInMemSampling,
	)
	if err != nil {
		return nil, err
	}

	u.stores[userID] = bs
	u.metaFetcherMetrics.AddUserRegistry(userID, fetcherReg)
	u.bucketStoreMetrics.AddUserRegistry(userID, bucketStoreReg)

	return bs, nil
}

func getUserIDFromGRPCContext(ctx context.Context) string {
	meta, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}

	values := meta.Get(tsdb.TenantIDExternalLabel)
	if len(values) != 1 {
		return ""
	}

	return values[0]
}
