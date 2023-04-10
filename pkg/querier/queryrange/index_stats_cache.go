package queryrange

import (
	"context"
	"fmt"

	"github.com/go-kit/log"

	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
)

type IndexStatsSplitter struct{}

// GenerateCacheKey generates a cache key based on the userID, Request and interval.
func (i IndexStatsSplitter) GenerateCacheKey(_ context.Context, userID string, r queryrangebase.Request) string {
	return fmt.Sprintf("indexStats:%s:%s:%d:%d:%d", userID, r.GetQuery(), r.GetStep(), r.GetStart(), r.GetEnd())
}

type IndexStatsExtractor struct{}

func (p IndexStatsExtractor) Extract(start, end int64, from queryrangebase.Response) queryrangebase.Response {
	return from.(*IndexStatsResponse)
}

func (p IndexStatsExtractor) ResponseWithoutHeaders(resp queryrangebase.Response) queryrangebase.Response {
	statsRes := resp.(*IndexStatsResponse)
	return &IndexStatsResponse{
		Response: statsRes.Response,
	}
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
	metrics *queryrangebase.ResultsCacheMetrics,
) (queryrangebase.Middleware, error) {
	return queryrangebase.NewResultsCacheMiddleware(
		log,
		c,
		IndexStatsSplitter{},
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
