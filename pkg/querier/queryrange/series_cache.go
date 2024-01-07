package queryrange

import (
	"context"
	"flag"
	"fmt"
	"sort"
	strings "strings"
	"time"

	"github.com/go-kit/log"

	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/cache/resultscache"
)

type cacheKeySeries struct {
	Limits
	transformer UserIDTransformer
}

// GenerateCacheKey generates a cache key based on the userID, matchers, split duration and the interval of the request.
func (i cacheKeySeries) GenerateCacheKey(ctx context.Context, userID string, r resultscache.Request) string {
	sr := r.(*LokiSeriesRequest)
	split := i.MetadataQuerySplitDuration(userID)

	var currentInterval int64
	if denominator := int64(split / time.Millisecond); denominator > 0 {
		currentInterval = sr.GetStart().UnixMilli() / denominator
	}

	if i.transformer != nil {
		userID = i.transformer(ctx, userID)
	}

	matchers := sr.GetMatch()
	sort.Strings(matchers)
	matcherStr := strings.Join(matchers, ",")

	return fmt.Sprintf("series:%s:%s:%d:%d", userID, matcherStr, currentInterval, split)
}

type seriesExtractor struct{}

// Extract extracts the series response for the specific time range.
// It is a no-op since it is not possible to partition the series data by time range as it is just a list of kv pairs.
func (p seriesExtractor) Extract(_, _ int64, res resultscache.Response, _, _ int64) resultscache.Response {
	return res
}

func (p seriesExtractor) ResponseWithoutHeaders(resp queryrangebase.Response) queryrangebase.Response {
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
	logger log.Logger,
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
		logger,
		c,
		cacheKeySeries{limits, transformer},
		limits,
		merger,
		seriesExtractor{},
		cacheGenNumberLoader,
		shouldCache,
		parallelismForReq,
		retentionEnabled,
		metrics,
	)
}
