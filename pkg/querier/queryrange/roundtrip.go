package queryrange

import (
	"flag"
	"net/http"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/util/validation"
)

// Config is the configuration for the queryrange tripperware
type Config struct {
	queryrangebase.Config `yaml:",inline"`
}

// RegisterFlags adds the flags required to configure this flag set.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Config.RegisterFlags(f)
}

// Stopper gracefully shutdown resources created
type Stopper interface {
	Stop()
}

// NewTripperware returns a Tripperware configured with middlewares to align, split and cache requests.
func NewTripperware(
	cfg Config,
	log log.Logger,
	limits Limits,
	schema config.SchemaConfig,
	cacheGenNumLoader queryrangebase.CacheGenNumberLoader,
	registerer prometheus.Registerer,
) (queryrangebase.Tripperware, Stopper, error) {
	metrics := NewMetrics(registerer)

	var (
		c   cache.Cache
		err error
	)
	if cfg.CacheResults {
		c, err = cache.New(cfg.CacheConfig, registerer, log)
		if err != nil {
			return nil, nil, err
		}
		if cfg.Compression == "snappy" {
			c = cache.NewSnappy(c, log)
		}
	}

	metricsTripperware, err := NewMetricTripperware(cfg, log, limits, schema, LokiCodec, c,
		cacheGenNumLoader, PrometheusExtractor{}, metrics, registerer)
	if err != nil {
		return nil, nil, err
	}

	// NOTE: When we would start caching response from non-metric queries we would have to consider cache gen headers as well in
	// MergeResponse implementation for Loki codecs same as it is done in Cortex at https://github.com/cortexproject/cortex/blob/21bad57b346c730d684d6d0205efef133422ab28/pkg/querier/queryrange/query_range.go#L170
	logFilterTripperware, err := NewLogFilterTripperware(cfg, log, limits, schema, LokiCodec, c, metrics)
	if err != nil {
		return nil, nil, err
	}

	seriesTripperware, err := NewSeriesTripperware(cfg, log, limits, LokiCodec, metrics, schema)
	if err != nil {
		return nil, nil, err
	}

	labelsTripperware, err := NewLabelsTripperware(cfg, log, limits, LokiCodec, metrics)
	if err != nil {
		return nil, nil, err
	}

	instantMetricTripperware, err := NewInstantMetricTripperware(cfg, log, limits, schema, LokiCodec, metrics)
	if err != nil {
		return nil, nil, err
	}
	return func(next http.RoundTripper) http.RoundTripper {
		metricRT := metricsTripperware(next)
		logFilterRT := logFilterTripperware(next)
		seriesRT := seriesTripperware(next)
		labelsRT := labelsTripperware(next)
		instantRT := instantMetricTripperware(next)
		return newRoundTripper(next, logFilterRT, metricRT, seriesRT, labelsRT, instantRT, limits)
	}, c, nil
}

type roundTripper struct {
	next, log, metric, series, labels, instantMetric http.RoundTripper

	limits Limits
}

// newRoundTripper creates a new queryrange roundtripper
func newRoundTripper(next, log, metric, series, labels, instantMetric http.RoundTripper, limits Limits) roundTripper {
	return roundTripper{
		log:           log,
		limits:        limits,
		metric:        metric,
		series:        series,
		labels:        labels,
		instantMetric: instantMetric,
		next:          next,
	}
}

func (r roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
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
		switch e := expr.(type) {
		case syntax.SampleExpr:
			return r.metric.RoundTrip(req)
		case syntax.LogSelectorExpr:
			expr, err := transformRegexQuery(req, e)
			if err != nil {
				return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
			}
			if err := validateLimits(req, rangeQuery.Limit, r.limits); err != nil {
				return nil, err
			}
			// Only filter expressions are query sharded
			if !expr.HasFilter() {
				return r.next.RoundTrip(req)
			}
			return r.log.RoundTrip(req)

		default:
			return r.next.RoundTrip(req)
		}
	case SeriesOp:
		_, err := loghttp.ParseAndValidateSeriesQuery(req)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}
		return r.series.RoundTrip(req)
	case LabelNamesOp:
		_, err := loghttp.ParseLabelQuery(req)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}
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
		switch expr.(type) {
		case syntax.SampleExpr:
			return r.instantMetric.RoundTrip(req)
		default:
			return r.next.RoundTrip(req)
		}
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

// validates log entries limits
func validateLimits(req *http.Request, reqLimit uint32, limits Limits) error {
	tenantIDs, err := tenant.TenantIDs(req.Context())
	if err != nil {
		return httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	maxEntriesLimit := validation.SmallestPositiveNonZeroIntPerTenant(tenantIDs, limits.MaxEntriesLimitPerQuery)
	if int(reqLimit) > maxEntriesLimit && maxEntriesLimit != 0 {
		return httpgrpc.Errorf(http.StatusBadRequest,
			"max entries limit per query exceeded, limit > max_entries_limit (%d > %d)", reqLimit, maxEntriesLimit)
	}
	return nil
}

const (
	InstantQueryOp = "instant_query"
	QueryRangeOp   = "query_range"
	SeriesOp       = "series"
	LabelNamesOp   = "labels"
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
	default:
		return ""
	}
}

// NewLogFilterTripperware creates a new frontend tripperware responsible for handling log requests with regex.
func NewLogFilterTripperware(
	cfg Config,
	log log.Logger,
	limits Limits,
	schema config.SchemaConfig,
	codec queryrangebase.Codec,
	c cache.Cache,
	metrics *Metrics,
) (queryrangebase.Tripperware, error) {
	queryRangeMiddleware := []queryrangebase.Middleware{
		StatsCollectorMiddleware(),
		NewLimitsMiddleware(limits),
		queryrangebase.InstrumentMiddleware("split_by_interval", metrics.InstrumentMiddlewareMetrics),
		SplitByIntervalMiddleware(limits, codec, splitByTime, metrics.SplitByMetrics),
	}

	if cfg.CacheResults {
		queryCacheMiddleware := NewLogResultCache(
			log,
			limits,
			c,
			func(r queryrangebase.Request) bool {
				return !r.GetCachingOptions().Disabled
			},
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
				metrics.InstrumentMiddlewareMetrics, // instrumentation is included in the sharding middleware
				metrics.MiddlewareMapperMetrics.shardMapper,
				limits,
			),
		)
	}

	if cfg.MaxRetries > 0 {
		queryRangeMiddleware = append(
			queryRangeMiddleware, queryrangebase.InstrumentMiddleware("retry", metrics.InstrumentMiddlewareMetrics),
			queryrangebase.NewRetryMiddleware(log, cfg.MaxRetries, metrics.RetryMiddlewareMetrics),
		)
	}

	return func(next http.RoundTripper) http.RoundTripper {
		if len(queryRangeMiddleware) > 0 {
			return NewLimitedRoundTripper(next, codec, limits, queryRangeMiddleware...)
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
		SplitByIntervalMiddleware(WithSplitByLimits(limits, 24*time.Hour), codec, splitByTime, metrics.SplitByMetrics),
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
			return NewLimitedRoundTripper(next, codec, limits, queryRangeMiddleware...)
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
) (queryrangebase.Tripperware, error) {
	queryRangeMiddleware := []queryrangebase.Middleware{
		StatsCollectorMiddleware(),
		NewLimitsMiddleware(limits),
		queryrangebase.InstrumentMiddleware("split_by_interval", metrics.InstrumentMiddlewareMetrics),
		// Force a 24 hours split by for labels API, this will be more efficient with our static daily bucket storage.
		// This is because the labels API is an index-only operation.
		SplitByIntervalMiddleware(WithSplitByLimits(limits, 24*time.Hour), codec, splitByTime, metrics.SplitByMetrics),
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
	log log.Logger,
	limits Limits,
	schema config.SchemaConfig,
	codec queryrangebase.Codec,
	c cache.Cache,
	cacheGenNumLoader queryrangebase.CacheGenNumberLoader,
	extractor queryrangebase.Extractor,
	metrics *Metrics,
	registerer prometheus.Registerer,
) (queryrangebase.Tripperware, error) {
	queryRangeMiddleware := []queryrangebase.Middleware{StatsCollectorMiddleware(), NewLimitsMiddleware(limits)}
	if cfg.AlignQueriesWithStep {
		queryRangeMiddleware = append(
			queryRangeMiddleware,
			queryrangebase.InstrumentMiddleware("step_align", metrics.InstrumentMiddlewareMetrics),
			queryrangebase.StepAlignMiddleware,
		)
	}

	queryRangeMiddleware = append(
		queryRangeMiddleware,
		queryrangebase.InstrumentMiddleware("split_by_interval", metrics.InstrumentMiddlewareMetrics),
		SplitByIntervalMiddleware(limits, codec, splitMetricByTime, metrics.SplitByMetrics),
	)

	if cfg.CacheResults {
		queryCacheMiddleware, err := queryrangebase.NewResultsCacheMiddleware(
			log,
			c,
			cacheKeyLimits{limits},
			limits,
			codec,
			extractor,
			cacheGenNumLoader,
			func(r queryrangebase.Request) bool {
				return !r.GetCachingOptions().Disabled
			},
			registerer,
		)
		if err != nil {
			return nil, err
		}
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
				metrics.InstrumentMiddlewareMetrics, // instrumentation is included in the sharding middleware
				metrics.MiddlewareMapperMetrics.shardMapper,
				limits,
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

	return func(next http.RoundTripper) http.RoundTripper {
		// Finally, if the user selected any query range middleware, stitch it in.
		if len(queryRangeMiddleware) > 0 {
			rt := NewLimitedRoundTripper(next, codec, limits, queryRangeMiddleware...)
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
	log log.Logger,
	limits Limits,
	schema config.SchemaConfig,
	codec queryrangebase.Codec,
	metrics *Metrics,
) (queryrangebase.Tripperware, error) {
	queryRangeMiddleware := []queryrangebase.Middleware{StatsCollectorMiddleware(), NewLimitsMiddleware(limits)}

	if cfg.ShardedQueries {
		queryRangeMiddleware = append(queryRangeMiddleware,
			NewSplitByRangeMiddleware(log, limits, metrics.MiddlewareMapperMetrics.rangeMapper),
			NewQueryShardMiddleware(
				log,
				schema.Configs,
				metrics.InstrumentMiddlewareMetrics, // instrumentation is included in the sharding middleware
				metrics.MiddlewareMapperMetrics.shardMapper,
				limits,
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

	return func(next http.RoundTripper) http.RoundTripper {
		if len(queryRangeMiddleware) > 0 {
			return NewLimitedRoundTripper(next, codec, limits, queryRangeMiddleware...)
		}
		return next
	}, nil
}
