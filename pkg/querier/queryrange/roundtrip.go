package queryrange

import (
	"errors"
	"flag"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/cache"
	"github.com/cortexproject/cortex/pkg/querier/frontend"
	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/logql"
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
) (frontend.Tripperware, Stopper, error) {
	// Ensure that QuerySplitDuration uses configuration defaults.
	// This avoids divide by zero errors when determining cache keys where user specific overrides don't exist.
	limits = WithDefaultLimits(limits, cfg.Config)

	instrumentMetrics := queryrange.NewInstrumentMiddlewareMetrics(registerer)
	retryMetrics := queryrange.NewRetryMiddlewareMetrics(registerer)

	metricsTripperware, cache, err := NewMetricTripperware(cfg, log, limits, schema, minShardingLookback, lokiCodec, prometheusResponseExtractor, instrumentMetrics, retryMetrics, registerer)
	if err != nil {
		return nil, nil, err
	}
	logFilterTripperware, err := NewLogFilterTripperware(cfg, log, limits, schema, minShardingLookback, lokiCodec, instrumentMetrics, retryMetrics, registerer)
	if err != nil {
		return nil, nil, err
	}

	return func(next http.RoundTripper) http.RoundTripper {
		metricRT := metricsTripperware(next)
		logFilterRT := logFilterTripperware(next)
		return frontend.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if !strings.HasSuffix(req.URL.Path, "/query_range") && !strings.HasSuffix(req.URL.Path, "/prom/query") {
				return next.RoundTrip(req)
			}
			params := req.URL.Query()
			query := params.Get("query")
			expr, err := logql.ParseExpr(query)
			if err != nil {
				// weavework server uses httpgrpc errors for status code.
				return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
			}

			if _, ok := expr.(logql.SampleExpr); ok {
				return metricRT.RoundTrip(req)
			}
			if logSelector, ok := expr.(logql.LogSelectorExpr); ok {
				if err := validateLimits(req, params, limits); err != nil {
					return nil, err
				}

				// backport the old regexp params into the query params
				regexp := params.Get("regexp")
				if regexp != "" {
					logSelector = logql.NewFilterExpr(logSelector, labels.MatchRegexp, regexp)
					params.Set("query", logSelector.String())
					req.URL.RawQuery = params.Encode()
				}
				filter, err := logSelector.Filter()
				if err != nil {
					return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
				}
				if filter != nil {
					return logFilterRT.RoundTrip(req)
				}
			}
			return next.RoundTrip(req)
		})
	}, cache, nil
}

// validates log entries limits
func validateLimits(req *http.Request, params url.Values, limits Limits) error {
	userID, err := user.ExtractOrgID(req.Context())
	if err != nil {
		return httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	reqLimit, err := strconv.Atoi(params.Get("limit"))
	if err != nil {
		return httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	maxEntriesLimit := limits.MaxEntriesLimitPerQuery(userID)
	if reqLimit > maxEntriesLimit && maxEntriesLimit != 0 {
		return httpgrpc.Errorf(http.StatusBadRequest,
			"max entries limit per query exceeded, limit > max_entries_limit (%d > %d)", reqLimit, maxEntriesLimit)
	}
	return nil
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
	registerer prometheus.Registerer,
) (frontend.Tripperware, error) {
	queryRangeMiddleware := []queryrange.Middleware{StatsCollectorMiddleware(), queryrange.LimitsMiddleware(limits)}
	if cfg.SplitQueriesByInterval != 0 {
		queryRangeMiddleware = append(queryRangeMiddleware, queryrange.InstrumentMiddleware("split_by_interval", instrumentMetrics), SplitByIntervalMiddleware(limits, codec))
	}

	if cfg.ShardedQueries {
		if minShardingLookback == 0 {
			return nil, errors.New("a non-zero value is required for querier.query-ingesters-within when -querier.parallelise-shardable-queries is enabled")
		}
		queryRangeMiddleware = append(queryRangeMiddleware,
			NewQueryShardMiddleware(
				log,
				schema.Configs,
				minShardingLookback,
				instrumentMetrics, // instrumentation is included in the sharding middleware
				registerer,
			),
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
	registerer prometheus.Registerer,
) (frontend.Tripperware, Stopper, error) {
	queryRangeMiddleware := []queryrange.Middleware{StatsCollectorMiddleware(), queryrange.LimitsMiddleware(limits)}
	if cfg.AlignQueriesWithStep {
		queryRangeMiddleware = append(
			queryRangeMiddleware,
			queryrange.InstrumentMiddleware("step_align", instrumentMetrics),
			queryrange.StepAlignMiddleware,
		)
	}

	// SplitQueriesByDay is deprecated use SplitQueriesByInterval.
	if cfg.SplitQueriesByDay {
		level.Warn(log).Log("msg", "flag querier.split-queries-by-day (or config split_queries_by_day) is deprecated, use querier.split-queries-by-interval instead.")
	}

	queryRangeMiddleware = append(
		queryRangeMiddleware,
		queryrange.InstrumentMiddleware("split_by_interval", instrumentMetrics),
		SplitByIntervalMiddleware(limits, codec),
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
				minShardingLookback,
				instrumentMetrics, // instrumentation is included in the sharding middleware
				registerer,
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
			rt := queryrange.NewRoundTripper(next, codec, queryRangeMiddleware...)
			return frontend.RoundTripFunc(func(r *http.Request) (*http.Response, error) {
				if !strings.HasSuffix(r.URL.Path, "/query_range") {
					return next.RoundTrip(r)
				}
				return rt.RoundTrip(r)
			})
		}
		return next
	}, c, nil
}
