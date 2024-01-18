package queryrange

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/go-kit/log"

	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/cache/resultscache"
	"github.com/grafana/loki/pkg/util"
)

type cacheKeyLabels struct {
	Limits
	transformer UserIDTransformer
	iqo         util.IngesterQueryOptions
}

// GenerateCacheKey generates a cache key based on the userID, split duration and the interval of the request.
// It also includes the label name and the provided query for label values request.
func (i cacheKeyLabels) GenerateCacheKey(ctx context.Context, userID string, r resultscache.Request) string {
	lr := r.(*LabelRequest)

	split := SplitIntervalForTimeRange(i.iqo, i.Limits, i.MetadataQuerySplitDuration, []string{userID}, time.Now().UTC(), r.GetEnd().UTC())

	var currentInterval int64
	if denominator := int64(split / time.Millisecond); denominator > 0 {
		currentInterval = lr.GetStart().UnixMilli() / denominator
	}

	if i.transformer != nil {
		userID = i.transformer(ctx, userID)
	}

	if lr.GetValues() {
		return fmt.Sprintf("labelvalues:%s:%s:%s:%d:%d", userID, lr.GetName(), lr.GetQuery(), currentInterval, split)
	}

	return fmt.Sprintf("labels:%s:%d:%d", userID, currentInterval, split)
}

type labelsExtractor struct{}

// Extract extracts the labels response for the specific time range.
// It is a no-op since it is not possible to partition the labels data by time range as it is just a slice of strings.
func (p labelsExtractor) Extract(_, _ int64, res resultscache.Response, _, _ int64) resultscache.Response {
	return res
}

func (p labelsExtractor) ResponseWithoutHeaders(resp queryrangebase.Response) queryrangebase.Response {
	labelsResp := resp.(*LokiLabelNamesResponse)
	return &LokiLabelNamesResponse{
		Status:     labelsResp.Status,
		Data:       labelsResp.Data,
		Version:    labelsResp.Version,
		Statistics: labelsResp.Statistics,
	}
}

type LabelsCacheConfig struct {
	queryrangebase.ResultsCacheConfig `yaml:",inline"`
}

// RegisterFlags registers flags.
func (cfg *LabelsCacheConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix(f, "frontend.label-results-cache.")
}

func (cfg *LabelsCacheConfig) Validate() error {
	return cfg.ResultsCacheConfig.Validate()
}

func NewLabelsCacheMiddleware(
	logger log.Logger,
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
		logger,
		c,
		cacheKeyLabels{limits, transformer, iqo},
		limits,
		merger,
		labelsExtractor{},
		cacheGenNumberLoader,
		func(ctx context.Context, r queryrangebase.Request) bool {
			return shouldCacheMetadataReq(ctx, logger, shouldCache, r, limits)
		},
		parallelismForReq,
		retentionEnabled,
		metrics,
	)
}
