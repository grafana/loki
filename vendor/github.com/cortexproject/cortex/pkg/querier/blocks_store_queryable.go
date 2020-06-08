package querier

import (
	"context"
	"flag"
	"io"
	"sync"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gogo/protobuf/types"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/store/hintspb"
	"github.com/thanos-io/thanos/pkg/store/storepb"
	"github.com/weaveworks/common/user"
	"golang.org/x/sync/errgroup"
	grpc_metadata "google.golang.org/grpc/metadata"

	"github.com/cortexproject/cortex/pkg/querier/series"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/ring/kv"
	cortex_tsdb "github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/storegateway"
	"github.com/cortexproject/cortex/pkg/storegateway/storegatewaypb"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/services"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
)

var (
	errNoStoreGatewayAddress = errors.New("no store-gateway address configured")
)

// BlocksStoreSet is the interface used to get the clients to query series on a set of blocks.
type BlocksStoreSet interface {
	services.Service

	// GetClientsFor returns the store gateway clients that should be used to
	// query the set of blocks in input.
	GetClientsFor(blockIDs []ulid.ULID) ([]BlocksStoreClient, error)
}

// BlocksFinder is the interface used to find blocks for a given user and time range.
type BlocksFinder interface {
	services.Service

	// GetBlocks returns known blocks for userID containing samples within the range minT
	// and maxT (milliseconds, both included). Returned blocks are sorted by MaxTime descending.
	GetBlocks(userID string, minT, maxT int64) ([]*BlockMeta, map[ulid.ULID]*metadata.DeletionMark, error)
}

// BlocksStoreClient is the interface that should be implemented by any client used
// to query a backend store-gateway.
type BlocksStoreClient interface {
	storegatewaypb.StoreGatewayClient

	// RemoteAddress returns the address of the remote store-gateway and is used to uniquely
	// identify a store-gateway backend instance.
	RemoteAddress() string
}

type BlocksConsistencyCheckConfig struct {
	Enabled bool `yaml:"enabled"`
}

func (cfg *BlocksConsistencyCheckConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, prefix+".enabled", false, "Whether the querier should run a consistency check to ensure all expected blocks have been queried.")
}

// BlocksStoreQueryable is a queryable which queries blocks storage via
// the store-gateway.
type BlocksStoreQueryable struct {
	services.Service

	stores      BlocksStoreSet
	finder      BlocksFinder
	consistency *BlocksConsistencyChecker
	logger      log.Logger

	// Subservices manager.
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	// Metrics.
	storesHit prometheus.Histogram
}

func NewBlocksStoreQueryable(stores BlocksStoreSet, finder BlocksFinder, consistency *BlocksConsistencyChecker, logger log.Logger, reg prometheus.Registerer) (*BlocksStoreQueryable, error) {
	util.WarnExperimentalUse("Blocks storage engine")

	manager, err := services.NewManager(stores, finder)
	if err != nil {
		return nil, errors.Wrap(err, "register blocks storage queryable subservices")
	}

	q := &BlocksStoreQueryable{
		stores:             stores,
		finder:             finder,
		consistency:        consistency,
		logger:             logger,
		subservices:        manager,
		subservicesWatcher: services.NewFailureWatcher(),
		storesHit: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Namespace: "cortex",
			Name:      "querier_storegateway_instances_hit_per_query",
			Help:      "Number of store-gateway instances hit for a single query.",
			Buckets:   []float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		}),
	}

	q.Service = services.NewBasicService(q.starting, q.running, q.stopping)

	return q, nil
}

func NewBlocksStoreQueryableFromConfig(querierCfg Config, gatewayCfg storegateway.Config, storageCfg cortex_tsdb.Config, logger log.Logger, reg prometheus.Registerer) (*BlocksStoreQueryable, error) {
	var stores BlocksStoreSet

	bucketClient, err := cortex_tsdb.NewBucketClient(context.Background(), storageCfg, "querier", logger, reg)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create bucket client")
	}

	scanner := NewBlocksScanner(BlocksScannerConfig{
		ScanInterval:             storageCfg.BucketStore.SyncInterval,
		TenantsConcurrency:       storageCfg.BucketStore.TenantSyncConcurrency,
		MetasConcurrency:         storageCfg.BucketStore.BlockSyncConcurrency,
		CacheDir:                 storageCfg.BucketStore.SyncDir,
		ConsistencyDelay:         storageCfg.BucketStore.ConsistencyDelay,
		IgnoreDeletionMarksDelay: storageCfg.BucketStore.IgnoreDeletionMarksDelay,
	}, bucketClient, logger, reg)

	if gatewayCfg.ShardingEnabled {
		storesRingCfg := gatewayCfg.ShardingRing.ToRingConfig()
		storesRingBackend, err := kv.NewClient(storesRingCfg.KVStore, ring.GetCodec())
		if err != nil {
			return nil, errors.Wrap(err, "failed to create store-gateway ring backend")
		}

		storesRing, err := ring.NewWithStoreClientAndStrategy(storesRingCfg, storegateway.RingNameForClient, storegateway.RingKey, storesRingBackend, &storegateway.BlocksReplicationStrategy{})
		if err != nil {
			return nil, errors.Wrap(err, "failed to create store-gateway ring client")
		}

		if reg != nil {
			reg.MustRegister(storesRing)
		}

		stores, err = newBlocksStoreReplicationSet(storesRing, querierCfg.StoreGatewayClient, logger, reg)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create store set")
		}
	} else {
		if len(querierCfg.GetStoreGatewayAddresses()) == 0 {
			return nil, errNoStoreGatewayAddress
		}

		stores = newBlocksStoreBalancedSet(querierCfg.GetStoreGatewayAddresses(), querierCfg.StoreGatewayClient, logger, reg)
	}

	var consistency *BlocksConsistencyChecker
	if querierCfg.BlocksConsistencyCheck.Enabled {
		consistency = NewBlocksConsistencyChecker(
			// Exclude blocks which have been recently uploaded, in order to give enough time to store-gateways
			// to discover and load them (3 times the sync interval).
			storageCfg.BucketStore.ConsistencyDelay+(3*storageCfg.BucketStore.SyncInterval),
			// To avoid any false positive in the consistency check, we do exclude blocks which have been
			// recently marked for deletion, until the "ignore delay / 2". This means the consistency checker
			// exclude such blocks about 50% of the time before querier and store-gateway stops querying them.
			storageCfg.BucketStore.IgnoreDeletionMarksDelay/2,
			logger,
			reg,
		)
	}

	return NewBlocksStoreQueryable(stores, scanner, consistency, logger, reg)
}

func (q *BlocksStoreQueryable) starting(ctx context.Context) error {
	q.subservicesWatcher.WatchManager(q.subservices)

	if err := services.StartManagerAndAwaitHealthy(ctx, q.subservices); err != nil {
		return errors.Wrap(err, "unable to start blocks storage queryable subservices")
	}

	return nil
}

func (q *BlocksStoreQueryable) running(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-q.subservicesWatcher.Chan():
			return errors.Wrap(err, "block storage queryable subservice failed")
		}
	}
}

func (q *BlocksStoreQueryable) stopping(_ error) error {
	return services.StopManagerAndAwaitStopped(context.Background(), q.subservices)
}

// Querier returns a new Querier on the storage.
func (q *BlocksStoreQueryable) Querier(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
	if s := q.State(); s != services.Running {
		return nil, promql.ErrStorage{Err: errors.Errorf("BlocksStoreQueryable is not running: %v", s)}
	}

	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, promql.ErrStorage{Err: err}
	}

	return &blocksStoreQuerier{
		ctx:         ctx,
		minT:        mint,
		maxT:        maxt,
		userID:      userID,
		finder:      q.finder,
		stores:      q.stores,
		storesHit:   q.storesHit,
		consistency: q.consistency,
		logger:      q.logger,
	}, nil
}

type blocksStoreQuerier struct {
	ctx         context.Context
	minT, maxT  int64
	userID      string
	finder      BlocksFinder
	stores      BlocksStoreSet
	storesHit   prometheus.Histogram
	consistency *BlocksConsistencyChecker
	logger      log.Logger
}

// Select implements storage.Querier interface.
// The bool passed is ignored because the series is always sorted.
func (q *blocksStoreQuerier) Select(_ bool, sp *storage.SelectHints, matchers ...*labels.Matcher) (storage.SeriesSet, storage.Warnings, error) {
	set, warnings, err := q.selectSorted(sp, matchers...)

	// We need to wrap the error in order to have Prometheus returning a 5xx error.
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		err = promql.ErrStorage{Err: err}
	}

	return set, warnings, err
}

func (q *blocksStoreQuerier) LabelValues(name string) ([]string, storage.Warnings, error) {
	// Cortex doesn't use this. It will ask ingesters for metadata.
	return nil, nil, errors.New("not implemented")
}

func (q *blocksStoreQuerier) LabelNames() ([]string, storage.Warnings, error) {
	// Cortex doesn't use this. It will ask ingesters for metadata.
	return nil, nil, errors.New("not implemented")
}

func (q *blocksStoreQuerier) Close() error {
	return nil
}

func (q *blocksStoreQuerier) selectSorted(sp *storage.SelectHints, matchers ...*labels.Matcher) (storage.SeriesSet, storage.Warnings, error) {
	spanLog, spanCtx := spanlogger.New(q.ctx, "blocksStoreQuerier.selectSorted")
	defer spanLog.Span.Finish()

	minT, maxT := q.minT, q.maxT
	if sp != nil {
		minT, maxT = sp.Start, sp.End
	}

	// Find the list of blocks we need to query given the time range.
	metas, deletionMarks, err := q.finder.GetBlocks(q.userID, minT, maxT)
	if err != nil {
		return nil, nil, err
	}

	if len(metas) == 0 {
		if q.storesHit != nil {
			q.storesHit.Observe(0)
		}

		return series.NewEmptySeriesSet(), nil, nil
	}

	blockIDs := getULIDsFromBlockMetas(metas)
	level.Debug(spanLog).Log("expected blocks", blockIDs)

	// Find the set of store-gateway instances having the blocks.
	clients, err := q.stores.GetClientsFor(blockIDs)
	if err != nil {
		return nil, nil, err
	}
	level.Debug(spanLog).Log("num store-gateway instances", len(clients))

	req := &storepb.SeriesRequest{
		MinTime:                 minT,
		MaxTime:                 maxT,
		Matchers:                convertMatchersToLabelMatcher(matchers),
		PartialResponseStrategy: storepb.PartialResponseStrategy_ABORT,
	}

	var (
		reqCtx        = grpc_metadata.AppendToOutgoingContext(spanCtx, cortex_tsdb.TenantIDExternalLabel, q.userID)
		g, gCtx       = errgroup.WithContext(reqCtx)
		mtx           = sync.Mutex{}
		seriesSets    = []storage.SeriesSet(nil)
		warnings      = storage.Warnings(nil)
		queriedBlocks = map[string][]hintspb.Block{}
	)

	// Concurrently fetch series from all clients.
	for _, c := range clients {
		// Change variable scope since it will be used in a goroutine.
		c := c

		g.Go(func() error {
			stream, err := c.Series(gCtx, req)
			if err != nil {
				return errors.Wrapf(err, "failed to fetch series from %s", c)
			}

			mySeries := []*storepb.Series(nil)
			myWarnings := storage.Warnings(nil)
			myQueriedBlocks := []hintspb.Block(nil)

			for {
				resp, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					return errors.Wrapf(err, "failed to receive series from %s", c)
				}

				// Response may either contain series, warning or hints.
				if s := resp.GetSeries(); s != nil {
					mySeries = append(mySeries, s)
				}

				if w := resp.GetWarning(); w != "" {
					myWarnings = append(myWarnings, errors.New(w))
				}

				if h := resp.GetHints(); h != nil {
					hints := hintspb.SeriesResponseHints{}
					if err := types.UnmarshalAny(h, &hints); err != nil {
						return errors.Wrapf(err, "failed to unmarshal hints from %s", c)
					}
					myQueriedBlocks = append(myQueriedBlocks, hints.QueriedBlocks...)
				}
			}

			level.Debug(spanLog).Log("store-gateway", c, "num received series", len(mySeries), "bytes received series", countSeriesBytes(mySeries))

			// Store the result.
			mtx.Lock()
			seriesSets = append(seriesSets, &blockQuerierSeriesSet{series: mySeries})
			warnings = append(warnings, myWarnings...)
			queriedBlocks[c.RemoteAddress()] = myQueriedBlocks
			mtx.Unlock()

			return nil
		})
	}

	// Wait until all client requests complete.
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}

	level.Debug(spanLog).Log("queried blocks", queriedBlocks)
	if q.storesHit != nil {
		q.storesHit.Observe(float64(len(clients)))
	}

	// Ensure all expected blocks have been queried.
	if q.consistency != nil {
		if err := q.consistency.Check(metas, deletionMarks, queriedBlocks); err != nil {
			level.Warn(util.WithContext(q.ctx, q.logger)).Log("msg", "failed consistency check", "err", err)
			return nil, nil, err
		}
	}

	return storage.NewMergeSeriesSet(seriesSets, storage.ChainedSeriesMerge), warnings, nil
}

func countSeriesBytes(series []*storepb.Series) (count uint64) {
	for _, s := range series {
		for _, c := range s.Chunks {
			if c.Raw != nil {
				count += uint64(len(c.Raw.Data))
			}
		}
	}

	return count
}
