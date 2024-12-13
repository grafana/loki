package queryrange

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/go-kit/log"

	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

type cacheKeyLabels struct {
	Limits
	transformer UserIDTransformer
}

// metadataSplitIntervalForTimeRange returns split interval for series and label requests.
// If `recent_metadata_query_window` is configured and the query start interval is within this window,
// it returns `split_recent_metadata_queries_by_interval`.
// For other cases, the default split interval of `split_metadata_queries_by_interval` will be used.
func metadataSplitIntervalForTimeRange(limits Limits, tenantIDs []string, ref, start time.Time) time.Duration {
	split := validation.MaxDurationOrZeroPerTenant(tenantIDs, limits.MetadataQuerySplitDuration)

	recentMetadataQueryWindow := validation.MaxDurationOrZeroPerTenant(tenantIDs, limits.RecentMetadataQueryWindow)
	recentMetadataQuerySplitInterval := validation.MaxDurationOrZeroPerTenant(tenantIDs, limits.RecentMetadataQuerySplitDuration)

	// if either of the options are not configured, use the default metadata split interval
	if recentMetadataQueryWindow == 0 || recentMetadataQuerySplitInterval == 0 {
		return split
	}

	// if the query start is not before window start, it would be split using recentMetadataQuerySplitInterval
	if windowStart := ref.Add(-recentMetadataQueryWindow); !start.Before(windowStart) {
		split = recentMetadataQuerySplitInterval
	}

	return split
}

// GenerateCacheKey generates a cache key based on the userID, split duration and the interval of the request.
// It also includes the label name and the provided query for label values request.
func (i cacheKeyLabels) GenerateCacheKey(ctx context.Context, userID string, r resultscache.Request) string {
	lr := r.(*LabelRequest)
	split := metadataSplitIntervalForTimeRange(i.Limits, []string{userID}, time.Now().UTC(), r.GetStart().UTC())

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

	return fmt.Sprintf("labels:%s:%s:%d:%d", userID, lr.GetQuery(), currentInterval, split)
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
	shouldCache queryrangebase.ShouldCacheFn,
	parallelismForReq queryrangebase.ParallelismForReqFn,
	retentionEnabled bool,
	transformer UserIDTransformer,
	metrics *queryrangebase.ResultsCacheMetrics,
) (queryrangebase.Middleware, error) {
	return queryrangebase.NewResultsCacheMiddleware(
		logger,
		c,
		cacheKeyLabels{limits, transformer},
		limits,
		merger,
		labelsExtractor{},
		cacheGenNumberLoader,
		func(ctx context.Context, r queryrangebase.Request) bool {
			return shouldCacheMetadataReq(ctx, logger, shouldCache, r, limits)
		},
		parallelismForReq,
		retentionEnabled,
		true,
		metrics,
	)
}
