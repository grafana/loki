package querier

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	"github.com/thanos-io/thanos/pkg/strutil"
	"golang.org/x/sync/errgroup"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/purger"
	"github.com/cortexproject/cortex/pkg/querier/batch"
	"github.com/cortexproject/cortex/pkg/querier/chunkstore"
	"github.com/cortexproject/cortex/pkg/querier/iterators"
	"github.com/cortexproject/cortex/pkg/querier/lazyquery"
	"github.com/cortexproject/cortex/pkg/querier/series"
	"github.com/cortexproject/cortex/pkg/tenant"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/cortexproject/cortex/pkg/util/validation"
)

// Config contains the configuration require to create a querier
type Config struct {
	MaxConcurrent        int           `yaml:"max_concurrent"`
	Timeout              time.Duration `yaml:"timeout"`
	Iterators            bool          `yaml:"iterators"`
	BatchIterators       bool          `yaml:"batch_iterators"`
	IngesterStreaming    bool          `yaml:"ingester_streaming"`
	MaxSamples           int           `yaml:"max_samples"`
	QueryIngestersWithin time.Duration `yaml:"query_ingesters_within"`
	QueryStoreForLabels  bool          `yaml:"query_store_for_labels_enabled"`
	AtModifierEnabled    bool          `yaml:"at_modifier_enabled"`

	// QueryStoreAfter the time after which queries should also be sent to the store and not just ingesters.
	QueryStoreAfter    time.Duration `yaml:"query_store_after"`
	MaxQueryIntoFuture time.Duration `yaml:"max_query_into_future"`

	// The default evaluation interval for the promql engine.
	// Needs to be configured for subqueries to work as it is the default
	// step if not specified.
	DefaultEvaluationInterval time.Duration `yaml:"default_evaluation_interval"`

	// Directory for ActiveQueryTracker. If empty, ActiveQueryTracker will be disabled and MaxConcurrent will not be applied (!).
	// ActiveQueryTracker logs queries that were active during the last crash, but logs them on the next startup.
	// However, we need to use active query tracker, otherwise we cannot limit Max Concurrent queries in the PromQL
	// engine.
	ActiveQueryTrackerDir string `yaml:"active_query_tracker_dir"`
	// LookbackDelta determines the time since the last sample after which a time
	// series is considered stale.
	LookbackDelta time.Duration `yaml:"lookback_delta"`

	// Blocks storage only.
	StoreGatewayAddresses string       `yaml:"store_gateway_addresses"`
	StoreGatewayClient    ClientConfig `yaml:"store_gateway_client"`

	SecondStoreEngine        string       `yaml:"second_store_engine"`
	UseSecondStoreBeforeTime flagext.Time `yaml:"use_second_store_before_time"`

	ShuffleShardingIngestersLookbackPeriod time.Duration `yaml:"shuffle_sharding_ingesters_lookback_period"`
}

var (
	errBadLookbackConfigs                             = errors.New("bad settings, query_store_after >= query_ingesters_within which can result in queries not being sent")
	errShuffleShardingLookbackLessThanQueryStoreAfter = errors.New("the shuffle-sharding lookback period should be greater or equal than the configured 'query store after'")
	errEmptyTimeRange                                 = errors.New("empty time range")
)

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.StoreGatewayClient.RegisterFlagsWithPrefix("querier.store-gateway-client", f)
	f.IntVar(&cfg.MaxConcurrent, "querier.max-concurrent", 20, "The maximum number of concurrent queries.")
	f.DurationVar(&cfg.Timeout, "querier.timeout", 2*time.Minute, "The timeout for a query.")
	f.BoolVar(&cfg.Iterators, "querier.iterators", false, "Use iterators to execute query, as opposed to fully materialising the series in memory.")
	f.BoolVar(&cfg.BatchIterators, "querier.batch-iterators", true, "Use batch iterators to execute query, as opposed to fully materialising the series in memory.  Takes precedent over the -querier.iterators flag.")
	f.BoolVar(&cfg.IngesterStreaming, "querier.ingester-streaming", true, "Use streaming RPCs to query ingester.")
	f.IntVar(&cfg.MaxSamples, "querier.max-samples", 50e6, "Maximum number of samples a single query can load into memory.")
	f.DurationVar(&cfg.QueryIngestersWithin, "querier.query-ingesters-within", 0, "Maximum lookback beyond which queries are not sent to ingester. 0 means all queries are sent to ingester.")
	f.BoolVar(&cfg.QueryStoreForLabels, "querier.query-store-for-labels-enabled", false, "Query long-term store for series, label values and label names APIs. Works only with blocks engine.")
	f.BoolVar(&cfg.AtModifierEnabled, "querier.at-modifier-enabled", false, "Enable the @ modifier in PromQL.")
	f.DurationVar(&cfg.MaxQueryIntoFuture, "querier.max-query-into-future", 10*time.Minute, "Maximum duration into the future you can query. 0 to disable.")
	f.DurationVar(&cfg.DefaultEvaluationInterval, "querier.default-evaluation-interval", time.Minute, "The default evaluation interval or step size for subqueries.")
	f.DurationVar(&cfg.QueryStoreAfter, "querier.query-store-after", 0, "The time after which a metric should be queried from storage and not just ingesters. 0 means all queries are sent to store. When running the blocks storage, if this option is enabled, the time range of the query sent to the store will be manipulated to ensure the query end is not more recent than 'now - query-store-after'.")
	f.StringVar(&cfg.ActiveQueryTrackerDir, "querier.active-query-tracker-dir", "./active-query-tracker", "Active query tracker monitors active queries, and writes them to the file in given directory. If Cortex discovers any queries in this log during startup, it will log them to the log file. Setting to empty value disables active query tracker, which also disables -querier.max-concurrent option.")
	f.StringVar(&cfg.StoreGatewayAddresses, "querier.store-gateway-addresses", "", "Comma separated list of store-gateway addresses in DNS Service Discovery format. This option should be set when using the blocks storage and the store-gateway sharding is disabled (when enabled, the store-gateway instances form a ring and addresses are picked from the ring).")
	f.DurationVar(&cfg.LookbackDelta, "querier.lookback-delta", 5*time.Minute, "Time since the last sample after which a time series is considered stale and ignored by expression evaluations.")
	f.StringVar(&cfg.SecondStoreEngine, "querier.second-store-engine", "", "Second store engine to use for querying. Empty = disabled.")
	f.Var(&cfg.UseSecondStoreBeforeTime, "querier.use-second-store-before-time", "If specified, second store is only used for queries before this timestamp. Default value 0 means secondary store is always queried.")
	f.DurationVar(&cfg.ShuffleShardingIngestersLookbackPeriod, "querier.shuffle-sharding-ingesters-lookback-period", 0, "When distributor's sharding strategy is shuffle-sharding and this setting is > 0, queriers fetch in-memory series from the minimum set of required ingesters, selecting only ingesters which may have received series since 'now - lookback period'. The lookback period should be greater or equal than the configured 'query store after' and 'query ingesters within'. If this setting is 0, queriers always query all ingesters (ingesters shuffle sharding on read path is disabled).")
}

// Validate the config
func (cfg *Config) Validate() error {
	// Ensure the config wont create a situation where no queriers are returned.
	if cfg.QueryIngestersWithin != 0 && cfg.QueryStoreAfter != 0 {
		if cfg.QueryStoreAfter >= cfg.QueryIngestersWithin {
			return errBadLookbackConfigs
		}
	}

	if cfg.ShuffleShardingIngestersLookbackPeriod > 0 {
		if cfg.ShuffleShardingIngestersLookbackPeriod < cfg.QueryStoreAfter {
			return errShuffleShardingLookbackLessThanQueryStoreAfter
		}
	}

	return nil
}

func (cfg *Config) GetStoreGatewayAddresses() []string {
	if cfg.StoreGatewayAddresses == "" {
		return nil
	}

	return strings.Split(cfg.StoreGatewayAddresses, ",")
}

func getChunksIteratorFunction(cfg Config) chunkIteratorFunc {
	if cfg.BatchIterators {
		return batch.NewChunkMergeIterator
	} else if cfg.Iterators {
		return iterators.NewChunkMergeIterator
	}
	return mergeChunks
}

// NewChunkStoreQueryable returns the storage.Queryable implementation against the chunks store.
func NewChunkStoreQueryable(cfg Config, chunkStore chunkstore.ChunkStore) storage.Queryable {
	return newChunkStoreQueryable(chunkStore, getChunksIteratorFunction(cfg))
}

// New builds a queryable and promql engine.
func New(cfg Config, limits *validation.Overrides, distributor Distributor, stores []QueryableWithFilter, tombstonesLoader *purger.TombstonesLoader, reg prometheus.Registerer, logger log.Logger) (storage.SampleAndChunkQueryable, *promql.Engine) {
	iteratorFunc := getChunksIteratorFunction(cfg)

	distributorQueryable := newDistributorQueryable(distributor, cfg.IngesterStreaming, iteratorFunc, cfg.QueryIngestersWithin)

	ns := make([]QueryableWithFilter, len(stores))
	for ix, s := range stores {
		ns[ix] = storeQueryable{
			QueryableWithFilter: s,
			QueryStoreAfter:     cfg.QueryStoreAfter,
		}
	}

	queryable := NewQueryable(distributorQueryable, ns, iteratorFunc, cfg, limits, tombstonesLoader)

	lazyQueryable := storage.QueryableFunc(func(ctx context.Context, mint int64, maxt int64) (storage.Querier, error) {
		querier, err := queryable.Querier(ctx, mint, maxt)
		if err != nil {
			return nil, err
		}
		return lazyquery.NewLazyQuerier(querier), nil
	})

	engine := promql.NewEngine(promql.EngineOpts{
		Logger:             logger,
		Reg:                reg,
		ActiveQueryTracker: createActiveQueryTracker(cfg, logger),
		MaxSamples:         cfg.MaxSamples,
		Timeout:            cfg.Timeout,
		LookbackDelta:      cfg.LookbackDelta,
		EnableAtModifier:   cfg.AtModifierEnabled,
		NoStepSubqueryIntervalFn: func(int64) int64 {
			return cfg.DefaultEvaluationInterval.Milliseconds()
		},
	})
	return NewSampleAndChunkQueryable(lazyQueryable), engine
}

// NewSampleAndChunkQueryable creates a SampleAndChunkQueryable from a
// Queryable with a ChunkQueryable stub, that errors once it get's called.
func NewSampleAndChunkQueryable(q storage.Queryable) storage.SampleAndChunkQueryable {
	return &sampleAndChunkQueryable{q}
}

type sampleAndChunkQueryable struct {
	storage.Queryable
}

func (q *sampleAndChunkQueryable) ChunkQuerier(ctx context.Context, mint, maxt int64) (storage.ChunkQuerier, error) {
	return nil, errors.New("ChunkQuerier not implemented")
}

func createActiveQueryTracker(cfg Config, logger log.Logger) *promql.ActiveQueryTracker {
	dir := cfg.ActiveQueryTrackerDir

	if dir != "" {
		return promql.NewActiveQueryTracker(dir, cfg.MaxConcurrent, logger)
	}

	return nil
}

// QueryableWithFilter extends Queryable interface with `UseQueryable` filtering function.
type QueryableWithFilter interface {
	storage.Queryable

	// UseQueryable returns true if this queryable should be used to satisfy the query for given time range.
	// Query min and max time are in milliseconds since epoch.
	UseQueryable(now time.Time, queryMinT, queryMaxT int64) bool
}

// NewQueryable creates a new Queryable for cortex.
func NewQueryable(distributor QueryableWithFilter, stores []QueryableWithFilter, chunkIterFn chunkIteratorFunc, cfg Config, limits *validation.Overrides, tombstonesLoader *purger.TombstonesLoader) storage.Queryable {
	return storage.QueryableFunc(func(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
		now := time.Now()

		userID, err := tenant.TenantID(ctx)
		if err != nil {
			return nil, err
		}

		mint, maxt, err = validateQueryTimeRange(ctx, userID, mint, maxt, limits, cfg.MaxQueryIntoFuture)
		if err == errEmptyTimeRange {
			return storage.NoopQuerier(), nil
		} else if err != nil {
			return nil, err
		}

		q := querier{
			ctx:                 ctx,
			mint:                mint,
			maxt:                maxt,
			chunkIterFn:         chunkIterFn,
			tombstonesLoader:    tombstonesLoader,
			limits:              limits,
			maxQueryIntoFuture:  cfg.MaxQueryIntoFuture,
			queryStoreForLabels: cfg.QueryStoreForLabels,
		}

		dqr, err := distributor.Querier(ctx, mint, maxt)
		if err != nil {
			return nil, err
		}

		q.metadataQuerier = dqr

		if distributor.UseQueryable(now, mint, maxt) {
			q.queriers = append(q.queriers, dqr)
		}

		for _, s := range stores {
			if !s.UseQueryable(now, mint, maxt) {
				continue
			}

			cqr, err := s.Querier(ctx, mint, maxt)
			if err != nil {
				return nil, err
			}

			q.queriers = append(q.queriers, cqr)
		}

		return q, nil
	})
}

type querier struct {
	// used for labels and metadata queries
	metadataQuerier storage.Querier

	// used for selecting series
	queriers []storage.Querier

	chunkIterFn chunkIteratorFunc
	ctx         context.Context
	mint, maxt  int64

	tombstonesLoader    *purger.TombstonesLoader
	limits              *validation.Overrides
	maxQueryIntoFuture  time.Duration
	queryStoreForLabels bool
}

// Select implements storage.Querier interface.
// The bool passed is ignored because the series is always sorted.
func (q querier) Select(_ bool, sp *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	log, ctx := spanlogger.New(q.ctx, "querier.Select")
	defer log.Span.Finish()

	if sp != nil {
		level.Debug(log).Log("start", util.TimeFromMillis(sp.Start).UTC().String(), "end", util.TimeFromMillis(sp.End).UTC().String(), "step", sp.Step, "matchers", matchers)
	}

	// Kludge: Prometheus passes nil SelectHints if it is doing a 'series' operation,
	// which needs only metadata. Here we expect that metadataQuerier querier will handle that.
	// In Cortex it is not feasible to query entire history (with no mint/maxt), so we only ask ingesters and skip
	// querying the long-term storage.
	// Also, in the recent versions of Prometheus, we pass in the hint but with Func set to "series".
	// See: https://github.com/prometheus/prometheus/pull/8050
	if (sp == nil || sp.Func == "series") && !q.queryStoreForLabels {
		// In this case, the query time range has already been validated when the querier has been
		// created.
		return q.metadataQuerier.Select(true, sp, matchers...)
	}

	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}

	// Validate query time range. Even if the time range has already been validated when we created
	// the querier, we need to check it again here because the time range specified in hints may be
	// different.
	startMs, endMs, err := validateQueryTimeRange(ctx, userID, sp.Start, sp.End, q.limits, q.maxQueryIntoFuture)
	if err == errEmptyTimeRange {
		return storage.NoopSeriesSet()
	} else if err != nil {
		return storage.ErrSeriesSet(err)
	}

	// The time range may have been manipulated during the validation,
	// so we make sure changes are reflected back to hints.
	sp.Start = startMs
	sp.End = endMs

	startTime := model.Time(startMs)
	endTime := model.Time(endMs)

	// Validate query time range. This validation should be done only for instant / range queries and
	// NOT for metadata queries (series, labels) because the query-frontend doesn't support splitting
	// of such queries.
	if maxQueryLength := q.limits.MaxQueryLength(userID); maxQueryLength > 0 && endTime.Sub(startTime) > maxQueryLength {
		limitErr := validation.LimitError(fmt.Sprintf(validation.ErrQueryTooLong, endTime.Sub(startTime), maxQueryLength))
		return storage.ErrSeriesSet(limitErr)
	}

	tombstones, err := q.tombstonesLoader.GetPendingTombstonesForInterval(userID, startTime, endTime)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}

	if len(q.queriers) == 1 {
		seriesSet := q.queriers[0].Select(true, sp, matchers...)

		if tombstones.Len() != 0 {
			seriesSet = series.NewDeletedSeriesSet(seriesSet, tombstones, model.Interval{Start: startTime, End: endTime})
		}

		return seriesSet
	}

	sets := make(chan storage.SeriesSet, len(q.queriers))
	for _, querier := range q.queriers {
		go func(querier storage.Querier) {
			sets <- querier.Select(true, sp, matchers...)
		}(querier)
	}

	var result []storage.SeriesSet
	for range q.queriers {
		select {
		case set := <-sets:
			result = append(result, set)
		case <-ctx.Done():
			return storage.ErrSeriesSet(ctx.Err())
		}
	}

	// we have all the sets from different sources (chunk from store, chunks from ingesters,
	// time series from store and time series from ingesters).
	// mergeSeriesSets will return sorted set.
	seriesSet := q.mergeSeriesSets(result)

	if tombstones.Len() != 0 {
		seriesSet = series.NewDeletedSeriesSet(seriesSet, tombstones, model.Interval{Start: startTime, End: endTime})
	}
	return seriesSet
}

// LabelsValue implements storage.Querier.
func (q querier) LabelValues(name string, matchers ...*labels.Matcher) ([]string, storage.Warnings, error) {
	if !q.queryStoreForLabels {
		return q.metadataQuerier.LabelValues(name, matchers...)
	}

	if len(q.queriers) == 1 {
		return q.queriers[0].LabelValues(name, matchers...)
	}

	var (
		g, _     = errgroup.WithContext(q.ctx)
		sets     = [][]string{}
		warnings = storage.Warnings(nil)

		resMtx sync.Mutex
	)

	for _, querier := range q.queriers {
		// Need to reassign as the original variable will change and can't be relied on in a goroutine.
		querier := querier
		g.Go(func() error {
			// NB: Values are sorted in Cortex already.
			myValues, myWarnings, err := querier.LabelValues(name, matchers...)
			if err != nil {
				return err
			}

			resMtx.Lock()
			sets = append(sets, myValues)
			warnings = append(warnings, myWarnings...)
			resMtx.Unlock()

			return nil
		})
	}

	err := g.Wait()
	if err != nil {
		return nil, nil, err
	}

	return strutil.MergeSlices(sets...), warnings, nil
}

func (q querier) LabelNames() ([]string, storage.Warnings, error) {
	if !q.queryStoreForLabels {
		return q.metadataQuerier.LabelNames()
	}

	if len(q.queriers) == 1 {
		return q.queriers[0].LabelNames()
	}

	var (
		g, _     = errgroup.WithContext(q.ctx)
		sets     = [][]string{}
		warnings = storage.Warnings(nil)

		resMtx sync.Mutex
	)

	for _, querier := range q.queriers {
		// Need to reassign as the original variable will change and can't be relied on in a goroutine.
		querier := querier
		g.Go(func() error {
			// NB: Names are sorted in Cortex already.
			myNames, myWarnings, err := querier.LabelNames()
			if err != nil {
				return err
			}

			resMtx.Lock()
			sets = append(sets, myNames)
			warnings = append(warnings, myWarnings...)
			resMtx.Unlock()

			return nil
		})
	}

	err := g.Wait()
	if err != nil {
		return nil, nil, err
	}

	return strutil.MergeSlices(sets...), warnings, nil
}

func (querier) Close() error {
	return nil
}

func (q querier) mergeSeriesSets(sets []storage.SeriesSet) storage.SeriesSet {
	// Here we deal with sets that are based on chunks and build single set from them.
	// Remaining sets are merged with chunks-based one using storage.NewMergeSeriesSet

	otherSets := []storage.SeriesSet(nil)
	chunks := []chunk.Chunk(nil)

	for _, set := range sets {
		nonChunkSeries := []storage.Series(nil)

		// SeriesSet may have some series backed up by chunks, and some not.
		for set.Next() {
			s := set.At()

			if sc, ok := s.(SeriesWithChunks); ok {
				chunks = append(chunks, sc.Chunks()...)
			} else {
				nonChunkSeries = append(nonChunkSeries, s)
			}
		}

		if err := set.Err(); err != nil {
			otherSets = append(otherSets, storage.ErrSeriesSet(err))
		} else if len(nonChunkSeries) > 0 {
			otherSets = append(otherSets, &sliceSeriesSet{series: nonChunkSeries, ix: -1})
		}
	}

	if len(chunks) == 0 {
		return storage.NewMergeSeriesSet(otherSets, storage.ChainedSeriesMerge)
	}

	// partitionChunks returns set with sorted series, so it can be used by NewMergeSeriesSet
	chunksSet := partitionChunks(chunks, q.mint, q.maxt, q.chunkIterFn)

	if len(otherSets) == 0 {
		return chunksSet
	}

	otherSets = append(otherSets, chunksSet)
	return storage.NewMergeSeriesSet(otherSets, storage.ChainedSeriesMerge)
}

type sliceSeriesSet struct {
	series []storage.Series
	ix     int
}

func (s *sliceSeriesSet) Next() bool {
	s.ix++
	return s.ix < len(s.series)
}

func (s *sliceSeriesSet) At() storage.Series {
	if s.ix < 0 || s.ix >= len(s.series) {
		return nil
	}
	return s.series[s.ix]
}

func (s *sliceSeriesSet) Err() error {
	return nil
}

func (s *sliceSeriesSet) Warnings() storage.Warnings {
	return nil
}

type storeQueryable struct {
	QueryableWithFilter
	QueryStoreAfter time.Duration
}

func (s storeQueryable) UseQueryable(now time.Time, queryMinT, queryMaxT int64) bool {
	// Include this store only if mint is within QueryStoreAfter w.r.t current time.
	if s.QueryStoreAfter != 0 && queryMinT > util.TimeToMillis(now.Add(-s.QueryStoreAfter)) {
		return false
	}
	return s.QueryableWithFilter.UseQueryable(now, queryMinT, queryMaxT)
}

type alwaysTrueFilterQueryable struct {
	storage.Queryable
}

func (alwaysTrueFilterQueryable) UseQueryable(_ time.Time, _, _ int64) bool {
	return true
}

// Wraps storage.Queryable into QueryableWithFilter, with no query filtering.
func UseAlwaysQueryable(q storage.Queryable) QueryableWithFilter {
	return alwaysTrueFilterQueryable{Queryable: q}
}

type useBeforeTimestampQueryable struct {
	storage.Queryable
	ts int64 // Timestamp in milliseconds
}

func (u useBeforeTimestampQueryable) UseQueryable(_ time.Time, queryMinT, _ int64) bool {
	if u.ts == 0 {
		return true
	}
	return queryMinT < u.ts
}

// Returns QueryableWithFilter, that is used only if query starts before given timestamp.
// If timestamp is zero (time.IsZero), queryable is always used.
func UseBeforeTimestampQueryable(queryable storage.Queryable, ts time.Time) QueryableWithFilter {
	t := int64(0)
	if !ts.IsZero() {
		t = util.TimeToMillis(ts)
	}
	return useBeforeTimestampQueryable{
		Queryable: queryable,
		ts:        t,
	}
}

func validateQueryTimeRange(ctx context.Context, userID string, startMs, endMs int64, limits *validation.Overrides, maxQueryIntoFuture time.Duration) (int64, int64, error) {
	now := model.Now()
	startTime := model.Time(startMs)
	endTime := model.Time(endMs)

	// Clamp time range based on max query into future.
	if maxQueryIntoFuture > 0 && endTime.After(now.Add(maxQueryIntoFuture)) {
		origEndTime := endTime
		endTime = now.Add(maxQueryIntoFuture)

		// Make sure to log it in traces to ease debugging.
		level.Debug(spanlogger.FromContext(ctx)).Log(
			"msg", "the end time of the query has been manipulated because of the 'max query into future' setting",
			"original", util.FormatTimeModel(origEndTime),
			"updated", util.FormatTimeModel(endTime))

		if endTime.Before(startTime) {
			return 0, 0, errEmptyTimeRange
		}
	}

	// Clamp the time range based on the max query lookback.
	if maxQueryLookback := limits.MaxQueryLookback(userID); maxQueryLookback > 0 && startTime.Before(now.Add(-maxQueryLookback)) {
		origStartTime := startTime
		startTime = now.Add(-maxQueryLookback)

		// Make sure to log it in traces to ease debugging.
		level.Debug(spanlogger.FromContext(ctx)).Log(
			"msg", "the start time of the query has been manipulated because of the 'max query lookback' setting",
			"original", util.FormatTimeModel(origStartTime),
			"updated", util.FormatTimeModel(startTime))

		if endTime.Before(startTime) {
			return 0, 0, errEmptyTimeRange
		}
	}

	return int64(startTime), int64(endTime), nil
}
