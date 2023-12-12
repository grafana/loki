package queryrange

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/cache/resultscache"
)

type InstantMetricCacheKeyGenerator struct {
	limits    Limits
	transform UserIDTransformer
}

func (i *InstantMetricCacheKeyGenerator) GenerateCacheKey(ctx context.Context, userID string, r resultscache.Request) string {
	split := i.limits.QuerySplitDuration(userID)

	var interval int64
	if d := int64(split / time.Millisecond); d > 0 {
		interval = r.GetEnd().UnixMilli() / d // for instant query GetStart() == GetEnd()
	}

	if i.transform != nil {
		userID = i.transform(ctx, userID)
	}

	return fmt.Sprintf("%s:%s:%d:%d", userID, r.GetQuery(), interval, split)
}

type InstantMetricCacheConfig struct {
	queryrangebase.ResultsCacheConfig `yaml:",inline"`
}

// RegisterFlags registers flags.
func (cfg *InstantMetricCacheConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix(f, "frontend.instant-metric-cache.")
}

func (cfg *InstantMetricCacheConfig) Validate() error {
	return cfg.ResultsCacheConfig.Validate()
}

func NewInstantMetricCacheMiddleware(
	log log.Logger,
	limits Limits,
	merger queryrangebase.Merger,
	c cache.Cache,
	cacheGenNumberLoader queryrangebase.CacheGenNumberLoader,
	shouldCache queryrangebase.ShouldCacheFn,
	parallelismForReq queryrangebase.ParallelismForReqFn,
	retentionEnabled bool,
	extractor queryrangebase.Extractor,
	userTransform UserIDTransformer,
	metrics *queryrangebase.ResultsCacheMetrics,
) (queryrangebase.Middleware, error) {
	return queryrangebase.NewResultsCacheMiddleware(
		log,
		c,
		&InstantMetricCacheKeyGenerator{limits, userTransform},
		limits,
		merger,
		extractor,
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
		metrics,
	)
}

type InstantMetricSplitter struct {
}
