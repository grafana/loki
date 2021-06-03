package ingester

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gogo/status"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	tsdb_record "github.com/prometheus/prometheus/tsdb/record"
	"github.com/weaveworks/common/httpgrpc"
	"go.uber.org/atomic"
	"golang.org/x/time/rate"
	"google.golang.org/grpc/codes"

	cortex_chunk "github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/tenant"
	"github.com/cortexproject/cortex/pkg/util"
	logutil "github.com/cortexproject/cortex/pkg/util/log"
	util_math "github.com/cortexproject/cortex/pkg/util/math"
	"github.com/cortexproject/cortex/pkg/util/services"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/cortexproject/cortex/pkg/util/validation"
)

const (
	// Number of timeseries to return in each batch of a QueryStream.
	queryStreamBatchSize = 128

	// Discarded Metadata metric labels.
	perUserMetadataLimit   = "per_user_metadata_limit"
	perMetricMetadataLimit = "per_metric_metadata_limit"

	// Period at which to attempt purging metadata from memory.
	metadataPurgePeriod = 5 * time.Minute
)

var (
	// This is initialised if the WAL is enabled and the records are fetched from this pool.
	recordPool sync.Pool

	errIngesterStopping = errors.New("ingester stopping")
)

// Config for an Ingester.
type Config struct {
	WALConfig        WALConfig             `yaml:"walconfig" doc:"description=Configures the Write-Ahead Log (WAL) for the Cortex chunks storage. This config is ignored when running the Cortex blocks storage."`
	LifecyclerConfig ring.LifecyclerConfig `yaml:"lifecycler"`

	// Config for transferring chunks. Zero or negative = no retries.
	MaxTransferRetries int `yaml:"max_transfer_retries"`

	// Config for chunk flushing.
	FlushCheckPeriod  time.Duration `yaml:"flush_period"`
	RetainPeriod      time.Duration `yaml:"retain_period"`
	MaxChunkIdle      time.Duration `yaml:"max_chunk_idle_time"`
	MaxStaleChunkIdle time.Duration `yaml:"max_stale_chunk_idle_time"`
	FlushOpTimeout    time.Duration `yaml:"flush_op_timeout"`
	MaxChunkAge       time.Duration `yaml:"max_chunk_age"`
	ChunkAgeJitter    time.Duration `yaml:"chunk_age_jitter"`
	ConcurrentFlushes int           `yaml:"concurrent_flushes"`
	SpreadFlushes     bool          `yaml:"spread_flushes"`

	// Config for metadata purging.
	MetadataRetainPeriod time.Duration `yaml:"metadata_retain_period"`

	RateUpdatePeriod time.Duration `yaml:"rate_update_period"`

	ActiveSeriesMetricsEnabled      bool          `yaml:"active_series_metrics_enabled"`
	ActiveSeriesMetricsUpdatePeriod time.Duration `yaml:"active_series_metrics_update_period"`
	ActiveSeriesMetricsIdleTimeout  time.Duration `yaml:"active_series_metrics_idle_timeout"`

	// Use blocks storage.
	BlocksStorageEnabled        bool                     `yaml:"-"`
	BlocksStorageConfig         tsdb.BlocksStorageConfig `yaml:"-"`
	StreamChunksWhenUsingBlocks bool                     `yaml:"-"`
	// Runtime-override for type of streaming query to use (chunks or samples).
	StreamTypeFn func() QueryStreamType `yaml:"-"`

	// Injected at runtime and read from the distributor config, required
	// to accurately apply global limits.
	DistributorShardingStrategy string `yaml:"-"`
	DistributorShardByAllLabels bool   `yaml:"-"`

	DefaultLimits    InstanceLimits         `yaml:"instance_limits"`
	InstanceLimitsFn func() *InstanceLimits `yaml:"-"`

	// For testing, you can override the address and ID of this ingester.
	ingesterClientFactory func(addr string, cfg client.Config) (client.HealthAndIngesterClient, error)
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.LifecyclerConfig.RegisterFlags(f)
	cfg.WALConfig.RegisterFlags(f)

	f.IntVar(&cfg.MaxTransferRetries, "ingester.max-transfer-retries", 10, "Number of times to try and transfer chunks before falling back to flushing. Negative value or zero disables hand-over. This feature is supported only by the chunks storage.")

	f.DurationVar(&cfg.FlushCheckPeriod, "ingester.flush-period", 1*time.Minute, "Period with which to attempt to flush chunks.")
	f.DurationVar(&cfg.RetainPeriod, "ingester.retain-period", 5*time.Minute, "Period chunks will remain in memory after flushing.")
	f.DurationVar(&cfg.MaxChunkIdle, "ingester.max-chunk-idle", 5*time.Minute, "Maximum chunk idle time before flushing.")
	f.DurationVar(&cfg.MaxStaleChunkIdle, "ingester.max-stale-chunk-idle", 2*time.Minute, "Maximum chunk idle time for chunks terminating in stale markers before flushing. 0 disables it and a stale series is not flushed until the max-chunk-idle timeout is reached.")
	f.DurationVar(&cfg.FlushOpTimeout, "ingester.flush-op-timeout", 1*time.Minute, "Timeout for individual flush operations.")
	f.DurationVar(&cfg.MaxChunkAge, "ingester.max-chunk-age", 12*time.Hour, "Maximum chunk age before flushing.")
	f.DurationVar(&cfg.ChunkAgeJitter, "ingester.chunk-age-jitter", 0, "Range of time to subtract from -ingester.max-chunk-age to spread out flushes")
	f.IntVar(&cfg.ConcurrentFlushes, "ingester.concurrent-flushes", 50, "Number of concurrent goroutines flushing to dynamodb.")
	f.BoolVar(&cfg.SpreadFlushes, "ingester.spread-flushes", true, "If true, spread series flushes across the whole period of -ingester.max-chunk-age.")

	f.DurationVar(&cfg.MetadataRetainPeriod, "ingester.metadata-retain-period", 10*time.Minute, "Period at which metadata we have not seen will remain in memory before being deleted.")

	f.DurationVar(&cfg.RateUpdatePeriod, "ingester.rate-update-period", 15*time.Second, "Period with which to update the per-user ingestion rates.")
	f.BoolVar(&cfg.ActiveSeriesMetricsEnabled, "ingester.active-series-metrics-enabled", false, "Enable tracking of active series and export them as metrics.")
	f.DurationVar(&cfg.ActiveSeriesMetricsUpdatePeriod, "ingester.active-series-metrics-update-period", 1*time.Minute, "How often to update active series metrics.")
	f.DurationVar(&cfg.ActiveSeriesMetricsIdleTimeout, "ingester.active-series-metrics-idle-timeout", 10*time.Minute, "After what time a series is considered to be inactive.")
	f.BoolVar(&cfg.StreamChunksWhenUsingBlocks, "ingester.stream-chunks-when-using-blocks", false, "Stream chunks when using blocks. This is experimental feature and not yet tested. Once ready, it will be made default and this config option removed.")

	f.Float64Var(&cfg.DefaultLimits.MaxIngestionRate, "ingester.instance-limits.max-ingestion-rate", 0, "Max ingestion rate (samples/sec) that ingester will accept. This limit is per-ingester, not per-tenant. Additional push requests will be rejected. Current ingestion rate is computed as exponentially weighted moving average, updated every second. This limit only works when using blocks engine. 0 = unlimited.")
	f.Int64Var(&cfg.DefaultLimits.MaxInMemoryTenants, "ingester.instance-limits.max-tenants", 0, "Max users that this ingester can hold. Requests from additional users will be rejected. This limit only works when using blocks engine. 0 = unlimited.")
	f.Int64Var(&cfg.DefaultLimits.MaxInMemorySeries, "ingester.instance-limits.max-series", 0, "Max series that this ingester can hold (across all tenants). Requests to create additional series will be rejected. This limit only works when using blocks engine. 0 = unlimited.")
	f.Int64Var(&cfg.DefaultLimits.MaxInflightPushRequests, "ingester.instance-limits.max-inflight-push-requests", 0, "Max inflight push requests that this ingester can handle (across all tenants). Additional requests will be rejected. 0 = unlimited.")
}

// Ingester deals with "in flight" chunks.  Based on Prometheus 1.x
// MemorySeriesStorage.
type Ingester struct {
	*services.BasicService

	cfg          Config
	clientConfig client.Config

	metrics *ingesterMetrics
	logger  log.Logger

	chunkStore         ChunkStore
	lifecycler         *ring.Lifecycler
	limits             *validation.Overrides
	limiter            *Limiter
	subservicesWatcher *services.FailureWatcher

	userStatesMtx sync.RWMutex // protects userStates and stopped
	userStates    *userStates
	stopped       bool // protected by userStatesMtx

	// For storing metadata ingested.
	usersMetadataMtx sync.RWMutex
	usersMetadata    map[string]*userMetricsMetadata

	// One queue per flush thread.  Fingerprint is used to
	// pick a queue.
	flushQueues     []*util.PriorityQueue
	flushQueuesDone sync.WaitGroup

	// Spread out calls to the chunk store over the flush period
	flushRateLimiter *rate.Limiter

	// This should never be nil.
	wal WAL
	// To be passed to the WAL.
	registerer prometheus.Registerer

	// Hooks for injecting behaviour from tests.
	preFlushUserSeries func()
	preFlushChunks     func()

	// Prometheus block storage
	TSDBState TSDBState

	// Rate of pushed samples. Only used by V2-ingester to limit global samples push rate.
	ingestionRate        *util_math.EwmaRate
	inflightPushRequests atomic.Int64
}

// ChunkStore is the interface we need to store chunks
type ChunkStore interface {
	Put(ctx context.Context, chunks []cortex_chunk.Chunk) error
}

// New constructs a new Ingester.
func New(cfg Config, clientConfig client.Config, limits *validation.Overrides, chunkStore ChunkStore, registerer prometheus.Registerer, logger log.Logger) (*Ingester, error) {
	defaultInstanceLimits = &cfg.DefaultLimits

	if cfg.ingesterClientFactory == nil {
		cfg.ingesterClientFactory = client.MakeIngesterClient
	}

	if cfg.BlocksStorageEnabled {
		return NewV2(cfg, clientConfig, limits, registerer, logger)
	}

	if cfg.WALConfig.WALEnabled {
		// If WAL is enabled, we don't transfer out the data to any ingester.
		// Either the next ingester which takes it's place should recover from WAL
		// or the data has to be flushed during scaledown.
		cfg.MaxTransferRetries = 0

		// Transfers are disabled with WAL, hence no need to wait for transfers.
		cfg.LifecyclerConfig.JoinAfter = 0

		recordPool = sync.Pool{
			New: func() interface{} {
				return &WALRecord{}
			},
		}
	}

	if cfg.WALConfig.WALEnabled || cfg.WALConfig.Recover {
		if err := os.MkdirAll(cfg.WALConfig.Dir, os.ModePerm); err != nil {
			return nil, err
		}
	}

	i := &Ingester{
		cfg:          cfg,
		clientConfig: clientConfig,

		limits:           limits,
		chunkStore:       chunkStore,
		flushQueues:      make([]*util.PriorityQueue, cfg.ConcurrentFlushes),
		flushRateLimiter: rate.NewLimiter(rate.Inf, 1),
		usersMetadata:    map[string]*userMetricsMetadata{},
		registerer:       registerer,
		logger:           logger,
	}
	i.metrics = newIngesterMetrics(registerer, true, cfg.ActiveSeriesMetricsEnabled, i.getInstanceLimits, nil, &i.inflightPushRequests)

	var err error
	// During WAL recovery, it will create new user states which requires the limiter.
	// Hence initialise the limiter before creating the WAL.
	// The '!cfg.WALConfig.WALEnabled' argument says don't flush on shutdown if the WAL is enabled.
	i.lifecycler, err = ring.NewLifecycler(cfg.LifecyclerConfig, i, "ingester", ring.IngesterRingKey, !cfg.WALConfig.WALEnabled || cfg.WALConfig.FlushOnShutdown, registerer)
	if err != nil {
		return nil, err
	}

	i.limiter = NewLimiter(
		limits,
		i.lifecycler,
		cfg.DistributorShardingStrategy,
		cfg.DistributorShardByAllLabels,
		cfg.LifecyclerConfig.RingConfig.ReplicationFactor,
		cfg.LifecyclerConfig.RingConfig.ZoneAwarenessEnabled)

	i.subservicesWatcher = services.NewFailureWatcher()
	i.subservicesWatcher.WatchService(i.lifecycler)

	i.BasicService = services.NewBasicService(i.starting, i.loop, i.stopping)
	return i, nil
}

func (i *Ingester) starting(ctx context.Context) error {
	if i.cfg.WALConfig.Recover {
		level.Info(i.logger).Log("msg", "recovering from WAL")
		start := time.Now()
		if err := recoverFromWAL(i); err != nil {
			level.Error(i.logger).Log("msg", "failed to recover from WAL", "time", time.Since(start).String())
			return errors.Wrap(err, "failed to recover from WAL")
		}
		elapsed := time.Since(start)
		level.Info(i.logger).Log("msg", "recovery from WAL completed", "time", elapsed.String())
		i.metrics.walReplayDuration.Set(elapsed.Seconds())
	}

	// If the WAL recover happened, then the userStates would already be set.
	if i.userStates == nil {
		i.userStates = newUserStates(i.limiter, i.cfg, i.metrics, i.logger)
	}

	var err error
	i.wal, err = newWAL(i.cfg.WALConfig, i.userStates.cp, i.registerer, i.logger)
	if err != nil {
		return errors.Wrap(err, "starting WAL")
	}

	// Now that user states have been created, we can start the lifecycler.
	// Important: we want to keep lifecycler running until we ask it to stop, so we need to give it independent context
	if err := i.lifecycler.StartAsync(context.Background()); err != nil {
		return errors.Wrap(err, "failed to start lifecycler")
	}
	if err := i.lifecycler.AwaitRunning(ctx); err != nil {
		return errors.Wrap(err, "failed to start lifecycler")
	}

	i.startFlushLoops()

	return nil
}

func (i *Ingester) startFlushLoops() {
	i.flushQueuesDone.Add(i.cfg.ConcurrentFlushes)
	for j := 0; j < i.cfg.ConcurrentFlushes; j++ {
		i.flushQueues[j] = util.NewPriorityQueue(i.metrics.flushQueueLength)
		go i.flushLoop(j)
	}
}

// NewForFlusher constructs a new Ingester to be used by flusher target.
// Compared to the 'New' method:
//   * Always replays the WAL.
//   * Does not start the lifecycler.
func NewForFlusher(cfg Config, chunkStore ChunkStore, limits *validation.Overrides, registerer prometheus.Registerer, logger log.Logger) (*Ingester, error) {
	if cfg.BlocksStorageEnabled {
		return NewV2ForFlusher(cfg, limits, registerer, logger)
	}

	i := &Ingester{
		cfg:              cfg,
		chunkStore:       chunkStore,
		flushQueues:      make([]*util.PriorityQueue, cfg.ConcurrentFlushes),
		flushRateLimiter: rate.NewLimiter(rate.Inf, 1),
		wal:              &noopWAL{},
		limits:           limits,
		logger:           logger,
	}
	i.metrics = newIngesterMetrics(registerer, true, false, i.getInstanceLimits, nil, &i.inflightPushRequests)

	i.BasicService = services.NewBasicService(i.startingForFlusher, i.loopForFlusher, i.stopping)
	return i, nil
}

func (i *Ingester) startingForFlusher(ctx context.Context) error {
	level.Info(i.logger).Log("msg", "recovering from WAL")

	// We recover from WAL always.
	start := time.Now()
	if err := recoverFromWAL(i); err != nil {
		level.Error(i.logger).Log("msg", "failed to recover from WAL", "time", time.Since(start).String())
		return err
	}
	elapsed := time.Since(start)

	level.Info(i.logger).Log("msg", "recovery from WAL completed", "time", elapsed.String())
	i.metrics.walReplayDuration.Set(elapsed.Seconds())

	i.startFlushLoops()
	return nil
}

func (i *Ingester) loopForFlusher(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil

		case err := <-i.subservicesWatcher.Chan():
			return errors.Wrap(err, "ingester subservice failed")
		}
	}
}

func (i *Ingester) loop(ctx context.Context) error {
	flushTicker := time.NewTicker(i.cfg.FlushCheckPeriod)
	defer flushTicker.Stop()

	rateUpdateTicker := time.NewTicker(i.cfg.RateUpdatePeriod)
	defer rateUpdateTicker.Stop()

	metadataPurgeTicker := time.NewTicker(metadataPurgePeriod)
	defer metadataPurgeTicker.Stop()

	var activeSeriesTickerChan <-chan time.Time
	if i.cfg.ActiveSeriesMetricsEnabled {
		t := time.NewTicker(i.cfg.ActiveSeriesMetricsUpdatePeriod)
		activeSeriesTickerChan = t.C
		defer t.Stop()
	}

	for {
		select {
		case <-metadataPurgeTicker.C:
			i.purgeUserMetricsMetadata()

		case <-flushTicker.C:
			i.sweepUsers(false)

		case <-rateUpdateTicker.C:
			i.userStates.updateRates()

		case <-activeSeriesTickerChan:
			i.userStates.purgeAndUpdateActiveSeries(time.Now().Add(-i.cfg.ActiveSeriesMetricsIdleTimeout))

		case <-ctx.Done():
			return nil

		case err := <-i.subservicesWatcher.Chan():
			return errors.Wrap(err, "ingester subservice failed")
		}
	}
}

// stopping is run when ingester is asked to stop
func (i *Ingester) stopping(_ error) error {
	i.wal.Stop()

	// This will prevent us accepting any more samples
	i.stopIncomingRequests()

	// Lifecycler can be nil if the ingester is for a flusher.
	if i.lifecycler != nil {
		// Next initiate our graceful exit from the ring.
		return services.StopAndAwaitTerminated(context.Background(), i.lifecycler)
	}

	return nil
}

// ShutdownHandler triggers the following set of operations in order:
//     * Change the state of ring to stop accepting writes.
//     * Flush all the chunks.
func (i *Ingester) ShutdownHandler(w http.ResponseWriter, r *http.Request) {
	originalFlush := i.lifecycler.FlushOnShutdown()
	// We want to flush the chunks if transfer fails irrespective of original flag.
	i.lifecycler.SetFlushOnShutdown(true)

	// In the case of an HTTP shutdown, we want to unregister no matter what.
	originalUnregister := i.lifecycler.ShouldUnregisterOnShutdown()
	i.lifecycler.SetUnregisterOnShutdown(true)

	_ = services.StopAndAwaitTerminated(context.Background(), i)
	// Set state back to original.
	i.lifecycler.SetFlushOnShutdown(originalFlush)
	i.lifecycler.SetUnregisterOnShutdown(originalUnregister)

	w.WriteHeader(http.StatusNoContent)
}

// stopIncomingRequests is called during the shutdown process.
func (i *Ingester) stopIncomingRequests() {
	i.userStatesMtx.Lock()
	defer i.userStatesMtx.Unlock()
	i.stopped = true
}

// check that ingester has finished starting, i.e. it is in Running or Stopping state.
// Why Stopping? Because ingester still runs, even when it is transferring data out in Stopping state.
// Ingester handles this state on its own (via `stopped` flag).
func (i *Ingester) checkRunningOrStopping() error {
	s := i.State()
	if s == services.Running || s == services.Stopping {
		return nil
	}
	return status.Error(codes.Unavailable, s.String())
}

// Push implements client.IngesterServer
func (i *Ingester) Push(ctx context.Context, req *cortexpb.WriteRequest) (*cortexpb.WriteResponse, error) {
	if err := i.checkRunningOrStopping(); err != nil {
		return nil, err
	}

	// We will report *this* request in the error too.
	inflight := i.inflightPushRequests.Inc()
	defer i.inflightPushRequests.Dec()

	gl := i.getInstanceLimits()
	if gl != nil && gl.MaxInflightPushRequests > 0 {
		if inflight > gl.MaxInflightPushRequests {
			return nil, errTooManyInflightPushRequests
		}
	}

	if i.cfg.BlocksStorageEnabled {
		return i.v2Push(ctx, req)
	}

	// NOTE: because we use `unsafe` in deserialisation, we must not
	// retain anything from `req` past the call to ReuseSlice
	defer cortexpb.ReuseSlice(req.Timeseries)

	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	// Given metadata is a best-effort approach, and we don't halt on errors
	// process it before samples. Otherwise, we risk returning an error before ingestion.
	i.pushMetadata(ctx, userID, req.GetMetadata())

	var firstPartialErr *validationError
	var record *WALRecord
	if i.cfg.WALConfig.WALEnabled {
		record = recordPool.Get().(*WALRecord)
		record.UserID = userID
		// Assuming there is not much churn in most cases, there is no use
		// keeping the record.Labels slice hanging around.
		record.Series = nil
		if cap(record.Samples) < len(req.Timeseries) {
			record.Samples = make([]tsdb_record.RefSample, 0, len(req.Timeseries))
		} else {
			record.Samples = record.Samples[:0]
		}
	}

	for _, ts := range req.Timeseries {
		seriesSamplesIngested := 0
		for _, s := range ts.Samples {
			// append() copies the memory in `ts.Labels` except on the error path
			err := i.append(ctx, userID, ts.Labels, model.Time(s.TimestampMs), model.SampleValue(s.Value), req.Source, record)
			if err == nil {
				seriesSamplesIngested++
				continue
			}

			i.metrics.ingestedSamplesFail.Inc()
			if ve, ok := err.(*validationError); ok {
				if firstPartialErr == nil {
					firstPartialErr = ve
				}
				continue
			}

			// non-validation error: abandon this request
			return nil, grpcForwardableError(userID, http.StatusInternalServerError, err)
		}

		if i.cfg.ActiveSeriesMetricsEnabled && seriesSamplesIngested > 0 {
			// updateActiveSeries will copy labels if necessary.
			i.updateActiveSeries(userID, time.Now(), ts.Labels)
		}
	}

	if record != nil {
		// Log the record only if there was no error in ingestion.
		if err := i.wal.Log(record); err != nil {
			return nil, err
		}
		recordPool.Put(record)
	}

	if firstPartialErr != nil {
		// grpcForwardableError turns the error into a string so it no longer references `req`
		return &cortexpb.WriteResponse{}, grpcForwardableError(userID, firstPartialErr.code, firstPartialErr)
	}

	return &cortexpb.WriteResponse{}, nil
}

// NOTE: memory for `labels` is unsafe; anything retained beyond the
// life of this function must be copied
func (i *Ingester) append(ctx context.Context, userID string, labels labelPairs, timestamp model.Time, value model.SampleValue, source cortexpb.WriteRequest_SourceEnum, record *WALRecord) error {
	labels.removeBlanks()

	var (
		state *userState
		fp    model.Fingerprint
	)
	i.userStatesMtx.RLock()
	defer func() {
		i.userStatesMtx.RUnlock()
		if state != nil {
			state.fpLocker.Unlock(fp)
		}
	}()
	if i.stopped {
		return errIngesterStopping
	}

	// getOrCreateSeries copies the memory for `labels`, except on the error path.
	state, fp, series, err := i.userStates.getOrCreateSeries(ctx, userID, labels, record)
	if err != nil {
		if ve, ok := err.(*validationError); ok {
			state.discardedSamples.WithLabelValues(ve.errorType).Inc()
		}

		// Reset the state so that the defer will not try to unlock the fpLocker
		// in case of error, because that lock has already been released on error.
		state = nil
		return err
	}

	prevNumChunks := len(series.chunkDescs)
	if i.cfg.SpreadFlushes && prevNumChunks > 0 {
		// Map from the fingerprint hash to a point in the cycle of period MaxChunkAge
		startOfCycle := timestamp.Add(-(timestamp.Sub(model.Time(0)) % i.cfg.MaxChunkAge))
		slot := startOfCycle.Add(time.Duration(uint64(fp) % uint64(i.cfg.MaxChunkAge)))
		// If adding this sample means the head chunk will span that point in time, close so it will get flushed
		if series.head().FirstTime < slot && timestamp >= slot {
			series.closeHead(reasonSpreadFlush)
		}
	}

	if err := series.add(model.SamplePair{
		Value:     value,
		Timestamp: timestamp,
	}); err != nil {
		if ve, ok := err.(*validationError); ok {
			state.discardedSamples.WithLabelValues(ve.errorType).Inc()
			if ve.noReport {
				return nil
			}
		}
		return err
	}

	if record != nil {
		record.Samples = append(record.Samples, tsdb_record.RefSample{
			Ref: uint64(fp),
			T:   int64(timestamp),
			V:   float64(value),
		})
	}

	i.metrics.memoryChunks.Add(float64(len(series.chunkDescs) - prevNumChunks))
	i.metrics.ingestedSamples.Inc()
	switch source {
	case cortexpb.RULE:
		state.ingestedRuleSamples.Inc()
	case cortexpb.API:
		fallthrough
	default:
		state.ingestedAPISamples.Inc()
	}

	return err
}

// pushMetadata returns number of ingested metadata.
func (i *Ingester) pushMetadata(ctx context.Context, userID string, metadata []*cortexpb.MetricMetadata) int {
	ingestedMetadata := 0
	failedMetadata := 0

	var firstMetadataErr error
	for _, metadata := range metadata {
		err := i.appendMetadata(userID, metadata)
		if err == nil {
			ingestedMetadata++
			continue
		}

		failedMetadata++
		if firstMetadataErr == nil {
			firstMetadataErr = err
		}
	}

	i.metrics.ingestedMetadata.Add(float64(ingestedMetadata))
	i.metrics.ingestedMetadataFail.Add(float64(failedMetadata))

	// If we have any error with regard to metadata we just log and no-op.
	// We consider metadata a best effort approach, errors here should not stop processing.
	if firstMetadataErr != nil {
		logger := logutil.WithContext(ctx, i.logger)
		level.Warn(logger).Log("msg", "failed to ingest some metadata", "err", firstMetadataErr)
	}

	return ingestedMetadata
}

func (i *Ingester) appendMetadata(userID string, m *cortexpb.MetricMetadata) error {
	i.userStatesMtx.RLock()
	if i.stopped {
		i.userStatesMtx.RUnlock()
		return errIngesterStopping
	}
	i.userStatesMtx.RUnlock()

	userMetadata := i.getOrCreateUserMetadata(userID)

	return userMetadata.add(m.GetMetricFamilyName(), m)
}

func (i *Ingester) getOrCreateUserMetadata(userID string) *userMetricsMetadata {
	userMetadata := i.getUserMetadata(userID)
	if userMetadata != nil {
		return userMetadata
	}

	i.usersMetadataMtx.Lock()
	defer i.usersMetadataMtx.Unlock()

	// Ensure it was not created between switching locks.
	userMetadata, ok := i.usersMetadata[userID]
	if !ok {
		userMetadata = newMetadataMap(i.limiter, i.metrics, userID)
		i.usersMetadata[userID] = userMetadata
	}
	return userMetadata
}

func (i *Ingester) getUserMetadata(userID string) *userMetricsMetadata {
	i.usersMetadataMtx.RLock()
	defer i.usersMetadataMtx.RUnlock()
	return i.usersMetadata[userID]
}

func (i *Ingester) deleteUserMetadata(userID string) {
	i.usersMetadataMtx.Lock()
	um := i.usersMetadata[userID]
	delete(i.usersMetadata, userID)
	i.usersMetadataMtx.Unlock()

	if um != nil {
		// We need call purge to update i.metrics.memMetadata correctly (it counts number of metrics with metadata in memory).
		// Passing zero time means purge everything.
		um.purge(time.Time{})
	}
}

func (i *Ingester) getUsersWithMetadata() []string {
	i.usersMetadataMtx.RLock()
	defer i.usersMetadataMtx.RUnlock()

	userIDs := make([]string, 0, len(i.usersMetadata))
	for userID := range i.usersMetadata {
		userIDs = append(userIDs, userID)
	}

	return userIDs
}

func (i *Ingester) purgeUserMetricsMetadata() {
	deadline := time.Now().Add(-i.cfg.MetadataRetainPeriod)

	for _, userID := range i.getUsersWithMetadata() {
		metadata := i.getUserMetadata(userID)
		if metadata == nil {
			continue
		}

		// Remove all metadata that we no longer need to retain.
		metadata.purge(deadline)
	}
}

// Query implements service.IngesterServer
func (i *Ingester) Query(ctx context.Context, req *client.QueryRequest) (*client.QueryResponse, error) {
	if err := i.checkRunningOrStopping(); err != nil {
		return nil, err
	}

	if i.cfg.BlocksStorageEnabled {
		return i.v2Query(ctx, req)
	}

	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	from, through, matchers, err := client.FromQueryRequest(req)
	if err != nil {
		return nil, err
	}

	i.metrics.queries.Inc()

	i.userStatesMtx.RLock()
	state, ok, err := i.userStates.getViaContext(ctx)
	i.userStatesMtx.RUnlock()
	if err != nil {
		return nil, err
	} else if !ok {
		return &client.QueryResponse{}, nil
	}

	result := &client.QueryResponse{}
	numSeries, numSamples := 0, 0
	maxSamplesPerQuery := i.limits.MaxSamplesPerQuery(userID)
	err = state.forSeriesMatching(ctx, matchers, func(ctx context.Context, _ model.Fingerprint, series *memorySeries) error {
		values, err := series.samplesForRange(from, through)
		if err != nil {
			return err
		}
		if len(values) == 0 {
			return nil
		}
		numSeries++

		numSamples += len(values)
		if numSamples > maxSamplesPerQuery {
			return httpgrpc.Errorf(http.StatusRequestEntityTooLarge, "exceeded maximum number of samples in a query (%d)", maxSamplesPerQuery)
		}

		ts := cortexpb.TimeSeries{
			Labels:  cortexpb.FromLabelsToLabelAdapters(series.metric),
			Samples: make([]cortexpb.Sample, 0, len(values)),
		}
		for _, s := range values {
			ts.Samples = append(ts.Samples, cortexpb.Sample{
				Value:       float64(s.Value),
				TimestampMs: int64(s.Timestamp),
			})
		}
		result.Timeseries = append(result.Timeseries, ts)
		return nil
	}, nil, 0)
	i.metrics.queriedSeries.Observe(float64(numSeries))
	i.metrics.queriedSamples.Observe(float64(numSamples))
	return result, err
}

// QueryStream implements service.IngesterServer
func (i *Ingester) QueryStream(req *client.QueryRequest, stream client.Ingester_QueryStreamServer) error {
	if err := i.checkRunningOrStopping(); err != nil {
		return err
	}

	if i.cfg.BlocksStorageEnabled {
		return i.v2QueryStream(req, stream)
	}

	spanLog, ctx := spanlogger.New(stream.Context(), "QueryStream")
	defer spanLog.Finish()

	from, through, matchers, err := client.FromQueryRequest(req)
	if err != nil {
		return err
	}

	i.metrics.queries.Inc()

	i.userStatesMtx.RLock()
	state, ok, err := i.userStates.getViaContext(ctx)
	i.userStatesMtx.RUnlock()
	if err != nil {
		return err
	} else if !ok {
		return nil
	}

	numSeries, numChunks := 0, 0
	reuseWireChunks := [queryStreamBatchSize][]client.Chunk{}
	batch := make([]client.TimeSeriesChunk, 0, queryStreamBatchSize)
	// We'd really like to have series in label order, not FP order, so we
	// can iteratively merge them with entries coming from the chunk store.  But
	// that would involve locking all the series & sorting, so until we have
	// a better solution in the ingesters I'd rather take the hit in the queriers.
	err = state.forSeriesMatching(stream.Context(), matchers, func(ctx context.Context, _ model.Fingerprint, series *memorySeries) error {
		chunks := make([]*desc, 0, len(series.chunkDescs))
		for _, chunk := range series.chunkDescs {
			if !(chunk.FirstTime.After(through) || chunk.LastTime.Before(from)) {
				chunks = append(chunks, chunk.slice(from, through))
			}
		}

		if len(chunks) == 0 {
			return nil
		}

		numSeries++
		reusePos := len(batch)
		wireChunks, err := toWireChunks(chunks, reuseWireChunks[reusePos])
		if err != nil {
			return err
		}
		reuseWireChunks[reusePos] = wireChunks

		numChunks += len(wireChunks)
		batch = append(batch, client.TimeSeriesChunk{
			Labels: cortexpb.FromLabelsToLabelAdapters(series.metric),
			Chunks: wireChunks,
		})

		return nil
	}, func(ctx context.Context) error {
		if len(batch) == 0 {
			return nil
		}
		err = client.SendQueryStream(stream, &client.QueryStreamResponse{
			Chunkseries: batch,
		})
		batch = batch[:0]
		return err
	}, queryStreamBatchSize)
	if err != nil {
		return err
	}

	i.metrics.queriedSeries.Observe(float64(numSeries))
	i.metrics.queriedChunks.Observe(float64(numChunks))
	level.Debug(spanLog).Log("streams", numSeries)
	level.Debug(spanLog).Log("chunks", numChunks)
	return err
}

// LabelValues returns all label values that are associated with a given label name.
func (i *Ingester) LabelValues(ctx context.Context, req *client.LabelValuesRequest) (*client.LabelValuesResponse, error) {
	if err := i.checkRunningOrStopping(); err != nil {
		return nil, err
	}

	if i.cfg.BlocksStorageEnabled {
		return i.v2LabelValues(ctx, req)
	}

	i.userStatesMtx.RLock()
	defer i.userStatesMtx.RUnlock()
	state, ok, err := i.userStates.getViaContext(ctx)
	if err != nil {
		return nil, err
	} else if !ok {
		return &client.LabelValuesResponse{}, nil
	}

	resp := &client.LabelValuesResponse{}
	resp.LabelValues = append(resp.LabelValues, state.index.LabelValues(req.LabelName)...)

	return resp, nil
}

// LabelNames return all the label names.
func (i *Ingester) LabelNames(ctx context.Context, req *client.LabelNamesRequest) (*client.LabelNamesResponse, error) {
	if err := i.checkRunningOrStopping(); err != nil {
		return nil, err
	}

	if i.cfg.BlocksStorageEnabled {
		return i.v2LabelNames(ctx, req)
	}

	i.userStatesMtx.RLock()
	defer i.userStatesMtx.RUnlock()
	state, ok, err := i.userStates.getViaContext(ctx)
	if err != nil {
		return nil, err
	} else if !ok {
		return &client.LabelNamesResponse{}, nil
	}

	resp := &client.LabelNamesResponse{}
	resp.LabelNames = append(resp.LabelNames, state.index.LabelNames()...)

	return resp, nil
}

// MetricsForLabelMatchers returns all the metrics which match a set of matchers.
func (i *Ingester) MetricsForLabelMatchers(ctx context.Context, req *client.MetricsForLabelMatchersRequest) (*client.MetricsForLabelMatchersResponse, error) {
	if err := i.checkRunningOrStopping(); err != nil {
		return nil, err
	}

	if i.cfg.BlocksStorageEnabled {
		return i.v2MetricsForLabelMatchers(ctx, req)
	}

	i.userStatesMtx.RLock()
	defer i.userStatesMtx.RUnlock()
	state, ok, err := i.userStates.getViaContext(ctx)
	if err != nil {
		return nil, err
	} else if !ok {
		return &client.MetricsForLabelMatchersResponse{}, nil
	}

	// TODO Right now we ignore start and end.
	_, _, matchersSet, err := client.FromMetricsForLabelMatchersRequest(req)
	if err != nil {
		return nil, err
	}

	lss := map[model.Fingerprint]labels.Labels{}
	for _, matchers := range matchersSet {
		if err := state.forSeriesMatching(ctx, matchers, func(ctx context.Context, fp model.Fingerprint, series *memorySeries) error {
			if _, ok := lss[fp]; !ok {
				lss[fp] = series.metric
			}
			return nil
		}, nil, 0); err != nil {
			return nil, err
		}
	}

	result := &client.MetricsForLabelMatchersResponse{
		Metric: make([]*cortexpb.Metric, 0, len(lss)),
	}
	for _, ls := range lss {
		result.Metric = append(result.Metric, &cortexpb.Metric{Labels: cortexpb.FromLabelsToLabelAdapters(ls)})
	}

	return result, nil
}

// MetricsMetadata returns all the metric metadata of a user.
func (i *Ingester) MetricsMetadata(ctx context.Context, req *client.MetricsMetadataRequest) (*client.MetricsMetadataResponse, error) {
	i.userStatesMtx.RLock()
	if err := i.checkRunningOrStopping(); err != nil {
		i.userStatesMtx.RUnlock()
		return nil, err
	}
	i.userStatesMtx.RUnlock()

	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	userMetadata := i.getUserMetadata(userID)

	if userMetadata == nil {
		return &client.MetricsMetadataResponse{}, nil
	}

	return &client.MetricsMetadataResponse{Metadata: userMetadata.toClientMetadata()}, nil
}

// UserStats returns ingestion statistics for the current user.
func (i *Ingester) UserStats(ctx context.Context, req *client.UserStatsRequest) (*client.UserStatsResponse, error) {
	if err := i.checkRunningOrStopping(); err != nil {
		return nil, err
	}

	if i.cfg.BlocksStorageEnabled {
		return i.v2UserStats(ctx, req)
	}

	i.userStatesMtx.RLock()
	defer i.userStatesMtx.RUnlock()
	state, ok, err := i.userStates.getViaContext(ctx)
	if err != nil {
		return nil, err
	} else if !ok {
		return &client.UserStatsResponse{}, nil
	}

	apiRate := state.ingestedAPISamples.Rate()
	ruleRate := state.ingestedRuleSamples.Rate()
	return &client.UserStatsResponse{
		IngestionRate:     apiRate + ruleRate,
		ApiIngestionRate:  apiRate,
		RuleIngestionRate: ruleRate,
		NumSeries:         uint64(state.fpToSeries.length()),
	}, nil
}

// AllUserStats returns ingestion statistics for all users known to this ingester.
func (i *Ingester) AllUserStats(ctx context.Context, req *client.UserStatsRequest) (*client.UsersStatsResponse, error) {
	if err := i.checkRunningOrStopping(); err != nil {
		return nil, err
	}

	if i.cfg.BlocksStorageEnabled {
		return i.v2AllUserStats(ctx, req)
	}

	i.userStatesMtx.RLock()
	defer i.userStatesMtx.RUnlock()
	users := i.userStates.cp()

	response := &client.UsersStatsResponse{
		Stats: make([]*client.UserIDStatsResponse, 0, len(users)),
	}
	for userID, state := range users {
		apiRate := state.ingestedAPISamples.Rate()
		ruleRate := state.ingestedRuleSamples.Rate()
		response.Stats = append(response.Stats, &client.UserIDStatsResponse{
			UserId: userID,
			Data: &client.UserStatsResponse{
				IngestionRate:     apiRate + ruleRate,
				ApiIngestionRate:  apiRate,
				RuleIngestionRate: ruleRate,
				NumSeries:         uint64(state.fpToSeries.length()),
			},
		})
	}
	return response, nil
}

// CheckReady is the readiness handler used to indicate to k8s when the ingesters
// are ready for the addition or removal of another ingester.
func (i *Ingester) CheckReady(ctx context.Context) error {
	if err := i.checkRunningOrStopping(); err != nil {
		return fmt.Errorf("ingester not ready: %v", err)
	}
	return i.lifecycler.CheckReady(ctx)
}

// labels will be copied if needed.
func (i *Ingester) updateActiveSeries(userID string, now time.Time, labels []cortexpb.LabelAdapter) {
	i.userStatesMtx.RLock()
	defer i.userStatesMtx.RUnlock()

	i.userStates.updateActiveSeriesForUser(userID, now, cortexpb.FromLabelAdaptersToLabels(labels))
}
