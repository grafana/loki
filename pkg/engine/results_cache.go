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
	split := s.limits.EngineResultsCacheTimeBucketInterval(userID) // Guaranteed to be >= 1m
	var currentInterval int64
	ms := int64(split / time.Millisecond)
	currentInterval = r.GetStart().UnixMilli() / ms
	// step=0 for instant queries; included to prevent key collisions across step sizes.
	// ms is included so that key changes when the interval is reconfigured at runtime.
	return fmt.Sprintf("%s:%s:%d:%d:%d", userID, r.GetQuery(), r.GetStep(), currentInterval, ms)
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
	intervalCapture := func(id string) time.Duration { return g.limits.EngineResultsCacheTimeBucketInterval(id) }
	interval := validation.SmallestPositiveNonZeroDurationPerTenant(tenantIDs, intervalCapture)
	// Skip caching if interval is unset or limit is 0 (limit=0 would register an
	// empty result even if the time range contains log lines).
	if interval == 0 || req.Limit == 0 {
		return ""
	}
	alignedStart := time.Unix(0, req.GetStartTs().UnixNano()-(req.GetStartTs().UnixNano()%interval.Nanoseconds()))
	return fmt.Sprintf("log:%s:%s:%d:%d",
		tenant.JoinTenantIDs(tenantIDs),
		req.GetQuery(),
		interval.Nanoseconds(),
		alignedStart.UnixNano()/interval.Nanoseconds(),
	)
	// Note: no pipeline-disabled prefix — separate cache backend; engine does not use that header.
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
// metric or log cache based on query type. Both share the same cache backend.
type cacheMiddleware struct {
	metrics queryrangebase.Middleware
	logs    queryrangebase.Middleware
}

func (m *cacheMiddleware) Wrap(next queryrangebase.Handler) queryrangebase.Handler {
	return &cacheHandler{
		metricHandler: m.metrics.Wrap(next),
		logHandler:    m.logs.Wrap(next),
		next:          next,
	}
}

type cacheHandler struct {
	metricHandler queryrangebase.Handler
	logHandler    queryrangebase.Handler
	next          queryrangebase.Handler
}

func (h *cacheHandler) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	expr, err := syntax.ParseExpr(req.GetQuery())
	if err != nil {
		return h.next.Do(ctx, req)
	}
	if isMetricQuery(expr) {
		return h.metricHandler.Do(ctx, req)
	}
	return h.logHandler.Do(ctx, req)
}

// NewCacheMiddleware returns a single middleware that caches metric queries
// via the Prometheus results cache and log (non-metric) range queries via the
// empty-result log cache. Both use the same cache backend c.
func NewCacheMiddleware(
	logger log.Logger,
	limits Limits,
	c cache.Cache,
	reg prometheus.Registerer,
) (queryrangebase.Middleware, error) {
	metricsMiddleware, err := NewMetricCacheMiddleware(logger, limits, c, NewResultsCacheMetrics(reg))
	if err != nil {
		return nil, err
	}
	logsMiddleware, err := NewLogResultCache(logger, limits, c, NewLogResultCacheMetrics(reg))
	if err != nil {
		return nil, err
	}
	return &cacheMiddleware{metrics: metricsMiddleware, logs: logsMiddleware}, nil
}
