package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

// MetricCacheKeyGenerator generates cache keys for the Thor (V2) query engine.
// It buckets keys by EngineResultsCacheTimeBucketInterval to allow cache sharing
// across queries that start within the same bucket.
type MetricCacheKeyGenerator struct {
	limits Limits
}

// GenerateCacheKey generates a cache key based on the userID, query, step, and time bucket.
func (s MetricCacheKeyGenerator) GenerateCacheKey(_ context.Context, userID string, r resultscache.Request) string {
	splitMs := s.limits.EngineResultsCacheTimeBucketInterval(userID).Milliseconds() // Guaranteed to be >= 1m
	currentInterval := r.GetStart().UnixMilli() / splitMs
	// step is included to prevent key collisions across step sizes.
	// splitMs is included so the key changes when the interval is reconfigured.
	return fmt.Sprintf("%s:%s:%d:%d:%d", userID, r.GetQuery(), r.GetStep(), currentInterval, splitMs)
}

// NewMetricCacheMiddleware creates a metric results cache middleware for the Thor engine.
// It only caches metric (SampleExpr) queries; log queries pass through untouched.
func NewMetricCacheMiddleware(
	logger log.Logger,
	limits Limits,
	c cache.Cache,
	metrics *queryrangebase.ResultsCacheMetrics,
) (queryrangebase.Middleware, error) {
	return queryrangebase.NewResultsCacheMiddleware(
		logger,
		c,
		MetricCacheKeyGenerator{limits},
		limits,
		queryrange.DefaultCodec,
		queryrange.PrometheusExtractor{},
		// TODO(salvacorts): pass a non-nil cacheGenNumberLoader once delete requests
		// are supported by the Thor engine, to invalidate cached results on delete.
		nil,
		shouldCacheRequest,
		func(ctx context.Context, userIDs []string, _ queryrangebase.Request) int {
			return validation.SmallestPositiveIntPerTenant(userIDs, func(userID string) int {
				return limits.MaxQueryParallelism(ctx, userID)
			})
		},
		false, // retentionEnabled (handled elsewhere)
		false, // onlyUseEntireExtent=false: partial hits are used; missing range is fetched and merged
		metrics,
	)
}

// InstantMetricCacheKeyGenerator generates cache keys for instant metric queries
// in the Thor (V2) query engine. It omits the step from the key because instant
// queries always have step=0.
type InstantMetricCacheKeyGenerator struct {
	limits Limits
}

// GenerateCacheKey generates a cache key based on userID, query, and time bucket.
func (s InstantMetricCacheKeyGenerator) GenerateCacheKey(_ context.Context, userID string, r resultscache.Request) string {
	splitMs := s.limits.EngineResultsCacheTimeBucketInterval(userID).Milliseconds() // Guaranteed to be >= 1m
	currentInterval := r.GetStart().UnixMilli() / splitMs
	// No step in key: instant queries always have step=0.
	// splitMs is included so the key changes when the interval is reconfigured.
	return fmt.Sprintf("instant-metric:%s:%s:%d:%d", userID, r.GetQuery(), currentInterval, splitMs)
}

// NewInstantMetricCacheMiddleware creates an instant metric results cache middleware
// for the Thor engine. It caches instant metric (SampleExpr) queries.
func NewInstantMetricCacheMiddleware(
	logger log.Logger,
	limits Limits,
	c cache.Cache,
	metrics *queryrangebase.ResultsCacheMetrics,
) (queryrangebase.Middleware, error) {
	return queryrangebase.NewResultsCacheMiddleware(
		logger,
		c,
		InstantMetricCacheKeyGenerator{limits},
		limits,
		queryrange.DefaultCodec,
		queryrange.PrometheusExtractor{},
		// TODO(salvacorts): pass a non-nil cacheGenNumberLoader once delete requests
		// are supported by the Thor engine, to invalidate cached results on delete.
		nil,
		shouldCacheRequest,
		func(ctx context.Context, userIDs []string, _ queryrangebase.Request) int {
			return validation.SmallestPositiveIntPerTenant(userIDs, func(userID string) int {
				return limits.MaxQueryParallelism(ctx, userID)
			})
		},
		false, // retentionEnabled (handled elsewhere)
		false, // onlyUseEntireExtent=false: partial hits are used; missing range is fetched and merged
		metrics,
	)
}

// shouldCacheRequest returns true when caching is not disabled for the request.
func shouldCacheRequest(_ context.Context, r queryrangebase.Request) bool {
	return !r.GetCachingOptions().Disabled
}

// NewResultsCacheMetrics creates metrics for the engine results cache middleware.
func NewResultsCacheMetrics(reg prometheus.Registerer) *queryrangebase.ResultsCacheMetrics {
	return queryrangebase.NewResultsCacheMetrics(reg)
}

// logCacheLimits is the subset of Limits actually used by the engine log result cache.
// It is intentionally smaller than engine.Limits to make the dependency explicit.
type logCacheLimits interface {
	MaxCacheFreshness(context.Context, string) time.Duration
	EngineResultsCacheTimeBucketInterval(string) time.Duration
}

// NewLogResultCacheMetrics creates metrics for the engine log result cache using
// engine-specific Prometheus metric names.
func NewLogResultCacheMetrics(registerer prometheus.Registerer) *queryrange.LogResultCacheMetrics {
	return &queryrange.LogResultCacheMetrics{
		CacheHit: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "engine_log_result_cache_hit_total",
		}),
		CacheMiss: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "engine_log_result_cache_miss_total",
		}),
	}
}

// LogCacheKeyGenerator implements queryrange.LogCacheKeyGenerator using
// EngineResultsCacheTimeBucketInterval instead of QuerySplitDuration.
type LogCacheKeyGenerator struct {
	limits logCacheLimits
}

func (g *LogCacheKeyGenerator) GenerateCacheKey(_ context.Context, tenantIDs []string, req *queryrange.LokiRequest) string {
	// Skip caching if the limit is 0 (limit=0 would register an empty result even if the time range contains log lines).
	if req.Limit == 0 {
		return ""
	}

	interval := validation.SmallestPositiveNonZeroDurationPerTenant(tenantIDs, g.limits.EngineResultsCacheTimeBucketInterval)
	splitMs := interval.Milliseconds()
	currentInterval := req.GetStartTs().UnixMilli() / splitMs
	// splitMs is included so the key changes when the interval is reconfigured.
	return fmt.Sprintf("log:%s:%s:%d:%d", tenant.JoinTenantIDs(tenantIDs), req.GetQuery(), currentInterval, splitMs)
}

// NewLogResultCache creates a log result cache middleware for the Thor engine.
// It only caches empty results for log range queries; metric queries pass through to
// the metric cache middleware sitting above this one in the chain.
func NewLogResultCache(
	logger log.Logger,
	limits logCacheLimits,
	c cache.Cache,
	metrics *queryrange.LogResultCacheMetrics,
) (queryrangebase.Middleware, error) {
	return queryrange.NewLogResultCache(
		logger,
		limits, // satisfies queryrange.LogCacheLimits (has MaxCacheFreshness)
		c,
		shouldCacheRequest,
		&LogCacheKeyGenerator{limits: limits},
		metrics,
	)
}

// cacheMiddleware is a single middleware that routes requests to the
// metric, instant-metric, or log cache based on query type.
type cacheMiddleware struct {
	metrics       queryrangebase.Middleware
	instantMetric queryrangebase.Middleware
	logs          queryrangebase.Middleware
}

func (m *cacheMiddleware) Wrap(next queryrangebase.Handler) queryrangebase.Handler {
	return &cacheHandler{
		metricHandler:        m.metrics.Wrap(next),
		instantMetricHandler: m.instantMetric.Wrap(next),
		logHandler:           m.logs.Wrap(next),
		next:                 next,
	}
}

type cacheHandler struct {
	metricHandler        queryrangebase.Handler
	instantMetricHandler queryrangebase.Handler
	logHandler           queryrangebase.Handler
	next                 queryrangebase.Handler
}

func (h *cacheHandler) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	expr, err := syntax.ParseExpr(req.GetQuery())
	if err != nil {
		return h.next.Do(ctx, req)
	}
	if isMetricQuery(expr) {
		if req.GetStep() == 0 { // instant metric query
			return h.instantMetricHandler.Do(ctx, req)
		}
		return h.metricHandler.Do(ctx, req)
	}
	return h.logHandler.Do(ctx, req)
}

// NewCacheMiddleware returns a single middleware that routes requests to one of
// three separate cache backends: metric range queries, instant metric queries,
// and log (non-metric) range queries.
func NewCacheMiddleware(
	logger log.Logger,
	limits Limits,
	metricCache cache.Cache,
	instantMetricCache cache.Cache,
	logCache cache.Cache,
	reg prometheus.Registerer,
) (queryrangebase.Middleware, error) {
	// Shared metrics to avoid duplicate Prometheus counter registration.
	rcMetrics := NewResultsCacheMetrics(reg)

	metricsMiddleware, err := NewMetricCacheMiddleware(logger, limits, metricCache, rcMetrics)
	if err != nil {
		return nil, err
	}
	instantMetricsMiddleware, err := NewInstantMetricCacheMiddleware(logger, limits, instantMetricCache, rcMetrics)
	if err != nil {
		return nil, err
	}
	logsMiddleware, err := NewLogResultCache(logger, limits, logCache, NewLogResultCacheMetrics(reg))
	if err != nil {
		return nil, err
	}
	return &cacheMiddleware{
		metrics:       metricsMiddleware,
		instantMetric: instantMetricsMiddleware,
		logs:          logsMiddleware,
	}, nil
}
