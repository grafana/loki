package queryrangebase

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

var (
	// Value that cacheControlHeader has if the response indicates that the results should not be cached.
	noStoreValue = "no-store"
)

const (
	reasonMissing  = "missing"
	reasonMismatch = "mismatch"
)

type ResultsCacheMetrics struct {
	versionComparisons        prometheus.Counter
	versionComparisonFailures *prometheus.CounterVec
}

func NewResultsCacheMetrics(registerer prometheus.Registerer) *ResultsCacheMetrics {
	return &ResultsCacheMetrics{
		versionComparisons: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "results_cache_version_comparisons_total",
			Help:      "Comparisons of cache key versions in the results cache between query-frontends & queriers",
		}),
		versionComparisonFailures: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "results_cache_version_comparisons_failed",
			Help:      "Comparison failures of cache key versions in the results cache between query-frontends & queriers",
		}, []string{"reason"}),
	}
}

// ResultsCacheConfig is the config for the results cache.
type ResultsCacheConfig struct {
	resultscache.Config `yaml:",inline"`
}

// RegisterFlags registers flags.
func (cfg *ResultsCacheConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix(f, "frontend.")
}

// Extractor is used by the cache to extract a subset of a response from a cache entry.
type Extractor interface {
	resultscache.Extractor
	ResponseWithoutHeaders(resp Response) Response
}

// PrometheusResponseExtractor helps extracting specific info from Query Response.
type PrometheusResponseExtractor struct{}

// Extract extracts response for specific a range from a response.
func (PrometheusResponseExtractor) Extract(start, end int64, res resultscache.Response, _, _ int64) resultscache.Response {
	promRes := res.(*PrometheusResponse)
	return &PrometheusResponse{
		Status:   StatusSuccess,
		Warnings: promRes.Warnings,
		Data: PrometheusData{
			ResultType: promRes.Data.ResultType,
			Result:     extractMatrix(start, end, promRes.Data.Result),
		},
		Headers: promRes.Headers,
	}
}

// ResponseWithoutHeaders is useful in caching data without headers since
// we anyways do not need headers for sending back the response so this saves some space by reducing size of the objects.
func (PrometheusResponseExtractor) ResponseWithoutHeaders(resp Response) Response {
	promRes := resp.(*PrometheusResponse)
	return &PrometheusResponse{
		Status:   StatusSuccess,
		Warnings: promRes.Warnings,
		Data: PrometheusData{
			ResultType: promRes.Data.ResultType,
			Result:     promRes.Data.Result,
		},
	}
}

// ShouldCacheFn checks whether the current request should go to cache
// or not. If not, just send the request to next handler.
type ShouldCacheFn func(ctx context.Context, r Request) bool

// ParallelismForReqFn returns the parallelism for a given request.
type ParallelismForReqFn func(ctx context.Context, tenantIDs []string, r Request) int

type resultsCache struct {
	cache                *resultscache.ResultsCache
	logger               log.Logger
	cacheGenNumberLoader resultscache.CacheGenNumberLoader
	metrics              *ResultsCacheMetrics
}

// NewResultsCacheMiddleware creates results cache middleware from config.
// The middleware cache result using a unique cache key for a given request (step,query,user) and interval.
// The cache assumes that each request length (end-start) is below or equal the interval.
// Each request starting from within the same interval will hit the same cache entry.
// If the cache doesn't have the entire duration of the request cached, it will query the uncached parts and append them to the cache entries.
// see `generateKey`.
func NewResultsCacheMiddleware(
	logger log.Logger,
	c cache.Cache,
	keygen resultscache.KeyGenerator,
	limits Limits,
	merger Merger,
	extractor Extractor,
	cacheGenNumberLoader resultscache.CacheGenNumberLoader,
	shouldCache ShouldCacheFn,
	parallelismForReq ParallelismForReqFn,
	retentionEnabled bool,
	onlyUseEntireExtent bool,
	metrics *ResultsCacheMetrics,
) (Middleware, error) {
	if cacheGenNumberLoader != nil {
		c = cache.NewCacheGenNumMiddleware(c)
	}

	out := &resultsCache{
		logger:               logger,
		cacheGenNumberLoader: cacheGenNumberLoader,
		metrics:              metrics,
	}

	return MiddlewareFunc(func(next Handler) Handler {
		nextCacheWrapper := resultscache.HandlerFunc(func(ctx context.Context, req resultscache.Request) (resultscache.Response, error) {
			return next.Do(ctx, req.(Request))
		})

		shouldCacheReqWrapper := func(ctx context.Context, req resultscache.Request) bool {
			if shouldCache == nil {
				return true
			}
			return shouldCache(ctx, req.(Request))
		}

		shouldCacheResWrapper := func(ctx context.Context, req resultscache.Request, res resultscache.Response, maxCacheTime int64) bool {
			return out.shouldCacheResponse(ctx, req.(Request), res.(Response), maxCacheTime)
		}

		parallelismForReqWrapper := func(ctx context.Context, tenantIDs []string, req resultscache.Request) int {
			return parallelismForReq(ctx, tenantIDs, req.(Request))
		}

		out.cache = resultscache.NewResultsCache(
			logger,
			c,
			nextCacheWrapper,
			keygen,
			limits,
			FromQueryResponseMergerToCacheResponseMerger(merger),
			extractor,
			shouldCacheReqWrapper,
			shouldCacheResWrapper,
			parallelismForReqWrapper,
			cacheGenNumberLoader,
			retentionEnabled,
			onlyUseEntireExtent,
		)

		return out
	}), nil
}

func (s resultsCache) Do(ctx context.Context, r Request) (Response, error) {
	res, err := s.cache.Do(ctx, r.(resultscache.Request))
	if err != nil {
		return nil, err
	}

	queryRes, ok := res.(Response)
	if !ok {
		return nil, fmt.Errorf("could not cast cache response to query response")
	}

	return queryRes, nil
}

// shouldCacheResponse says whether the response should be cached or not.
func (s resultsCache) shouldCacheResponse(ctx context.Context, req Request, r Response, maxCacheTime int64) bool {
	headerValues := getHeaderValuesWithName(r, cacheControlHeader)

	user, _ := user.ExtractOrgID(ctx)
	logger := log.With(s.logger, "org_id", user)

	for _, v := range headerValues {
		if v == noStoreValue {
			level.Debug(logger).Log("msg", fmt.Sprintf("%s header in response is equal to %s, not caching the response", cacheControlHeader, noStoreValue))
			return false
		}
	}

	if !s.isAtModifierCachable(req, maxCacheTime) {
		return false
	}

	if s.cacheGenNumberLoader == nil {
		return true
	}

	genNumbersFromResp := getHeaderValuesWithName(r, ResultsCacheGenNumberHeaderName)
	genNumberFromCtx := cache.ExtractCacheGenNumber(ctx)

	s.metrics.versionComparisons.Inc()

	if len(genNumbersFromResp) == 0 && genNumberFromCtx != "" {
		level.Debug(logger).Log("msg", fmt.Sprintf("we found results cache gen number %s set in store but none in headers", genNumberFromCtx), "query", req.GetQuery())

		// NB(owen-d):
		// If the queriers aren't returning cache numbers, something is likely broken
		// (i.e. the queriers aren't reporting the cache numbers they see or aren't seeing cache numbers at all).
		s.metrics.versionComparisonFailures.WithLabelValues(reasonMissing).Inc()
		return false
	}

	for _, gen := range genNumbersFromResp {
		if gen != genNumberFromCtx {
			level.Debug(logger).Log("msg", fmt.Sprintf("inconsistency in results cache gen numbers %s (GEN-FROM-RESPONSE) != %s (GEN-FROM-STORE), not caching the response", gen, genNumberFromCtx), "query", req.GetQuery())
			s.metrics.versionComparisonFailures.WithLabelValues(reasonMismatch).Inc()
			return false
		}
	}

	return true
}

var errAtModifierAfterEnd = errors.New("at modifier after end")

// isAtModifierCachable returns true if the @ modifier result
// is safe to cache.
func (s resultsCache) isAtModifierCachable(r Request, maxCacheTime int64) bool {
	// There are 2 cases when @ modifier is not safe to cache:
	//   1. When @ modifier points to time beyond the maxCacheTime.
	//   2. If the @ modifier time is > the query range end while being
	//      below maxCacheTime. In such cases if any tenant is intentionally
	//      playing with old data, we could cache empty result if we look
	//      beyond query end.
	query := r.GetQuery()
	if !strings.Contains(query, "@") {
		return true
	}
	expr, err := parser.ParseExpr(query)
	if err != nil {
		// We are being pessimistic in such cases.
		level.Warn(s.logger).Log("msg", "failed to parse query, considering @ modifier as not cachable", "query", query, "err", err)
		return false
	}

	// This resolves the start() and end() used with the @ modifier.
	expr = promql.PreprocessExpr(expr, r.GetStart(), r.GetEnd())

	end := r.GetEnd().UnixMilli()
	atModCachable := true
	parser.Inspect(expr, func(n parser.Node, _ []parser.Node) error {
		switch e := n.(type) {
		case *parser.VectorSelector:
			if e.Timestamp != nil && (*e.Timestamp > end || *e.Timestamp > maxCacheTime) {
				atModCachable = false
				return errAtModifierAfterEnd
			}
		case *parser.MatrixSelector:
			ts := e.VectorSelector.(*parser.VectorSelector).Timestamp
			if ts != nil && (*ts > end || *ts > maxCacheTime) {
				atModCachable = false
				return errAtModifierAfterEnd
			}
		case *parser.SubqueryExpr:
			if e.Timestamp != nil && (*e.Timestamp > end || *e.Timestamp > maxCacheTime) {
				atModCachable = false
				return errAtModifierAfterEnd
			}
		}
		return nil
	})

	return atModCachable
}

func getHeaderValuesWithName(r Response, headerName string) (headerValues []string) {
	for _, hv := range r.GetHeaders() {
		if hv.GetName() != headerName {
			continue
		}

		headerValues = append(headerValues, hv.GetValues()...)
	}

	return
}

func extractMatrix(start, end int64, matrix []SampleStream) []SampleStream {
	result := make([]SampleStream, 0, len(matrix))
	for _, stream := range matrix {
		extracted, ok := extractSampleStream(start, end, stream)
		if ok {
			result = append(result, extracted)
		}
	}
	return result
}

func extractSampleStream(start, end int64, stream SampleStream) (SampleStream, bool) {
	result := SampleStream{
		Labels:  stream.Labels,
		Samples: make([]logproto.LegacySample, 0, len(stream.Samples)),
	}
	for _, sample := range stream.Samples {
		if start <= sample.TimestampMs && sample.TimestampMs <= end {
			result.Samples = append(result.Samples, sample)
		}
	}
	if len(result.Samples) == 0 {
		return SampleStream{}, false
	}
	return result, true
}
