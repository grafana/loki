package querier

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gogo/protobuf/types"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/extprom"
	"github.com/thanos-io/thanos/pkg/store/hintspb"
	"github.com/thanos-io/thanos/pkg/store/storepb"
	"github.com/weaveworks/common/user"
	"go.uber.org/atomic"
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

const (
	// The maximum number of times we attempt fetching missing blocks from different
	// store-gateways. If no more store-gateways are left (ie. due to lower replication
	// factor) than we'll end the retries earlier.
	maxFetchSeriesAttempts = 3
)

var (
	errNoStoreGatewayAddress  = errors.New("no store-gateway address configured")
	errMaxChunksPerQueryLimit = "the query hit the max number of chunks limit while fetching chunks for %s (limit: %d)"
)

// BlocksStoreSet is the interface used to get the clients to query series on a set of blocks.
type BlocksStoreSet interface {
	services.Service

	// GetClientsFor returns the store gateway clients that should be used to
	// query the set of blocks in input. The exclude parameter is the map of
	// blocks -> store-gateway addresses that should be excluded.
	GetClientsFor(userID string, blockIDs []ulid.ULID, exclude map[ulid.ULID][]string) (map[BlocksStoreClient][]ulid.ULID, error)
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

// BlocksStoreLimits is the interface that should be implemented by the limits provider.
type BlocksStoreLimits interface {
	MaxChunksPerQuery(userID string) int
	StoreGatewayTenantShardSize(userID string) int
}

type blocksStoreQueryableMetrics struct {
	storesHit prometheus.Histogram
	refetches prometheus.Histogram
}

func newBlocksStoreQueryableMetrics(reg prometheus.Registerer) *blocksStoreQueryableMetrics {
	return &blocksStoreQueryableMetrics{
		storesHit: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Namespace: "cortex",
			Name:      "querier_storegateway_instances_hit_per_query",
			Help:      "Number of store-gateway instances hit for a single query.",
			Buckets:   []float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		}),
		refetches: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Namespace: "cortex",
			Name:      "querier_storegateway_refetches_per_query",
			Help:      "Number of re-fetches attempted while querying store-gateway instances due to missing blocks.",
			Buckets:   []float64{0, 1, 2},
		}),
	}
}

// BlocksStoreQueryable is a queryable which queries blocks storage via
// the store-gateway.
type BlocksStoreQueryable struct {
	services.Service

	stores          BlocksStoreSet
	finder          BlocksFinder
	consistency     *BlocksConsistencyChecker
	logger          log.Logger
	queryStoreAfter time.Duration
	metrics         *blocksStoreQueryableMetrics
	limits          BlocksStoreLimits

	// Subservices manager.
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher
}

func NewBlocksStoreQueryable(stores BlocksStoreSet, finder BlocksFinder, consistency *BlocksConsistencyChecker, limits BlocksStoreLimits, queryStoreAfter time.Duration, logger log.Logger, reg prometheus.Registerer) (*BlocksStoreQueryable, error) {
	util.WarnExperimentalUse("Blocks storage engine")

	manager, err := services.NewManager(stores, finder)
	if err != nil {
		return nil, errors.Wrap(err, "register blocks storage queryable subservices")
	}

	q := &BlocksStoreQueryable{
		stores:             stores,
		finder:             finder,
		consistency:        consistency,
		queryStoreAfter:    queryStoreAfter,
		logger:             logger,
		subservices:        manager,
		subservicesWatcher: services.NewFailureWatcher(),
		metrics:            newBlocksStoreQueryableMetrics(reg),
		limits:             limits,
	}

	q.Service = services.NewBasicService(q.starting, q.running, q.stopping)

	return q, nil
}

func NewBlocksStoreQueryableFromConfig(querierCfg Config, gatewayCfg storegateway.Config, storageCfg cortex_tsdb.BlocksStorageConfig, limits BlocksStoreLimits, logger log.Logger, reg prometheus.Registerer) (*BlocksStoreQueryable, error) {
	var stores BlocksStoreSet

	bucketClient, err := cortex_tsdb.NewBucketClient(context.Background(), storageCfg.Bucket, "querier", logger, reg)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create bucket client")
	}

	// Blocks scanner doesn't use chunks, but we pass config for consistency.
	cachingBucket, err := cortex_tsdb.CreateCachingBucket(storageCfg.BucketStore.ChunksCache, storageCfg.BucketStore.MetadataCache, bucketClient, logger, extprom.WrapRegistererWith(prometheus.Labels{"component": "querier"}, reg))
	if err != nil {
		return nil, errors.Wrapf(err, "create caching bucket")
	}
	bucketClient = cachingBucket

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
		storesRingBackend, err := kv.NewClient(
			storesRingCfg.KVStore,
			ring.GetCodec(),
			kv.RegistererWithKVName(reg, "querier-store-gateway"),
		)
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

		stores, err = newBlocksStoreReplicationSet(storesRing, gatewayCfg.ShardingStrategy, limits, querierCfg.StoreGatewayClient, logger, reg)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create store set")
		}
	} else {
		if len(querierCfg.GetStoreGatewayAddresses()) == 0 {
			return nil, errNoStoreGatewayAddress
		}

		stores = newBlocksStoreBalancedSet(querierCfg.GetStoreGatewayAddresses(), querierCfg.StoreGatewayClient, logger, reg)
	}

	consistency := NewBlocksConsistencyChecker(
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

	return NewBlocksStoreQueryable(stores, scanner, consistency, limits, querierCfg.QueryStoreAfter, logger, reg)
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
		return nil, errors.Errorf("BlocksStoreQueryable is not running: %v", s)
	}

	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	return &blocksStoreQuerier{
		ctx:             ctx,
		minT:            mint,
		maxT:            maxt,
		userID:          userID,
		finder:          q.finder,
		stores:          q.stores,
		metrics:         q.metrics,
		limits:          q.limits,
		consistency:     q.consistency,
		logger:          q.logger,
		queryStoreAfter: q.queryStoreAfter,
	}, nil
}

type blocksStoreQuerier struct {
	ctx         context.Context
	minT, maxT  int64
	userID      string
	finder      BlocksFinder
	stores      BlocksStoreSet
	metrics     *blocksStoreQueryableMetrics
	consistency *BlocksConsistencyChecker
	limits      BlocksStoreLimits
	logger      log.Logger

	// If set, the querier manipulates the max time to not be greater than
	// "now - queryStoreAfter" so that most recent blocks are not queried.
	queryStoreAfter time.Duration
}

// Select implements storage.Querier interface.
// The bool passed is ignored because the series is always sorted.
func (q *blocksStoreQuerier) Select(_ bool, sp *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	return q.selectSorted(sp, matchers...)
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

func (q *blocksStoreQuerier) selectSorted(sp *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	spanLog, spanCtx := spanlogger.New(q.ctx, "blocksStoreQuerier.selectSorted")
	defer spanLog.Span.Finish()

	minT, maxT := q.minT, q.maxT
	if sp != nil {
		minT, maxT = sp.Start, sp.End
	}

	// If queryStoreAfter is enabled, we do manipulate the query maxt to query samples up until
	// now - queryStoreAfter, because the most recent time range is covered by ingesters. This
	// optimization is particularly important for the blocks storage because can be used to skip
	// querying most recent not-compacted-yet blocks from the storage.
	if q.queryStoreAfter > 0 {
		now := time.Now()
		origMaxT := maxT
		maxT = util.Min64(maxT, util.TimeToMillis(now.Add(-q.queryStoreAfter)))

		if origMaxT != maxT {
			level.Debug(spanLog).Log("msg", "the max time of the query to blocks storage has been manipulated", "original", origMaxT, "updated", maxT)
		}

		if maxT < minT {
			q.metrics.storesHit.Observe(0)
			level.Debug(spanLog).Log("msg", "empty query time range after max time manipulation")
			return storage.EmptySeriesSet()
		}
	}

	// Find the list of blocks we need to query given the time range.
	knownMetas, knownDeletionMarks, err := q.finder.GetBlocks(q.userID, minT, maxT)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}

	if len(knownMetas) == 0 {
		q.metrics.storesHit.Observe(0)
		level.Debug(spanLog).Log("msg", "no blocks found")
		return storage.EmptySeriesSet()
	}

	level.Debug(spanLog).Log("msg", "found blocks to query", "expected", BlockMetas(knownMetas).String())

	var (
		// At the beginning the list of blocks to query are all known blocks.
		remainingBlocks = getULIDsFromBlockMetas(knownMetas)
		attemptedBlocks = map[ulid.ULID][]string{}
		touchedStores   = map[string]struct{}{}

		convertedMatchers = convertMatchersToLabelMatcher(matchers)
		resSeriesSets     = []storage.SeriesSet(nil)
		resWarnings       = storage.Warnings(nil)
		resQueriedBlocks  = []ulid.ULID(nil)

		maxChunksLimit  = q.limits.MaxChunksPerQuery(q.userID)
		leftChunksLimit = maxChunksLimit
	)

	for attempt := 1; attempt <= maxFetchSeriesAttempts; attempt++ {
		// Find the set of store-gateway instances having the blocks. The exclude parameter is the
		// map of blocks queried so far, with the list of store-gateway addresses for each block.
		clients, err := q.stores.GetClientsFor(q.userID, remainingBlocks, attemptedBlocks)
		if err != nil {
			// If it's a retry and we get an error, it means there are no more store-gateways left
			// from which running another attempt, so we're just stopping retrying.
			if attempt > 1 {
				level.Warn(spanLog).Log("msg", "unable to get store-gateway clients while retrying to fetch missing blocks", "err", err)
				break
			}

			return storage.ErrSeriesSet(err)
		}
		level.Debug(spanLog).Log("msg", "found store-gateway instances to query", "num instances", len(clients), "attempt", attempt)

		// Fetch series from stores. If an error occur we do not retry because retries
		// are only meant to cover missing blocks.
		seriesSets, queriedBlocks, warnings, numChunks, err := q.fetchSeriesFromStores(spanCtx, clients, minT, maxT, matchers, convertedMatchers, maxChunksLimit, leftChunksLimit)
		if err != nil {
			return storage.ErrSeriesSet(err)
		}
		level.Debug(spanLog).Log("msg", "received series from all store-gateways", "queried blocks", strings.Join(convertULIDsToString(queriedBlocks), " "))

		resSeriesSets = append(resSeriesSets, seriesSets...)
		resWarnings = append(resWarnings, warnings...)
		resQueriedBlocks = append(resQueriedBlocks, queriedBlocks...)

		// Given a single block is guaranteed to not be queried twice, we can safely decrease the number of
		// chunks we can still read before hitting the limit (max == 0 means disabled).
		if maxChunksLimit > 0 {
			leftChunksLimit -= numChunks
		}

		// Update the map of blocks we attempted to query.
		for client, blockIDs := range clients {
			touchedStores[client.RemoteAddress()] = struct{}{}

			for _, blockID := range blockIDs {
				attemptedBlocks[blockID] = append(attemptedBlocks[blockID], client.RemoteAddress())
			}
		}

		// Ensure all expected blocks have been queried (during all tries done so far).
		missingBlocks := q.consistency.Check(knownMetas, knownDeletionMarks, resQueriedBlocks)
		if len(missingBlocks) == 0 {
			q.metrics.storesHit.Observe(float64(len(touchedStores)))
			q.metrics.refetches.Observe(float64(attempt - 1))

			return series.NewSeriesSetWithWarnings(
				storage.NewMergeSeriesSet(resSeriesSets, storage.ChainedSeriesMerge),
				resWarnings)
		}

		level.Debug(spanLog).Log("msg", "consistency check failed", "attempt", attempt, "missing blocks", strings.Join(convertULIDsToString(missingBlocks), " "))

		// The next attempt should just query the missing blocks.
		remainingBlocks = missingBlocks
	}

	// We've not been able to query all expected blocks after all retries.
	err = fmt.Errorf("consistency check failed because some blocks were not queried: %s", strings.Join(convertULIDsToString(remainingBlocks), " "))
	level.Warn(util.WithContext(spanCtx, spanLog)).Log("msg", "failed consistency check", "err", err)

	return storage.ErrSeriesSet(err)
}

func (q *blocksStoreQuerier) fetchSeriesFromStores(
	ctx context.Context,
	clients map[BlocksStoreClient][]ulid.ULID,
	minT int64,
	maxT int64,
	matchers []*labels.Matcher,
	convertedMatchers []storepb.LabelMatcher,
	maxChunksLimit int,
	leftChunksLimit int,
) ([]storage.SeriesSet, []ulid.ULID, storage.Warnings, int, error) {
	var (
		reqCtx        = grpc_metadata.AppendToOutgoingContext(ctx, cortex_tsdb.TenantIDExternalLabel, q.userID)
		g, gCtx       = errgroup.WithContext(reqCtx)
		mtx           = sync.Mutex{}
		seriesSets    = []storage.SeriesSet(nil)
		warnings      = storage.Warnings(nil)
		queriedBlocks = []ulid.ULID(nil)
		numChunks     = atomic.NewInt32(0)
		spanLog       = spanlogger.FromContext(ctx)
	)

	// Concurrently fetch series from all clients.
	for c, blockIDs := range clients {
		// Change variables scope since it will be used in a goroutine.
		c := c
		blockIDs := blockIDs

		g.Go(func() error {
			req, err := createSeriesRequest(minT, maxT, convertedMatchers, blockIDs)
			if err != nil {
				return errors.Wrapf(err, "failed to create series request")
			}

			stream, err := c.Series(gCtx, req)
			if err != nil {
				return errors.Wrapf(err, "failed to fetch series from %s", c)
			}

			mySeries := []*storepb.Series(nil)
			myWarnings := storage.Warnings(nil)
			myQueriedBlocks := []ulid.ULID(nil)

			for {
				// Ensure the context hasn't been canceled in the meanwhile (eg. an error occurred
				// in another goroutine).
				if gCtx.Err() != nil {
					return gCtx.Err()
				}

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

					// Ensure the max number of chunks limit hasn't been reached (max == 0 means disabled).
					if maxChunksLimit > 0 {
						actual := numChunks.Add(int32(len(s.Chunks)))
						if actual > int32(leftChunksLimit) {
							return fmt.Errorf(errMaxChunksPerQueryLimit, convertMatchersToString(matchers), maxChunksLimit)
						}
					}
				}

				if w := resp.GetWarning(); w != "" {
					myWarnings = append(myWarnings, errors.New(w))
				}

				if h := resp.GetHints(); h != nil {
					hints := hintspb.SeriesResponseHints{}
					if err := types.UnmarshalAny(h, &hints); err != nil {
						return errors.Wrapf(err, "failed to unmarshal hints from %s", c)
					}

					ids, err := convertBlockHintsToULIDs(hints.QueriedBlocks)
					if err != nil {
						return errors.Wrapf(err, "failed to parse queried block IDs from received hints")
					}

					myQueriedBlocks = append(myQueriedBlocks, ids...)
				}
			}

			level.Debug(spanLog).Log("msg", "received series from store-gateway",
				"instance", c,
				"num series", len(mySeries),
				"bytes series", countSeriesBytes(mySeries),
				"requested blocks", strings.Join(convertULIDsToString(blockIDs), " "),
				"queried blocks", strings.Join(convertULIDsToString(myQueriedBlocks), " "))

			// Store the result.
			mtx.Lock()
			seriesSets = append(seriesSets, &blockQuerierSeriesSet{series: mySeries})
			warnings = append(warnings, myWarnings...)
			queriedBlocks = append(queriedBlocks, myQueriedBlocks...)
			mtx.Unlock()

			return nil
		})
	}

	// Wait until all client requests complete.
	if err := g.Wait(); err != nil {
		return nil, nil, nil, 0, err
	}

	return seriesSets, queriedBlocks, warnings, int(numChunks.Load()), nil
}

func createSeriesRequest(minT, maxT int64, matchers []storepb.LabelMatcher, blockIDs []ulid.ULID) (*storepb.SeriesRequest, error) {
	// Selectively query only specific blocks.
	hints := &hintspb.SeriesRequestHints{
		BlockMatchers: []storepb.LabelMatcher{
			{
				Type:  storepb.LabelMatcher_RE,
				Name:  block.BlockIDLabel,
				Value: strings.Join(convertULIDsToString(blockIDs), "|"),
			},
		},
	}

	anyHints, err := types.MarshalAny(hints)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to marshal request hints")
	}

	return &storepb.SeriesRequest{
		MinTime:                 minT,
		MaxTime:                 maxT,
		Matchers:                matchers,
		PartialResponseStrategy: storepb.PartialResponseStrategy_ABORT,
		Hints:                   anyHints,
	}, nil
}

func convertULIDsToString(ids []ulid.ULID) []string {
	res := make([]string, len(ids))
	for idx, id := range ids {
		res[idx] = id.String()
	}
	return res
}

func convertBlockHintsToULIDs(hints []hintspb.Block) ([]ulid.ULID, error) {
	res := make([]ulid.ULID, len(hints))

	for idx, hint := range hints {
		blockID, err := ulid.Parse(hint.Id)
		if err != nil {
			return nil, err
		}

		res[idx] = blockID
	}

	return res, nil
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

func convertMatchersToString(matchers []*labels.Matcher) string {
	out := strings.Builder{}
	out.WriteRune('{')

	for idx, m := range matchers {
		if idx > 0 {
			out.WriteRune(',')
		}

		out.WriteString(m.String())
	}

	out.WriteRune('}')
	return out.String()
}
