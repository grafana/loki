package queryrange

import (
	"context"
	"flag"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/cache/resultscache"
)

type SeriesSplitter struct {
	cacheKeyLimits
}

// GenerateCacheKey generates a cache key based on the userID, Request and interval.
func (i SeriesSplitter) GenerateCacheKey(ctx context.Context, userID string, r resultscache.Request) string {
	cacheKey := i.cacheKeyLimits.GenerateCacheKey(ctx, userID, r)
	return fmt.Sprintf("series:%s", cacheKey)
}

type SeriesExtractor struct{}

// Extract favors the ability to cache over exactness of results. It assumes a constant distribution
// of log volumes over a range and will extract subsets proportionally.
func (p SeriesExtractor) Extract(start, end int64, res resultscache.Response, resStart, resEnd int64) resultscache.Response {
	// factor := util.GetFactorOfTime(start, end, resStart, resEnd)

	seriesRes := res.(*LokiSeriesResponse)
	// TODO(kavi): Use factor to split the series list propotionally?
	return &LokiSeriesResponse{
		Status:     seriesRes.Status,
		Version:    seriesRes.Version,
		Data:       seriesRes.Data,
		Statistics: seriesRes.Statistics,
	}
}

func (p SeriesExtractor) ResponseWithoutHeaders(resp queryrangebase.Response) queryrangebase.Response {
	seriesRes := resp.(*LokiSeriesResponse)
	return &LokiSeriesResponse{
		Data:       seriesRes.Data,
		Status:     seriesRes.Status,
		Version:    seriesRes.Version,
		Statistics: seriesRes.Statistics,
	}
}

type SeriesCacheConfig struct {
	queryrangebase.ResultsCacheConfig `yaml:",inline"`
}

// RegisterFlags registers flags.
func (cfg *SeriesCacheConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix(f, "frontend.series-results-cache.")
}

func (cfg *SeriesCacheConfig) Validate() error {
	return cfg.ResultsCacheConfig.Validate()
}

func NewSeriesCacheMiddleware(
	log log.Logger,
	limits Limits,
	merger queryrangebase.Merger,
	c cache.Cache,
	cacheGenNumberLoader queryrangebase.CacheGenNumberLoader,
	shouldCache queryrangebase.ShouldCacheFn,
	parallelismForReq queryrangebase.ParallelismForReqFn,
	retentionEnabled bool,
	transformer UserIDTransformer,
	metrics *queryrangebase.ResultsCacheMetrics,
) (queryrangebase.Middleware, error) {
	return queryrangebase.NewResultsCacheMiddleware(
		log,
		c,
		SeriesSplitter{cacheKeyLimits{limits, transformer}},
		limits,
		merger,
		SeriesExtractor{},
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
		metrics,
	)
}
