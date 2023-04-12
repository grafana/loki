package queryrange

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/log"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/util/math"
)

type IndexStatsSplitter struct {
	Limits
}

// GenerateCacheKey generates a cache key based on the userID, Request and interval.
func (i IndexStatsSplitter) GenerateCacheKey(_ context.Context, userID string, r queryrangebase.Request) string {
	split := i.QuerySplitDuration(userID)

	var currentInterval int64
	if denominator := int64(split / time.Millisecond); denominator > 0 {
		currentInterval = r.GetStart() / denominator
	}

	return fmt.Sprintf("indexStats:%s:%s:%d", userID, r.GetQuery(), currentInterval)
}

type IndexStatsExtractor struct{}

func (p IndexStatsExtractor) Extract(start, end int64, res queryrangebase.Response, resStart, resEnd int64) queryrangebase.Response {
	// TODO: Reuse functionality from https://github.com/grafana/loki/pull/9096 once merged.
	totalTime := resEnd - resStart
	leadingTime := math.Max64(0, start-resStart)
	trailingTime := math.Max64(0, resEnd-end)
	factor := float64(totalTime-(leadingTime+trailingTime)) / float64(totalTime)

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
		IndexStatsSplitter{limits},
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
