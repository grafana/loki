package queryrange

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/util"
)

type IndexStatsSplitter struct {
	cacheKeyLimits
}

// GenerateCacheKey generates a cache key based on the userID, Request and interval.
func (i IndexStatsSplitter) GenerateCacheKey(ctx context.Context, userID string, r queryrangebase.Request) string {
	cacheKey := i.cacheKeyLimits.GenerateCacheKey(ctx, userID, r)
	return fmt.Sprintf("indexStats:%s", cacheKey)
}

type IndexStatsExtractor struct{}

// Extract favors the ability to cache over exactness of results. It assumes a constant distribution
// of log volumes over a range and will extract subsets proportionally.
func (p IndexStatsExtractor) Extract(start, end int64, res queryrangebase.Response, resStart, resEnd int64) queryrangebase.Response {
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
	// NOTE: We cannot use something similar to the per-tenant `max_cache_freshness_per_query` limit
	// because resultsCache.filterRecentExtents would extract already inflated stats.
	// Instead, we need to filter out whole requests that ask for stats in recent data.
	DoNotCacheRequestWithin time.Duration `yaml:"do_not_cache_request_within"`
}

// RegisterFlags registers flags.
func (cfg *IndexStatsCacheConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.ResultsCacheConfig.RegisterFlagsWithPrefix(f, "frontend.index-stats-results-cache.")
	f.DurationVar(&cfg.DoNotCacheRequestWithin, "frontend.index-stats-results-cache.do-not-cache-request-within", 0, "Do not cache requests with an end time that falls within Now minus this duration. 0 disables this feature.")
}

func (cfg *IndexStatsCacheConfig) Validate() error {
	return cfg.ResultsCacheConfig.Validate()
}

// statsCacheMiddlewareNowTimeFunc is a function that returns the current time.
// It is used to allow tests to override the current time.
var statsCacheMiddlewareNowTimeFunc = model.Now

// ShouldCache returns true if the request should be cached.
// It returns false if the request end time falls within the DoNotCacheRequestWithin duration.
func (cfg *IndexStatsCacheConfig) ShouldCache(req queryrangebase.Request) bool {
	now := statsCacheMiddlewareNowTimeFunc()
	return cfg.DoNotCacheRequestWithin == 0 || model.Time(req.GetEnd()).Before(now.Add(-cfg.DoNotCacheRequestWithin))
}

func NewIndexStatsCacheMiddleware(
	cfg IndexStatsCacheConfig,
	log log.Logger,
	limits Limits,
	merger queryrangebase.Merger,
	c cache.Cache,
	cacheGenNumberLoader queryrangebase.CacheGenNumberLoader,
	shouldCache queryrangebase.ShouldCacheFn,
	parallelismForReq func(ctx context.Context, tenantIDs []string, r queryrangebase.Request) int,
	retentionEnabled bool,
	transformer UserIDTransformer,
	metrics *queryrangebase.ResultsCacheMetrics,
) (queryrangebase.Middleware, error) {
	return queryrangebase.NewResultsCacheMiddleware(
		log,
		c,
		IndexStatsSplitter{cacheKeyLimits{limits, transformer}},
		limits,
		merger,
		IndexStatsExtractor{},
		cacheGenNumberLoader,
		func(r queryrangebase.Request) bool {
			if shouldCache != nil && !shouldCache(r) {
				return false
			}
			return cfg.ShouldCache(r)
		},
		parallelismForReq,
		retentionEnabled,
		metrics,
	)
}
