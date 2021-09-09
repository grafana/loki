package queryrange

import (
	"errors"
	"flag"
	"net/http"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/go-kit/kit/log"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
)

// Config is the configuration for the queryrange tripperware
type Config struct {
	queryrange.Config `yaml:",inline"`
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
	schema chunk.SchemaConfig,
	minShardingLookback time.Duration,
	registerer prometheus.Registerer,
) (queryrange.Tripperware, Stopper, error) {
	// Ensure that QuerySplitDuration uses configuration defaults.
	// This avoids divide by zero errors when determining cache keys where user specific overrides don't exist.
	limits = WithDefaultLimits(limits, cfg.Config)

	instrumentMetrics := queryrange.NewInstrumentMiddlewareMetrics(registerer)
	retryMetrics := queryrange.NewRetryMiddlewareMetrics(registerer)
	shardingMetrics := logql.NewShardingMetrics(registerer)
	splitByMetrics := NewSplitByMetrics(registerer)

	metricsTripperware, cache, err := NewMetricTripperware(cfg, log, limits, schema, minShardingLookback, LokiCodec,
		PrometheusExtractor{}, instrumentMetrics, retryMetrics, shardingMetrics, splitByMetrics, registerer)
	if err != nil {
		return nil, nil, err
	}

	// NOTE: When we would start caching response from non-metric queries we would have to consider cache gen headers as well in
	// MergeResponse implementation for Loki codecs same as it is done in Cortex at https://github.com/cortexproject/cortex/blob/21bad57b346c730d684d6d0205efef133422ab28/pkg/querier/queryrange/query_range.go#L170
	logFilterTripperware, err := NewLogFilterTripperware(cfg, log, limits, schema, minShardingLookback, LokiCodec, instrumentMetrics, retryMetrics, shardingMetrics, splitByMetrics)
	if err != nil {
		return nil, nil, err
	}

	seriesTripperware, err := NewSeriesTripperware(cfg, log, limits, LokiCodec, instrumentMetrics, retryMetrics, splitByMetrics, shardingMetrics, schema)
	if err != nil {
		return nil, nil, err
	}

	labelsTripperware, err := NewLabelsTripperware(cfg, log, limits, LokiCodec, instrumentMetrics, retryMetrics, splitByMetrics)
	if err != nil {
		return nil, nil, err
	}

	instantMetricTripperware, err := NewInstantMetricTripperware(cfg, log, limits, schema, LokiCodec, instrumentMetrics, retryMetrics, shardingMetrics, splitByMetrics)
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
	}, cache, nil
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
		expr, err := logql.ParseExpr(rangeQuery.Query)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}
		switch e := expr.(type) {
		case logql.SampleExpr:
			return r.metric.RoundTrip(req)
		case logql.LogSelectorExpr:
			expr, err := transformRegexQuery(req, e)
			if err != nil {
				return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
			}
			if err := validateLimits(req, rangeQuery.Limit, r.limits); err != nil {
				return nil, err
			}
			if !expr.HasFilter() {
				return r.next.RoundTrip(req)
			}
			return r.log.RoundTrip(req)

		default:
			return r.next.RoundTrip(req)
		}
	case SeriesOp:
		_, err := logql.ParseAndValidateSeriesQuery(req)
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
		expr, err := logql.ParseExpr(instantQuery.Query)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}
		switch expr.(type) {
		case logql.SampleExpr:
			return r.instantMetric.RoundTrip(req)
		default:
			return r.next.RoundTrip(req)
		}
	default:
		return r.next.RoundTrip(req)
	}
}

// transformRegexQuery backport the old regexp params into the v1 query format
func transformRegexQuery(req *http.Request, expr logql.LogSelectorExpr) (logql.LogSelectorExpr, error) {
	regexp := req.Form.Get("regexp")
	if regexp != "" {
		filterExpr, err := logql.AddFilterExpr(expr, labels.MatchRegexp, "", regexp)
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
	userID, err := tenant.ID(req.Context())
	if err != nil {
		return httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	maxEntriesLimit := limits.MaxEntriesLimitPerQuery(userID)
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
	case strings.HasSuffix(path, "/labels") || strings.HasSuffix(path, "/label"):
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
	schema chunk.SchemaConfig,
	minShardingLookback time.Duration,
	codec queryrange.Codec,
	instrumentMetrics *queryrange.InstrumentMiddlewareMetrics,
	retryMiddlewareMetrics *queryrange.RetryMiddlewareMetrics,
	shardingMetrics *logql.ShardingMetrics,
	splitByMetrics *SplitByMetrics,
) (queryrange.Tripperware, error) {
	queryRangeMiddleware := []queryrange.Middleware{StatsCollectorMiddleware(), queryrange.NewLimitsMiddleware(limits)}
	if cfg.SplitQueriesByInterval != 0 {
		queryRangeMiddleware = append(queryRangeMiddleware, queryrange.InstrumentMiddleware("split_by_interval", instrumentMetrics), SplitByIntervalMiddleware(limits, codec, splitByTime, splitByMetrics))
	}

	if cfg.ShardedQueries {
		if minShardingLookback == 0 {
			return nil, errors.New("a non-zero value is required for querier.query-ingesters-within when -querier.parallelise-shardable-queries is enabled")
		}
		queryRangeMiddleware = append(queryRangeMiddleware,
			NewQueryShardMiddleware(
				log,
				schema.Configs,
				instrumentMetrics, // instrumentation is included in the sharding middleware
				shardingMetrics,
				limits,
			),
		)
	}

	if cfg.MaxRetries > 0 {
		queryRangeMiddleware = append(queryRangeMiddleware, queryrange.InstrumentMiddleware("retry", instrumentMetrics), queryrange.NewRetryMiddleware(log, cfg.MaxRetries, retryMiddlewareMetrics))
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
	codec queryrange.Codec,
	instrumentMetrics *queryrange.InstrumentMiddlewareMetrics,
	retryMiddlewareMetrics *queryrange.RetryMiddlewareMetrics,
	splitByMetrics *SplitByMetrics,
	shardingMetrics *logql.ShardingMetrics,
	schema chunk.SchemaConfig,
) (queryrange.Tripperware, error) {
	queryRangeMiddleware := []queryrange.Middleware{}
	if cfg.SplitQueriesByInterval != 0 {
		queryRangeMiddleware = append(queryRangeMiddleware,
			queryrange.InstrumentMiddleware("split_by_interval", instrumentMetrics),
			// The Series API needs to pull one chunk per series to extract the label set, which is much cheaper than iterating through all matching chunks.
			// Force a 24 hours split by for series API, this will be more efficient with our static daily bucket storage.
			// This would avoid queriers downloading chunks for same series over and over again for serving smaller queries.
			SplitByIntervalMiddleware(WithSplitByLimits(limits, 24*time.Hour), codec, splitByTime, splitByMetrics),
		)
	}
	if cfg.MaxRetries > 0 {
		queryRangeMiddleware = append(queryRangeMiddleware, queryrange.InstrumentMiddleware("retry", instrumentMetrics), queryrange.NewRetryMiddleware(log, cfg.MaxRetries, retryMiddlewareMetrics))
	}

	if cfg.ShardedQueries {
		queryRangeMiddleware = append(queryRangeMiddleware,
			NewSeriesQueryShardMiddleware(
				log,
				schema.Configs,
				instrumentMetrics,
				shardingMetrics,
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
	codec queryrange.Codec,
	instrumentMetrics *queryrange.InstrumentMiddlewareMetrics,
	retryMiddlewareMetrics *queryrange.RetryMiddlewareMetrics,
	splitByMetrics *SplitByMetrics,
) (queryrange.Tripperware, error) {
	queryRangeMiddleware := []queryrange.Middleware{}
	if cfg.SplitQueriesByInterval != 0 {
		queryRangeMiddleware = append(queryRangeMiddleware,
			queryrange.InstrumentMiddleware("split_by_interval", instrumentMetrics),
			// Force a 24 hours split by for labels API, this will be more efficient with our static daily bucket storage.
			// This is because the labels API is an index-only operation.
			SplitByIntervalMiddleware(WithSplitByLimits(limits, 24*time.Hour), codec, splitByTime, splitByMetrics),
		)
	}
	if cfg.MaxRetries > 0 {
		queryRangeMiddleware = append(queryRangeMiddleware, queryrange.InstrumentMiddleware("retry", instrumentMetrics), queryrange.NewRetryMiddleware(log, cfg.MaxRetries, retryMiddlewareMetrics))
	}

	return func(next http.RoundTripper) http.RoundTripper {
		if len(queryRangeMiddleware) > 0 {
			return queryrange.NewRoundTripper(next, codec, queryRangeMiddleware...)
		}
		return next
	}, nil
}

// NewMetricTripperware creates a new frontend tripperware responsible for handling metric queries
func NewMetricTripperware(
	cfg Config,
	log log.Logger,
	limits Limits,
	schema chunk.SchemaConfig,
	minShardingLookback time.Duration,
	codec queryrange.Codec,
	extractor queryrange.Extractor,
	instrumentMetrics *queryrange.InstrumentMiddlewareMetrics,
	retryMiddlewareMetrics *queryrange.RetryMiddlewareMetrics,
	shardingMetrics *logql.ShardingMetrics,
	splitByMetrics *SplitByMetrics,
	registerer prometheus.Registerer,
) (queryrange.Tripperware, Stopper, error) {
	queryRangeMiddleware := []queryrange.Middleware{StatsCollectorMiddleware(), queryrange.NewLimitsMiddleware(limits)}
	if cfg.AlignQueriesWithStep {
		queryRangeMiddleware = append(
			queryRangeMiddleware,
			queryrange.InstrumentMiddleware("step_align", instrumentMetrics),
			queryrange.StepAlignMiddleware,
		)
	}

	queryRangeMiddleware = append(
		queryRangeMiddleware,
		queryrange.InstrumentMiddleware("split_by_interval", instrumentMetrics),
		SplitByIntervalMiddleware(limits, codec, splitMetricByTime, splitByMetrics),
	)

	var c cache.Cache
	if cfg.CacheResults {
		queryCacheMiddleware, cache, err := queryrange.NewResultsCacheMiddleware(
			log,
			cfg.ResultsCacheConfig,
			cacheKeyLimits{limits},
			limits,
			codec,
			extractor,
			nil,
			func(r queryrange.Request) bool {
				return !r.GetCachingOptions().Disabled
			},
			registerer,
		)
		if err != nil {
			return nil, nil, err
		}
		c = cache
		queryRangeMiddleware = append(
			queryRangeMiddleware,
			queryrange.InstrumentMiddleware("results_cache", instrumentMetrics),
			queryCacheMiddleware,
		)
	}

	if cfg.ShardedQueries {
		if minShardingLookback == 0 {
			return nil, nil, errors.New("a non-zero value is required for querier.query-ingesters-within when -querier.parallelise-shardable-queries is enabled")
		}
		queryRangeMiddleware = append(queryRangeMiddleware,
			NewQueryShardMiddleware(
				log,
				schema.Configs,
				instrumentMetrics, // instrumentation is included in the sharding middleware
				shardingMetrics,
				limits,
			),
		)
	}

	if cfg.MaxRetries > 0 {
		queryRangeMiddleware = append(
			queryRangeMiddleware,
			queryrange.InstrumentMiddleware("retry", instrumentMetrics),
			queryrange.NewRetryMiddleware(log, cfg.MaxRetries, retryMiddlewareMetrics),
		)
	}

	return func(next http.RoundTripper) http.RoundTripper {
		// Finally, if the user selected any query range middleware, stitch it in.
		if len(queryRangeMiddleware) > 0 {
			rt := NewLimitedRoundTripper(next, codec, limits, queryRangeMiddleware...)
			return queryrange.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
				if !strings.HasSuffix(r.URL.Path, "/query_range") {
					return next.RoundTrip(r)
				}
				return rt.RoundTrip(r)
			})
		}
		return next
	}, c, nil
}

// NewInstantMetricTripperware creates a new frontend tripperware responsible for handling metric queries
func NewInstantMetricTripperware(
	cfg Config,
	log log.Logger,
	limits Limits,
	schema chunk.SchemaConfig,
	codec queryrange.Codec,
	instrumentMetrics *queryrange.InstrumentMiddlewareMetrics,
	retryMiddlewareMetrics *queryrange.RetryMiddlewareMetrics,
	shardingMetrics *logql.ShardingMetrics,
	splitByMetrics *SplitByMetrics,
) (queryrange.Tripperware, error) {
	queryRangeMiddleware := []queryrange.Middleware{StatsCollectorMiddleware(), queryrange.NewLimitsMiddleware(limits)}

	if cfg.ShardedQueries {
		queryRangeMiddleware = append(queryRangeMiddleware,
			NewQueryShardMiddleware(
				log,
				schema.Configs,
				instrumentMetrics, // instrumentation is included in the sharding middleware
				shardingMetrics,
				limits,
			),
		)
	}

	if cfg.MaxRetries > 0 {
		queryRangeMiddleware = append(
			queryRangeMiddleware,
			queryrange.InstrumentMiddleware("retry", instrumentMetrics),
			queryrange.NewRetryMiddleware(log, cfg.MaxRetries, retryMiddlewareMetrics),
		)
	}

	return func(next http.RoundTripper) http.RoundTripper {
		if len(queryRangeMiddleware) > 0 {
			return NewLimitedRoundTripper(next, codec, limits, queryRangeMiddleware...)
		}
		return next
	}, nil
}
