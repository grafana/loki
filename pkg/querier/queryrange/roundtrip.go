package queryrange

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/user"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	base "github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/constants"
	logutil "github.com/grafana/loki/pkg/util/log"
)

const (
	// Parallelize the index stats requests, so it doesn't send a huge request to a single index-gw (i.e. {app=~".+"} for 30d).
	// Indices are sharded by 24 hours, so we split the stats request in 24h intervals.
	indexStatsQuerySplitInterval = 24 * time.Hour

	// Limited queries only need to fetch up to the requested line limit worth of logs,
	// Our defaults for splitting and parallelism are much too aggressive for large customers and result in
	// potentially GB of logs being returned by all the shards and splits which will overwhelm the frontend
	// Therefore we force max parallelism to `1` so that these queries are executed sequentially.
	// Below we also fix the number of shards to a static number.
	limitedQuerySplits = 1
)

// Config is the configuration for the queryrange tripperware
type Config struct {
	base.Config                  `yaml:",inline"`
	Transformer                  UserIDTransformer        `yaml:"-"`
	CacheIndexStatsResults       bool                     `yaml:"cache_index_stats_results"`
	StatsCacheConfig             IndexStatsCacheConfig    `yaml:"index_stats_results_cache" doc:"description=If a cache config is not specified and cache_index_stats_results is true, the config for the results cache is used."`
	CacheVolumeResults           bool                     `yaml:"cache_volume_results"`
	VolumeCacheConfig            VolumeCacheConfig        `yaml:"volume_results_cache" doc:"description=If a cache config is not specified and cache_volume_results is true, the config for the results cache is used."`
	CacheInstantMetricResults    bool                     `yaml:"cache_instant_metric_results"`
	InstantMetricCacheConfig     InstantMetricCacheConfig `yaml:"instant_metric_results_cache" doc:"description=If a cache config is not specified and cache_instant_metric_results is true, the config for the results cache is used."`
	InstantMetricQuerySplitAlign bool                     `yaml:"instant_metric_query_split_align" doc:"description=Whether to align the splits of instant metric query with splitByInterval and query's exec time. Useful when instant_metric_cache is enabled"`
	CacheSeriesResults           bool                     `yaml:"cache_series_results"`
	SeriesCacheConfig            SeriesCacheConfig        `yaml:"series_results_cache" doc:"description=If series_results_cache is not configured and cache_series_results is true, the config for the results cache is used."`
	CacheLabelResults            bool                     `yaml:"cache_label_results"`
	LabelsCacheConfig            LabelsCacheConfig        `yaml:"label_results_cache" doc:"description=If label_results_cache is not configured and cache_label_results is true, the config for the results cache is used."`
}

// RegisterFlags adds the flags required to configure this flag set.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Config.RegisterFlags(f)
	f.BoolVar(&cfg.CacheIndexStatsResults, "querier.cache-index-stats-results", false, "Cache index stats query results.")
	cfg.StatsCacheConfig.RegisterFlags(f)
	f.BoolVar(&cfg.CacheVolumeResults, "querier.cache-volume-results", false, "Cache volume query results.")
	cfg.VolumeCacheConfig.RegisterFlags(f)
	f.BoolVar(&cfg.CacheInstantMetricResults, "querier.cache-instant-metric-results", false, "Cache instant metric query results.")
	cfg.InstantMetricCacheConfig.RegisterFlags(f)
	f.BoolVar(&cfg.InstantMetricQuerySplitAlign, "querier.instant-metric-query-split-align", false, "Align the instant metric splits with splityByInterval and query's exec time.")
	f.BoolVar(&cfg.CacheSeriesResults, "querier.cache-series-results", false, "Cache series query results.")
	cfg.SeriesCacheConfig.RegisterFlags(f)
	f.BoolVar(&cfg.CacheLabelResults, "querier.cache-label-results", false, "Cache label query results.")
	cfg.LabelsCacheConfig.RegisterFlags(f)
}

// Validate validates the config.
func (cfg *Config) Validate() error {
	if err := cfg.Config.Validate(); err != nil {
		return err
	}

	if cfg.CacheIndexStatsResults {
		if err := cfg.StatsCacheConfig.Validate(); err != nil {
			return errors.Wrap(err, "invalid index_stats_results_cache config")
		}
	}
	return nil
}

// Stopper gracefully shutdown resources created
type Stopper interface {
	Stop()
}

type StopperWrapper []Stopper

// Stop gracefully shutdowns created resources
func (s StopperWrapper) Stop() {
	for _, stopper := range s {
		if stopper != nil {
			stopper.Stop()
		}
	}
}

func newResultsCacheFromConfig(cfg base.ResultsCacheConfig, registerer prometheus.Registerer, log log.Logger, cacheType stats.CacheType) (cache.Cache, error) {
	if !cache.IsCacheConfigured(cfg.CacheConfig) {
		return nil, errors.Errorf("%s cache is not configured", cacheType)
	}

	c, err := cache.New(cfg.CacheConfig, registerer, log, cacheType, constants.Loki)
	if err != nil {
		return nil, err
	}

	if cfg.Compression == "snappy" {
		c = cache.NewSnappy(c, log)
	}

	return c, nil
}

// NewMiddleware returns a Middleware configured with middlewares to align, split and cache requests.
func NewMiddleware(
	cfg Config,
	engineOpts logql.EngineOpts,
	iqo util.IngesterQueryOptions,
	log log.Logger,
	limits Limits,
	schema config.SchemaConfig,
	cacheGenNumLoader base.CacheGenNumberLoader,
	retentionEnabled bool,
	registerer prometheus.Registerer,
	metricsNamespace string,
) (base.Middleware, Stopper, error) {
	metrics := NewMetrics(registerer, metricsNamespace)

	var (
		resultsCache       cache.Cache
		statsCache         cache.Cache
		volumeCache        cache.Cache
		instantMetricCache cache.Cache
		seriesCache        cache.Cache
		labelsCache        cache.Cache
		err                error
	)

	if cfg.CacheResults {
		resultsCache, err = newResultsCacheFromConfig(cfg.ResultsCacheConfig, registerer, log, stats.ResultCache)
		if err != nil {
			return nil, nil, err
		}
	}

	if cfg.CacheIndexStatsResults {
		statsCache, err = newResultsCacheFromConfig(cfg.StatsCacheConfig.ResultsCacheConfig, registerer, log, stats.StatsResultCache)
		if err != nil {
			return nil, nil, err
		}
	}

	if cfg.CacheVolumeResults {
		volumeCache, err = newResultsCacheFromConfig(cfg.VolumeCacheConfig.ResultsCacheConfig, registerer, log, stats.VolumeResultCache)
		if err != nil {
			return nil, nil, err
		}
	}

	if cfg.CacheInstantMetricResults {
		instantMetricCache, err = newResultsCacheFromConfig(cfg.InstantMetricCacheConfig.ResultsCacheConfig, registerer, log, stats.InstantMetricResultsCache)
		if err != nil {
			return nil, nil, err
		}
	}

	if cfg.CacheSeriesResults {
		seriesCache, err = newResultsCacheFromConfig(cfg.SeriesCacheConfig.ResultsCacheConfig, registerer, log, stats.SeriesResultCache)
		if err != nil {
			return nil, nil, err
		}
	}

	if cfg.CacheLabelResults {
		labelsCache, err = newResultsCacheFromConfig(cfg.LabelsCacheConfig.ResultsCacheConfig, registerer, log, stats.LabelResultCache)
		if err != nil {
			return nil, nil, err
		}
	}

	var codec base.Codec = DefaultCodec

	indexStatsTripperware, err := NewIndexStatsTripperware(cfg, log, limits, schema, codec, iqo, statsCache,
		cacheGenNumLoader, retentionEnabled, metrics, metricsNamespace)
	if err != nil {
		return nil, nil, err
	}

	metricsTripperware, err := NewMetricTripperware(cfg, engineOpts, log, limits, schema, codec, iqo, resultsCache,
		cacheGenNumLoader, retentionEnabled, PrometheusExtractor{}, metrics, indexStatsTripperware, metricsNamespace)
	if err != nil {
		return nil, nil, err
	}

	limitedTripperware, err := NewLimitedTripperware(cfg, engineOpts, log, limits, schema, metrics, indexStatsTripperware, codec, iqo)
	if err != nil {
		return nil, nil, err
	}

	// NOTE: When we would start caching response from non-metric queries we would have to consider cache gen headers as well in
	// MergeResponse implementation for Loki codecs same as it is done in Cortex at https://github.com/cortexproject/cortex/blob/21bad57b346c730d684d6d0205efef133422ab28/pkg/querier/queryrange/query_range.go#L170
	logFilterTripperware, err := NewLogFilterTripperware(cfg, engineOpts, log, limits, schema, codec, iqo, resultsCache, metrics, indexStatsTripperware, metricsNamespace)
	if err != nil {
		return nil, nil, err
	}

	seriesTripperware, err := NewSeriesTripperware(cfg, log, limits, metrics, schema, codec, iqo, seriesCache, cacheGenNumLoader, retentionEnabled, metricsNamespace)
	if err != nil {
		return nil, nil, err
	}

	labelsTripperware, err := NewLabelsTripperware(cfg, log, limits, codec, iqo, labelsCache, cacheGenNumLoader, retentionEnabled, metrics, schema, metricsNamespace)
	if err != nil {
		return nil, nil, err
	}

	instantMetricTripperware, err := NewInstantMetricTripperware(cfg, engineOpts, log, limits, schema, metrics, codec, instantMetricCache, cacheGenNumLoader, retentionEnabled, indexStatsTripperware, metricsNamespace)
	if err != nil {
		return nil, nil, err
	}

	seriesVolumeTripperware, err := NewVolumeTripperware(cfg, log, limits, schema, codec, iqo, volumeCache, cacheGenNumLoader, retentionEnabled, metrics, metricsNamespace)
	if err != nil {
		return nil, nil, err
	}

	return base.MiddlewareFunc(func(next base.Handler) base.Handler {
		var (
			metricRT       = metricsTripperware.Wrap(next)
			limitedRT      = limitedTripperware.Wrap(next)
			logFilterRT    = logFilterTripperware.Wrap(next)
			seriesRT       = seriesTripperware.Wrap(next)
			labelsRT       = labelsTripperware.Wrap(next)
			instantRT      = instantMetricTripperware.Wrap(next)
			statsRT        = indexStatsTripperware.Wrap(next)
			seriesVolumeRT = seriesVolumeTripperware.Wrap(next)
		)

		return newRoundTripper(log, next, limitedRT, logFilterRT, metricRT, seriesRT, labelsRT, instantRT, statsRT, seriesVolumeRT, limits)
	}), StopperWrapper{resultsCache, statsCache, volumeCache}, nil
}

type roundTripper struct {
	logger log.Logger

	next, limited, log, metric, series, labels, instantMetric, indexStats, seriesVolume base.Handler

	limits Limits
}

// newRoundTripper creates a new queryrange roundtripper
func newRoundTripper(logger log.Logger, next, limited, log, metric, series, labels, instantMetric, indexStats, seriesVolume base.Handler, limits Limits) roundTripper {
	return roundTripper{
		logger:        logger,
		limited:       limited,
		log:           log,
		limits:        limits,
		metric:        metric,
		series:        series,
		labels:        labels,
		instantMetric: instantMetric,
		indexStats:    indexStats,
		seriesVolume:  seriesVolume,
		next:          next,
	}
}

func (r roundTripper) Do(ctx context.Context, req base.Request) (base.Response, error) {
	logger := logutil.WithContext(ctx, r.logger)

	switch op := req.(type) {
	case *LokiRequest:
		queryHash := util.HashedQuery(op.Query)
		level.Info(logger).Log(
			"msg", "executing query",
			"type", "range",
			"query", op.Query,
			"start", op.StartTs.Format(time.RFC3339Nano),
			"end", op.EndTs.Format(time.RFC3339Nano),
			"start_delta", time.Since(op.StartTs),
			"end_delta", time.Since(op.EndTs),
			"length", op.EndTs.Sub(op.StartTs),
			"step", op.Step,
			"query_hash", queryHash,
		)

		if op.Plan == nil {
			return nil, errors.New("query plan is empty")
		}

		switch e := op.Plan.AST.(type) {
		case syntax.SampleExpr:
			// The error will be handled later.
			groups, err := e.MatcherGroups()
			if err != nil {
				level.Warn(logger).Log("msg", "unexpected matcher groups error in roundtripper", "err", err)
			}

			for _, g := range groups {
				if err := validateMatchers(ctx, r.limits, g.Matchers); err != nil {
					return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
				}
			}
			return r.metric.Do(ctx, req)
		case syntax.LogSelectorExpr:
			if err := validateMaxEntriesLimits(ctx, op.Limit, r.limits); err != nil {
				return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
			}

			if err := validateMatchers(ctx, r.limits, e.Matchers()); err != nil {
				return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
			}

			// Only filter expressions are query sharded
			if !e.HasFilter() {
				return r.limited.Do(ctx, req)
			}
			return r.log.Do(ctx, req)

		default:
			return r.next.Do(ctx, req)
		}
	case *LokiSeriesRequest:
		level.Info(logger).Log("msg", "executing query", "type", "series", "match", logql.PrintMatches(op.Match), "length", op.EndTs.Sub(op.StartTs))

		return r.series.Do(ctx, req)
	case *LabelRequest:
		level.Info(logger).Log("msg", "executing query", "type", "labels", "label", op.Name, "length", op.LabelRequest.End.Sub(*op.LabelRequest.Start), "query", op.Query)

		return r.labels.Do(ctx, req)
	case *LokiInstantRequest:
		queryHash := util.HashedQuery(op.Query)
		level.Info(logger).Log("msg", "executing query", "type", "instant", "query", op.Query, "query_hash", queryHash)

		switch op.Plan.AST.(type) {
		case syntax.SampleExpr:
			return r.instantMetric.Do(ctx, req)
		default:
			return r.next.Do(ctx, req)
		}
	case *logproto.IndexStatsRequest:
		level.Info(logger).Log("msg", "executing query", "type", "stats", "query", op.Matchers, "length", op.Through.Sub(op.From))

		return r.indexStats.Do(ctx, req)
	case *logproto.VolumeRequest:
		level.Info(logger).Log(
			"msg", "executing query",
			"type", "volume_range",
			"query", op.Matchers,
			"length", op.Through.Sub(op.From),
			"step", op.Step,
			"limit", op.Limit,
			"aggregate_by", op.AggregateBy,
		)

		return r.seriesVolume.Do(ctx, req)
	default:
		return r.next.Do(ctx, req)
	}
}

// transformRegexQuery backport the old regexp params into the v1 query format
func transformRegexQuery(req *http.Request, expr syntax.LogSelectorExpr) (syntax.LogSelectorExpr, error) {
	regexp := req.Form.Get("regexp")
	if regexp != "" {
		filterExpr, err := syntax.AddFilterExpr(expr, labels.MatchRegexp, "", regexp)
		if err != nil {
			return nil, err
		}
		params := req.URL.Query()
		params.Set("query", filterExpr.String())
		req.URL.RawQuery = params.Encode()
		// force the form and query to be parsed again.
		req.Form = nil
		req.PostForm = nil
		return filterExpr, nil
	}
	return expr, nil
}

const (
	InstantQueryOp = "instant_query"
	QueryRangeOp   = "query_range"
	SeriesOp       = "series"
	LabelNamesOp   = "labels"
	IndexStatsOp   = "index_stats"
	VolumeOp       = "volume"
	VolumeRangeOp  = "volume_range"
	IndexShardsOp  = "index_shards"
)

func getOperation(path string) string {
	switch {
	case strings.HasSuffix(path, "/query_range") || strings.HasSuffix(path, "/prom/query"):
		return QueryRangeOp
	case strings.HasSuffix(path, "/series"):
		return SeriesOp
	case strings.HasSuffix(path, "/labels") || strings.HasSuffix(path, "/label") || strings.HasSuffix(path, "/values"):
		return LabelNamesOp
	case strings.HasSuffix(path, "/v1/query"):
		return InstantQueryOp
	case path == "/loki/api/v1/index/stats":
		return IndexStatsOp
	case path == "/loki/api/v1/index/volume":
		return VolumeOp
	case path == "/loki/api/v1/index/volume_range":
		return VolumeRangeOp
	case path == "/loki/api/v1/index/shards":
		return IndexShardsOp
	default:
		return ""
	}
}

// NewLogFilterTripperware creates a new frontend tripperware responsible for handling log requests.
func NewLogFilterTripperware(cfg Config, engineOpts logql.EngineOpts, log log.Logger, limits Limits, schema config.SchemaConfig, merger base.Merger, iqo util.IngesterQueryOptions, c cache.Cache, metrics *Metrics, indexStatsTripperware base.Middleware, metricsNamespace string) (base.Middleware, error) {
	return base.MiddlewareFunc(func(next base.Handler) base.Handler {
		statsHandler := indexStatsTripperware.Wrap(next)

		queryRangeMiddleware := []base.Middleware{
			QueryMetricsMiddleware(metrics.QueryMetrics),
			StatsCollectorMiddleware(),
			NewLimitsMiddleware(limits),
			NewQuerySizeLimiterMiddleware(schema.Configs, engineOpts, log, limits, statsHandler),
			base.InstrumentMiddleware("split_by_interval", metrics.InstrumentMiddlewareMetrics),
			SplitByIntervalMiddleware(schema.Configs, limits, merger, newDefaultSplitter(limits, iqo), metrics.SplitByMetrics),
		}

		if cfg.CacheResults {
			queryCacheMiddleware := NewLogResultCache(
				log,
				limits,
				c,
				func(_ context.Context, r base.Request) bool {
					return !r.GetCachingOptions().Disabled
				},
				cfg.Transformer,
				metrics.LogResultCacheMetrics,
			)
			queryRangeMiddleware = append(
				queryRangeMiddleware,
				base.InstrumentMiddleware("log_results_cache", metrics.InstrumentMiddlewareMetrics),
				queryCacheMiddleware,
			)
		}

		if cfg.ShardedQueries {
			queryRangeMiddleware = append(queryRangeMiddleware,
				NewQueryShardMiddleware(
					log,
					schema.Configs,
					engineOpts,
					metrics.InstrumentMiddlewareMetrics, // instrumentation is included in the sharding middleware
					metrics.MiddlewareMapperMetrics.shardMapper,
					limits,
					0, // 0 is unlimited shards
					statsHandler,
					cfg.ShardAggregations,
				),
			)
		} else {
			// The sharding middleware takes care of enforcing this limit for both shardable and non-shardable queries.
			// If we are not using sharding, we enforce the limit by adding this middleware after time splitting.
			queryRangeMiddleware = append(queryRangeMiddleware,
				NewQuerierSizeLimiterMiddleware(schema.Configs, engineOpts, log, limits, statsHandler),
			)
		}

		if cfg.MaxRetries > 0 {
			queryRangeMiddleware = append(
				queryRangeMiddleware, base.InstrumentMiddleware("retry", metrics.InstrumentMiddlewareMetrics),
				base.NewRetryMiddleware(log, cfg.MaxRetries, metrics.RetryMiddlewareMetrics, metricsNamespace),
			)
		}

		return NewLimitedRoundTripper(next, limits, schema.Configs, queryRangeMiddleware...)
	}), nil
}

// NewLimitedTripperware creates a new frontend tripperware responsible for handling log requests which are label matcher only, no filter expression.
func NewLimitedTripperware(_ Config, engineOpts logql.EngineOpts, log log.Logger, limits Limits, schema config.SchemaConfig, metrics *Metrics, indexStatsTripperware base.Middleware, merger base.Merger, iqo util.IngesterQueryOptions) (base.Middleware, error) {
	return base.MiddlewareFunc(func(next base.Handler) base.Handler {
		statsHandler := indexStatsTripperware.Wrap(next)

		queryRangeMiddleware := []base.Middleware{
			StatsCollectorMiddleware(),
			NewLimitsMiddleware(limits),
			NewQuerySizeLimiterMiddleware(schema.Configs, engineOpts, log, limits, statsHandler),
			base.InstrumentMiddleware("split_by_interval", metrics.InstrumentMiddlewareMetrics),
			SplitByIntervalMiddleware(schema.Configs, WithMaxParallelism(limits, limitedQuerySplits), merger, newDefaultSplitter(limits, iqo), metrics.SplitByMetrics),
			NewQuerierSizeLimiterMiddleware(schema.Configs, engineOpts, log, limits, statsHandler),
		}

		if len(queryRangeMiddleware) > 0 {
			return NewLimitedRoundTripper(next, limits, schema.Configs, queryRangeMiddleware...)
		}
		return next
	}), nil
}

// NewSeriesTripperware creates a new frontend tripperware responsible for handling series requests
func NewSeriesTripperware(
	cfg Config,
	log log.Logger,
	limits Limits,
	metrics *Metrics,
	schema config.SchemaConfig,
	merger base.Merger,
	iqo util.IngesterQueryOptions,
	c cache.Cache,
	cacheGenNumLoader base.CacheGenNumberLoader,
	retentionEnabled bool,
	metricsNamespace string,
) (base.Middleware, error) {
	var cacheMiddleware base.Middleware
	if cfg.CacheSeriesResults {
		var err error
		cacheMiddleware, err = NewSeriesCacheMiddleware(
			log,
			limits,
			merger,
			c,
			cacheGenNumLoader,
			func(_ context.Context, r base.Request) bool {
				return !r.GetCachingOptions().Disabled
			},
			func(ctx context.Context, tenantIDs []string, r base.Request) int {
				return MinWeightedParallelism(
					ctx,
					tenantIDs,
					schema.Configs,
					limits,
					model.Time(r.GetStart().UnixMilli()),
					model.Time(r.GetEnd().UnixMilli()),
				)
			},
			retentionEnabled,
			cfg.Transformer,
			metrics.ResultsCacheMetrics,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create series cache middleware: %w", err)
		}
	}

	queryRangeMiddleware := []base.Middleware{
		StatsCollectorMiddleware(),
		NewLimitsMiddleware(limits),
		base.InstrumentMiddleware("split_by_interval", metrics.InstrumentMiddlewareMetrics),
		SplitByIntervalMiddleware(schema.Configs, limits, merger, newDefaultSplitter(limits, iqo), metrics.SplitByMetrics),
	}

	if cfg.CacheSeriesResults {
		queryRangeMiddleware = append(
			queryRangeMiddleware,
			base.InstrumentMiddleware("series_results_cache", metrics.InstrumentMiddlewareMetrics),
			cacheMiddleware,
		)
	}

	if cfg.MaxRetries > 0 {
		queryRangeMiddleware = append(queryRangeMiddleware,
			base.InstrumentMiddleware("retry", metrics.InstrumentMiddlewareMetrics),
			base.NewRetryMiddleware(log, cfg.MaxRetries, metrics.RetryMiddlewareMetrics, metricsNamespace),
		)
	}

	if cfg.ShardedQueries {
		queryRangeMiddleware = append(queryRangeMiddleware,
			NewSeriesQueryShardMiddleware(
				log,
				schema.Configs,
				metrics.InstrumentMiddlewareMetrics,
				metrics.MiddlewareMapperMetrics.shardMapper,
				limits,
				merger,
			),
		)
	}

	return base.MiddlewareFunc(func(next base.Handler) base.Handler {
		return NewLimitedRoundTripper(next, limits, schema.Configs, queryRangeMiddleware...)
	}), nil
}

// NewLabelsTripperware creates a new frontend tripperware responsible for handling labels requests.
func NewLabelsTripperware(
	cfg Config,
	log log.Logger,
	limits Limits,
	merger base.Merger,
	iqo util.IngesterQueryOptions,
	c cache.Cache,
	cacheGenNumLoader base.CacheGenNumberLoader,
	retentionEnabled bool,
	metrics *Metrics,
	schema config.SchemaConfig,
	metricsNamespace string,
) (base.Middleware, error) {
	var cacheMiddleware base.Middleware
	if cfg.CacheLabelResults {
		var err error
		cacheMiddleware, err = NewLabelsCacheMiddleware(
			log,
			limits,
			merger,
			c,
			cacheGenNumLoader,
			func(_ context.Context, r base.Request) bool {
				return !r.GetCachingOptions().Disabled
			},
			func(ctx context.Context, tenantIDs []string, r base.Request) int {
				return MinWeightedParallelism(
					ctx,
					tenantIDs,
					schema.Configs,
					limits,
					model.Time(r.GetStart().UnixMilli()),
					model.Time(r.GetEnd().UnixMilli()),
				)
			},
			retentionEnabled,
			cfg.Transformer,
			metrics.ResultsCacheMetrics,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create labels cache middleware: %w", err)
		}
	}

	queryRangeMiddleware := []base.Middleware{
		StatsCollectorMiddleware(),
		NewLimitsMiddleware(limits),
		base.InstrumentMiddleware("split_by_interval", metrics.InstrumentMiddlewareMetrics),
		SplitByIntervalMiddleware(schema.Configs, limits, merger, newDefaultSplitter(limits, iqo), metrics.SplitByMetrics),
	}

	if cfg.CacheLabelResults {
		queryRangeMiddleware = append(
			queryRangeMiddleware,
			base.InstrumentMiddleware("label_results_cache", metrics.InstrumentMiddlewareMetrics),
			cacheMiddleware,
		)
	}

	if cfg.MaxRetries > 0 {
		queryRangeMiddleware = append(queryRangeMiddleware,
			base.InstrumentMiddleware("retry", metrics.InstrumentMiddlewareMetrics),
			base.NewRetryMiddleware(log, cfg.MaxRetries, metrics.RetryMiddlewareMetrics, metricsNamespace),
		)
	}

	return base.MiddlewareFunc(func(next base.Handler) base.Handler {
		// Do not forward any request header.
		return base.MergeMiddlewares(queryRangeMiddleware...).Wrap(next)
	}), nil
}

// NewMetricTripperware creates a new frontend tripperware responsible for handling metric queries
func NewMetricTripperware(cfg Config, engineOpts logql.EngineOpts, log log.Logger, limits Limits, schema config.SchemaConfig, merger base.Merger, iqo util.IngesterQueryOptions, c cache.Cache, cacheGenNumLoader base.CacheGenNumberLoader, retentionEnabled bool, extractor base.Extractor, metrics *Metrics, indexStatsTripperware base.Middleware, metricsNamespace string) (base.Middleware, error) {
	cacheKey := cacheKeyLimits{limits, cfg.Transformer, iqo}
	var queryCacheMiddleware base.Middleware
	if cfg.CacheResults {
		var err error
		queryCacheMiddleware, err = base.NewResultsCacheMiddleware(
			log,
			c,
			cacheKey,
			limits,
			merger,
			extractor,
			cacheGenNumLoader,
			func(_ context.Context, r base.Request) bool {
				return !r.GetCachingOptions().Disabled
			},
			func(ctx context.Context, tenantIDs []string, r base.Request) int {
				return MinWeightedParallelism(
					ctx,
					tenantIDs,
					schema.Configs,
					limits,
					model.Time(r.GetStart().UnixMilli()),
					model.Time(r.GetEnd().UnixMilli()),
				)
			},
			retentionEnabled,
			false,
			metrics.ResultsCacheMetrics,
		)
		if err != nil {
			return nil, err
		}
	}

	return base.MiddlewareFunc(func(next base.Handler) base.Handler {
		statsHandler := indexStatsTripperware.Wrap(next)

		queryRangeMiddleware := []base.Middleware{
			QueryMetricsMiddleware(metrics.QueryMetrics),
			StatsCollectorMiddleware(),
			NewLimitsMiddleware(limits),
		}

		if cfg.AlignQueriesWithStep {
			queryRangeMiddleware = append(
				queryRangeMiddleware,
				base.InstrumentMiddleware("step_align", metrics.InstrumentMiddlewareMetrics),
				base.StepAlignMiddleware,
			)
		}

		queryRangeMiddleware = append(
			queryRangeMiddleware,
			NewQuerySizeLimiterMiddleware(schema.Configs, engineOpts, log, limits, statsHandler),
			base.InstrumentMiddleware("split_by_interval", metrics.InstrumentMiddlewareMetrics),
			SplitByIntervalMiddleware(schema.Configs, limits, merger, newMetricQuerySplitter(limits, iqo), metrics.SplitByMetrics),
		)

		if cfg.CacheResults {
			queryRangeMiddleware = append(
				queryRangeMiddleware,
				base.InstrumentMiddleware("results_cache", metrics.InstrumentMiddlewareMetrics),
				queryCacheMiddleware,
			)
		}

		if cfg.ShardedQueries {
			queryRangeMiddleware = append(queryRangeMiddleware,
				NewQueryShardMiddleware(
					log,
					schema.Configs,
					engineOpts,
					metrics.InstrumentMiddlewareMetrics, // instrumentation is included in the sharding middleware
					metrics.MiddlewareMapperMetrics.shardMapper,
					limits,
					0, // 0 is unlimited shards
					statsHandler,
					cfg.ShardAggregations,
				),
			)
		} else {
			// The sharding middleware takes care of enforcing this limit for both shardable and non-shardable queries.
			// If we are not using sharding, we enforce the limit by adding this middleware after time splitting.
			queryRangeMiddleware = append(queryRangeMiddleware,
				NewQuerierSizeLimiterMiddleware(schema.Configs, engineOpts, log, limits, statsHandler),
			)
		}

		if cfg.MaxRetries > 0 {
			queryRangeMiddleware = append(
				queryRangeMiddleware,
				base.InstrumentMiddleware("retry", metrics.InstrumentMiddlewareMetrics),
				base.NewRetryMiddleware(log, cfg.MaxRetries, metrics.RetryMiddlewareMetrics, metricsNamespace),
			)
		}

		// Finally, if the user selected any query range middleware, stitch it in.
		if len(queryRangeMiddleware) > 0 {
			rt := NewLimitedRoundTripper(next, limits, schema.Configs, queryRangeMiddleware...)
			return base.HandlerFunc(func(ctx context.Context, r base.Request) (base.Response, error) {
				if _, ok := r.(*LokiRequest); !ok {
					return next.Do(ctx, r)
				}
				return rt.Do(ctx, r)
			})
		}
		return next
	}), nil
}

// NewInstantMetricTripperware creates a new frontend tripperware responsible for handling metric queries
func NewInstantMetricTripperware(
	cfg Config,
	engineOpts logql.EngineOpts,
	log log.Logger,
	limits Limits,
	schema config.SchemaConfig,
	metrics *Metrics,
	merger base.Merger,
	c cache.Cache,
	cacheGenNumLoader base.CacheGenNumberLoader,
	retentionEnabled bool,
	indexStatsTripperware base.Middleware,
	metricsNamespace string,
) (base.Middleware, error) {
	var cacheMiddleware base.Middleware
	if cfg.CacheInstantMetricResults {
		var err error
		cacheMiddleware, err = NewInstantMetricCacheMiddleware(
			log,
			limits,
			merger,
			c,
			cacheGenNumLoader,
			func(_ context.Context, r base.Request) bool {
				return !r.GetCachingOptions().Disabled
			},
			func(ctx context.Context, tenantIDs []string, r base.Request) int {
				return MinWeightedParallelism(
					ctx,
					tenantIDs,
					schema.Configs,
					limits,
					model.Time(r.GetStart().UnixMilli()),
					model.Time(r.GetEnd().UnixMilli()),
				)
			},
			retentionEnabled,
			cfg.Transformer,
			metrics.ResultsCacheMetrics,
		)
		if err != nil {
			return nil, err
		}
	}

	return base.MiddlewareFunc(func(next base.Handler) base.Handler {
		statsHandler := indexStatsTripperware.Wrap(next)

		queryRangeMiddleware := []base.Middleware{
			StatsCollectorMiddleware(),
			NewLimitsMiddleware(limits),
			NewQuerySizeLimiterMiddleware(schema.Configs, engineOpts, log, limits, statsHandler),
			NewSplitByRangeMiddleware(log, engineOpts, limits, cfg.InstantMetricQuerySplitAlign, metrics.MiddlewareMapperMetrics.rangeMapper),
		}

		if cfg.CacheInstantMetricResults {
			queryRangeMiddleware = append(
				queryRangeMiddleware,
				base.InstrumentMiddleware("instant_metric_results_cache", metrics.InstrumentMiddlewareMetrics),
				cacheMiddleware,
			)
		}

		if cfg.ShardedQueries {
			queryRangeMiddleware = append(queryRangeMiddleware,
				NewQueryShardMiddleware(
					log,
					schema.Configs,
					engineOpts,
					metrics.InstrumentMiddlewareMetrics, // instrumentation is included in the sharding middleware
					metrics.MiddlewareMapperMetrics.shardMapper,
					limits,
					0, // 0 is unlimited shards
					statsHandler,
					cfg.ShardAggregations,
				),
			)
		}

		if cfg.MaxRetries > 0 {
			queryRangeMiddleware = append(
				queryRangeMiddleware,
				base.InstrumentMiddleware("retry", metrics.InstrumentMiddlewareMetrics),
				base.NewRetryMiddleware(log, cfg.MaxRetries, metrics.RetryMiddlewareMetrics, metricsNamespace),
			)
		}

		if len(queryRangeMiddleware) > 0 {
			return NewLimitedRoundTripper(next, limits, schema.Configs, queryRangeMiddleware...)
		}
		return next
	}), nil
}

func NewVolumeTripperware(cfg Config, log log.Logger, limits Limits, schema config.SchemaConfig, merger base.Merger, iqo util.IngesterQueryOptions, c cache.Cache, cacheGenNumLoader base.CacheGenNumberLoader, retentionEnabled bool, metrics *Metrics, metricsNamespace string) (base.Middleware, error) {
	// Parallelize the volume requests, so it doesn't send a huge request to a single index-gw (i.e. {app=~".+"} for 30d).
	// Indices are sharded by 24 hours, so we split the volume request in 24h intervals.
	limits = WithSplitByLimits(limits, indexStatsQuerySplitInterval)
	var cacheMiddleware base.Middleware
	if cfg.CacheVolumeResults {
		var err error
		cacheMiddleware, err = NewVolumeCacheMiddleware(
			log,
			limits,
			merger,
			c,
			cacheGenNumLoader,
			iqo,
			func(_ context.Context, r base.Request) bool {
				return !r.GetCachingOptions().Disabled
			},
			func(ctx context.Context, tenantIDs []string, r base.Request) int {
				return MinWeightedParallelism(
					ctx,
					tenantIDs,
					schema.Configs,
					limits,
					model.Time(r.GetStart().UnixMilli()),
					model.Time(r.GetEnd().UnixMilli()),
				)
			},
			retentionEnabled,
			cfg.Transformer,
			metrics.ResultsCacheMetrics,
		)
		if err != nil {
			return nil, err
		}
	}

	indexTw, err := sharedIndexTripperware(
		cacheMiddleware,
		cfg,
		merger,
		newDefaultSplitter(limits, iqo),
		limits,
		log,
		metrics,
		schema,
		metricsNamespace,
	)

	if err != nil {
		return nil, err
	}

	return volumeFeatureFlagRoundTripper(
		volumeRangeTripperware(indexTw),
		limits,
	), nil
}

func statsTripperware(nextTW base.Middleware) base.Middleware {
	return base.MiddlewareFunc(func(next base.Handler) base.Handler {
		return base.HandlerFunc(func(ctx context.Context, r base.Request) (base.Response, error) {
			cacheMiddlewares := []base.Middleware{
				StatsCollectorMiddleware(),
				nextTW,
			}

			// wrap nextRT with our new middleware
			return base.MergeMiddlewares(
				cacheMiddlewares...,
			).Wrap(next).Do(ctx, r)
		})
	})
}

func volumeRangeTripperware(nextTW base.Middleware) base.Middleware {
	return base.MiddlewareFunc(func(next base.Handler) base.Handler {
		return base.HandlerFunc(func(ctx context.Context, r base.Request) (base.Response, error) {
			seriesVolumeMiddlewares := []base.Middleware{
				StatsCollectorMiddleware(),
				NewVolumeMiddleware(),
				nextTW,
			}

			// wrap nextRT with our new middleware
			return base.MergeMiddlewares(
				seriesVolumeMiddlewares...,
			).Wrap(next).Do(ctx, r)
		})
	})
}

func volumeFeatureFlagRoundTripper(nextTW base.Middleware, limits Limits) base.Middleware {
	return base.MiddlewareFunc(func(next base.Handler) base.Handler {
		nextRt := nextTW.Wrap(next)
		return base.HandlerFunc(func(ctx context.Context, r base.Request) (base.Response, error) {
			userID, err := user.ExtractOrgID(ctx)
			if err != nil {
				return nil, err
			}

			if !limits.VolumeEnabled(userID) {
				return nil, httpgrpc.Errorf(http.StatusNotFound, "not found")
			}

			return nextRt.Do(ctx, r)
		})
	})
}

func NewIndexStatsTripperware(cfg Config, log log.Logger, limits Limits, schema config.SchemaConfig, merger base.Merger, iqo util.IngesterQueryOptions, c cache.Cache, cacheGenNumLoader base.CacheGenNumberLoader, retentionEnabled bool, metrics *Metrics, metricsNamespace string) (base.Middleware, error) {
	limits = WithSplitByLimits(limits, indexStatsQuerySplitInterval)

	var cacheMiddleware base.Middleware
	if cfg.CacheIndexStatsResults {
		var err error
		cacheMiddleware, err = NewIndexStatsCacheMiddleware(
			log,
			limits,
			merger,
			c,
			cacheGenNumLoader,
			iqo,
			func(_ context.Context, r base.Request) bool {
				return !r.GetCachingOptions().Disabled
			},
			func(ctx context.Context, tenantIDs []string, r base.Request) int {
				return MinWeightedParallelism(
					ctx,
					tenantIDs,
					schema.Configs,
					limits,
					model.Time(r.GetStart().UnixMilli()),
					model.Time(r.GetEnd().UnixMilli()),
				)
			},
			retentionEnabled,
			cfg.Transformer,
			metrics.ResultsCacheMetrics,
		)
		if err != nil {
			return nil, err
		}
	}

	tw, err := sharedIndexTripperware(
		cacheMiddleware,
		cfg,
		merger,
		newDefaultSplitter(limits, iqo),
		limits,
		log,
		metrics,
		schema,
		metricsNamespace,
	)
	if err != nil {
		return nil, err
	}

	return statsTripperware(tw), nil
}

func sharedIndexTripperware(
	cacheMiddleware base.Middleware,
	cfg Config,
	merger base.Merger,
	split splitter,
	limits Limits,
	log log.Logger,
	metrics *Metrics,
	schema config.SchemaConfig,
	metricsNamespace string,
) (base.Middleware, error) {
	return base.MiddlewareFunc(func(next base.Handler) base.Handler {
		middlewares := []base.Middleware{
			NewLimitsMiddleware(limits),
			base.InstrumentMiddleware("split_by_interval", metrics.InstrumentMiddlewareMetrics),
			SplitByIntervalMiddleware(schema.Configs, limits, merger, split, metrics.SplitByMetrics),
		}

		if cacheMiddleware != nil {
			middlewares = append(
				middlewares,
				base.InstrumentMiddleware("log_results_cache", metrics.InstrumentMiddlewareMetrics),
				cacheMiddleware,
			)
		}

		if cfg.MaxRetries > 0 {
			middlewares = append(
				middlewares,
				base.InstrumentMiddleware("retry", metrics.InstrumentMiddlewareMetrics),
				base.NewRetryMiddleware(log, cfg.MaxRetries, metrics.RetryMiddlewareMetrics, metricsNamespace),
			)
		}

		return base.MergeMiddlewares(middlewares...).Wrap(next)
	}), nil
}
