package queryrange

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

type IndexStatsSplitter struct {
	cacheKeyLimits
}

// GenerateCacheKey generates a cache key based on the userID, Request and interval.
func (i IndexStatsSplitter) GenerateCacheKey(ctx context.Context, userID string, r resultscache.Request) string {
	cacheKey := i.cacheKeyLimits.GenerateCacheKey(ctx, userID, r)
	return fmt.Sprintf("indexStats:%s", cacheKey)
}

type IndexStatsExtractor struct{}

// Extract favors the ability to cache over exactness of results. It assumes a constant distribution
// of log volumes over a range and will extract subsets proportionally.
func (p IndexStatsExtractor) Extract(start, end int64, res resultscache.Response, resStart, resEnd int64) resultscache.Response {
	factor := util.GetFactorOfTime(start, end, resStart, resEnd)

	statsRes := res.(*IndexStatsResponse)
	return &IndexStatsResponse{
		Response: &logproto.IndexStatsResponse{
			Streams: statsRes.Response.GetStreams(),
			Chunks:  statsRes.Response.GetChunks(),
			Bytes:   uint64(float64(statsRes.Response.GetBytes()) * factor),
			Entries: uint64(float64(statsRes.Response.GetEntries()) * factor),
		},
	}
}

func (p IndexStatsExtractor) ResponseWithoutHeaders(resp queryrangebase.Response) queryrangebase.Response {
	statsRes := resp.(*IndexStatsResponse)
	return &IndexStatsResponse{
		Response: statsRes.Response,
	}
}

type IndexStatsCacheConfig struct {
	queryrangebase.ResultsCacheConfig `yaml:",inline"`
}

// RegisterFlags registers flags.
func (cfg *IndexStatsCacheConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.ResultsCacheConfig.RegisterFlagsWithPrefix(f, "frontend.index-stats-results-cache.")
}

func (cfg *IndexStatsCacheConfig) Validate() error {
	return cfg.ResultsCacheConfig.Validate()
}

// statsCacheMiddlewareNowTimeFunc is a function that returns the current time.
// It is used to allow tests to override the current time.
var statsCacheMiddlewareNowTimeFunc = model.Now

// shouldCacheStats returns true if the request should be cached.
// It returns false if:
// - The request end time falls within the max_stats_cache_freshness duration.
func shouldCacheStats(ctx context.Context, req queryrangebase.Request, lim Limits) (bool, error) {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return false, err
	}

	cacheFreshnessCapture := func(id string) time.Duration { return lim.MaxStatsCacheFreshness(ctx, id) }
	maxCacheFreshness := validation.MaxDurationPerTenant(tenantIDs, cacheFreshnessCapture)

	now := statsCacheMiddlewareNowTimeFunc()
	return maxCacheFreshness == 0 || model.Time(req.GetEnd().UnixMilli()).Before(now.Add(-maxCacheFreshness)), nil
}

func NewIndexStatsCacheMiddleware(
	log log.Logger,
	limits Limits,
	merger queryrangebase.Merger,
	c cache.Cache,
	cacheGenNumberLoader queryrangebase.CacheGenNumberLoader,
	iqo util.IngesterQueryOptions,
	shouldCache queryrangebase.ShouldCacheFn,
	parallelismForReq queryrangebase.ParallelismForReqFn,
	retentionEnabled bool,
	transformer UserIDTransformer,
	metrics *queryrangebase.ResultsCacheMetrics,
) (queryrangebase.Middleware, error) {
	return queryrangebase.NewResultsCacheMiddleware(
		log,
		c,
		IndexStatsSplitter{cacheKeyLimits{limits, transformer, iqo}},
		limits,
		merger,
		IndexStatsExtractor{},
		cacheGenNumberLoader,
		func(ctx context.Context, r queryrangebase.Request) bool {
			if shouldCache != nil && !shouldCache(ctx, r) {
				return false
			}

			cacheStats, err := shouldCacheStats(ctx, r, limits)
			if err != nil {
				level.Error(log).Log("msg", "failed to determine if stats should be cached. Won't cache", "err", err)
				return false
			}

			return cacheStats
		},
		parallelismForReq,
		retentionEnabled,
		false,
		metrics,
	)
}
