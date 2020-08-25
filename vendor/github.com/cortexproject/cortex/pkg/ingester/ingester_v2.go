package ingester

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/gate"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/objstore"
	"github.com/thanos-io/thanos/pkg/shipper"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"
	"go.uber.org/atomic"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/ring"
	cortex_tsdb "github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/extract"
	"github.com/cortexproject/cortex/pkg/util/services"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/cortexproject/cortex/pkg/util/validation"
)

const (
	errTSDBCreateIncompatibleState = "cannot create a new TSDB while the ingester is not in active state (current state: %s)"
)

// Shipper interface is used to have an easy way to mock it in tests.
type Shipper interface {
	Sync(ctx context.Context) (uploaded int, err error)
}

type userTSDB struct {
	*tsdb.DB
	userID         string
	refCache       *cortex_tsdb.RefCache
	seriesInMetric *metricCounter
	limiter        *Limiter

	// Used to detect idle TSDBs.
	lastUpdate *atomic.Int64

	// Thanos shipper used to ship blocks to the storage.
	shipper Shipper

	// for statistics
	ingestedAPISamples  *ewmaRate
	ingestedRuleSamples *ewmaRate
}

// PreCreation implements SeriesLifecycleCallback interface.
func (u *userTSDB) PreCreation(metric labels.Labels) error {
	if u.limiter == nil {
		return nil
	}

	// Total series limit.
	if err := u.limiter.AssertMaxSeriesPerUser(u.userID, int(u.DB.Head().NumSeries())); err != nil {
		return makeLimitError(perUserSeriesLimit, err)
	}

	// Series per metric name limit.
	metricName, err := extract.MetricNameFromLabels(metric)
	if err != nil {
		return err
	}
	if err := u.seriesInMetric.canAddSeriesFor(u.userID, metricName); err != nil {
		return makeMetricLimitError(perMetricSeriesLimit, metric, err)
	}

	return nil
}

// PostCreation implements SeriesLifecycleCallback interface.
func (u *userTSDB) PostCreation(metric labels.Labels) {
	metricName, err := extract.MetricNameFromLabels(metric)
	if err != nil {
		// This should never happen because it has already been checked in PreCreation().
		return
	}
	u.seriesInMetric.increaseSeriesForMetric(metricName)
}

// PostDeletion implements SeriesLifecycleCallback interface.
func (u *userTSDB) PostDeletion(metrics ...labels.Labels) {
	for _, metric := range metrics {
		metricName, err := extract.MetricNameFromLabels(metric)
		if err != nil {
			// This should never happen because it has already been checked in PreCreation().
			continue
		}
		u.seriesInMetric.decreaseSeriesForMetric(metricName)
	}
}

func (u *userTSDB) isIdle(now time.Time, idle time.Duration) bool {
	lu := u.lastUpdate.Load()

	return time.Unix(lu, 0).Add(idle).Before(now)
}

func (u *userTSDB) setLastUpdate(t time.Time) {
	u.lastUpdate.Store(t.Unix())
}

// TSDBState holds data structures used by the TSDB storage engine
type TSDBState struct {
	dbs    map[string]*userTSDB // tsdb sharded by userID
	bucket objstore.Bucket

	// Value used by shipper as external label.
	shipperIngesterID string

	subservices *services.Manager

	tsdbMetrics *tsdbMetrics

	forceCompactTrigger chan chan<- struct{}
	shipTrigger         chan chan<- struct{}

	// Head compactions metrics.
	compactionsTriggered   prometheus.Counter
	compactionsFailed      prometheus.Counter
	walReplayTime          prometheus.Histogram
	appenderAddDuration    prometheus.Histogram
	appenderCommitDuration prometheus.Histogram
	refCachePurgeDuration  prometheus.Histogram
}

func newTSDBState(bucketClient objstore.Bucket, registerer prometheus.Registerer) TSDBState {
	return TSDBState{
		dbs:                 make(map[string]*userTSDB),
		bucket:              bucketClient,
		tsdbMetrics:         newTSDBMetrics(registerer),
		forceCompactTrigger: make(chan chan<- struct{}),
		shipTrigger:         make(chan chan<- struct{}),

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
		refCachePurgeDuration: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Name:    "cortex_ingester_tsdb_refcache_purge_duration_seconds",
			Help:    "The total time it takes to purge the TSDB series reference cache for a single tenant.",
			Buckets: prometheus.DefBuckets,
		}),
	}
}

// NewV2 returns a new Ingester that uses Cortex block storage instead of chunks storage.
func NewV2(cfg Config, clientConfig client.Config, limits *validation.Overrides, registerer prometheus.Registerer) (*Ingester, error) {
	util.WarnExperimentalUse("Blocks storage engine")
	bucketClient, err := cortex_tsdb.NewBucketClient(context.Background(), cfg.BlocksStorageConfig.Bucket, "ingester", util.Logger, registerer)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the bucket client")
	}

	i := &Ingester{
		cfg:           cfg,
		clientConfig:  clientConfig,
		metrics:       newIngesterMetrics(registerer, false),
		limits:        limits,
		chunkStore:    nil,
		usersMetadata: map[string]*userMetricsMetadata{},
		wal:           &noopWAL{},
		TSDBState:     newTSDBState(bucketClient, registerer),
	}

	// Replace specific metrics which we can't directly track but we need to read
	// them from the underlying system (ie. TSDB).
	if registerer != nil {
		registerer.Unregister(i.metrics.memSeries)
		promauto.With(registerer).NewGaugeFunc(prometheus.GaugeOpts{
			Name: "cortex_ingester_memory_series",
			Help: "The current number of series in memory.",
		}, i.numSeriesInTSDB)
	}

	i.lifecycler, err = ring.NewLifecycler(cfg.LifecyclerConfig, i, "ingester", ring.IngesterRingKey, cfg.BlocksStorageConfig.TSDB.FlushBlocksOnShutdown, registerer)
	if err != nil {
		return nil, err
	}
	i.subservicesWatcher = services.NewFailureWatcher()
	i.subservicesWatcher.WatchService(i.lifecycler)

	// Init the limter and instantiate the user states which depend on it
	i.limiter = NewLimiter(limits, i.lifecycler, cfg.LifecyclerConfig.RingConfig.ReplicationFactor, cfg.ShardByAllLabels)
	i.userStates = newUserStates(i.limiter, cfg, i.metrics)

	i.TSDBState.shipperIngesterID = i.lifecycler.ID

	i.BasicService = services.NewBasicService(i.startingV2, i.updateLoop, i.stoppingV2)
	return i, nil
}

// Special version of ingester used by Flusher. This ingester is not ingesting anything, its only purpose is to react
// on Flush method and flush all openened TSDBs when called.
func NewV2ForFlusher(cfg Config, registerer prometheus.Registerer) (*Ingester, error) {
	util.WarnExperimentalUse("Blocks storage engine")
	bucketClient, err := cortex_tsdb.NewBucketClient(context.Background(), cfg.BlocksStorageConfig.Bucket, "ingester", util.Logger, registerer)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the bucket client")
	}

	i := &Ingester{
		cfg:       cfg,
		metrics:   newIngesterMetrics(registerer, false),
		wal:       &noopWAL{},
		TSDBState: newTSDBState(bucketClient, registerer),
	}

	i.TSDBState.shipperIngesterID = "flusher"

	// This ingester will not start any subservices (lifecycler, compaction, shipping),
	// and will only open TSDBs, wait for Flush to be called, and then close TSDBs again.
	i.BasicService = services.NewIdleService(i.startingV2ForFlusher, i.stoppingV2ForFlusher)
	return i, nil
}

func (i *Ingester) startingV2ForFlusher(ctx context.Context) error {
	if err := i.openExistingTSDB(ctx); err != nil {
		return errors.Wrap(err, "opening existing TSDBs")
	}

	// Don't start any sub-services (lifecycler, compaction, shipper) at all.
	return nil
}

func (i *Ingester) startingV2(ctx context.Context) error {
	if err := i.openExistingTSDB(ctx); err != nil {
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

	if i.cfg.BlocksStorageConfig.TSDB.ShipInterval > 0 {
		shippingService := services.NewBasicService(nil, i.shipBlocksLoop, nil)
		servs = append(servs, shippingService)
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
		level.Warn(util.Logger).Log("msg", "failed to stop ingester subservices", "err", err)
	}

	// Next initiate our graceful exit from the ring.
	if err := services.StopAndAwaitTerminated(context.Background(), i.lifecycler); err != nil {
		level.Warn(util.Logger).Log("msg", "failed to stop ingester lifecycler", "err", err)
	}

	if !i.cfg.BlocksStorageConfig.TSDB.KeepUserTSDBOpenOnShutdown {
		i.closeAllTSDB()
	}
	return nil
}

func (i *Ingester) updateLoop(ctx context.Context) error {
	rateUpdateTicker := time.NewTicker(i.cfg.RateUpdatePeriod)
	defer rateUpdateTicker.Stop()

	// We use an hardcoded value for this ticker because there should be no
	// real value in customizing it.
	refCachePurgeTicker := time.NewTicker(5 * time.Minute)
	defer refCachePurgeTicker.Stop()

	// Similarly to the above, this is a hardcoded value.
	metadataPurgeTicker := time.NewTicker(metadataPurgePeriod)
	defer metadataPurgeTicker.Stop()

	for {
		select {
		case <-metadataPurgeTicker.C:
			i.purgeUserMetricsMetadata()
		case <-rateUpdateTicker.C:
			i.userStatesMtx.RLock()
			for _, db := range i.TSDBState.dbs {
				db.ingestedAPISamples.tick()
				db.ingestedRuleSamples.tick()
			}
			i.userStatesMtx.RUnlock()
		case <-refCachePurgeTicker.C:
			for _, userID := range i.getTSDBUsers() {
				userDB := i.getTSDB(userID)
				if userDB == nil {
					continue
				}

				startTime := time.Now()
				userDB.refCache.Purge(startTime.Add(-cortex_tsdb.DefaultRefCacheTTL))
				i.TSDBState.refCachePurgeDuration.Observe(time.Since(startTime).Seconds())
			}
		case <-ctx.Done():
			return nil
		case err := <-i.subservicesWatcher.Chan():
			return errors.Wrap(err, "ingester subservice failed")
		}
	}
}

// v2Push adds metrics to a block
func (i *Ingester) v2Push(ctx context.Context, req *client.WriteRequest) (*client.WriteResponse, error) {
	var firstPartialErr error

	// NOTE: because we use `unsafe` in deserialisation, we must not
	// retain anything from `req` past the call to ReuseSlice
	defer client.ReuseSlice(req.Timeseries)

	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, fmt.Errorf("no user id")
	}

	db, err := i.getOrCreateTSDB(userID, false)
	if err != nil {
		return nil, wrapWithUser(err, userID)
	}

	// Ensure the ingester shutdown procedure hasn't started
	i.userStatesMtx.RLock()
	if i.stopped {
		i.userStatesMtx.RUnlock()
		return nil, fmt.Errorf("ingester stopping")
	}
	i.userStatesMtx.RUnlock()

	// Given metadata is a best-effort approach, and we don't halt on errors
	// process it before samples. Otherwise, we risk returning an error before ingestion.
	i.pushMetadata(ctx, userID, req.GetMetadata())

	// Keep track of some stats which are tracked only if the samples will be
	// successfully committed
	succeededSamplesCount := 0
	failedSamplesCount := 0
	startAppend := time.Now()

	// Walk the samples, appending them to the users database
	app := db.Appender(ctx)
	for _, ts := range req.Timeseries {
		// Check if we already have a cached reference for this series. Be aware
		// that even if we have a reference it's not guaranteed to be still valid.
		// The labels must be sorted (in our case, it's guaranteed a write request
		// has sorted labels once hit the ingester).
		cachedRef, cachedRefExists := db.refCache.Ref(startAppend, client.FromLabelAdaptersToLabels(ts.Labels))

		for _, s := range ts.Samples {
			var err error

			// If the cached reference exists, we try to use it.
			if cachedRefExists {
				if err = app.AddFast(cachedRef, s.TimestampMs, s.Value); err == nil {
					succeededSamplesCount++
					continue
				}

				if errors.Cause(err) == storage.ErrNotFound {
					cachedRefExists = false
					err = nil
				}
			}

			// If the cached reference doesn't exist, we (re)try without using the reference.
			if !cachedRefExists {
				var ref uint64

				// Copy the label set because both TSDB and the cache may retain it.
				copiedLabels := client.FromLabelAdaptersToLabelsWithCopy(ts.Labels)

				if ref, err = app.Add(copiedLabels, s.TimestampMs, s.Value); err == nil {
					db.refCache.SetRef(startAppend, copiedLabels, ref)
					cachedRef = ref
					cachedRefExists = true

					succeededSamplesCount++
					continue
				}
			}

			failedSamplesCount++

			// Check if the error is a soft error we can proceed on. If so, we keep track
			// of it, so that we can return it back to the distributor, which will return a
			// 400 error to the client. The client (Prometheus) will not retry on 400, and
			// we actually ingested all samples which haven't failed.
			cause := errors.Cause(err)
			if cause == storage.ErrOutOfBounds || cause == storage.ErrOutOfOrderSample || cause == storage.ErrDuplicateSampleForTimestamp {
				if firstPartialErr == nil {
					firstPartialErr = errors.Wrapf(err, "series=%s, timestamp=%v", client.FromLabelAdaptersToLabels(ts.Labels).String(), model.Time(s.TimestampMs).Time().UTC().Format(time.RFC3339Nano))
				}

				switch cause {
				case storage.ErrOutOfBounds:
					validation.DiscardedSamples.WithLabelValues(sampleOutOfBounds, userID).Inc()
				case storage.ErrOutOfOrderSample:
					validation.DiscardedSamples.WithLabelValues(sampleOutOfOrder, userID).Inc()
				case storage.ErrDuplicateSampleForTimestamp:
					validation.DiscardedSamples.WithLabelValues(newValueForTimestamp, userID).Inc()
				}

				continue
			}

			var ve *validationError
			if errors.As(cause, &ve) {
				// Caused by limits.
				if firstPartialErr == nil {
					firstPartialErr = ve
				}
				validation.DiscardedSamples.WithLabelValues(ve.errorType, userID).Inc()
				continue
			}

			// The error looks an issue on our side, so we should rollback
			if rollbackErr := app.Rollback(); rollbackErr != nil {
				level.Warn(util.Logger).Log("msg", "failed to rollback on error", "user", userID, "err", rollbackErr)
			}

			return nil, wrapWithUser(err, userID)
		}
	}

	// At this point all samples have been added to the appender, so we can track the time it took.
	i.TSDBState.appenderAddDuration.Observe(time.Since(startAppend).Seconds())

	startCommit := time.Now()
	if err := app.Commit(); err != nil {
		return nil, wrapWithUser(err, userID)
	}
	i.TSDBState.appenderCommitDuration.Observe(time.Since(startCommit).Seconds())

	db.setLastUpdate(time.Now())

	// Increment metrics only if the samples have been successfully committed.
	// If the code didn't reach this point, it means that we returned an error
	// which will be converted into an HTTP 5xx and the client should/will retry.
	i.metrics.ingestedSamples.Add(float64(succeededSamplesCount))
	i.metrics.ingestedSamplesFail.Add(float64(failedSamplesCount))

	switch req.Source {
	case client.RULE:
		db.ingestedRuleSamples.add(int64(succeededSamplesCount))
	case client.API:
		fallthrough
	default:
		db.ingestedAPISamples.add(int64(succeededSamplesCount))
	}

	if firstPartialErr != nil {
		code := http.StatusBadRequest
		var ve *validationError
		if errors.As(firstPartialErr, &ve) {
			code = ve.code
		}
		return &client.WriteResponse{}, httpgrpc.Errorf(code, wrapWithUser(firstPartialErr, userID).Error())
	}

	return &client.WriteResponse{}, nil
}

func (i *Ingester) v2Query(ctx context.Context, req *client.QueryRequest) (*client.QueryResponse, error) {
	userID, err := user.ExtractOrgID(ctx)
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

		ts := client.TimeSeries{
			Labels: client.FromLabelsToLabelAdapters(series.Labels()),
		}

		it := series.Iterator()
		for it.Next() {
			t, v := it.At()
			ts.Samples = append(ts.Samples, client.Sample{Value: v, TimestampMs: t})
		}

		numSamples += len(ts.Samples)
		result.Timeseries = append(result.Timeseries, ts)
	}

	i.metrics.queriedSeries.Observe(float64(len(result.Timeseries)))
	i.metrics.queriedSamples.Observe(float64(numSamples))

	return result, ss.Err()
}

func (i *Ingester) v2LabelValues(ctx context.Context, req *client.LabelValuesRequest) (*client.LabelValuesResponse, error) {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	db := i.getTSDB(userID)
	if db == nil {
		return &client.LabelValuesResponse{}, nil
	}

	// Since ingester may run with a variable TSDB retention which could be few days long,
	// we only query the TSDB head time range in order to avoid heavy queries (which could
	// lead to ingesters out-of-memory) in case the TSDB retention is several days.
	q, err := db.Querier(ctx, db.Head().MinTime(), db.Head().MaxTime())
	if err != nil {
		return nil, err
	}
	defer q.Close()

	vals, _, err := q.LabelValues(req.LabelName)
	if err != nil {
		return nil, err
	}

	return &client.LabelValuesResponse{
		LabelValues: vals,
	}, nil
}

func (i *Ingester) v2LabelNames(ctx context.Context, req *client.LabelNamesRequest) (*client.LabelNamesResponse, error) {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	db := i.getTSDB(userID)
	if db == nil {
		return &client.LabelNamesResponse{}, nil
	}

	// Since ingester may run with a variable TSDB retention which could be few days long,
	// we only query the TSDB head time range in order to avoid heavy queries (which could
	// lead to ingesters out-of-memory) in case the TSDB retention is several days.
	q, err := db.Querier(ctx, db.Head().MinTime(), db.Head().MaxTime())
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
	userID, err := user.ExtractOrgID(ctx)
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

	// Since ingester may run with a variable TSDB retention which could be few days long,
	// we only query the TSDB head time range in order to avoid heavy queries (which could
	// lead to ingesters out-of-memory) in case the TSDB retention is several days.
	q, err := db.Querier(ctx, db.Head().MinTime(), db.Head().MaxTime())
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

		seriesSet := q.Select(true, nil, matchers...)
		sets = append(sets, seriesSet)
	}

	// Generate the response merging all series sets.
	result := &client.MetricsForLabelMatchersResponse{
		Metric: make([]*client.Metric, 0),
	}

	mergedSet := storage.NewMergeSeriesSet(sets, storage.ChainedSeriesMerge)
	for mergedSet.Next() {
		// Interrupt if the context has been canceled.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		result.Metric = append(result.Metric, &client.Metric{
			Labels: client.FromLabelsToLabelAdapters(mergedSet.At().Labels()),
		})
	}

	return result, nil
}

func (i *Ingester) v2UserStats(ctx context.Context, req *client.UserStatsRequest) (*client.UserStatsResponse, error) {
	userID, err := user.ExtractOrgID(ctx)
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
	apiRate := db.ingestedAPISamples.rate()
	ruleRate := db.ingestedRuleSamples.rate()
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
	log, ctx := spanlogger.New(stream.Context(), "v2QueryStream")
	defer log.Finish()

	userID, err := user.ExtractOrgID(ctx)
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

	q, err := db.Querier(ctx, int64(from), int64(through))
	if err != nil {
		return err
	}
	defer q.Close()

	// It's not required to return sorted series because series are sorted by the Cortex querier.
	ss := q.Select(false, nil, matchers...)
	if ss.Err() != nil {
		return ss.Err()
	}

	timeseries := make([]client.TimeSeries, 0, queryStreamBatchSize)
	batchSizeBytes := 0
	numSamples := 0
	numSeries := 0
	for ss.Next() {
		series := ss.At()

		// convert labels to LabelAdapter
		ts := client.TimeSeries{
			Labels: client.FromLabelsToLabelAdapters(series.Labels()),
		}

		it := series.Iterator()
		for it.Next() {
			t, v := it.At()
			ts.Samples = append(ts.Samples, client.Sample{Value: v, TimestampMs: t})
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
				return err
			}

			batchSizeBytes = 0
			timeseries = timeseries[:0]
		}

		timeseries = append(timeseries, ts)
		batchSizeBytes += tsSize
	}

	// Ensure no error occurred while iterating the series set.
	if err := ss.Err(); err != nil {
		return err
	}

	// Final flush any existing metrics
	if batchSizeBytes != 0 {
		err = client.SendQueryStream(stream, &client.QueryStreamResponse{
			Timeseries: timeseries,
		})
		if err != nil {
			return err
		}
	}

	i.metrics.queriedSeries.Observe(float64(numSeries))
	i.metrics.queriedSamples.Observe(float64(numSamples))
	level.Debug(log).Log("series", numSeries, "samples", numSamples)
	return nil
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
	userLogger := util.WithUserID(userID, util.Logger)

	blockRanges := i.cfg.BlocksStorageConfig.TSDB.BlockRanges.ToMilliseconds()

	userDB := &userTSDB{
		userID:              userID,
		refCache:            cortex_tsdb.NewRefCache(),
		seriesInMetric:      newMetricCounter(i.limiter),
		ingestedAPISamples:  newEWMARate(0.2, i.cfg.RateUpdatePeriod),
		ingestedRuleSamples: newEWMARate(0.2, i.cfg.RateUpdatePeriod),
		lastUpdate:          atomic.NewInt64(0),
	}

	// Create a new user database
	db, err := tsdb.Open(udir, userLogger, tsdbPromReg, &tsdb.Options{
		RetentionDuration:       i.cfg.BlocksStorageConfig.TSDB.Retention.Milliseconds(),
		MinBlockDuration:        blockRanges[0],
		MaxBlockDuration:        blockRanges[len(blockRanges)-1],
		NoLockfile:              true,
		StripeSize:              i.cfg.BlocksStorageConfig.TSDB.StripeSize,
		WALCompression:          i.cfg.BlocksStorageConfig.TSDB.WALCompressionEnabled,
		SeriesLifecycleCallback: userDB,
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

	userDB.DB = db
	// We set the limiter here because we don't want to limit
	// series during WAL replay.
	userDB.limiter = i.limiter
	userDB.setLastUpdate(time.Now()) // After WAL replay.

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
	if i.cfg.BlocksStorageConfig.TSDB.ShipInterval > 0 {
		userDB.shipper = shipper.New(
			userLogger,
			tsdbPromReg,
			udir,
			cortex_tsdb.NewUserBucketClient(userID, i.TSDBState.bucket),
			func() labels.Labels { return l },
			metadata.ReceiveSource,
			false, // No need to upload compacted blocks. Cortex compactor takes care of that.
			true,  // Allow out of order uploads. It's fine in Cortex's context.
		)
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
				level.Warn(util.Logger).Log("msg", "unable to close TSDB", "err", err, "user", userID)
				return
			}

			// Now that the TSDB has been closed, we should remove it from the
			// set of open ones. This lock acquisition doesn't deadlock with the
			// outer one, because the outer one is released as soon as all go
			// routines are started.
			i.userStatesMtx.Lock()
			delete(i.TSDBState.dbs, userID)
			i.userStatesMtx.Unlock()
		}(userDB)
	}

	// Wait until all Close() completed
	i.userStatesMtx.Unlock()
	wg.Wait()
}

// openExistingTSDB walks the user tsdb dir, and opens a tsdb for each user. This may start a WAL replay, so we limit the number of
// concurrently opening TSDB.
func (i *Ingester) openExistingTSDB(ctx context.Context) error {
	level.Info(util.Logger).Log("msg", "opening existing TSDBs")
	wg := &sync.WaitGroup{}
	openGate := gate.New(i.cfg.BlocksStorageConfig.TSDB.MaxTSDBOpeningConcurrencyOnStartup)

	err := filepath.Walk(i.cfg.BlocksStorageConfig.TSDB.Dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return filepath.SkipDir
		}

		// Skip root dir and all other files
		if path == i.cfg.BlocksStorageConfig.TSDB.Dir || !info.IsDir() {
			return nil
		}

		// Top level directories are assumed to be user TSDBs
		userID := info.Name()
		f, err := os.Open(path)
		if err != nil {
			level.Error(util.Logger).Log("msg", "unable to open user TSDB dir", "err", err, "user", userID, "path", path)
			return filepath.SkipDir
		}
		defer f.Close()

		// If the dir is empty skip it
		if _, err := f.Readdirnames(1); err != nil {
			if err != io.EOF {
				level.Error(util.Logger).Log("msg", "unable to read TSDB dir", "err", err, "user", userID, "path", path)
			}

			return filepath.SkipDir
		}

		// Limit the number of TSDB's opening concurrently. Start blocks until there's a free spot available or the context is cancelled.
		if err := openGate.Start(ctx); err != nil {
			return err
		}

		wg.Add(1)
		go func(userID string) {
			defer wg.Done()
			defer openGate.Done()
			defer func(ts time.Time) {
				i.TSDBState.walReplayTime.Observe(time.Since(ts).Seconds())
			}(time.Now())

			db, err := i.createTSDB(userID)
			if err != nil {
				level.Error(util.Logger).Log("msg", "unable to open user TSDB", "err", err, "user", userID)
				return
			}

			// Add the database to the map of user databases
			i.userStatesMtx.Lock()
			i.TSDBState.dbs[userID] = db
			i.userStatesMtx.Unlock()
			i.metrics.memUsers.Inc()
		}(userID)

		return filepath.SkipDir // Don't descend into directories
	})

	// Wait for all opening routines to finish
	wg.Wait()
	if err != nil {
		level.Error(util.Logger).Log("msg", "error while opening existing TSDBs")
	} else {
		level.Info(util.Logger).Log("msg", "successfully opened existing TSDBs")
	}
	return err
}

// numSeriesInTSDB returns the total number of in-memory series across all open TSDBs.
func (i *Ingester) numSeriesInTSDB() float64 {
	i.userStatesMtx.RLock()
	defer i.userStatesMtx.RUnlock()

	count := uint64(0)
	for _, db := range i.TSDBState.dbs {
		count += db.Head().NumSeries()
	}

	return float64(count)
}

func (i *Ingester) shipBlocksLoop(ctx context.Context) error {
	shipTicker := time.NewTicker(i.cfg.BlocksStorageConfig.TSDB.ShipInterval)
	defer shipTicker.Stop()

	for {
		select {
		case <-shipTicker.C:
			i.shipBlocks(ctx)

		case ch := <-i.TSDBState.shipTrigger:
			i.shipBlocks(ctx)

			// Notify back.
			select {
			case ch <- struct{}{}:
			default: // Nobody is waiting for notification, don't block this loop.
			}

		case <-ctx.Done():
			return nil
		}
	}
}

func (i *Ingester) shipBlocks(ctx context.Context) {
	// Do not ship blocks if the ingester is PENDING or JOINING. It's
	// particularly important for the JOINING state because there could
	// be a blocks transfer in progress (from another ingester) and if we
	// run the shipper in such state we could end up with race conditions.
	if i.lifecycler != nil {
		if ingesterState := i.lifecycler.GetState(); ingesterState == ring.PENDING || ingesterState == ring.JOINING {
			level.Info(util.Logger).Log("msg", "TSDB blocks shipping has been skipped because of the current ingester state", "state", ingesterState)
			return
		}
	}

	// Number of concurrent workers is limited in order to avoid to concurrently sync a lot
	// of tenants in a large cluster.
	i.runConcurrentUserWorkers(ctx, i.cfg.BlocksStorageConfig.TSDB.ShipConcurrency, func(userID string) {
		// Get the user's DB. If the user doesn't exist, we skip it.
		userDB := i.getTSDB(userID)
		if userDB == nil || userDB.shipper == nil {
			return
		}

		// Run the shipper's Sync() to upload unshipped blocks.
		if uploaded, err := userDB.shipper.Sync(ctx); err != nil {
			level.Warn(util.Logger).Log("msg", "shipper failed to synchronize TSDB blocks with the storage", "user", userID, "uploaded", uploaded, "err", err)
		} else {
			level.Debug(util.Logger).Log("msg", "shipper successfully synchronized TSDB blocks with storage", "user", userID, "uploaded", uploaded)
		}
	})
}

func (i *Ingester) compactionLoop(ctx context.Context) error {
	ticker := time.NewTicker(i.cfg.BlocksStorageConfig.TSDB.HeadCompactionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			i.compactBlocks(ctx, false)

		case ch := <-i.TSDBState.forceCompactTrigger:
			i.compactBlocks(ctx, true)

			// Notify back.
			select {
			case ch <- struct{}{}:
			default: // Nobody is waiting for notification, don't block this loop.
			}

		case <-ctx.Done():
			return nil
		}
	}
}

// Compacts all compactable blocks. Force flag will force compaction even if head is not compactable yet.
func (i *Ingester) compactBlocks(ctx context.Context, force bool) {
	// Don't compact TSDB blocks while JOINING as there may be ongoing blocks transfers.
	// Compaction loop is not running in LEAVING state, so if we get here in LEAVING state, we're flushing blocks.
	if i.lifecycler != nil {
		if ingesterState := i.lifecycler.GetState(); ingesterState == ring.JOINING {
			level.Info(util.Logger).Log("msg", "TSDB blocks compaction has been skipped because of the current ingester state", "state", ingesterState)
			return
		}
	}

	i.runConcurrentUserWorkers(ctx, i.cfg.BlocksStorageConfig.TSDB.HeadCompactionConcurrency, func(userID string) {
		userDB := i.getTSDB(userID)
		if userDB == nil {
			return
		}

		// Don't do anything, if there is nothing to compact.
		h := userDB.Head()
		if h.NumSeries() == 0 {
			return
		}

		var err error

		i.TSDBState.compactionsTriggered.Inc()

		reason := ""
		switch {
		case force:
			reason = "forced"
			err = userDB.CompactHead(tsdb.NewRangeHead(h, h.MinTime(), h.MaxTime()))

		case i.cfg.BlocksStorageConfig.TSDB.HeadCompactionIdleTimeout > 0 && userDB.isIdle(time.Now(), i.cfg.BlocksStorageConfig.TSDB.HeadCompactionIdleTimeout):
			reason = "idle"
			level.Info(util.Logger).Log("msg", "TSDB is idle, forcing compaction", "user", userID)
			err = userDB.CompactHead(tsdb.NewRangeHead(h, h.MinTime(), h.MaxTime()))

		default:
			reason = "regular"
			err = userDB.Compact()
		}

		if err != nil {
			i.TSDBState.compactionsFailed.Inc()
			level.Warn(util.Logger).Log("msg", "TSDB blocks compaction for user has failed", "user", userID, "err", err, "compactReason", reason)
		} else {
			level.Debug(util.Logger).Log("msg", "TSDB blocks compaction completed successfully", "user", userID, "compactReason", reason)
		}
	})
}

func (i *Ingester) runConcurrentUserWorkers(ctx context.Context, concurrency int, userFunc func(userID string)) {
	wg := sync.WaitGroup{}
	ch := make(chan string)

	for ix := 0; ix < concurrency; ix++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for userID := range ch {
				userFunc(userID)
			}
		}()
	}

sendLoop:
	for _, userID := range i.getTSDBUsers() {
		select {
		case ch <- userID:
			// ok
		case <-ctx.Done():
			// don't start new tasks.
			break sendLoop
		}
	}

	close(ch)

	// wait for ongoing workers to finish.
	wg.Wait()
}

// This method will flush all data. It is called as part of Lifecycler's shutdown (if flush on shutdown is configured), or from the flusher.
//
// When called as during Lifecycler shutdown, this happens as part of normal Ingester shutdown (see stoppingV2 method).
// Samples are not received at this stage. Compaction and Shipping loops have already been stopped as well.
//
// When used from flusher, ingester is constructed in a way that compaction, shipping and receiving of samples is never started.
func (i *Ingester) v2LifecyclerFlush() {
	level.Info(util.Logger).Log("msg", "starting to flush and ship TSDB blocks")

	ctx := context.Background()

	i.compactBlocks(ctx, true)
	if i.cfg.BlocksStorageConfig.TSDB.ShipInterval > 0 {
		i.shipBlocks(ctx)
	}

	level.Info(util.Logger).Log("msg", "finished flushing and shipping TSDB blocks")
}

// Blocks version of Flush handler. It force-compacts blocks, and triggers shipping.
func (i *Ingester) v2FlushHandler(w http.ResponseWriter, _ *http.Request) {
	go func() {
		ingCtx := i.BasicService.ServiceContext()
		if ingCtx == nil || ingCtx.Err() != nil {
			level.Info(util.Logger).Log("msg", "flushing TSDB blocks: ingester not running, ignoring flush request")
			return
		}

		ch := make(chan struct{}, 1)

		level.Info(util.Logger).Log("msg", "flushing TSDB blocks: triggering compaction")
		select {
		case i.TSDBState.forceCompactTrigger <- ch:
			// Compacting now.
		case <-ingCtx.Done():
			level.Warn(util.Logger).Log("msg", "failed to compact TSDB blocks, ingester not running anymore")
			return
		}

		// Wait until notified about compaction being finished.
		select {
		case <-ch:
			level.Info(util.Logger).Log("msg", "finished compacting TSDB blocks")
		case <-ingCtx.Done():
			level.Warn(util.Logger).Log("msg", "failed to compact TSDB blocks, ingester not running anymore")
			return
		}

		if i.cfg.BlocksStorageConfig.TSDB.ShipInterval > 0 {
			level.Info(util.Logger).Log("msg", "flushing TSDB blocks: triggering shipping")

			select {
			case i.TSDBState.shipTrigger <- ch:
				// shipping now
			case <-ingCtx.Done():
				level.Warn(util.Logger).Log("msg", "failed to ship TSDB blocks, ingester not running anymore")
				return
			}

			// Wait until shipping finished.
			select {
			case <-ch:
				level.Info(util.Logger).Log("msg", "shipping of TSDB blocks finished")
			case <-ingCtx.Done():
				level.Warn(util.Logger).Log("msg", "failed to ship TSDB blocks, ingester not running anymore")
				return
			}
		}

		level.Info(util.Logger).Log("msg", "flushing TSDB blocks: finished")
	}()

	w.WriteHeader(http.StatusNoContent)
}
