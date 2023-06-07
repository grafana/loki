package queryrange

import (
	"context"
	"flag"
	"fmt"

	"github.com/go-kit/log"

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
	factor, _, _ := util.GetFactorOfTime(start, end, resStart, resEnd)

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

func NewIndexStatsCacheMiddleware(
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
		shouldCache,
		parallelismForReq,
		retentionEnabled,
		metrics,
	)
}
