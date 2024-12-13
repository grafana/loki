package queryrange

import (
	"context"
	"flag"
	"fmt"
	strings "strings"
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

type VolumeSplitter struct {
	cacheKeyLimits
}

// GenerateCacheKey generates a cache key based on the userID, Request and interval.
func (i VolumeSplitter) GenerateCacheKey(ctx context.Context, userID string, r resultscache.Request) string {
	cacheKey := i.cacheKeyLimits.GenerateCacheKey(ctx, userID, r)

	volumeReq := r.(*logproto.VolumeRequest)
	limit := volumeReq.GetLimit()
	aggregateBy := volumeReq.GetAggregateBy()
	targetLabels := volumeReq.GetTargetLabels()
	return fmt.Sprintf("volume:%s:%d:%s:%s", cacheKey, limit, aggregateBy, strings.Join(targetLabels, ","))
}

type VolumeExtractor struct{}

// Extract favors the ability to cache over exactness of results. It assumes a constant distribution
// of log volumes over a range and will extract subsets proportionally.
func (p VolumeExtractor) Extract(start, end int64, res resultscache.Response, resStart, resEnd int64) resultscache.Response {
	factor := util.GetFactorOfTime(start, end, resStart, resEnd)

	volumeRes := res.(*VolumeResponse)
	volumes := volumeRes.Response.GetVolumes()
	for i, v := range volumes {
		volumes[i].Volume = uint64(float64(v.Volume) * factor)
	}
	return &VolumeResponse{
		Response: &logproto.VolumeResponse{
			Volumes: volumes,
			Limit:   volumeRes.Response.GetLimit(),
		},
	}
}

func (p VolumeExtractor) ResponseWithoutHeaders(resp queryrangebase.Response) queryrangebase.Response {
	volumeRes := resp.(*VolumeResponse)
	return &VolumeResponse{
		Response: volumeRes.Response,
	}
}

type VolumeCacheConfig struct {
	queryrangebase.ResultsCacheConfig `yaml:",inline"`
}

// RegisterFlags registers flags.
func (cfg *VolumeCacheConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix(f, "frontend.volume-results-cache.")
}

func (cfg *VolumeCacheConfig) Validate() error {
	return cfg.ResultsCacheConfig.Validate()
}

// volumeCacheMiddlewareNowTimeFunc is a function that returns the current time.
// It is used to allow tests to override the current time.
var volumeCacheMiddlewareNowTimeFunc = model.Now

// shouldCacheVolume returns true if the request should be cached.
// It returns false if:
// - The request end time falls within the max_stats_cache_freshness duration.
func shouldCacheVolume(ctx context.Context, req queryrangebase.Request, lim Limits) (bool, error) {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return false, err
	}

	cacheFreshnessCapture := func(id string) time.Duration { return lim.MaxStatsCacheFreshness(ctx, id) }
	maxCacheFreshness := validation.MaxDurationPerTenant(tenantIDs, cacheFreshnessCapture)

	now := volumeCacheMiddlewareNowTimeFunc()
	return maxCacheFreshness == 0 || model.Time(req.GetEnd().UnixMilli()).Before(now.Add(-maxCacheFreshness)), nil
}

func NewVolumeCacheMiddleware(
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
		VolumeSplitter{cacheKeyLimits{limits, transformer, iqo}},
		limits,
		merger,
		VolumeExtractor{},
		cacheGenNumberLoader,
		func(ctx context.Context, r queryrangebase.Request) bool {
			if shouldCache != nil && !shouldCache(ctx, r) {
				return false
			}

			cacheStats, err := shouldCacheVolume(ctx, r, limits)
			if err != nil {
				level.Error(log).Log("msg", "failed to determine if volume should be cached. Won't cache", "err", err)
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
