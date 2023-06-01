package queryrange

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/validation"
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
}

// RegisterFlags registers flags.
func (cfg *IndexStatsCacheConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.ResultsCacheConfig.RegisterFlagsWithPrefix(f, "frontend.index-stats-results-cache.")
}

func (cfg *IndexStatsCacheConfig) Validate() error {
	return cfg.ResultsCacheConfig.Validate()
}

type statsCacheMiddleware struct {
	logger    log.Logger
	limits    Limits
	next      queryrangebase.Handler
	cacheWare queryrangebase.Handler
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
	cacheWare, err := queryrangebase.NewResultsCacheMiddleware(
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
	if err != nil {
		return nil, err
	}

	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return &statsCacheMiddleware{
			logger:    log,
			limits:    limits,
			next:      next,
			cacheWare: cacheWare.Wrap(next),
		}
	}), nil
}

// statsCacheMiddlewareNowTimeFunc is a function that returns the current time.
// It is used to allow tests to override the current time.
var statsCacheMiddlewareNowTimeFunc = model.Now

// Do implements the queryrangebase.Handler interface.
// It enforces the MaxStatsCacheFreshness limit before forwarding the request to the cache middleware.
func (s *statsCacheMiddleware) Do(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	cacheFreshnessCapture := func(id string) time.Duration { return s.limits.MaxStatsCacheFreshness(ctx, id) }
	maxCacheFreshness := validation.MaxDurationPerTenant(tenantIDs, cacheFreshnessCapture)

	// If part of the request is too recent, we need to split the request into two parts:
	// 1. A part that can be cached (before freshness). Forwarded to the cache middleware.
	// 2. A part that cannot be cached (after freshness). Skips the cache middleware.
	// The results of both parts are then merged and returned.
	now := statsCacheMiddlewareNowTimeFunc()
	if maxCacheFreshness != 0 && model.Time(r.GetEnd()).After(now.Add(-maxCacheFreshness)) {
		cachableReq := r.WithStartEnd(r.GetStart(), int64(now.Add(-maxCacheFreshness)))
		uncachableReq := r.WithStartEnd(int64(now.Add(-maxCacheFreshness)), r.GetEnd())

		type f func() (queryrangebase.Response, error)
		jobs := []f{
			f(func() (queryrangebase.Response, error) {
				return s.cacheWare.Do(ctx, cachableReq)
			}),
			f(func() (queryrangebase.Response, error) {
				return s.next.Do(ctx, uncachableReq)
			}),
		}

		results := make([]queryrangebase.Response, len(jobs))
		if err := concurrency.ForEachJob(ctx, len(jobs), len(jobs), func(ctx context.Context, i int) error {
			res, err := jobs[i]()
			results[i] = res
			return err
		}); err != nil {
			return nil, err
		}

		// Merge the results.
		merged, err := LokiCodec.MergeResponse(results...)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusInternalServerError, err.Error())
		}

		return merged, nil
	}

	return s.cacheWare.Do(ctx, r)
}
