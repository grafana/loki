package queryrange

import (
	"flag"
	"net/http"
	"net/url"
	"strconv"
	"strings"

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
func NewTripperware(cfg Config, log log.Logger, limits Limits, registerer prometheus.Registerer) (frontend.Tripperware, Stopper, error) {
	// Ensure that QuerySplitDuration uses configuration defaults.
	// This avoids divide by zero errors when determining cache keys where user specific overrides don't exist.
	limits = WithDefaultLimits(limits, cfg.Config)

	instrumentMetrics := queryrange.NewInstrumentMiddlewareMetrics(registerer)
	retryMetrics := queryrange.NewRetryMiddlewareMetrics(registerer)

	metricsWare := []queryrange.Middleware{StatsCollectorMiddleware(), queryrange.LimitsMiddleware(limits)}
	logFilterWare := []queryrange.Middleware{StatsCollectorMiddleware(), queryrange.LimitsMiddleware(limits)}

	// ----Alignment----

	if cfg.AlignQueriesWithStep {
		metricsWare = append(
			metricsWare,
			queryrange.InstrumentMiddleware("step_align", instrumentMetrics),
			queryrange.StepAlignMiddleware,
		)
	}

	// ----Splitting----

	// SplitQueriesByDay is deprecated use SplitQueriesByInterval.
	if cfg.SplitQueriesByDay {
		level.Warn(log).Log("msg", "flag querier.split-queries-by-day (or config split_queries_by_day) is deprecated, use querier.split-queries-by-interval instead.")
	}

	if cfg.SplitQueriesByInterval != 0 {

		splitWare := queryrange.MergeMiddlewares(
			queryrange.InstrumentMiddleware("split_by_interval", instrumentMetrics),
			SplitByIntervalMiddleware(limits, lokiCodec),
		)

		metricsWare = append(metricsWare, splitWare)
		logFilterWare = append(logFilterWare, splitWare)
	}

	// ----Caching----

	var c cache.Cache
	if cfg.CacheResults {
		queryCacheMiddleware, cache, err := queryrange.NewResultsCacheMiddleware(
			log,
			cfg.ResultsCacheConfig,
			cacheKeyLimits{limits},
			limits,
			lokiCodec,
			prometheusResponseExtractor,
		)
		if err != nil {
			return nil, nil, err
		}
		c = cache
		metricsWare = append(
			metricsWare,
			queryrange.InstrumentMiddleware("results_cache", instrumentMetrics),
			queryCacheMiddleware,
		)
	}

	// ----Sharding----

	// ----Retries----

	if cfg.MaxRetries > 0 {
		retryWare := queryrange.MergeMiddlewares(
			queryrange.InstrumentMiddleware("retry", instrumentMetrics),
			queryrange.NewRetryMiddleware(log, cfg.MaxRetries, retryMetrics),
		)
		logFilterWare = append(logFilterWare, retryWare)
		metricsWare = append(metricsWare, retryWare)
	}

	return func(next http.RoundTripper) http.RoundTripper {
		metricRT := queryrange.NewRoundTripper(next, lokiCodec, metricsWare...)
		logFilterRT := queryrange.NewRoundTripper(next, lokiCodec, logFilterWare...)
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
	}, c, nil
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
