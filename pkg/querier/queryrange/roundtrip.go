package queryrange

import (
	"context"
	"flag"
	"net/http"
	"strings"
	"time"

	"github.com/grafana/dskit/user"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/config"
	logutil "github.com/grafana/loki/pkg/util/log"
)

// Config is the configuration for the queryrange tripperware
type Config struct {
	queryrangebase.Config  `yaml:",inline"`
	Transformer            UserIDTransformer     `yaml:"-"`
	CacheIndexStatsResults bool                  `yaml:"cache_index_stats_results"`
	StatsCacheConfig       IndexStatsCacheConfig `yaml:"index_stats_results_cache" doc:"description=If a cache config is not specified and cache_index_stats_results is true, the config for the results cache is used."`
	CacheVolumeResults     bool                  `yaml:"cache_volume_results"`
	VolumeCacheConfig      VolumeCacheConfig     `yaml:"volume_results_cache" doc:"description=If a cache config is not specified and cache_volume_results is true, the config for the results cache is used."`
}

// RegisterFlags adds the flags required to configure this flag set.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Config.RegisterFlags(f)
	f.BoolVar(&cfg.CacheIndexStatsResults, "querier.cache-index-stats-results", false, "Cache index stats query results.")
	cfg.StatsCacheConfig.RegisterFlags(f)
	f.BoolVar(&cfg.CacheVolumeResults, "querier.cache-volume-results", false, "Cache volume query results.")
	cfg.VolumeCacheConfig.RegisterFlags(f)
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

func newResultsCacheFromConfig(cfg queryrangebase.ResultsCacheConfig, registerer prometheus.Registerer, log log.Logger, cacheType stats.CacheType) (cache.Cache, error) {
	if !cache.IsCacheConfigured(cfg.CacheConfig) {
		return nil, errors.Errorf("%s cache is not configured", cacheType)
	}

	c, err := cache.New(cfg.CacheConfig, registerer, log, cacheType)
	if err != nil {
		return nil, err
	}

	if cfg.Compression == "snappy" {
		c = cache.NewSnappy(c, log)
	}

	return c, nil
}

// NewTripperware returns a Tripperware configured with middlewares to align, split and cache requests.
func NewTripperware(
	cfg Config,
	engineOpts logql.EngineOpts,
	log log.Logger,
	limits Limits,
	schema config.SchemaConfig,
	cacheGenNumLoader queryrangebase.CacheGenNumberLoader,
	retentionEnabled bool,
	registerer prometheus.Registerer,
) (queryrangebase.Tripperware, Stopper, error) {
	metrics := NewMetrics(registerer)

	var (
		resultsCache cache.Cache
		statsCache   cache.Cache
		volumeCache  cache.Cache
		err          error
	)

	if cfg.CacheResults {
		resultsCache, err = newResultsCacheFromConfig(cfg.ResultsCacheConfig, registerer, log, stats.ResultCache)
		if err != nil {
			return nil, nil, err
		}
	}

	if cfg.CacheIndexStatsResults {
		// If the stats cache is not configured, use the results cache config.
		cacheCfg := cfg.StatsCacheConfig.ResultsCacheConfig
		if !cache.IsCacheConfigured(cacheCfg.CacheConfig) {
			level.Debug(log).Log("msg", "using results cache config for stats cache")
			cacheCfg = cfg.ResultsCacheConfig
		}

		statsCache, err = newResultsCacheFromConfig(cacheCfg, registerer, log, stats.StatsResultCache)
		if err != nil {
			return nil, nil, err
		}
	}

	if cfg.CacheVolumeResults {
		// If the volume cache is not configured, use the results cache config.
		cacheCfg := cfg.VolumeCacheConfig.ResultsCacheConfig
		if !cache.IsCacheConfigured(cacheCfg.CacheConfig) {
			level.Debug(log).Log("msg", "using results cache config for volume cache")
			cacheCfg = cfg.ResultsCacheConfig
		}

		volumeCache, err = newResultsCacheFromConfig(cacheCfg, registerer, log, stats.VolumeResultCache)
		if err != nil {
			return nil, nil, err
		}
	}

	var codec queryrangebase.Codec = DefaultCodec
	if cfg.RequiredQueryResponseFormat == "protobuf" {
		codec = &RequestProtobufCodec{}
	}

	indexStatsTripperware, err := NewIndexStatsTripperware(cfg, log, limits, schema, codec, statsCache,
		cacheGenNumLoader, retentionEnabled, metrics)
	if err != nil {
		return nil, nil, err
	}

	metricsTripperware, err := NewMetricTripperware(cfg, engineOpts, log, limits, schema, codec, resultsCache,
		cacheGenNumLoader, retentionEnabled, PrometheusExtractor{}, metrics, indexStatsTripperware)
	if err != nil {
		return nil, nil, err
	}

	limitedTripperware, err := NewLimitedTripperware(cfg, engineOpts, log, limits, schema, codec, metrics, indexStatsTripperware)
	if err != nil {
		return nil, nil, err
	}

	// NOTE: When we would start caching response from non-metric queries we would have to consider cache gen headers as well in
	// MergeResponse implementation for Loki codecs same as it is done in Cortex at https://github.com/cortexproject/cortex/blob/21bad57b346c730d684d6d0205efef133422ab28/pkg/querier/queryrange/query_range.go#L170
	logFilterTripperware, err := NewLogFilterTripperware(cfg, engineOpts, log, limits, schema, codec, resultsCache, metrics, indexStatsTripperware)
	if err != nil {
		return nil, nil, err
	}

	seriesTripperware, err := NewSeriesTripperware(cfg, log, limits, codec, metrics, schema)
	if err != nil {
		return nil, nil, err
	}

	labelsTripperware, err := NewLabelsTripperware(cfg, log, limits, codec, metrics, schema)
	if err != nil {
		return nil, nil, err
	}

	instantMetricTripperware, err := NewInstantMetricTripperware(cfg, engineOpts, log, limits, schema, codec, metrics, indexStatsTripperware)
	if err != nil {
		return nil, nil, err
	}

	seriesVolumeTripperware, err := NewVolumeTripperware(cfg, log, limits, schema, codec, volumeCache, cacheGenNumLoader, retentionEnabled, metrics)
	if err != nil {
		return nil, nil, err
	}

	return func(next http.RoundTripper) http.RoundTripper {
		var (
			metricRT       = metricsTripperware(next)
			limitedRT      = limitedTripperware(next)
			logFilterRT    = logFilterTripperware(next)
			seriesRT       = seriesTripperware(next)
			labelsRT       = labelsTripperware(next)
			instantRT      = instantMetricTripperware(next)
			statsRT        = indexStatsTripperware(next)
			seriesVolumeRT = seriesVolumeTripperware(next)
		)

		return newRoundTripper(log, next, limitedRT, logFilterRT, metricRT, seriesRT, labelsRT, instantRT, statsRT, seriesVolumeRT, limits)
	}, StopperWrapper{resultsCache, statsCache, volumeCache}, nil
}

type roundTripper struct {
	logger log.Logger

	next, limited, log, metric, series, labels, instantMetric, indexStats, seriesVolume http.RoundTripper

	limits Limits
}

// newRoundTripper creates a new queryrange roundtripper
func newRoundTripper(logger log.Logger, next, limited, log, metric, series, labels, instantMetric, indexStats, seriesVolume http.RoundTripper, limits Limits) roundTripper {
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

func (r roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	logger := logutil.WithContext(req.Context(), r.logger)
	err := req.ParseForm()
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	switch op := getOperation(req.URL.Path); op {
	case QueryRangeOp:
		rangeQuery, err := loghttp.ParseRangeQuery(req)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}
		expr, err := syntax.ParseExpr(rangeQuery.Query)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}

		queryHash := logql.HashedQuery(rangeQuery.Query)
		level.Info(logger).Log("msg", "executing query", "type", "range", "query", rangeQuery.Query, "length", rangeQuery.End.Sub(rangeQuery.Start), "step", rangeQuery.Step, "query_hash", queryHash)

		switch e := expr.(type) {
		case syntax.SampleExpr:
			// The error will be handled later.
			groups, err := e.MatcherGroups()
			if err != nil {
				level.Warn(logger).Log("msg", "unexpected matcher groups error in roundtripper", "err", err)
			}

			for _, g := range groups {
				if err := validateMatchers(req, r.limits, g.Matchers); err != nil {
					return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
				}
			}
			return r.metric.RoundTrip(req)
		case syntax.LogSelectorExpr:
			// Note, this function can mutate the request
			expr, err := transformRegexQuery(req, e)
			if err != nil {
				return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
			}
			if err := validateMaxEntriesLimits(req, rangeQuery.Limit, r.limits); err != nil {
				return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
			}

			if err := validateMatchers(req, r.limits, e.Matchers()); err != nil {
				return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
			}

			// Only filter expressions are query sharded
			if !expr.HasFilter() {
				return r.limited.RoundTrip(req)
			}
			return r.log.RoundTrip(req)

		default:
			return r.next.RoundTrip(req)
		}
	case SeriesOp:
		sr, err := loghttp.ParseAndValidateSeriesQuery(req)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}

		level.Info(logger).Log("msg", "executing query", "type", "series", "match", logql.PrintMatches(sr.Groups), "length", sr.End.Sub(sr.Start))

		return r.series.RoundTrip(req)
	case LabelNamesOp:
		lr, err := loghttp.ParseLabelQuery(req)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}

		level.Info(logger).Log("msg", "executing query", "type", "labels", "label", lr.Name, "length", lr.End.Sub(*lr.Start), "query", lr.Query)

		return r.labels.RoundTrip(req)
	case InstantQueryOp:
		instantQuery, err := loghttp.ParseInstantQuery(req)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}
		expr, err := syntax.ParseExpr(instantQuery.Query)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}

		queryHash := logql.HashedQuery(instantQuery.Query)
		level.Info(logger).Log("msg", "executing query", "type", "instant", "query", instantQuery.Query, "query_hash", queryHash)

		switch expr.(type) {
		case syntax.SampleExpr:
			return r.instantMetric.RoundTrip(req)
		default:
			return r.next.RoundTrip(req)
		}
	case IndexStatsOp:
		statsQuery, err := loghttp.ParseIndexStatsQuery(req)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}
		level.Info(logger).Log("msg", "executing query", "type", "stats", "query", statsQuery.Query, "length", statsQuery.End.Sub(statsQuery.Start))

		return r.indexStats.RoundTrip(req)
	case VolumeOp:
		volumeQuery, err := loghttp.ParseVolumeInstantQuery(req)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}
		level.Info(logger).Log(
			"msg", "executing query",
			"type", "volume",
			"query", volumeQuery.Query,
			"length", volumeQuery.End.Sub(volumeQuery.Start),
			"limit", volumeQuery.Limit,
			"aggregate_by", volumeQuery.AggregateBy,
		)

		return r.seriesVolume.RoundTrip(req)
	case VolumeRangeOp:
		volumeQuery, err := loghttp.ParseVolumeRangeQuery(req)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}
		level.Info(logger).Log(
			"msg", "executing query",
			"type", "volume_range",
			"query", volumeQuery.Query,
			"length", volumeQuery.End.Sub(volumeQuery.Start),
			"step", volumeQuery.Step,
			"limit", volumeQuery.Limit,
			"aggregate_by", volumeQuery.AggregateBy,
		)

		return r.seriesVolume.RoundTrip(req)
	default:
		return r.next.RoundTrip(req)
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
	default:
		return ""
	}
}

// NewLogFilterTripperware creates a new frontend tripperware responsible for handling log requests.
func NewLogFilterTripperware(
	cfg Config,
	engineOpts logql.EngineOpts,
	log log.Logger,
	limits Limits,
	schema config.SchemaConfig,
	codec queryrangebase.Codec,
	c cache.Cache,
	metrics *Metrics,
	indexStatsTripperware queryrangebase.Tripperware,
) (queryrangebase.Tripperware, error) {
	return func(next http.RoundTripper) http.RoundTripper {
		statsHandler := queryrangebase.NewRoundTripperHandler(indexStatsTripperware(next), codec)

		queryRangeMiddleware := []queryrangebase.Middleware{
			StatsCollectorMiddleware(),
			NewLimitsMiddleware(limits),
			NewQuerySizeLimiterMiddleware(schema.Configs, engineOpts, log, limits, statsHandler),
			queryrangebase.InstrumentMiddleware("split_by_interval", metrics.InstrumentMiddlewareMetrics),
			SplitByIntervalMiddleware(schema.Configs, limits, codec, splitByTime, metrics.SplitByMetrics),
		}

		if cfg.CacheResults {
			queryCacheMiddleware := NewLogResultCache(
				log,
				limits,
				c,
				func(_ context.Context, r queryrangebase.Request) bool {
					return !r.GetCachingOptions().Disabled
				},
				cfg.Transformer,
				metrics.LogResultCacheMetrics,
			)
			queryRangeMiddleware = append(
				queryRangeMiddleware,
				queryrangebase.InstrumentMiddleware("log_results_cache", metrics.InstrumentMiddlewareMetrics),
				queryCacheMiddleware,
			)
		}

		if cfg.ShardedQueries {
			queryRangeMiddleware = append(queryRangeMiddleware,
				NewQueryShardMiddleware(
					log,
					schema.Configs,
					engineOpts,
					codec,
					metrics.InstrumentMiddlewareMetrics, // instrumentation is included in the sharding middleware
					metrics.MiddlewareMapperMetrics.shardMapper,
					limits,
					0, // 0 is unlimited shards
					statsHandler,
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
				queryRangeMiddleware, queryrangebase.InstrumentMiddleware("retry", metrics.InstrumentMiddlewareMetrics),
				queryrangebase.NewRetryMiddleware(log, cfg.MaxRetries, metrics.RetryMiddlewareMetrics),
			)
		}

		if len(queryRangeMiddleware) > 0 {
			return NewLimitedRoundTripper(next, codec, limits, schema.Configs, queryRangeMiddleware...)
		}
		return next
	}, nil
}

// NewLimitedTripperware creates a new frontend tripperware responsible for handling log requests which are label matcher only, no filter expression.
func NewLimitedTripperware(
	_ Config,
	engineOpts logql.EngineOpts,
	log log.Logger,
	limits Limits,
	schema config.SchemaConfig,
	codec queryrangebase.Codec,
	metrics *Metrics,
	indexStatsTripperware queryrangebase.Tripperware,
) (queryrangebase.Tripperware, error) {
	return func(next http.RoundTripper) http.RoundTripper {
		statsHandler := queryrangebase.NewRoundTripperHandler(indexStatsTripperware(next), codec)

		queryRangeMiddleware := []queryrangebase.Middleware{
			StatsCollectorMiddleware(),
			NewLimitsMiddleware(limits),
			NewQuerySizeLimiterMiddleware(schema.Configs, engineOpts, log, limits, statsHandler),
			queryrangebase.InstrumentMiddleware("split_by_interval", metrics.InstrumentMiddlewareMetrics),
			// Limited queries only need to fetch up to the requested line limit worth of logs,
			// Our defaults for splitting and parallelism are much too aggressive for large customers and result in
			// potentially GB of logs being returned by all the shards and splits which will overwhelm the frontend
			// Therefore we force max parallelism to one so that these queries are executed sequentially.
			// Below we also fix the number of shards to a static number.
			SplitByIntervalMiddleware(schema.Configs, WithMaxParallelism(limits, 1), codec, splitByTime, metrics.SplitByMetrics),
			NewQuerierSizeLimiterMiddleware(schema.Configs, engineOpts, log, limits, statsHandler),
		}

		if len(queryRangeMiddleware) > 0 {
			return NewLimitedRoundTripper(next, codec, limits, schema.Configs, queryRangeMiddleware...)
		}
		return next
	}, nil
}

// NewSeriesTripperware creates a new frontend tripperware responsible for handling series requests
func NewSeriesTripperware(
	cfg Config,
	log log.Logger,
	limits Limits,
	codec queryrangebase.Codec,
	metrics *Metrics,
	schema config.SchemaConfig,
) (queryrangebase.Tripperware, error) {
	queryRangeMiddleware := []queryrangebase.Middleware{
		StatsCollectorMiddleware(),
		NewLimitsMiddleware(limits),
		queryrangebase.InstrumentMiddleware("split_by_interval", metrics.InstrumentMiddlewareMetrics),
		// The Series API needs to pull one chunk per series to extract the label set, which is much cheaper than iterating through all matching chunks.
		// Force a 24 hours split by for series API, this will be more efficient with our static daily bucket storage.
		// This would avoid queriers downloading chunks for same series over and over again for serving smaller queries.
		SplitByIntervalMiddleware(schema.Configs, WithSplitByLimits(limits, 24*time.Hour), codec, splitByTime, metrics.SplitByMetrics),
	}

	if cfg.MaxRetries > 0 {
		queryRangeMiddleware = append(queryRangeMiddleware,
			queryrangebase.InstrumentMiddleware("retry", metrics.InstrumentMiddlewareMetrics),
			queryrangebase.NewRetryMiddleware(log, cfg.MaxRetries, metrics.RetryMiddlewareMetrics),
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
				codec,
			),
		)
	}

	return func(next http.RoundTripper) http.RoundTripper {
		if len(queryRangeMiddleware) > 0 {
			return NewLimitedRoundTripper(next, codec, limits, schema.Configs, queryRangeMiddleware...)
		}
		return next
	}, nil
}

// NewLabelsTripperware creates a new frontend tripperware responsible for handling labels requests.
func NewLabelsTripperware(
	cfg Config,
	log log.Logger,
	limits Limits,
	codec queryrangebase.Codec,
	metrics *Metrics,
	schema config.SchemaConfig,
) (queryrangebase.Tripperware, error) {
	queryRangeMiddleware := []queryrangebase.Middleware{
		StatsCollectorMiddleware(),
		NewLimitsMiddleware(limits),
		queryrangebase.InstrumentMiddleware("split_by_interval", metrics.InstrumentMiddlewareMetrics),
		// Force a 24 hours split by for labels API, this will be more efficient with our static daily bucket storage.
		// This is because the labels API is an index-only operation.
		SplitByIntervalMiddleware(schema.Configs, WithSplitByLimits(limits, 24*time.Hour), codec, splitByTime, metrics.SplitByMetrics),
	}

	if cfg.MaxRetries > 0 {
		queryRangeMiddleware = append(queryRangeMiddleware,
			queryrangebase.InstrumentMiddleware("retry", metrics.InstrumentMiddlewareMetrics),
			queryrangebase.NewRetryMiddleware(log, cfg.MaxRetries, metrics.RetryMiddlewareMetrics),
		)
	}

	return func(next http.RoundTripper) http.RoundTripper {
		if len(queryRangeMiddleware) > 0 {
			// Do not forward any request header.
			return queryrangebase.NewRoundTripper(next, codec, nil, queryRangeMiddleware...)
		}
		return next
	}, nil
}

// NewMetricTripperware creates a new frontend tripperware responsible for handling metric queries
func NewMetricTripperware(
	cfg Config,
	engineOpts logql.EngineOpts,
	log log.Logger,
	limits Limits,
	schema config.SchemaConfig,
	codec queryrangebase.Codec,
	c cache.Cache,
	cacheGenNumLoader queryrangebase.CacheGenNumberLoader,
	retentionEnabled bool,
	extractor queryrangebase.Extractor,
	metrics *Metrics,
	indexStatsTripperware queryrangebase.Tripperware,
) (queryrangebase.Tripperware, error) {
	cacheKey := cacheKeyLimits{limits, cfg.Transformer}
	var queryCacheMiddleware queryrangebase.Middleware
	if cfg.CacheResults {
		var err error
		queryCacheMiddleware, err = queryrangebase.NewResultsCacheMiddleware(
			log,
			c,
			cacheKey,
			limits,
			codec,
			extractor,
			cacheGenNumLoader,
			func(_ context.Context, r queryrangebase.Request) bool {
				return !r.GetCachingOptions().Disabled
			},
			func(ctx context.Context, tenantIDs []string, r queryrangebase.Request) int {
				return MinWeightedParallelism(
					ctx,
					tenantIDs,
					schema.Configs,
					limits,
					model.Time(r.GetStart()),
					model.Time(r.GetEnd()),
				)
			},
			retentionEnabled,
			metrics.ResultsCacheMetrics,
		)
		if err != nil {
			return nil, err
		}
	}

	return func(next http.RoundTripper) http.RoundTripper {
		statsHandler := queryrangebase.NewRoundTripperHandler(indexStatsTripperware(next), codec)

		queryRangeMiddleware := []queryrangebase.Middleware{
			StatsCollectorMiddleware(),
			NewLimitsMiddleware(limits),
		}

		if cfg.AlignQueriesWithStep {
			queryRangeMiddleware = append(
				queryRangeMiddleware,
				queryrangebase.InstrumentMiddleware("step_align", metrics.InstrumentMiddlewareMetrics),
				queryrangebase.StepAlignMiddleware,
			)
		}

		queryRangeMiddleware = append(
			queryRangeMiddleware,
			NewQuerySizeLimiterMiddleware(schema.Configs, engineOpts, log, limits, statsHandler),
			queryrangebase.InstrumentMiddleware("split_by_interval", metrics.InstrumentMiddlewareMetrics),
			SplitByIntervalMiddleware(schema.Configs, limits, codec, splitMetricByTime, metrics.SplitByMetrics),
		)

		if cfg.CacheResults {
			queryRangeMiddleware = append(
				queryRangeMiddleware,
				queryrangebase.InstrumentMiddleware("results_cache", metrics.InstrumentMiddlewareMetrics),
				queryCacheMiddleware,
			)
		}

		if cfg.ShardedQueries {
			queryRangeMiddleware = append(queryRangeMiddleware,
				NewQueryShardMiddleware(
					log,
					schema.Configs,
					engineOpts,
					codec,
					metrics.InstrumentMiddlewareMetrics, // instrumentation is included in the sharding middleware
					metrics.MiddlewareMapperMetrics.shardMapper,
					limits,
					0, // 0 is unlimited shards
					statsHandler,
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
				queryrangebase.InstrumentMiddleware("retry", metrics.InstrumentMiddlewareMetrics),
				queryrangebase.NewRetryMiddleware(log, cfg.MaxRetries, metrics.RetryMiddlewareMetrics),
			)
		}

		// Finally, if the user selected any query range middleware, stitch it in.
		if len(queryRangeMiddleware) > 0 {
			rt := NewLimitedRoundTripper(next, codec, limits, schema.Configs, queryRangeMiddleware...)
			return queryrangebase.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
				if !strings.HasSuffix(r.URL.Path, "/query_range") {
					return next.RoundTrip(r)
				}
				return rt.RoundTrip(r)
			})
		}
		return next
	}, nil
}

// NewInstantMetricTripperware creates a new frontend tripperware responsible for handling metric queries
func NewInstantMetricTripperware(
	cfg Config,
	engineOpts logql.EngineOpts,
	log log.Logger,
	limits Limits,
	schema config.SchemaConfig,
	codec queryrangebase.Codec,
	metrics *Metrics,
	indexStatsTripperware queryrangebase.Tripperware,
) (queryrangebase.Tripperware, error) {
	return func(next http.RoundTripper) http.RoundTripper {
		statsHandler := queryrangebase.NewRoundTripperHandler(indexStatsTripperware(next), codec)

		queryRangeMiddleware := []queryrangebase.Middleware{
			StatsCollectorMiddleware(),
			NewLimitsMiddleware(limits),
			NewQuerySizeLimiterMiddleware(schema.Configs, engineOpts, log, limits, statsHandler),
		}

		if cfg.ShardedQueries {
			queryRangeMiddleware = append(queryRangeMiddleware,
				NewSplitByRangeMiddleware(log, engineOpts, limits, metrics.MiddlewareMapperMetrics.rangeMapper),
				NewQueryShardMiddleware(
					log,
					schema.Configs,
					engineOpts,
					codec,
					metrics.InstrumentMiddlewareMetrics, // instrumentation is included in the sharding middleware
					metrics.MiddlewareMapperMetrics.shardMapper,
					limits,
					0, // 0 is unlimited shards
					statsHandler,
				),
			)
		}

		if cfg.MaxRetries > 0 {
			queryRangeMiddleware = append(
				queryRangeMiddleware,
				queryrangebase.InstrumentMiddleware("retry", metrics.InstrumentMiddlewareMetrics),
				queryrangebase.NewRetryMiddleware(log, cfg.MaxRetries, metrics.RetryMiddlewareMetrics),
			)
		}

		if len(queryRangeMiddleware) > 0 {
			return NewLimitedRoundTripper(next, codec, limits, schema.Configs, queryRangeMiddleware...)
		}
		return next
	}, nil
}

func NewVolumeTripperware(
	cfg Config,
	log log.Logger,
	limits Limits,
	schema config.SchemaConfig,
	codec queryrangebase.Codec,
	c cache.Cache,
	cacheGenNumLoader queryrangebase.CacheGenNumberLoader,
	retentionEnabled bool,
	metrics *Metrics,
) (queryrangebase.Tripperware, error) {
	// Parallelize the volume requests, so it doesn't send a huge request to a single index-gw (i.e. {app=~".+"} for 30d).
	// Indices are sharded by 24 hours, so we split the volume request in 24h intervals.
	limits = WithSplitByLimits(limits, 24*time.Hour)
	var cacheMiddleware queryrangebase.Middleware
	if cfg.CacheVolumeResults {
		var err error
		cacheMiddleware, err = NewVolumeCacheMiddleware(
			log,
			limits,
			codec,
			c,
			cacheGenNumLoader,
			func(_ context.Context, r queryrangebase.Request) bool {
				return !r.GetCachingOptions().Disabled
			},
			func(ctx context.Context, tenantIDs []string, r queryrangebase.Request) int {
				return MinWeightedParallelism(
					ctx,
					tenantIDs,
					schema.Configs,
					limits,
					model.Time(r.GetStart()),
					model.Time(r.GetEnd()),
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
		codec,
		limits,
		log,
		metrics,
		schema,
	)

	if err != nil {
		return nil, err
	}

	return volumeFeatureFlagRoundTripper(
		volumeRangeTripperware(codec, indexTw),
		limits,
	), nil
}

func volumeRangeTripperware(codec queryrangebase.Codec, nextTW queryrangebase.Tripperware) func(next http.RoundTripper) http.RoundTripper {
	return func(next http.RoundTripper) http.RoundTripper {
		nextRT := nextTW(next)

		return queryrangebase.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
			request, err := codec.DecodeRequest(r.Context(), r, nil)
			if err != nil {
				return nil, err
			}

			seriesVolumeMiddlewares := []queryrangebase.Middleware{
				StatsCollectorMiddleware(),
				NewVolumeMiddleware(),
			}

			// wrap nextRT with our new middleware
			response, err := queryrangebase.MergeMiddlewares(
				seriesVolumeMiddlewares...,
			).Wrap(
				VolumeDownstreamHandler(nextRT, codec),
			).Do(r.Context(), request)

			if err != nil {
				return nil, err
			}

			return codec.EncodeResponse(r.Context(), r, response)
		})
	}
}

func volumeFeatureFlagRoundTripper(nextTW queryrangebase.Tripperware, limits Limits) func(next http.RoundTripper) http.RoundTripper {
	return func(next http.RoundTripper) http.RoundTripper {
		nextRt := nextTW(next)
		return queryrangebase.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
			userID, err := user.ExtractOrgID(r.Context())
			if err != nil {
				return nil, err
			}

			if !limits.VolumeEnabled(userID) {
				return nil, httpgrpc.Errorf(http.StatusNotFound, "not found")
			}

			return nextRt.RoundTrip(r)
		})
	}
}

func NewIndexStatsTripperware(
	cfg Config,
	log log.Logger,
	limits Limits,
	schema config.SchemaConfig,
	codec queryrangebase.Codec,
	c cache.Cache,
	cacheGenNumLoader queryrangebase.CacheGenNumberLoader,
	retentionEnabled bool,
	metrics *Metrics,
) (queryrangebase.Tripperware, error) {
	// Parallelize the index stats requests, so it doesn't send a huge request to a single index-gw (i.e. {app=~".+"} for 30d).
	// Indices are sharded by 24 hours, so we split the stats request in 24h intervals.
	limits = WithSplitByLimits(limits, 24*time.Hour)

	var cacheMiddleware queryrangebase.Middleware
	if cfg.CacheIndexStatsResults {
		var err error
		cacheMiddleware, err = NewIndexStatsCacheMiddleware(
			log,
			limits,
			codec,
			c,
			cacheGenNumLoader,
			func(_ context.Context, r queryrangebase.Request) bool {
				return !r.GetCachingOptions().Disabled
			},
			func(ctx context.Context, tenantIDs []string, r queryrangebase.Request) int {
				return MinWeightedParallelism(
					ctx,
					tenantIDs,
					schema.Configs,
					limits,
					model.Time(r.GetStart()),
					model.Time(r.GetEnd()),
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

	return sharedIndexTripperware(
		cacheMiddleware,
		cfg,
		codec,
		limits,
		log,
		metrics,
		schema,
	)
}

func sharedIndexTripperware(
	cacheMiddleware queryrangebase.Middleware,
	cfg Config,
	codec queryrangebase.Codec,
	limits Limits,
	log log.Logger,
	metrics *Metrics,
	schema config.SchemaConfig,
) (queryrangebase.Tripperware, error) {
	return func(next http.RoundTripper) http.RoundTripper {
		middlewares := []queryrangebase.Middleware{
			NewLimitsMiddleware(limits),
			queryrangebase.InstrumentMiddleware("split_by_interval", metrics.InstrumentMiddlewareMetrics),
			SplitByIntervalMiddleware(schema.Configs, limits, codec, splitByTime, metrics.SplitByMetrics),
		}

		if cacheMiddleware != nil {
			middlewares = append(
				middlewares,
				queryrangebase.InstrumentMiddleware("log_results_cache", metrics.InstrumentMiddlewareMetrics),
				cacheMiddleware,
			)
		}

		if cfg.MaxRetries > 0 {
			middlewares = append(
				middlewares,
				queryrangebase.InstrumentMiddleware("retry", metrics.InstrumentMiddlewareMetrics),
				queryrangebase.NewRetryMiddleware(log, cfg.MaxRetries, metrics.RetryMiddlewareMetrics),
			)
		}

		return queryrangebase.NewRoundTripper(next, codec, nil, middlewares...)
	}, nil
}
