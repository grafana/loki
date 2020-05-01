package queryrange

import (
	"flag"
	"net/http"
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

	"github.com/grafana/loki/pkg/loghttp"
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

	metricsTripperware, cache, err := NewMetricTripperware(cfg, log, limits, lokiCodec, PrometheusExtractor{}, instrumentMetrics, retryMetrics)
	if err != nil {
		return nil, nil, err
	}
	logFilterTripperware, err := NewLogFilterTripperware(cfg, log, limits, lokiCodec, instrumentMetrics, retryMetrics)
	if err != nil {
		return nil, nil, err
	}
	return func(next http.RoundTripper) http.RoundTripper {
		metricRT := metricsTripperware(next)
		logFilterRT := logFilterTripperware(next)
		return newRoundTripper(next, logFilterRT, metricRT, limits)
	}, cache, nil
}

type roundTripper struct {
	next, log, metric http.RoundTripper

	limits Limits
}

// newRoundTripper creates a new queryrange roundtripper
func newRoundTripper(next, log, metric http.RoundTripper, limits Limits) roundTripper {
	return roundTripper{
		log:    log,
		limits: limits,
		metric: metric,
		next:   next,
	}
}

func (r roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if !strings.HasSuffix(req.URL.Path, "/query_range") && !strings.HasSuffix(req.URL.Path, "/prom/query") {
		return r.next.RoundTrip(req)
	}
	err := req.ParseForm()
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}
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
		filter, err := transformRegexQuery(req, e).Filter()
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
		}
		if err := validateLimits(req, rangeQuery.Limit, r.limits); err != nil {
			return nil, err
		}
		if filter == nil {
			return r.next.RoundTrip(req)
		}
		return r.log.RoundTrip(req)

	default:
		return r.next.RoundTrip(req)
	}
}

// transformRegexQuery backport the old regexp params into the v1 query format
func transformRegexQuery(req *http.Request, expr logql.LogSelectorExpr) logql.LogSelectorExpr {
	regexp := req.Form.Get("regexp")
	if regexp != "" {
		expr = logql.NewFilterExpr(expr, labels.MatchRegexp, regexp)
		params := req.URL.Query()
		params.Set("query", expr.String())
		req.URL.RawQuery = params.Encode()
		// force the form and query to be parsed again.
		req.Form = nil
		req.PostForm = nil
	}
	return expr
}

// validates log entries limits
func validateLimits(req *http.Request, reqLimit uint32, limits Limits) error {
	userID, err := user.ExtractOrgID(req.Context())
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

// NewLogFilterTripperware creates a new frontend tripperware responsible for handling log requests with regex.
func NewLogFilterTripperware(
	cfg Config,
	log log.Logger,
	limits Limits,
	codec queryrange.Codec,
	instrumentMetrics *queryrange.InstrumentMiddlewareMetrics,
	retryMiddlewareMetrics *queryrange.RetryMiddlewareMetrics,
) (frontend.Tripperware, error) {
	queryRangeMiddleware := []queryrange.Middleware{StatsCollectorMiddleware(), queryrange.LimitsMiddleware(limits)}
	if cfg.SplitQueriesByInterval != 0 {
		queryRangeMiddleware = append(queryRangeMiddleware, queryrange.InstrumentMiddleware("split_by_interval", instrumentMetrics), SplitByIntervalMiddleware(limits, codec))
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
	codec queryrange.Codec,
	extractor queryrange.Extractor,
	instrumentMetrics *queryrange.InstrumentMiddlewareMetrics,
	retryMiddlewareMetrics *queryrange.RetryMiddlewareMetrics,
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
			nil,
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
