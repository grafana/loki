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
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	tsdb_errors "github.com/prometheus/prometheus/tsdb/errors"
	"github.com/thanos-io/thanos/pkg/block"
	thanos_metadata "github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/extprom"
	"github.com/thanos-io/thanos/pkg/gate"
	"github.com/thanos-io/thanos/pkg/objstore"
	"github.com/thanos-io/thanos/pkg/store"
	storecache "github.com/thanos-io/thanos/pkg/store/cache"
	"github.com/thanos-io/thanos/pkg/store/storepb"
	"github.com/weaveworks/common/logging"
	"google.golang.org/grpc/metadata"

	"github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/cortexproject/cortex/pkg/util/validation"
)

// BucketStores is a multi-tenant wrapper of Thanos BucketStore.
type BucketStores struct {
	logger             log.Logger
	cfg                tsdb.BlocksStorageConfig
	limits             *validation.Overrides
	bucket             objstore.Bucket
	logLevel           logging.Level
	bucketStoreMetrics *BucketStoreMetrics
	metaFetcherMetrics *MetadataFetcherMetrics
	filters            []block.MetadataFilter

	// Index cache shared across all tenants.
	indexCache storecache.IndexCache

	// Gate used to limit query concurrency across all tenants.
	queryGate gate.Gate

	// Keeps a bucket store for each tenant.
	storesMu sync.RWMutex
	stores   map[string]*store.BucketStore

	// Metrics.
	syncTimes       prometheus.Histogram
	syncLastSuccess prometheus.Gauge
}

// NewBucketStores makes a new BucketStores.
func NewBucketStores(cfg tsdb.BlocksStorageConfig, filters []block.MetadataFilter, bucketClient objstore.Bucket, limits *validation.Overrides, logLevel logging.Level, logger log.Logger, reg prometheus.Registerer) (*BucketStores, error) {
	cachingBucket, err := tsdb.CreateCachingBucket(cfg.BucketStore.ChunksCache, cfg.BucketStore.MetadataCache, bucketClient, logger, reg)
	if err != nil {
		return nil, errors.Wrapf(err, "create caching bucket")
	}

	// The number of concurrent queries against the tenants BucketStores are limited.
	queryGateReg := extprom.WrapRegistererWithPrefix("cortex_bucket_stores_", reg)
	queryGate := gate.NewKeeper(queryGateReg).NewGate(cfg.BucketStore.MaxConcurrent)
	promauto.With(reg).NewGauge(prometheus.GaugeOpts{
		Name: "cortex_bucket_stores_gate_queries_concurrent_max",
		Help: "Number of maximum concurrent queries allowed.",
	}).Set(float64(cfg.BucketStore.MaxConcurrent))

	u := &BucketStores{
		logger:             logger,
		cfg:                cfg,
		limits:             limits,
		bucket:             cachingBucket,
		filters:            filters,
		stores:             map[string]*store.BucketStore{},
		logLevel:           logLevel,
		bucketStoreMetrics: NewBucketStoreMetrics(),
		metaFetcherMetrics: NewMetadataFetcherMetrics(),
		queryGate:          queryGate,
		syncTimes: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "cortex_bucket_stores_blocks_sync_seconds",
			Help:    "The total time it takes to perform a sync stores",
			Buckets: []float64{0.1, 1, 10, 30, 60, 120, 300, 600, 900},
		}),
		syncLastSuccess: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "cortex_bucket_stores_blocks_last_successful_sync_timestamp_seconds",
			Help: "Unix timestamp of the last successful blocks sync.",
		}),
	}

	// Init the index cache.
	if u.indexCache, err = tsdb.NewIndexCache(cfg.BucketStore.IndexCache, logger, reg); err != nil {
		return nil, errors.Wrap(err, "create index cache")
	}

	if reg != nil {
		reg.MustRegister(u.bucketStoreMetrics, u.metaFetcherMetrics)
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
	return u.syncUsersBlocks(ctx, func(ctx context.Context, s *store.BucketStore) error {
		return s.SyncBlocks(ctx)
	})
}

func (u *BucketStores) syncUsersBlocks(ctx context.Context, f func(context.Context, *store.BucketStore) error) (returnErr error) {
	defer func(start time.Time) {
		u.syncTimes.Observe(time.Since(start).Seconds())
		if returnErr == nil {
			u.syncLastSuccess.SetToCurrentTime()
		}
	}(time.Now())

	type job struct {
		userID string
		store  *store.BucketStore
	}

	wg := &sync.WaitGroup{}
	jobs := make(chan job)
	errs := tsdb_errors.MultiError{}
	errsMx := sync.Mutex{}

	// Create a pool of workers which will synchronize blocks. The pool size
	// is limited in order to avoid to concurrently sync a lot of tenants in
	// a large cluster.
	for i := 0; i < u.cfg.BucketStore.TenantSyncConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for job := range jobs {
				if err := f(ctx, job.store); err != nil {
					errsMx.Lock()
					errs.Add(errors.Wrapf(err, "failed to synchronize TSDB blocks for user %s", job.userID))
					errsMx.Unlock()
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

	if err != nil {
		errsMx.Lock()
		errs.Add(err)
		errsMx.Unlock()
	}

	// Wait until all workers completed.
	close(jobs)
	wg.Wait()

	return errs.Err()
}

// Series makes a series request to the underlying user bucket store.
func (u *BucketStores) Series(req *storepb.SeriesRequest, srv storepb.Store_SeriesServer) error {
	spanLog, spanCtx := spanlogger.New(srv.Context(), "BucketStores.Series")
	defer spanLog.Span.Finish()

	userID := getUserIDFromGRPCContext(spanCtx)
	if userID == "" {
		return fmt.Errorf("no userID")
	}

	store := u.getStore(userID)
	if store == nil {
		return nil
	}

	return store.Series(req, spanSeriesServer{
		Store_SeriesServer: srv,
		ctx:                spanCtx,
	})
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
			// The duplicate filter has been intentionally omitted because it could cause troubles with
			// the consistency check done on the querier. The duplicate filter removes redundant blocks
			// but if the store-gateway removes redundant blocks before the querier discovers them, the
			// consistency check on the querier will fail.
		}...),
		[]block.MetadataModifier{
			// Remove Cortex external labels so that they're not injected when querying blocks.
			NewReplicaLabelRemover(userLogger, []string{
				tsdb.TenantIDExternalLabel,
				tsdb.IngesterIDExternalLabel,
				tsdb.ShardIDExternalLabel,
			}),
		},
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
		u.queryGate,
		u.cfg.BucketStore.MaxChunkPoolBytes,
		newChunksLimiterFactory(u.limits, userID),
		u.logLevel.String() == "debug", // Turn on debug logging, if the log level is set to debug
		u.cfg.BucketStore.BlockSyncConcurrency,
		nil,   // Do not limit timerange.
		false, // No need to enable backward compatibility with Thanos pre 0.8.0 queriers
		u.cfg.BucketStore.IndexCache.PostingsCompression,
		u.cfg.BucketStore.PostingOffsetsInMemSampling,
		true, // Enable series hints.
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

// ReplicaLabelRemover is a BaseFetcher modifier modifies external labels of existing blocks, it removes given replica labels from the metadata of blocks that have it.
type ReplicaLabelRemover struct {
	logger log.Logger

	replicaLabels []string
}

// NewReplicaLabelRemover creates a ReplicaLabelRemover.
func NewReplicaLabelRemover(logger log.Logger, replicaLabels []string) *ReplicaLabelRemover {
	return &ReplicaLabelRemover{logger: logger, replicaLabels: replicaLabels}
}

// Modify modifies external labels of existing blocks, it removes given replica labels from the metadata of blocks that have it.
func (r *ReplicaLabelRemover) Modify(_ context.Context, metas map[ulid.ULID]*thanos_metadata.Meta, modified *extprom.TxGaugeVec) error {
	for u, meta := range metas {
		l := meta.Thanos.Labels
		for _, replicaLabel := range r.replicaLabels {
			if _, exists := l[replicaLabel]; exists {
				level.Debug(r.logger).Log("msg", "replica label removed", "label", replicaLabel)
				delete(l, replicaLabel)
			}
		}
		metas[u].Thanos.Labels = l
	}
	return nil
}

type spanSeriesServer struct {
	storepb.Store_SeriesServer

	ctx context.Context
}

func (s spanSeriesServer) Context() context.Context {
	return s.ctx
}

func newChunksLimiterFactory(limits *validation.Overrides, userID string) store.ChunksLimiterFactory {
	return func(failedCounter prometheus.Counter) store.ChunksLimiter {
		// Since limit overrides could be live reloaded, we have to get the current user's limit
		// each time a new limiter is instantiated.
		return store.NewLimiter(uint64(limits.MaxChunksPerQuery(userID)), failedCounter)
	}
}
