package ingester

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/exemplar"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/objstore"
	"github.com/thanos-io/thanos/pkg/shipper"
	"github.com/weaveworks/common/httpgrpc"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"github.com/cortexproject/cortex/pkg/chunk/encoding"
	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/storage/bucket"
	cortex_tsdb "github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/tenant"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/concurrency"
	"github.com/cortexproject/cortex/pkg/util/extract"
	logutil "github.com/cortexproject/cortex/pkg/util/log"
	util_math "github.com/cortexproject/cortex/pkg/util/math"
	"github.com/cortexproject/cortex/pkg/util/services"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/cortexproject/cortex/pkg/util/validation"
)

const (
	errTSDBCreateIncompatibleState = "cannot create a new TSDB while the ingester is not in active state (current state: %s)"
	errTSDBIngest                  = "err: %v. timestamp=%s, series=%s" // Using error.Wrap puts the message before the error and if the series is too long, its truncated.
	errTSDBIngestExemplar          = "err: %v. timestamp=%s, series=%s, exemplar=%s"

	// Jitter applied to the idle timeout to prevent compaction in all ingesters concurrently.
	compactionIdleTimeoutJitter = 0.25

	instanceIngestionRateTickInterval = time.Second
)

var (
	errExemplarRef = errors.New("exemplars not ingested because series not already present")
)

// Shipper interface is used to have an easy way to mock it in tests.
type Shipper interface {
	Sync(ctx context.Context) (uploaded int, err error)
}

type tsdbState int

const (
	active          tsdbState = iota // Pushes are allowed.
	activeShipping                   // Pushes are allowed. Blocks shipping is in progress.
	forceCompacting                  // TSDB is being force-compacted.
	closing                          // Used while closing idle TSDB.
	closed                           // Used to avoid setting closing back to active in closeAndDeleteIdleUsers method.
)

// Describes result of TSDB-close check. String is used as metric label.
type tsdbCloseCheckResult string

const (
	tsdbIdle                    tsdbCloseCheckResult = "idle" // Not reported via metrics. Metrics use tsdbIdleClosed on success.
	tsdbShippingDisabled        tsdbCloseCheckResult = "shipping_disabled"
	tsdbNotIdle                 tsdbCloseCheckResult = "not_idle"
	tsdbNotCompacted            tsdbCloseCheckResult = "not_compacted"
	tsdbNotShipped              tsdbCloseCheckResult = "not_shipped"
	tsdbCheckFailed             tsdbCloseCheckResult = "check_failed"
	tsdbCloseFailed             tsdbCloseCheckResult = "close_failed"
	tsdbNotActive               tsdbCloseCheckResult = "not_active"
	tsdbDataRemovalFailed       tsdbCloseCheckResult = "data_removal_failed"
	tsdbTenantMarkedForDeletion tsdbCloseCheckResult = "tenant_marked_for_deletion"
	tsdbIdleClosed              tsdbCloseCheckResult = "idle_closed" // Success.
)

func (r tsdbCloseCheckResult) shouldClose() bool {
	return r == tsdbIdle || r == tsdbTenantMarkedForDeletion
}

// QueryStreamType defines type of function to use when doing query-stream operation.
type QueryStreamType int

const (
	QueryStreamDefault QueryStreamType = iota // Use default configured value.
	QueryStreamSamples                        // Stream individual samples.
	QueryStreamChunks                         // Stream entire chunks.
)

type userTSDB struct {
	db             *tsdb.DB
	userID         string
	activeSeries   *ActiveSeries
	seriesInMetric *metricCounter
	limiter        *Limiter

	instanceSeriesCount *atomic.Int64 // Shared across all userTSDB instances created by ingester.
	instanceLimitsFn    func() *InstanceLimits

	stateMtx       sync.RWMutex
	state          tsdbState
	pushesInFlight sync.WaitGroup // Increased with stateMtx read lock held, only if state == active or activeShipping.

	// Used to detect idle TSDBs.
	lastUpdate atomic.Int64

	// Thanos shipper used to ship blocks to the storage.
	shipper Shipper

	// When deletion marker is found for the tenant (checked before shipping),
	// shipping stops and TSDB is closed before reaching idle timeout time (if enabled).
	deletionMarkFound atomic.Bool

	// Unix timestamp of last deletion mark check.
	lastDeletionMarkCheck atomic.Int64

	// for statistics
	ingestedAPISamples  *util_math.EwmaRate
	ingestedRuleSamples *util_math.EwmaRate

	// Cached shipped blocks.
	shippedBlocksMtx sync.Mutex
	shippedBlocks    map[ulid.ULID]struct{}
}

// Explicitly wrapping the tsdb.DB functions that we use.

func (u *userTSDB) Appender(ctx context.Context) storage.Appender {
	return u.db.Appender(ctx)
}

func (u *userTSDB) Querier(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
	return u.db.Querier(ctx, mint, maxt)
}

func (u *userTSDB) ChunkQuerier(ctx context.Context, mint, maxt int64) (storage.ChunkQuerier, error) {
	return u.db.ChunkQuerier(ctx, mint, maxt)
}

func (u *userTSDB) Head() *tsdb.Head {
	return u.db.Head()
}

func (u *userTSDB) Blocks() []*tsdb.Block {
	return u.db.Blocks()
}

func (u *userTSDB) Close() error {
	return u.db.Close()
}

func (u *userTSDB) Compact() error {
	return u.db.Compact()
}

func (u *userTSDB) StartTime() (int64, error) {
	return u.db.StartTime()
}

func (u *userTSDB) casState(from, to tsdbState) bool {
	u.stateMtx.Lock()
	defer u.stateMtx.Unlock()

	if u.state != from {
		return false
	}
	u.state = to
	return true
}

// compactHead compacts the Head block at specified block durations avoiding a single huge block.
func (u *userTSDB) compactHead(blockDuration int64) error {
	if !u.casState(active, forceCompacting) {
		return errors.New("TSDB head cannot be compacted because it is not in active state (possibly being closed or blocks shipping in progress)")
	}

	defer u.casState(forceCompacting, active)

	// Ingestion of samples in parallel with forced compaction can lead to overlapping blocks,
	// and possible invalidation of the references returned from Appender.GetRef().
	// So we wait for existing in-flight requests to finish. Future push requests would fail until compaction is over.
	u.pushesInFlight.Wait()

	h := u.Head()

	minTime, maxTime := h.MinTime(), h.MaxTime()

	for (minTime/blockDuration)*blockDuration != (maxTime/blockDuration)*blockDuration {
		// Data in Head spans across multiple block ranges, so we break it into blocks here.
		// Block max time is exclusive, so we do a -1 here.
		blockMaxTime := ((minTime/blockDuration)+1)*blockDuration - 1
		if err := u.db.CompactHead(tsdb.NewRangeHead(h, minTime, blockMaxTime)); err != nil {
			return err
		}

		// Get current min/max times after compaction.
		minTime, maxTime = h.MinTime(), h.MaxTime()
	}

	return u.db.CompactHead(tsdb.NewRangeHead(h, minTime, maxTime))
}

// PreCreation implements SeriesLifecycleCallback interface.
func (u *userTSDB) PreCreation(metric labels.Labels) error {
	if u.limiter == nil {
		return nil
	}

	// Verify ingester's global limit
	gl := u.instanceLimitsFn()
	if gl != nil && gl.MaxInMemorySeries > 0 {
		if series := u.instanceSeriesCount.Load(); series >= gl.MaxInMemorySeries {
			return errMaxSeriesLimitReached
		}
	}

	// Total series limit.
	if err := u.limiter.AssertMaxSeriesPerUser(u.userID, int(u.Head().NumSeries())); err != nil {
		return err
	}

	// Series per metric name limit.
	metricName, err := extract.MetricNameFromLabels(metric)
	if err != nil {
		return err
	}
	if err := u.seriesInMetric.canAddSeriesFor(u.userID, metricName); err != nil {
		return err
	}

	return nil
}

// PostCreation implements SeriesLifecycleCallback interface.
func (u *userTSDB) PostCreation(metric labels.Labels) {
	u.instanceSeriesCount.Inc()

	metricName, err := extract.MetricNameFromLabels(metric)
	if err != nil {
		// This should never happen because it has already been checked in PreCreation().
		return
	}
	u.seriesInMetric.increaseSeriesForMetric(metricName)
}

// PostDeletion implements SeriesLifecycleCallback interface.
func (u *userTSDB) PostDeletion(metrics ...labels.Labels) {
	u.instanceSeriesCount.Sub(int64(len(metrics)))

	for _, metric := range metrics {
		metricName, err := extract.MetricNameFromLabels(metric)
		if err != nil {
			// This should never happen because it has already been checked in PreCreation().
			continue
		}
		u.seriesInMetric.decreaseSeriesForMetric(metricName)
	}
}

// blocksToDelete filters the input blocks and returns the blocks which are safe to be deleted from the ingester.
func (u *userTSDB) blocksToDelete(blocks []*tsdb.Block) map[ulid.ULID]struct{} {
	if u.db == nil {
		return nil
	}
	deletable := tsdb.DefaultBlocksToDelete(u.db)(blocks)
	if u.shipper == nil {
		return deletable
	}

	shippedBlocks := u.getCachedShippedBlocks()

	result := map[ulid.ULID]struct{}{}
	for shippedID := range shippedBlocks {
		if _, ok := deletable[shippedID]; ok {
			result[shippedID] = struct{}{}
		}
	}
	return result
}

// updateCachedShipperBlocks reads the shipper meta file and updates the cached shipped blocks.
func (u *userTSDB) updateCachedShippedBlocks() error {
	shipperMeta, err := shipper.ReadMetaFile(u.db.Dir())
	if os.IsNotExist(err) {
		// If the meta file doesn't exist it means the shipper hasn't run yet.
		shipperMeta = &shipper.Meta{}
	} else if err != nil {
		return err
	}

	// Build a map.
	shippedBlocks := make(map[ulid.ULID]struct{}, len(shipperMeta.Uploaded))
	for _, blockID := range shipperMeta.Uploaded {
		shippedBlocks[blockID] = struct{}{}
	}

	// Cache it.
	u.shippedBlocksMtx.Lock()
	u.shippedBlocks = shippedBlocks
	u.shippedBlocksMtx.Unlock()

	return nil
}

// getCachedShippedBlocks returns the cached shipped blocks.
func (u *userTSDB) getCachedShippedBlocks() map[ulid.ULID]struct{} {
	u.shippedBlocksMtx.Lock()
	defer u.shippedBlocksMtx.Unlock()

	// It's safe to directly return the map because it's never updated in-place.
	return u.shippedBlocks
}

// getOldestUnshippedBlockTime returns the unix timestamp with milliseconds precision of the oldest
// TSDB block not shipped to the storage yet, or 0 if all blocks have been shipped.
func (u *userTSDB) getOldestUnshippedBlockTime() uint64 {
	shippedBlocks := u.getCachedShippedBlocks()
	oldestTs := uint64(0)

	for _, b := range u.Blocks() {
		if _, ok := shippedBlocks[b.Meta().ULID]; ok {
			continue
		}

		if oldestTs == 0 || b.Meta().ULID.Time() < oldestTs {
			oldestTs = b.Meta().ULID.Time()
		}
	}

	return oldestTs
}

func (u *userTSDB) isIdle(now time.Time, idle time.Duration) bool {
	lu := u.lastUpdate.Load()

	return time.Unix(lu, 0).Add(idle).Before(now)
}

func (u *userTSDB) setLastUpdate(t time.Time) {
	u.lastUpdate.Store(t.Unix())
}

// Checks if TSDB can be closed.
func (u *userTSDB) shouldCloseTSDB(idleTimeout time.Duration) tsdbCloseCheckResult {
	if u.deletionMarkFound.Load() {
		return tsdbTenantMarkedForDeletion
	}

	if !u.isIdle(time.Now(), idleTimeout) {
		return tsdbNotIdle
	}

	// If head is not compacted, we cannot close this yet.
	if u.Head().NumSeries() > 0 {
		return tsdbNotCompacted
	}

	// Ensure that all blocks have been shipped.
	if oldest := u.getOldestUnshippedBlockTime(); oldest > 0 {
		return tsdbNotShipped
	}

	return tsdbIdle
}

// TSDBState holds data structures used by the TSDB storage engine
type TSDBState struct {
	dbs    map[string]*userTSDB // tsdb sharded by userID
	bucket objstore.Bucket

	// Value used by shipper as external label.
	shipperIngesterID string

	subservices *services.Manager

	tsdbMetrics *tsdbMetrics

	forceCompactTrigger chan requestWithUsersAndCallback
	shipTrigger         chan requestWithUsersAndCallback

	// Timeout chosen for idle compactions.
	compactionIdleTimeout time.Duration

	// Number of series in memory, across all tenants.
	seriesCount atomic.Int64

	// Head compactions metrics.
	compactionsTriggered   prometheus.Counter
	compactionsFailed      prometheus.Counter
	walReplayTime          prometheus.Histogram
	appenderAddDuration    prometheus.Histogram
	appenderCommitDuration prometheus.Histogram
	idleTsdbChecks         *prometheus.CounterVec
}

type requestWithUsersAndCallback struct {
	users    *util.AllowedTenants // if nil, all tenants are allowed.
	callback chan<- struct{}      // when compaction/shipping is finished, this channel is closed
}

func newTSDBState(bucketClient objstore.Bucket, registerer prometheus.Registerer) TSDBState {
	idleTsdbChecks := promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
		Name: "cortex_ingester_idle_tsdb_checks_total",
		Help: "The total number of various results for idle TSDB checks.",
	}, []string{"result"})

	idleTsdbChecks.WithLabelValues(string(tsdbShippingDisabled))
	idleTsdbChecks.WithLabelValues(string(tsdbNotIdle))
	idleTsdbChecks.WithLabelValues(string(tsdbNotCompacted))
	idleTsdbChecks.WithLabelValues(string(tsdbNotShipped))
	idleTsdbChecks.WithLabelValues(string(tsdbCheckFailed))
	idleTsdbChecks.WithLabelValues(string(tsdbCloseFailed))
	idleTsdbChecks.WithLabelValues(string(tsdbNotActive))
	idleTsdbChecks.WithLabelValues(string(tsdbDataRemovalFailed))
	idleTsdbChecks.WithLabelValues(string(tsdbTenantMarkedForDeletion))
	idleTsdbChecks.WithLabelValues(string(tsdbIdleClosed))

	return TSDBState{
		dbs:                 make(map[string]*userTSDB),
		bucket:              bucketClient,
		tsdbMetrics:         newTSDBMetrics(registerer),
		forceCompactTrigger: make(chan requestWithUsersAndCallback),
		shipTrigger:         make(chan requestWithUsersAndCallback),

		compactionsTriggered: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Name: "cortex_ingester_tsdb_compactions_triggered_total",
			Help: "Total number of triggered compactions.",
		}),

		compactionsFailed: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Name: "cortex_ingester_tsdb_compactions_failed_total",
			Help: "Total number of compactions that failed.",
		}),
		walReplayTime: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Name:    "cortex_ingester_tsdb_wal_replay_duration_seconds",
			Help:    "The total time it takes to open and replay a TSDB WAL.",
			Buckets: prometheus.DefBuckets,
		}),
		appenderAddDuration: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Name:    "cortex_ingester_tsdb_appender_add_duration_seconds",
			Help:    "The total time it takes for a push request to add samples to the TSDB appender.",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		}),
		appenderCommitDuration: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Name:    "cortex_ingester_tsdb_appender_commit_duration_seconds",
			Help:    "The total time it takes for a push request to commit samples appended to TSDB.",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		}),

		idleTsdbChecks: idleTsdbChecks,
	}
}

// NewV2 returns a new Ingester that uses Cortex block storage instead of chunks storage.
func NewV2(cfg Config, clientConfig client.Config, limits *validation.Overrides, registerer prometheus.Registerer, logger log.Logger) (*Ingester, error) {
	bucketClient, err := bucket.NewClient(context.Background(), cfg.BlocksStorageConfig.Bucket, "ingester", logger, registerer)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the bucket client")
	}

	i := &Ingester{
		cfg:           cfg,
		clientConfig:  clientConfig,
		limits:        limits,
		chunkStore:    nil,
		usersMetadata: map[string]*userMetricsMetadata{},
		wal:           &noopWAL{},
		TSDBState:     newTSDBState(bucketClient, registerer),
		logger:        logger,
		ingestionRate: util_math.NewEWMARate(0.2, instanceIngestionRateTickInterval),
	}
	i.metrics = newIngesterMetrics(registerer, false, cfg.ActiveSeriesMetricsEnabled, i.getInstanceLimits, i.ingestionRate, &i.inflightPushRequests)

	// Replace specific metrics which we can't directly track but we need to read
	// them from the underlying system (ie. TSDB).
	if registerer != nil {
		registerer.Unregister(i.metrics.memSeries)

		promauto.With(registerer).NewGaugeFunc(prometheus.GaugeOpts{
			Name: "cortex_ingester_memory_series",
			Help: "The current number of series in memory.",
		}, i.getMemorySeriesMetric)

		promauto.With(registerer).NewGaugeFunc(prometheus.GaugeOpts{
			Name: "cortex_ingester_oldest_unshipped_block_timestamp_seconds",
			Help: "Unix timestamp of the oldest TSDB block not shipped to the storage yet. 0 if ingester has no blocks or all blocks have been shipped.",
		}, i.getOldestUnshippedBlockMetric)
	}

	i.lifecycler, err = ring.NewLifecycler(cfg.LifecyclerConfig, i, "ingester", ring.IngesterRingKey, cfg.BlocksStorageConfig.TSDB.FlushBlocksOnShutdown, registerer)
	if err != nil {
		return nil, err
	}
	i.subservicesWatcher = services.NewFailureWatcher()
	i.subservicesWatcher.WatchService(i.lifecycler)

	// Init the limter and instantiate the user states which depend on it
	i.limiter = NewLimiter(
		limits,
		i.lifecycler,
		cfg.DistributorShardingStrategy,
		cfg.DistributorShardByAllLabels,
		cfg.LifecyclerConfig.RingConfig.ReplicationFactor,
		cfg.LifecyclerConfig.RingConfig.ZoneAwarenessEnabled)

	i.TSDBState.shipperIngesterID = i.lifecycler.ID

	// Apply positive jitter only to ensure that the minimum timeout is adhered to.
	i.TSDBState.compactionIdleTimeout = util.DurationWithPositiveJitter(i.cfg.BlocksStorageConfig.TSDB.HeadCompactionIdleTimeout, compactionIdleTimeoutJitter)
	level.Info(i.logger).Log("msg", "TSDB idle compaction timeout set", "timeout", i.TSDBState.compactionIdleTimeout)

	i.BasicService = services.NewBasicService(i.startingV2, i.updateLoop, i.stoppingV2)
	return i, nil
}

// NewV2ForFlusher is a special version of ingester used by Flusher. This ingester is not ingesting anything, its only purpose is to react
// on Flush method and flush all openened TSDBs when called.
func NewV2ForFlusher(cfg Config, limits *validation.Overrides, registerer prometheus.Registerer, logger log.Logger) (*Ingester, error) {
	bucketClient, err := bucket.NewClient(context.Background(), cfg.BlocksStorageConfig.Bucket, "ingester", logger, registerer)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the bucket client")
	}

	i := &Ingester{
		cfg:       cfg,
		limits:    limits,
		wal:       &noopWAL{},
		TSDBState: newTSDBState(bucketClient, registerer),
		logger:    logger,
	}
	i.metrics = newIngesterMetrics(registerer, false, false, i.getInstanceLimits, nil, &i.inflightPushRequests)

	i.TSDBState.shipperIngesterID = "flusher"

	// This ingester will not start any subservices (lifecycler, compaction, shipping),
	// and will only open TSDBs, wait for Flush to be called, and then close TSDBs again.
	i.BasicService = services.NewIdleService(i.startingV2ForFlusher, i.stoppingV2ForFlusher)
	return i, nil
}

func (i *Ingester) startingV2ForFlusher(ctx context.Context) error {
	if err := i.openExistingTSDB(ctx); err != nil {
		// Try to rollback and close opened TSDBs before halting the ingester.
		i.closeAllTSDB()

		return errors.Wrap(err, "opening existing TSDBs")
	}

	// Don't start any sub-services (lifecycler, compaction, shipper) at all.
	return nil
}

func (i *Ingester) startingV2(ctx context.Context) error {
	if err := i.openExistingTSDB(ctx); err != nil {
		// Try to rollback and close opened TSDBs before halting the ingester.
		i.closeAllTSDB()

		return errors.Wrap(err, "opening existing TSDBs")
	}

	// Important: we want to keep lifecycler running until we ask it to stop, so we need to give it independent context
	if err := i.lifecycler.StartAsync(context.Background()); err != nil {
		return errors.Wrap(err, "failed to start lifecycler")
	}
	if err := i.lifecycler.AwaitRunning(ctx); err != nil {
		return errors.Wrap(err, "failed to start lifecycler")
	}

	// let's start the rest of subservices via manager
	servs := []services.Service(nil)

	compactionService := services.NewBasicService(nil, i.compactionLoop, nil)
	servs = append(servs, compactionService)

	if i.cfg.BlocksStorageConfig.TSDB.IsBlocksShippingEnabled() {
		shippingService := services.NewBasicService(nil, i.shipBlocksLoop, nil)
		servs = append(servs, shippingService)
	}

	if i.cfg.BlocksStorageConfig.TSDB.CloseIdleTSDBTimeout > 0 {
		interval := i.cfg.BlocksStorageConfig.TSDB.CloseIdleTSDBInterval
		if interval == 0 {
			interval = cortex_tsdb.DefaultCloseIdleTSDBInterval
		}
		closeIdleService := services.NewTimerService(interval, nil, i.closeAndDeleteIdleUserTSDBs, nil)
		servs = append(servs, closeIdleService)
	}

	var err error
	i.TSDBState.subservices, err = services.NewManager(servs...)
	if err == nil {
		err = services.StartManagerAndAwaitHealthy(ctx, i.TSDBState.subservices)
	}
	return errors.Wrap(err, "failed to start ingester components")
}

func (i *Ingester) stoppingV2ForFlusher(_ error) error {
	if !i.cfg.BlocksStorageConfig.TSDB.KeepUserTSDBOpenOnShutdown {
		i.closeAllTSDB()
	}
	return nil
}

// runs when V2 ingester is stopping
func (i *Ingester) stoppingV2(_ error) error {
	// It's important to wait until shipper is finished,
	// because the blocks transfer should start only once it's guaranteed
	// there's no shipping on-going.

	if err := services.StopManagerAndAwaitStopped(context.Background(), i.TSDBState.subservices); err != nil {
		level.Warn(i.logger).Log("msg", "failed to stop ingester subservices", "err", err)
	}

	// Next initiate our graceful exit from the ring.
	if err := services.StopAndAwaitTerminated(context.Background(), i.lifecycler); err != nil {
		level.Warn(i.logger).Log("msg", "failed to stop ingester lifecycler", "err", err)
	}

	if !i.cfg.BlocksStorageConfig.TSDB.KeepUserTSDBOpenOnShutdown {
		i.closeAllTSDB()
	}
	return nil
}

func (i *Ingester) updateLoop(ctx context.Context) error {
	if limits := i.getInstanceLimits(); limits != nil && *limits != (InstanceLimits{}) {
		// This check will not cover enabling instance limits in runtime, but it will do for now.
		logutil.WarnExperimentalUse("ingester instance limits")
	}

	rateUpdateTicker := time.NewTicker(i.cfg.RateUpdatePeriod)
	defer rateUpdateTicker.Stop()

	ingestionRateTicker := time.NewTicker(instanceIngestionRateTickInterval)
	defer ingestionRateTicker.Stop()

	var activeSeriesTickerChan <-chan time.Time
	if i.cfg.ActiveSeriesMetricsEnabled {
		t := time.NewTicker(i.cfg.ActiveSeriesMetricsUpdatePeriod)
		activeSeriesTickerChan = t.C
		defer t.Stop()
	}

	// Similarly to the above, this is a hardcoded value.
	metadataPurgeTicker := time.NewTicker(metadataPurgePeriod)
	defer metadataPurgeTicker.Stop()

	for {
		select {
		case <-metadataPurgeTicker.C:
			i.purgeUserMetricsMetadata()
		case <-ingestionRateTicker.C:
			i.ingestionRate.Tick()
		case <-rateUpdateTicker.C:
			i.userStatesMtx.RLock()
			for _, db := range i.TSDBState.dbs {
				db.ingestedAPISamples.Tick()
				db.ingestedRuleSamples.Tick()
			}
			i.userStatesMtx.RUnlock()

		case <-activeSeriesTickerChan:
			i.v2UpdateActiveSeries()

		case <-ctx.Done():
			return nil
		case err := <-i.subservicesWatcher.Chan():
			return errors.Wrap(err, "ingester subservice failed")
		}
	}
}

func (i *Ingester) v2UpdateActiveSeries() {
	purgeTime := time.Now().Add(-i.cfg.ActiveSeriesMetricsIdleTimeout)

	for _, userID := range i.getTSDBUsers() {
		userDB := i.getTSDB(userID)
		if userDB == nil {
			continue
		}

		userDB.activeSeries.Purge(purgeTime)
		i.metrics.activeSeriesPerUser.WithLabelValues(userID).Set(float64(userDB.activeSeries.Active()))
	}
}

// GetRef() is an extra method added to TSDB to let Cortex check before calling Add()
type extendedAppender interface {
	storage.Appender
	storage.GetRef
}

// v2Push adds metrics to a block
func (i *Ingester) v2Push(ctx context.Context, req *cortexpb.WriteRequest) (*cortexpb.WriteResponse, error) {
	var firstPartialErr error

	// NOTE: because we use `unsafe` in deserialisation, we must not
	// retain anything from `req` past the call to ReuseSlice
	defer cortexpb.ReuseSlice(req.Timeseries)

	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	il := i.getInstanceLimits()
	if il != nil && il.MaxIngestionRate > 0 {
		if rate := i.ingestionRate.Rate(); rate >= il.MaxIngestionRate {
			return nil, errMaxSamplesPushRateLimitReached
		}
	}

	db, err := i.getOrCreateTSDB(userID, false)
	if err != nil {
		return nil, wrapWithUser(err, userID)
	}

	// Ensure the ingester shutdown procedure hasn't started
	i.userStatesMtx.RLock()
	if i.stopped {
		i.userStatesMtx.RUnlock()
		return nil, errIngesterStopping
	}
	i.userStatesMtx.RUnlock()

	if err := db.acquireAppendLock(); err != nil {
		return &cortexpb.WriteResponse{}, httpgrpc.Errorf(http.StatusServiceUnavailable, wrapWithUser(err, userID).Error())
	}
	defer db.releaseAppendLock()

	// Given metadata is a best-effort approach, and we don't halt on errors
	// process it before samples. Otherwise, we risk returning an error before ingestion.
	ingestedMetadata := i.pushMetadata(ctx, userID, req.GetMetadata())

	// Keep track of some stats which are tracked only if the samples will be
	// successfully committed
	var (
		succeededSamplesCount     = 0
		failedSamplesCount        = 0
		succeededExemplarsCount   = 0
		failedExemplarsCount      = 0
		startAppend               = time.Now()
		sampleOutOfBoundsCount    = 0
		sampleOutOfOrderCount     = 0
		newValueForTimestampCount = 0
		perUserSeriesLimitCount   = 0
		perMetricSeriesLimitCount = 0

		updateFirstPartial = func(errFn func() error) {
			if firstPartialErr == nil {
				firstPartialErr = errFn()
			}
		}
	)

	// Walk the samples, appending them to the users database
	app := db.Appender(ctx).(extendedAppender)
	for _, ts := range req.Timeseries {
		// The labels must be sorted (in our case, it's guaranteed a write request
		// has sorted labels once hit the ingester).

		// Look up a reference for this series.
		ref, copiedLabels := app.GetRef(cortexpb.FromLabelAdaptersToLabels(ts.Labels))

		// To find out if any sample was added to this series, we keep old value.
		oldSucceededSamplesCount := succeededSamplesCount

		for _, s := range ts.Samples {
			var err error

			// If the cached reference exists, we try to use it.
			if ref != 0 {
				if _, err = app.Append(ref, copiedLabels, s.TimestampMs, s.Value); err == nil {
					succeededSamplesCount++
					continue
				}

			} else {
				// Copy the label set because both TSDB and the active series tracker may retain it.
				copiedLabels = cortexpb.FromLabelAdaptersToLabelsWithCopy(ts.Labels)

				// Retain the reference in case there are multiple samples for the series.
				if ref, err = app.Append(0, copiedLabels, s.TimestampMs, s.Value); err == nil {
					succeededSamplesCount++
					continue
				}
			}

			failedSamplesCount++

			// Check if the error is a soft error we can proceed on. If so, we keep track
			// of it, so that we can return it back to the distributor, which will return a
			// 400 error to the client. The client (Prometheus) will not retry on 400, and
			// we actually ingested all samples which haven't failed.
			switch cause := errors.Cause(err); cause {
			case storage.ErrOutOfBounds:
				sampleOutOfBoundsCount++
				updateFirstPartial(func() error { return wrappedTSDBIngestErr(err, model.Time(s.TimestampMs), ts.Labels) })
				continue

			case storage.ErrOutOfOrderSample:
				sampleOutOfOrderCount++
				updateFirstPartial(func() error { return wrappedTSDBIngestErr(err, model.Time(s.TimestampMs), ts.Labels) })
				continue

			case storage.ErrDuplicateSampleForTimestamp:
				newValueForTimestampCount++
				updateFirstPartial(func() error { return wrappedTSDBIngestErr(err, model.Time(s.TimestampMs), ts.Labels) })
				continue

			case errMaxSeriesPerUserLimitExceeded:
				perUserSeriesLimitCount++
				updateFirstPartial(func() error { return makeLimitError(perUserSeriesLimit, i.limiter.FormatError(userID, cause)) })
				continue

			case errMaxSeriesPerMetricLimitExceeded:
				perMetricSeriesLimitCount++
				updateFirstPartial(func() error {
					return makeMetricLimitError(perMetricSeriesLimit, copiedLabels, i.limiter.FormatError(userID, cause))
				})
				continue
			}

			// The error looks an issue on our side, so we should rollback
			if rollbackErr := app.Rollback(); rollbackErr != nil {
				level.Warn(i.logger).Log("msg", "failed to rollback on error", "user", userID, "err", rollbackErr)
			}

			return nil, wrapWithUser(err, userID)
		}

		if i.cfg.ActiveSeriesMetricsEnabled && succeededSamplesCount > oldSucceededSamplesCount {
			db.activeSeries.UpdateSeries(cortexpb.FromLabelAdaptersToLabels(ts.Labels), startAppend, func(l labels.Labels) labels.Labels {
				// we must already have copied the labels if succeededSamplesCount has been incremented.
				return copiedLabels
			})
		}

		if i.cfg.BlocksStorageConfig.TSDB.MaxExemplars > 0 {
			// app.AppendExemplar currently doesn't create the series, it must
			// already exist.  If it does not then drop.
			if ref == 0 && len(ts.Exemplars) > 0 {
				updateFirstPartial(func() error {
					return wrappedTSDBIngestExemplarErr(errExemplarRef,
						model.Time(ts.Exemplars[0].TimestampMs), ts.Labels, ts.Exemplars[0].Labels)
				})
				failedExemplarsCount += len(ts.Exemplars)
			} else { // Note that else is explicit, rather than a continue in the above if, in case of additional logic post exemplar processing.
				for _, ex := range ts.Exemplars {
					e := exemplar.Exemplar{
						Value:  ex.Value,
						Ts:     ex.TimestampMs,
						HasTs:  true,
						Labels: cortexpb.FromLabelAdaptersToLabelsWithCopy(ex.Labels),
					}

					if _, err = app.AppendExemplar(ref, nil, e); err == nil {
						succeededExemplarsCount++
						continue
					}

					// Error adding exemplar
					updateFirstPartial(func() error {
						return wrappedTSDBIngestExemplarErr(err, model.Time(ex.TimestampMs), ts.Labels, ex.Labels)
					})
					failedExemplarsCount++
				}
			}
		}
	}

	// At this point all samples have been added to the appender, so we can track the time it took.
	i.TSDBState.appenderAddDuration.Observe(time.Since(startAppend).Seconds())

	startCommit := time.Now()
	if err := app.Commit(); err != nil {
		return nil, wrapWithUser(err, userID)
	}
	i.TSDBState.appenderCommitDuration.Observe(time.Since(startCommit).Seconds())

	// If only invalid samples are pushed, don't change "last update", as TSDB was not modified.
	if succeededSamplesCount > 0 {
		db.setLastUpdate(time.Now())
	}

	// Increment metrics only if the samples have been successfully committed.
	// If the code didn't reach this point, it means that we returned an error
	// which will be converted into an HTTP 5xx and the client should/will retry.
	i.metrics.ingestedSamples.Add(float64(succeededSamplesCount))
	i.metrics.ingestedSamplesFail.Add(float64(failedSamplesCount))
	i.metrics.ingestedExemplars.Add(float64(succeededExemplarsCount))
	i.metrics.ingestedExemplarsFail.Add(float64(failedExemplarsCount))

	if sampleOutOfBoundsCount > 0 {
		validation.DiscardedSamples.WithLabelValues(sampleOutOfBounds, userID).Add(float64(sampleOutOfBoundsCount))
	}
	if sampleOutOfOrderCount > 0 {
		validation.DiscardedSamples.WithLabelValues(sampleOutOfOrder, userID).Add(float64(sampleOutOfOrderCount))
	}
	if newValueForTimestampCount > 0 {
		validation.DiscardedSamples.WithLabelValues(newValueForTimestamp, userID).Add(float64(newValueForTimestampCount))
	}
	if perUserSeriesLimitCount > 0 {
		validation.DiscardedSamples.WithLabelValues(perUserSeriesLimit, userID).Add(float64(perUserSeriesLimitCount))
	}
	if perMetricSeriesLimitCount > 0 {
		validation.DiscardedSamples.WithLabelValues(perMetricSeriesLimit, userID).Add(float64(perMetricSeriesLimitCount))
	}

	// Distributor counts both samples and metadata, so for consistency ingester does the same.
	i.ingestionRate.Add(int64(succeededSamplesCount + ingestedMetadata))

	switch req.Source {
	case cortexpb.RULE:
		db.ingestedRuleSamples.Add(int64(succeededSamplesCount))
	case cortexpb.API:
		fallthrough
	default:
		db.ingestedAPISamples.Add(int64(succeededSamplesCount))
	}

	if firstPartialErr != nil {
		code := http.StatusBadRequest
		var ve *validationError
		if errors.As(firstPartialErr, &ve) {
			code = ve.code
		}
		return &cortexpb.WriteResponse{}, httpgrpc.Errorf(code, wrapWithUser(firstPartialErr, userID).Error())
	}

	return &cortexpb.WriteResponse{}, nil
}

func (u *userTSDB) acquireAppendLock() error {
	u.stateMtx.RLock()
	defer u.stateMtx.RUnlock()

	switch u.state {
	case active:
	case activeShipping:
		// Pushes are allowed.
	case forceCompacting:
		return errors.New("forced compaction in progress")
	case closing:
		return errors.New("TSDB is closing")
	default:
		return errors.New("TSDB is not active")
	}

	u.pushesInFlight.Add(1)
	return nil
}

func (u *userTSDB) releaseAppendLock() {
	u.pushesInFlight.Done()
}

func (i *Ingester) v2Query(ctx context.Context, req *client.QueryRequest) (*client.QueryResponse, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	from, through, matchers, err := client.FromQueryRequest(req)
	if err != nil {
		return nil, err
	}

	i.metrics.queries.Inc()

	db := i.getTSDB(userID)
	if db == nil {
		return &client.QueryResponse{}, nil
	}

	q, err := db.Querier(ctx, int64(from), int64(through))
	if err != nil {
		return nil, err
	}
	defer q.Close()

	// It's not required to return sorted series because series are sorted by the Cortex querier.
	ss := q.Select(false, nil, matchers...)
	if ss.Err() != nil {
		return nil, ss.Err()
	}

	numSamples := 0

	result := &client.QueryResponse{}
	for ss.Next() {
		series := ss.At()

		ts := cortexpb.TimeSeries{
			Labels: cortexpb.FromLabelsToLabelAdapters(series.Labels()),
		}

		it := series.Iterator()
		for it.Next() {
			t, v := it.At()
			ts.Samples = append(ts.Samples, cortexpb.Sample{Value: v, TimestampMs: t})
		}

		numSamples += len(ts.Samples)
		result.Timeseries = append(result.Timeseries, ts)
	}

	i.metrics.queriedSeries.Observe(float64(len(result.Timeseries)))
	i.metrics.queriedSamples.Observe(float64(numSamples))

	return result, ss.Err()
}

func (i *Ingester) v2LabelValues(ctx context.Context, req *client.LabelValuesRequest) (*client.LabelValuesResponse, error) {
	labelName, startTimestampMs, endTimestampMs, matchers, err := client.FromLabelValuesRequest(req)
	if err != nil {
		return nil, err
	}

	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	db := i.getTSDB(userID)
	if db == nil {
		return &client.LabelValuesResponse{}, nil
	}

	mint, maxt, err := metadataQueryRange(startTimestampMs, endTimestampMs, db)
	if err != nil {
		return nil, err
	}

	q, err := db.Querier(ctx, mint, maxt)
	if err != nil {
		return nil, err
	}
	defer q.Close()

	vals, _, err := q.LabelValues(labelName, matchers...)
	if err != nil {
		return nil, err
	}

	return &client.LabelValuesResponse{
		LabelValues: vals,
	}, nil
}

func (i *Ingester) v2LabelNames(ctx context.Context, req *client.LabelNamesRequest) (*client.LabelNamesResponse, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	db := i.getTSDB(userID)
	if db == nil {
		return &client.LabelNamesResponse{}, nil
	}

	mint, maxt, err := metadataQueryRange(req.StartTimestampMs, req.EndTimestampMs, db)
	if err != nil {
		return nil, err
	}

	q, err := db.Querier(ctx, mint, maxt)
	if err != nil {
		return nil, err
	}
	defer q.Close()

	names, _, err := q.LabelNames()
	if err != nil {
		return nil, err
	}

	return &client.LabelNamesResponse{
		LabelNames: names,
	}, nil
}

func (i *Ingester) v2MetricsForLabelMatchers(ctx context.Context, req *client.MetricsForLabelMatchersRequest) (*client.MetricsForLabelMatchersResponse, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	db := i.getTSDB(userID)
	if db == nil {
		return &client.MetricsForLabelMatchersResponse{}, nil
	}

	// Parse the request
	_, _, matchersSet, err := client.FromMetricsForLabelMatchersRequest(req)
	if err != nil {
		return nil, err
	}

	mint, maxt, err := metadataQueryRange(req.StartTimestampMs, req.EndTimestampMs, db)
	if err != nil {
		return nil, err
	}

	q, err := db.Querier(ctx, mint, maxt)
	if err != nil {
		return nil, err
	}
	defer q.Close()

	// Run a query for each matchers set and collect all the results.
	var sets []storage.SeriesSet

	for _, matchers := range matchersSet {
		// Interrupt if the context has been canceled.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		hints := &storage.SelectHints{
			Start: mint,
			End:   maxt,
			Func:  "series", // There is no series function, this token is used for lookups that don't need samples.
		}

		seriesSet := q.Select(true, hints, matchers...)
		sets = append(sets, seriesSet)
	}

	// Generate the response merging all series sets.
	result := &client.MetricsForLabelMatchersResponse{
		Metric: make([]*cortexpb.Metric, 0),
	}

	mergedSet := storage.NewMergeSeriesSet(sets, storage.ChainedSeriesMerge)
	for mergedSet.Next() {
		// Interrupt if the context has been canceled.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		result.Metric = append(result.Metric, &cortexpb.Metric{
			Labels: cortexpb.FromLabelsToLabelAdapters(mergedSet.At().Labels()),
		})
	}

	return result, nil
}

func (i *Ingester) v2UserStats(ctx context.Context, req *client.UserStatsRequest) (*client.UserStatsResponse, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	db := i.getTSDB(userID)
	if db == nil {
		return &client.UserStatsResponse{}, nil
	}

	return createUserStats(db), nil
}

func (i *Ingester) v2AllUserStats(ctx context.Context, req *client.UserStatsRequest) (*client.UsersStatsResponse, error) {
	i.userStatesMtx.RLock()
	defer i.userStatesMtx.RUnlock()

	users := i.TSDBState.dbs

	response := &client.UsersStatsResponse{
		Stats: make([]*client.UserIDStatsResponse, 0, len(users)),
	}
	for userID, db := range users {
		response.Stats = append(response.Stats, &client.UserIDStatsResponse{
			UserId: userID,
			Data:   createUserStats(db),
		})
	}
	return response, nil
}

func createUserStats(db *userTSDB) *client.UserStatsResponse {
	apiRate := db.ingestedAPISamples.Rate()
	ruleRate := db.ingestedRuleSamples.Rate()
	return &client.UserStatsResponse{
		IngestionRate:     apiRate + ruleRate,
		ApiIngestionRate:  apiRate,
		RuleIngestionRate: ruleRate,
		NumSeries:         db.Head().NumSeries(),
	}
}

const queryStreamBatchMessageSize = 1 * 1024 * 1024

// v2QueryStream streams metrics from a TSDB. This implements the client.IngesterServer interface
func (i *Ingester) v2QueryStream(req *client.QueryRequest, stream client.Ingester_QueryStreamServer) error {
	spanlog, ctx := spanlogger.New(stream.Context(), "v2QueryStream")
	defer spanlog.Finish()

	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return err
	}

	from, through, matchers, err := client.FromQueryRequest(req)
	if err != nil {
		return err
	}

	i.metrics.queries.Inc()

	db := i.getTSDB(userID)
	if db == nil {
		return nil
	}

	numSamples := 0
	numSeries := 0

	streamType := QueryStreamSamples
	if i.cfg.StreamChunksWhenUsingBlocks {
		streamType = QueryStreamChunks
	}

	if i.cfg.StreamTypeFn != nil {
		runtimeType := i.cfg.StreamTypeFn()
		switch runtimeType {
		case QueryStreamChunks:
			streamType = QueryStreamChunks
		case QueryStreamSamples:
			streamType = QueryStreamSamples
		default:
			// no change from config value.
		}
	}

	if streamType == QueryStreamChunks {
		level.Debug(spanlog).Log("msg", "using v2QueryStreamChunks")
		numSeries, numSamples, err = i.v2QueryStreamChunks(ctx, db, int64(from), int64(through), matchers, stream)
	} else {
		level.Debug(spanlog).Log("msg", "using v2QueryStreamSamples")
		numSeries, numSamples, err = i.v2QueryStreamSamples(ctx, db, int64(from), int64(through), matchers, stream)
	}
	if err != nil {
		return err
	}

	i.metrics.queriedSeries.Observe(float64(numSeries))
	i.metrics.queriedSamples.Observe(float64(numSamples))
	level.Debug(spanlog).Log("series", numSeries, "samples", numSamples)
	return nil
}

func (i *Ingester) v2QueryStreamSamples(ctx context.Context, db *userTSDB, from, through int64, matchers []*labels.Matcher, stream client.Ingester_QueryStreamServer) (numSeries, numSamples int, _ error) {
	q, err := db.Querier(ctx, from, through)
	if err != nil {
		return 0, 0, err
	}
	defer q.Close()

	// It's not required to return sorted series because series are sorted by the Cortex querier.
	ss := q.Select(false, nil, matchers...)
	if ss.Err() != nil {
		return 0, 0, ss.Err()
	}

	timeseries := make([]cortexpb.TimeSeries, 0, queryStreamBatchSize)
	batchSizeBytes := 0
	for ss.Next() {
		series := ss.At()

		// convert labels to LabelAdapter
		ts := cortexpb.TimeSeries{
			Labels: cortexpb.FromLabelsToLabelAdapters(series.Labels()),
		}

		it := series.Iterator()
		for it.Next() {
			t, v := it.At()
			ts.Samples = append(ts.Samples, cortexpb.Sample{Value: v, TimestampMs: t})
		}
		numSamples += len(ts.Samples)
		numSeries++
		tsSize := ts.Size()

		if (batchSizeBytes > 0 && batchSizeBytes+tsSize > queryStreamBatchMessageSize) || len(timeseries) >= queryStreamBatchSize {
			// Adding this series to the batch would make it too big,
			// flush the data and add it to new batch instead.
			err = client.SendQueryStream(stream, &client.QueryStreamResponse{
				Timeseries: timeseries,
			})
			if err != nil {
				return 0, 0, err
			}

			batchSizeBytes = 0
			timeseries = timeseries[:0]
		}

		timeseries = append(timeseries, ts)
		batchSizeBytes += tsSize
	}

	// Ensure no error occurred while iterating the series set.
	if err := ss.Err(); err != nil {
		return 0, 0, err
	}

	// Final flush any existing metrics
	if batchSizeBytes != 0 {
		err = client.SendQueryStream(stream, &client.QueryStreamResponse{
			Timeseries: timeseries,
		})
		if err != nil {
			return 0, 0, err
		}
	}

	return numSeries, numSamples, nil
}

// v2QueryStream streams metrics from a TSDB. This implements the client.IngesterServer interface
func (i *Ingester) v2QueryStreamChunks(ctx context.Context, db *userTSDB, from, through int64, matchers []*labels.Matcher, stream client.Ingester_QueryStreamServer) (numSeries, numSamples int, _ error) {
	q, err := db.ChunkQuerier(ctx, from, through)
	if err != nil {
		return 0, 0, err
	}
	defer q.Close()

	// It's not required to return sorted series because series are sorted by the Cortex querier.
	ss := q.Select(false, nil, matchers...)
	if ss.Err() != nil {
		return 0, 0, ss.Err()
	}

	chunkSeries := make([]client.TimeSeriesChunk, 0, queryStreamBatchSize)
	batchSizeBytes := 0
	for ss.Next() {
		series := ss.At()

		// convert labels to LabelAdapter
		ts := client.TimeSeriesChunk{
			Labels: cortexpb.FromLabelsToLabelAdapters(series.Labels()),
		}

		it := series.Iterator()
		for it.Next() {
			// Chunks are ordered by min time.
			meta := it.At()

			// It is not guaranteed that chunk returned by iterator is populated.
			// For now just return error. We could also try to figure out how to read the chunk.
			if meta.Chunk == nil {
				return 0, 0, errors.Errorf("unfilled chunk returned from TSDB chunk querier")
			}

			ch := client.Chunk{
				StartTimestampMs: meta.MinTime,
				EndTimestampMs:   meta.MaxTime,
				Data:             meta.Chunk.Bytes(),
			}

			switch meta.Chunk.Encoding() {
			case chunkenc.EncXOR:
				ch.Encoding = int32(encoding.PrometheusXorChunk)
			default:
				return 0, 0, errors.Errorf("unknown chunk encoding from TSDB chunk querier: %v", meta.Chunk.Encoding())
			}

			ts.Chunks = append(ts.Chunks, ch)
			numSamples += meta.Chunk.NumSamples()
		}
		numSeries++
		tsSize := ts.Size()

		if (batchSizeBytes > 0 && batchSizeBytes+tsSize > queryStreamBatchMessageSize) || len(chunkSeries) >= queryStreamBatchSize {
			// Adding this series to the batch would make it too big,
			// flush the data and add it to new batch instead.
			err = client.SendQueryStream(stream, &client.QueryStreamResponse{
				Chunkseries: chunkSeries,
			})
			if err != nil {
				return 0, 0, err
			}

			batchSizeBytes = 0
			chunkSeries = chunkSeries[:0]
		}

		chunkSeries = append(chunkSeries, ts)
		batchSizeBytes += tsSize
	}

	// Ensure no error occurred while iterating the series set.
	if err := ss.Err(); err != nil {
		return 0, 0, err
	}

	// Final flush any existing metrics
	if batchSizeBytes != 0 {
		err = client.SendQueryStream(stream, &client.QueryStreamResponse{
			Chunkseries: chunkSeries,
		})
		if err != nil {
			return 0, 0, err
		}
	}

	return numSeries, numSamples, nil
}

func (i *Ingester) getTSDB(userID string) *userTSDB {
	i.userStatesMtx.RLock()
	defer i.userStatesMtx.RUnlock()
	db := i.TSDBState.dbs[userID]
	return db
}

// List all users for which we have a TSDB. We do it here in order
// to keep the mutex locked for the shortest time possible.
func (i *Ingester) getTSDBUsers() []string {
	i.userStatesMtx.RLock()
	defer i.userStatesMtx.RUnlock()

	ids := make([]string, 0, len(i.TSDBState.dbs))
	for userID := range i.TSDBState.dbs {
		ids = append(ids, userID)
	}

	return ids
}

func (i *Ingester) getOrCreateTSDB(userID string, force bool) (*userTSDB, error) {
	db := i.getTSDB(userID)
	if db != nil {
		return db, nil
	}

	i.userStatesMtx.Lock()
	defer i.userStatesMtx.Unlock()

	// Check again for DB in the event it was created in-between locks
	var ok bool
	db, ok = i.TSDBState.dbs[userID]
	if ok {
		return db, nil
	}

	// We're ready to create the TSDB, however we must be sure that the ingester
	// is in the ACTIVE state, otherwise it may conflict with the transfer in/out.
	// The TSDB is created when the first series is pushed and this shouldn't happen
	// to a non-ACTIVE ingester, however we want to protect from any bug, cause we
	// may have data loss or TSDB WAL corruption if the TSDB is created before/during
	// a transfer in occurs.
	if ingesterState := i.lifecycler.GetState(); !force && ingesterState != ring.ACTIVE {
		return nil, fmt.Errorf(errTSDBCreateIncompatibleState, ingesterState)
	}

	gl := i.getInstanceLimits()
	if gl != nil && gl.MaxInMemoryTenants > 0 {
		if users := int64(len(i.TSDBState.dbs)); users >= gl.MaxInMemoryTenants {
			return nil, errMaxUsersLimitReached
		}
	}

	// Create the database and a shipper for a user
	db, err := i.createTSDB(userID)
	if err != nil {
		return nil, err
	}

	// Add the db to list of user databases
	i.TSDBState.dbs[userID] = db
	i.metrics.memUsers.Inc()

	return db, nil
}

// createTSDB creates a TSDB for a given userID, and returns the created db.
func (i *Ingester) createTSDB(userID string) (*userTSDB, error) {
	tsdbPromReg := prometheus.NewRegistry()
	udir := i.cfg.BlocksStorageConfig.TSDB.BlocksDir(userID)
	userLogger := logutil.WithUserID(userID, i.logger)

	blockRanges := i.cfg.BlocksStorageConfig.TSDB.BlockRanges.ToMilliseconds()

	userDB := &userTSDB{
		userID:              userID,
		activeSeries:        NewActiveSeries(),
		seriesInMetric:      newMetricCounter(i.limiter),
		ingestedAPISamples:  util_math.NewEWMARate(0.2, i.cfg.RateUpdatePeriod),
		ingestedRuleSamples: util_math.NewEWMARate(0.2, i.cfg.RateUpdatePeriod),

		instanceLimitsFn:    i.getInstanceLimits,
		instanceSeriesCount: &i.TSDBState.seriesCount,
	}

	// Create a new user database
	db, err := tsdb.Open(udir, userLogger, tsdbPromReg, &tsdb.Options{
		RetentionDuration:         i.cfg.BlocksStorageConfig.TSDB.Retention.Milliseconds(),
		MinBlockDuration:          blockRanges[0],
		MaxBlockDuration:          blockRanges[len(blockRanges)-1],
		NoLockfile:                true,
		StripeSize:                i.cfg.BlocksStorageConfig.TSDB.StripeSize,
		HeadChunksWriteBufferSize: i.cfg.BlocksStorageConfig.TSDB.HeadChunksWriteBufferSize,
		WALCompression:            i.cfg.BlocksStorageConfig.TSDB.WALCompressionEnabled,
		WALSegmentSize:            i.cfg.BlocksStorageConfig.TSDB.WALSegmentSizeBytes,
		SeriesLifecycleCallback:   userDB,
		BlocksToDelete:            userDB.blocksToDelete,
		MaxExemplars:              i.cfg.BlocksStorageConfig.TSDB.MaxExemplars,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open TSDB: %s", udir)
	}
	db.DisableCompactions() // we will compact on our own schedule

	// Run compaction before using this TSDB. If there is data in head that needs to be put into blocks,
	// this will actually create the blocks. If there is no data (empty TSDB), this is a no-op, although
	// local blocks compaction may still take place if configured.
	level.Info(userLogger).Log("msg", "Running compaction after WAL replay")
	err = db.Compact()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to compact TSDB: %s", udir)
	}

	userDB.db = db
	// We set the limiter here because we don't want to limit
	// series during WAL replay.
	userDB.limiter = i.limiter

	if db.Head().NumSeries() > 0 {
		// If there are series in the head, use max time from head. If this time is too old,
		// TSDB will be eligible for flushing and closing sooner, unless more data is pushed to it quickly.
		userDB.setLastUpdate(util.TimeFromMillis(db.Head().MaxTime()))
	} else {
		// If head is empty (eg. new TSDB), don't close it right after.
		userDB.setLastUpdate(time.Now())
	}

	// Thanos shipper requires at least 1 external label to be set. For this reason,
	// we set the tenant ID as external label and we'll filter it out when reading
	// the series from the storage.
	l := labels.Labels{
		{
			Name:  cortex_tsdb.TenantIDExternalLabel,
			Value: userID,
		}, {
			Name:  cortex_tsdb.IngesterIDExternalLabel,
			Value: i.TSDBState.shipperIngesterID,
		},
	}

	// Create a new shipper for this database
	if i.cfg.BlocksStorageConfig.TSDB.IsBlocksShippingEnabled() {
		userDB.shipper = shipper.New(
			userLogger,
			tsdbPromReg,
			udir,
			bucket.NewUserBucketClient(userID, i.TSDBState.bucket, i.limits),
			func() labels.Labels { return l },
			metadata.ReceiveSource,
			false, // No need to upload compacted blocks. Cortex compactor takes care of that.
			true,  // Allow out of order uploads. It's fine in Cortex's context.
			metadata.NoneFunc,
		)

		// Initialise the shipper blocks cache.
		if err := userDB.updateCachedShippedBlocks(); err != nil {
			level.Error(userLogger).Log("msg", "failed to update cached shipped blocks after shipper initialisation", "err", err)
		}
	}

	i.TSDBState.tsdbMetrics.setRegistryForUser(userID, tsdbPromReg)
	return userDB, nil
}

func (i *Ingester) closeAllTSDB() {
	i.userStatesMtx.Lock()

	wg := &sync.WaitGroup{}
	wg.Add(len(i.TSDBState.dbs))

	// Concurrently close all users TSDB
	for userID, userDB := range i.TSDBState.dbs {
		userID := userID

		go func(db *userTSDB) {
			defer wg.Done()

			if err := db.Close(); err != nil {
				level.Warn(i.logger).Log("msg", "unable to close TSDB", "err", err, "user", userID)
				return
			}

			// Now that the TSDB has been closed, we should remove it from the
			// set of open ones. This lock acquisition doesn't deadlock with the
			// outer one, because the outer one is released as soon as all go
			// routines are started.
			i.userStatesMtx.Lock()
			delete(i.TSDBState.dbs, userID)
			i.userStatesMtx.Unlock()

			i.metrics.memUsers.Dec()
			i.metrics.activeSeriesPerUser.DeleteLabelValues(userID)
		}(userDB)
	}

	// Wait until all Close() completed
	i.userStatesMtx.Unlock()
	wg.Wait()
}

// openExistingTSDB walks the user tsdb dir, and opens a tsdb for each user. This may start a WAL replay, so we limit the number of
// concurrently opening TSDB.
func (i *Ingester) openExistingTSDB(ctx context.Context) error {
	level.Info(i.logger).Log("msg", "opening existing TSDBs")

	queue := make(chan string)
	group, groupCtx := errgroup.WithContext(ctx)

	// Create a pool of workers which will open existing TSDBs.
	for n := 0; n < i.cfg.BlocksStorageConfig.TSDB.MaxTSDBOpeningConcurrencyOnStartup; n++ {
		group.Go(func() error {
			for userID := range queue {
				startTime := time.Now()

				db, err := i.createTSDB(userID)
				if err != nil {
					level.Error(i.logger).Log("msg", "unable to open TSDB", "err", err, "user", userID)
					return errors.Wrapf(err, "unable to open TSDB for user %s", userID)
				}

				// Add the database to the map of user databases
				i.userStatesMtx.Lock()
				i.TSDBState.dbs[userID] = db
				i.userStatesMtx.Unlock()
				i.metrics.memUsers.Inc()

				i.TSDBState.walReplayTime.Observe(time.Since(startTime).Seconds())
			}

			return nil
		})
	}

	// Spawn a goroutine to find all users with a TSDB on the filesystem.
	group.Go(func() error {
		// Close the queue once filesystem walking is done.
		defer close(queue)

		walkErr := filepath.Walk(i.cfg.BlocksStorageConfig.TSDB.Dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				// If the root directory doesn't exist, we're OK (not needed to be created upfront).
				if os.IsNotExist(err) && path == i.cfg.BlocksStorageConfig.TSDB.Dir {
					return filepath.SkipDir
				}

				level.Error(i.logger).Log("msg", "an error occurred while iterating the filesystem storing TSDBs", "path", path, "err", err)
				return errors.Wrapf(err, "an error occurred while iterating the filesystem storing TSDBs at %s", path)
			}

			// Skip root dir and all other files
			if path == i.cfg.BlocksStorageConfig.TSDB.Dir || !info.IsDir() {
				return nil
			}

			// Top level directories are assumed to be user TSDBs
			userID := info.Name()
			f, err := os.Open(path)
			if err != nil {
				level.Error(i.logger).Log("msg", "unable to open TSDB dir", "err", err, "user", userID, "path", path)
				return errors.Wrapf(err, "unable to open TSDB dir %s for user %s", path, userID)
			}
			defer f.Close()

			// If the dir is empty skip it
			if _, err := f.Readdirnames(1); err != nil {
				if err == io.EOF {
					return filepath.SkipDir
				}

				level.Error(i.logger).Log("msg", "unable to read TSDB dir", "err", err, "user", userID, "path", path)
				return errors.Wrapf(err, "unable to read TSDB dir %s for user %s", path, userID)
			}

			// Enqueue the user to be processed.
			select {
			case queue <- userID:
				// Nothing to do.
			case <-groupCtx.Done():
				// Interrupt in case a failure occurred in another goroutine.
				return nil
			}

			// Don't descend into subdirectories.
			return filepath.SkipDir
		})

		return errors.Wrapf(walkErr, "unable to walk directory %s containing existing TSDBs", i.cfg.BlocksStorageConfig.TSDB.Dir)
	})

	// Wait for all workers to complete.
	err := group.Wait()
	if err != nil {
		level.Error(i.logger).Log("msg", "error while opening existing TSDBs", "err", err)
		return err
	}

	level.Info(i.logger).Log("msg", "successfully opened existing TSDBs")
	return nil
}

// getMemorySeriesMetric returns the total number of in-memory series across all open TSDBs.
func (i *Ingester) getMemorySeriesMetric() float64 {
	i.userStatesMtx.RLock()
	defer i.userStatesMtx.RUnlock()

	count := uint64(0)
	for _, db := range i.TSDBState.dbs {
		count += db.Head().NumSeries()
	}

	return float64(count)
}

// getOldestUnshippedBlockMetric returns the unix timestamp of the oldest unshipped block or
// 0 if all blocks have been shipped.
func (i *Ingester) getOldestUnshippedBlockMetric() float64 {
	i.userStatesMtx.RLock()
	defer i.userStatesMtx.RUnlock()

	oldest := uint64(0)
	for _, db := range i.TSDBState.dbs {
		if ts := db.getOldestUnshippedBlockTime(); oldest == 0 || ts < oldest {
			oldest = ts
		}
	}

	return float64(oldest / 1000)
}

func (i *Ingester) shipBlocksLoop(ctx context.Context) error {
	// We add a slight jitter to make sure that if the head compaction interval and ship interval are set to the same
	// value they don't clash (if they both continuously run at the same exact time, the head compaction may not run
	// because can't successfully change the state).
	shipTicker := time.NewTicker(util.DurationWithJitter(i.cfg.BlocksStorageConfig.TSDB.ShipInterval, 0.01))
	defer shipTicker.Stop()

	for {
		select {
		case <-shipTicker.C:
			i.shipBlocks(ctx, nil)

		case req := <-i.TSDBState.shipTrigger:
			i.shipBlocks(ctx, req.users)
			close(req.callback) // Notify back.

		case <-ctx.Done():
			return nil
		}
	}
}

// shipBlocks runs shipping for all users.
func (i *Ingester) shipBlocks(ctx context.Context, allowed *util.AllowedTenants) {
	// Do not ship blocks if the ingester is PENDING or JOINING. It's
	// particularly important for the JOINING state because there could
	// be a blocks transfer in progress (from another ingester) and if we
	// run the shipper in such state we could end up with race conditions.
	if i.lifecycler != nil {
		if ingesterState := i.lifecycler.GetState(); ingesterState == ring.PENDING || ingesterState == ring.JOINING {
			level.Info(i.logger).Log("msg", "TSDB blocks shipping has been skipped because of the current ingester state", "state", ingesterState)
			return
		}
	}

	// Number of concurrent workers is limited in order to avoid to concurrently sync a lot
	// of tenants in a large cluster.
	_ = concurrency.ForEachUser(ctx, i.getTSDBUsers(), i.cfg.BlocksStorageConfig.TSDB.ShipConcurrency, func(ctx context.Context, userID string) error {
		if !allowed.IsAllowed(userID) {
			return nil
		}

		// Get the user's DB. If the user doesn't exist, we skip it.
		userDB := i.getTSDB(userID)
		if userDB == nil || userDB.shipper == nil {
			return nil
		}

		if userDB.deletionMarkFound.Load() {
			return nil
		}

		if time.Since(time.Unix(userDB.lastDeletionMarkCheck.Load(), 0)) > cortex_tsdb.DeletionMarkCheckInterval {
			// Even if check fails with error, we don't want to repeat it too often.
			userDB.lastDeletionMarkCheck.Store(time.Now().Unix())

			deletionMarkExists, err := cortex_tsdb.TenantDeletionMarkExists(ctx, i.TSDBState.bucket, userID)
			if err != nil {
				// If we cannot check for deletion mark, we continue anyway, even though in production shipper will likely fail too.
				// This however simplifies unit tests, where tenant deletion check is enabled by default, but tests don't setup bucket.
				level.Warn(i.logger).Log("msg", "failed to check for tenant deletion mark before shipping blocks", "user", userID, "err", err)
			} else if deletionMarkExists {
				userDB.deletionMarkFound.Store(true)

				level.Info(i.logger).Log("msg", "tenant deletion mark exists, not shipping blocks", "user", userID)
				return nil
			}
		}

		// Run the shipper's Sync() to upload unshipped blocks. Make sure the TSDB state is active, in order to
		// avoid any race condition with closing idle TSDBs.
		if !userDB.casState(active, activeShipping) {
			level.Info(i.logger).Log("msg", "shipper skipped because the TSDB is not active", "user", userID)
			return nil
		}
		defer userDB.casState(activeShipping, active)

		uploaded, err := userDB.shipper.Sync(ctx)
		if err != nil {
			level.Warn(i.logger).Log("msg", "shipper failed to synchronize TSDB blocks with the storage", "user", userID, "uploaded", uploaded, "err", err)
		} else {
			level.Debug(i.logger).Log("msg", "shipper successfully synchronized TSDB blocks with storage", "user", userID, "uploaded", uploaded)
		}

		// The shipper meta file could be updated even if the Sync() returned an error,
		// so it's safer to update it each time at least a block has been uploaded.
		// Moreover, the shipper meta file could be updated even if no blocks are uploaded
		// (eg. blocks removed due to retention) but doesn't cause any harm not updating
		// the cached list of blocks in such case, so we're not handling it.
		if uploaded > 0 {
			if err := userDB.updateCachedShippedBlocks(); err != nil {
				level.Error(i.logger).Log("msg", "failed to update cached shipped blocks after shipper synchronisation", "user", userID, "err", err)
			}
		}

		return nil
	})
}

func (i *Ingester) compactionLoop(ctx context.Context) error {
	ticker := time.NewTicker(i.cfg.BlocksStorageConfig.TSDB.HeadCompactionInterval)
	defer ticker.Stop()

	for ctx.Err() == nil {
		select {
		case <-ticker.C:
			i.compactBlocks(ctx, false, nil)

		case req := <-i.TSDBState.forceCompactTrigger:
			i.compactBlocks(ctx, true, req.users)
			close(req.callback) // Notify back.

		case <-ctx.Done():
			return nil
		}
	}
	return nil
}

// Compacts all compactable blocks. Force flag will force compaction even if head is not compactable yet.
func (i *Ingester) compactBlocks(ctx context.Context, force bool, allowed *util.AllowedTenants) {
	// Don't compact TSDB blocks while JOINING as there may be ongoing blocks transfers.
	// Compaction loop is not running in LEAVING state, so if we get here in LEAVING state, we're flushing blocks.
	if i.lifecycler != nil {
		if ingesterState := i.lifecycler.GetState(); ingesterState == ring.JOINING {
			level.Info(i.logger).Log("msg", "TSDB blocks compaction has been skipped because of the current ingester state", "state", ingesterState)
			return
		}
	}

	_ = concurrency.ForEachUser(ctx, i.getTSDBUsers(), i.cfg.BlocksStorageConfig.TSDB.HeadCompactionConcurrency, func(ctx context.Context, userID string) error {
		if !allowed.IsAllowed(userID) {
			return nil
		}

		userDB := i.getTSDB(userID)
		if userDB == nil {
			return nil
		}

		// Don't do anything, if there is nothing to compact.
		h := userDB.Head()
		if h.NumSeries() == 0 {
			return nil
		}

		var err error

		i.TSDBState.compactionsTriggered.Inc()

		reason := ""
		switch {
		case force:
			reason = "forced"
			err = userDB.compactHead(i.cfg.BlocksStorageConfig.TSDB.BlockRanges[0].Milliseconds())

		case i.TSDBState.compactionIdleTimeout > 0 && userDB.isIdle(time.Now(), i.TSDBState.compactionIdleTimeout):
			reason = "idle"
			level.Info(i.logger).Log("msg", "TSDB is idle, forcing compaction", "user", userID)
			err = userDB.compactHead(i.cfg.BlocksStorageConfig.TSDB.BlockRanges[0].Milliseconds())

		default:
			reason = "regular"
			err = userDB.Compact()
		}

		if err != nil {
			i.TSDBState.compactionsFailed.Inc()
			level.Warn(i.logger).Log("msg", "TSDB blocks compaction for user has failed", "user", userID, "err", err, "compactReason", reason)
		} else {
			level.Debug(i.logger).Log("msg", "TSDB blocks compaction completed successfully", "user", userID, "compactReason", reason)
		}

		return nil
	})
}

func (i *Ingester) closeAndDeleteIdleUserTSDBs(ctx context.Context) error {
	for _, userID := range i.getTSDBUsers() {
		if ctx.Err() != nil {
			return nil
		}

		result := i.closeAndDeleteUserTSDBIfIdle(userID)

		i.TSDBState.idleTsdbChecks.WithLabelValues(string(result)).Inc()
	}

	return nil
}

func (i *Ingester) closeAndDeleteUserTSDBIfIdle(userID string) tsdbCloseCheckResult {
	userDB := i.getTSDB(userID)
	if userDB == nil || userDB.shipper == nil {
		// We will not delete local data when not using shipping to storage.
		return tsdbShippingDisabled
	}

	if result := userDB.shouldCloseTSDB(i.cfg.BlocksStorageConfig.TSDB.CloseIdleTSDBTimeout); !result.shouldClose() {
		return result
	}

	// This disables pushes and force-compactions. Not allowed to close while shipping is in progress.
	if !userDB.casState(active, closing) {
		return tsdbNotActive
	}

	// If TSDB is fully closed, we will set state to 'closed', which will prevent this defered closing -> active transition.
	defer userDB.casState(closing, active)

	// Make sure we don't ignore any possible inflight pushes.
	userDB.pushesInFlight.Wait()

	// Verify again, things may have changed during the checks and pushes.
	tenantDeleted := false
	if result := userDB.shouldCloseTSDB(i.cfg.BlocksStorageConfig.TSDB.CloseIdleTSDBTimeout); !result.shouldClose() {
		// This will also change TSDB state back to active (via defer above).
		return result
	} else if result == tsdbTenantMarkedForDeletion {
		tenantDeleted = true
	}

	// At this point there are no more pushes to TSDB, and no possible compaction. Normally TSDB is empty,
	// but if we're closing TSDB because of tenant deletion mark, then it may still contain some series.
	// We need to remove these series from series count.
	i.TSDBState.seriesCount.Sub(int64(userDB.Head().NumSeries()))

	dir := userDB.db.Dir()

	if err := userDB.Close(); err != nil {
		level.Error(i.logger).Log("msg", "failed to close idle TSDB", "user", userID, "err", err)
		return tsdbCloseFailed
	}

	level.Info(i.logger).Log("msg", "closed idle TSDB", "user", userID)

	// This will prevent going back to "active" state in deferred statement.
	userDB.casState(closing, closed)

	// Only remove user from TSDBState when everything is cleaned up
	// This will prevent concurrency problems when cortex are trying to open new TSDB - Ie: New request for a given tenant
	// came in - while closing the tsdb for the same tenant.
	// If this happens now, the request will get reject as the push will not be able to acquire the lock as the tsdb will be
	// in closed state
	defer func() {
		i.userStatesMtx.Lock()
		delete(i.TSDBState.dbs, userID)
		i.userStatesMtx.Unlock()
	}()

	i.metrics.memUsers.Dec()
	i.TSDBState.tsdbMetrics.removeRegistryForUser(userID)

	i.deleteUserMetadata(userID)
	i.metrics.deletePerUserMetrics(userID)

	validation.DeletePerUserValidationMetrics(userID, i.logger)

	// And delete local data.
	if err := os.RemoveAll(dir); err != nil {
		level.Error(i.logger).Log("msg", "failed to delete local TSDB", "user", userID, "err", err)
		return tsdbDataRemovalFailed
	}

	if tenantDeleted {
		level.Info(i.logger).Log("msg", "deleted local TSDB, user marked for deletion", "user", userID, "dir", dir)
		return tsdbTenantMarkedForDeletion
	}

	level.Info(i.logger).Log("msg", "deleted local TSDB, due to being idle", "user", userID, "dir", dir)
	return tsdbIdleClosed
}

// This method will flush all data. It is called as part of Lifecycler's shutdown (if flush on shutdown is configured), or from the flusher.
//
// When called as during Lifecycler shutdown, this happens as part of normal Ingester shutdown (see stoppingV2 method).
// Samples are not received at this stage. Compaction and Shipping loops have already been stopped as well.
//
// When used from flusher, ingester is constructed in a way that compaction, shipping and receiving of samples is never started.
func (i *Ingester) v2LifecyclerFlush() {
	level.Info(i.logger).Log("msg", "starting to flush and ship TSDB blocks")

	ctx := context.Background()

	i.compactBlocks(ctx, true, nil)
	if i.cfg.BlocksStorageConfig.TSDB.IsBlocksShippingEnabled() {
		i.shipBlocks(ctx, nil)
	}

	level.Info(i.logger).Log("msg", "finished flushing and shipping TSDB blocks")
}

const (
	tenantParam = "tenant"
	waitParam   = "wait"
)

// Blocks version of Flush handler. It force-compacts blocks, and triggers shipping.
func (i *Ingester) v2FlushHandler(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		level.Warn(i.logger).Log("msg", "failed to parse HTTP request in flush handler", "err", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	tenants := r.Form[tenantParam]

	allowedUsers := util.NewAllowedTenants(tenants, nil)
	run := func() {
		ingCtx := i.BasicService.ServiceContext()
		if ingCtx == nil || ingCtx.Err() != nil {
			level.Info(i.logger).Log("msg", "flushing TSDB blocks: ingester not running, ignoring flush request")
			return
		}

		compactionCallbackCh := make(chan struct{})

		level.Info(i.logger).Log("msg", "flushing TSDB blocks: triggering compaction")
		select {
		case i.TSDBState.forceCompactTrigger <- requestWithUsersAndCallback{users: allowedUsers, callback: compactionCallbackCh}:
			// Compacting now.
		case <-ingCtx.Done():
			level.Warn(i.logger).Log("msg", "failed to compact TSDB blocks, ingester not running anymore")
			return
		}

		// Wait until notified about compaction being finished.
		select {
		case <-compactionCallbackCh:
			level.Info(i.logger).Log("msg", "finished compacting TSDB blocks")
		case <-ingCtx.Done():
			level.Warn(i.logger).Log("msg", "failed to compact TSDB blocks, ingester not running anymore")
			return
		}

		if i.cfg.BlocksStorageConfig.TSDB.IsBlocksShippingEnabled() {
			shippingCallbackCh := make(chan struct{}) // must be new channel, as compactionCallbackCh is closed now.

			level.Info(i.logger).Log("msg", "flushing TSDB blocks: triggering shipping")

			select {
			case i.TSDBState.shipTrigger <- requestWithUsersAndCallback{users: allowedUsers, callback: shippingCallbackCh}:
				// shipping now
			case <-ingCtx.Done():
				level.Warn(i.logger).Log("msg", "failed to ship TSDB blocks, ingester not running anymore")
				return
			}

			// Wait until shipping finished.
			select {
			case <-shippingCallbackCh:
				level.Info(i.logger).Log("msg", "shipping of TSDB blocks finished")
			case <-ingCtx.Done():
				level.Warn(i.logger).Log("msg", "failed to ship TSDB blocks, ingester not running anymore")
				return
			}
		}

		level.Info(i.logger).Log("msg", "flushing TSDB blocks: finished")
	}

	if len(r.Form[waitParam]) > 0 && r.Form[waitParam][0] == "true" {
		// Run synchronously. This simplifies and speeds up tests.
		run()
	} else {
		go run()
	}

	w.WriteHeader(http.StatusNoContent)
}

// metadataQueryRange returns the best range to query for metadata queries based on the timerange in the ingester.
func metadataQueryRange(queryStart, queryEnd int64, db *userTSDB) (mint, maxt int64, err error) {
	// Ingesters are run with limited retention and we don't support querying the store-gateway for labels yet.
	// This means if someone loads a dashboard that is outside the range of the ingester, and we only return the
	// data for the timerange requested (which will be empty), the dashboards will break. To fix this we should
	// return the "head block" range until we can query the store-gateway.

	// Now the question would be what to do when the query is partially in the ingester range. I would err on the side
	// of caution and query the entire db, as I can't think of a good way to query the head + the overlapping range.
	mint, maxt = queryStart, queryEnd

	lowestTs, err := db.StartTime()
	if err != nil {
		return mint, maxt, err
	}

	// Completely outside.
	if queryEnd < lowestTs {
		mint, maxt = db.Head().MinTime(), db.Head().MaxTime()
	} else if queryStart < lowestTs {
		// Partially inside.
		mint, maxt = 0, math.MaxInt64
	}

	return
}

func wrappedTSDBIngestErr(ingestErr error, timestamp model.Time, labels []cortexpb.LabelAdapter) error {
	if ingestErr == nil {
		return nil
	}

	return fmt.Errorf(errTSDBIngest, ingestErr, timestamp.Time().UTC().Format(time.RFC3339Nano), cortexpb.FromLabelAdaptersToLabels(labels).String())
}

func wrappedTSDBIngestExemplarErr(ingestErr error, timestamp model.Time, seriesLabels, exemplarLabels []cortexpb.LabelAdapter) error {
	if ingestErr == nil {
		return nil
	}

	return fmt.Errorf(errTSDBIngestExemplar, ingestErr, timestamp.Time().UTC().Format(time.RFC3339Nano),
		cortexpb.FromLabelAdaptersToLabels(seriesLabels).String(),
		cortexpb.FromLabelAdaptersToLabels(exemplarLabels).String(),
	)
}

func (i *Ingester) getInstanceLimits() *InstanceLimits {
	// Don't apply any limits while starting. We especially don't want to apply series in memory limit while replaying WAL.
	if i.State() == services.Starting {
		return nil
	}

	if i.cfg.InstanceLimitsFn == nil {
		return defaultInstanceLimits
	}

	l := i.cfg.InstanceLimitsFn()
	if l == nil {
		return defaultInstanceLimits
	}

	return l
}
