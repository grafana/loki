package queryrangebase

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/grafana/dskit/flagext"
	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/uber/jaeger-client-go"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/math"
	"github.com/grafana/loki/pkg/util/spanlogger"
	"github.com/grafana/loki/pkg/util/validation"
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
			Namespace: "loki",
			Name:      "results_cache_version_comparisons_total",
			Help:      "Comparisons of cache key versions in the results cache between query-frontends & queriers",
		}),
		versionComparisonFailures: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "results_cache_version_comparisons_failed",
			Help:      "Comparison failures of cache key versions in the results cache between query-frontends & queriers",
		}, []string{"reason"}),
	}
}

type CacheGenNumberLoader interface {
	GetResultsCacheGenNumber(tenantIDs []string) string
	Stop()
}

// ResultsCacheConfig is the config for the results cache.
type ResultsCacheConfig struct {
	CacheConfig cache.Config `yaml:"cache"`
	Compression string       `yaml:"compression"`
}

func (cfg *ResultsCacheConfig) RegisterFlagsWithPrefix(f *flag.FlagSet, prefix string) {
	cfg.CacheConfig.RegisterFlagsWithPrefix(prefix, "", f)

	f.StringVar(&cfg.Compression, prefix+"compression", "", "Use compression in results cache. Supported values are: 'snappy' and '' (disable compression).")
	//lint:ignore faillint Need to pass the global logger like this for warning on deprecated methods
	flagext.DeprecatedFlag(f, prefix+"cache-split-interval", "Deprecated: The maximum interval expected for each request, results will be cached per single interval. This behavior is now determined by querier.split-queries-by-interval.", util_log.Logger)
}

// RegisterFlags registers flags.
func (cfg *ResultsCacheConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix(f, "frontend.")
}

func (cfg *ResultsCacheConfig) Validate() error {
	switch cfg.Compression {
	case "snappy", "":
		// valid
	default:
		return errors.Errorf("unsupported compression type: %s", cfg.Compression)
	}

	return cfg.CacheConfig.Validate()
}

// Extractor is used by the cache to extract a subset of a response from a cache entry.
type Extractor interface {
	// Extract extracts a subset of a response from the `start` and `end` timestamps in milliseconds
	// in the `res` response which spans from `resStart` to `resEnd`.
	Extract(start, end int64, res Response, resStart, resEnd int64) Response
	ResponseWithoutHeaders(resp Response) Response
}

// PrometheusResponseExtractor helps extracting specific info from Query Response.
type PrometheusResponseExtractor struct{}

// Extract extracts response for specific a range from a response.
func (PrometheusResponseExtractor) Extract(start, end int64, res Response, resStart, resEnd int64) Response {
	promRes := res.(*PrometheusResponse)
	return &PrometheusResponse{
		Status: StatusSuccess,
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
		Status: StatusSuccess,
		Data: PrometheusData{
			ResultType: promRes.Data.ResultType,
			Result:     promRes.Data.Result,
		},
	}
}

// CacheSplitter generates cache keys. This is a useful interface for downstream
// consumers who wish to implement their own strategies.
type CacheSplitter interface {
	GenerateCacheKey(ctx context.Context, userID string, r Request) string
}

// constSplitter is a utility for using a constant split interval when determining cache keys
type constSplitter time.Duration

// GenerateCacheKey generates a cache key based on the userID, Request and interval.
func (t constSplitter) GenerateCacheKey(_ context.Context, userID string, r Request) string {
	currentInterval := r.GetStart() / int64(time.Duration(t)/time.Millisecond)
	return fmt.Sprintf("%s:%s:%d:%d", userID, r.GetQuery(), r.GetStep(), currentInterval)
}

// ShouldCacheFn checks whether the current request should go to cache
// or not. If not, just send the request to next handler.
type ShouldCacheFn func(ctx context.Context, r Request) bool

type resultsCache struct {
	logger   log.Logger
	next     Handler
	cache    cache.Cache
	limits   Limits
	splitter CacheSplitter

	extractor            Extractor
	minCacheExtent       int64 // discard any cache extent smaller than this
	merger               Merger
	cacheGenNumberLoader CacheGenNumberLoader
	shouldCache          ShouldCacheFn
	parallelismForReq    func(ctx context.Context, tenantIDs []string, r Request) int
	retentionEnabled     bool
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
	splitter CacheSplitter,
	limits Limits,
	merger Merger,
	extractor Extractor,
	cacheGenNumberLoader CacheGenNumberLoader,
	shouldCache ShouldCacheFn,
	parallelismForReq func(ctx context.Context, tenantIDs []string, r Request) int,
	retentionEnabled bool,
	metrics *ResultsCacheMetrics,
) (Middleware, error) {
	if cacheGenNumberLoader != nil {
		c = cache.NewCacheGenNumMiddleware(c)
	}

	return MiddlewareFunc(func(next Handler) Handler {
		return &resultsCache{
			logger:               logger,
			next:                 next,
			cache:                c,
			limits:               limits,
			merger:               merger,
			extractor:            extractor,
			minCacheExtent:       (5 * time.Minute).Milliseconds(),
			splitter:             splitter,
			cacheGenNumberLoader: cacheGenNumberLoader,
			shouldCache:          shouldCache,
			parallelismForReq:    parallelismForReq,
			retentionEnabled:     retentionEnabled,
			metrics:              metrics,
		}
	}), nil
}

func (s resultsCache) Do(ctx context.Context, r Request) (Response, error) {
	sp, ctx := opentracing.StartSpanFromContext(ctx, "resultsCache.Do")
	defer sp.Finish()
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	if s.shouldCache != nil && !s.shouldCache(ctx, r) {
		return s.next.Do(ctx, r)
	}

	if s.cacheGenNumberLoader != nil && s.retentionEnabled {
		ctx = cache.InjectCacheGenNumber(ctx, s.cacheGenNumberLoader.GetResultsCacheGenNumber(tenantIDs))
	}

	var (
		key      = s.splitter.GenerateCacheKey(ctx, tenant.JoinTenantIDs(tenantIDs), r)
		extents  []Extent
		response Response
	)

	sp.LogKV(
		"query", r.GetQuery(),
		"step", time.UnixMilli(r.GetStep()),
		"start", time.UnixMilli(r.GetStart()),
		"end", r.GetEnd(),
		"key", key,
	)

	cacheFreshnessCapture := func(id string) time.Duration { return s.limits.MaxCacheFreshness(ctx, id) }
	maxCacheFreshness := validation.MaxDurationPerTenant(tenantIDs, cacheFreshnessCapture)
	maxCacheTime := int64(model.Now().Add(-maxCacheFreshness))
	if r.GetStart() > maxCacheTime {
		return s.next.Do(ctx, r)
	}

	cached, ok := s.get(ctx, key)
	if ok {
		response, extents, err = s.handleHit(ctx, r, cached, maxCacheTime)
	} else {
		response, extents, err = s.handleMiss(ctx, r, maxCacheTime)
	}

	if err == nil && len(extents) > 0 {
		extents, err := s.filterRecentExtents(r, maxCacheFreshness, extents)
		if err != nil {
			return nil, err
		}
		s.put(ctx, key, extents)
	}

	return response, err
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
		level.Debug(logger).Log("msg", fmt.Sprintf("we found results cache gen number %s set in store but none in headers", genNumberFromCtx))

		// NB(owen-d):
		// If the queriers aren't returning cache numbers, something is likely broken
		// (i.e. the queriers aren't reporting the cache numbers they see or aren't seeing cache numbers at all).
		s.metrics.versionComparisonFailures.WithLabelValues(reasonMissing).Inc()
		return false
	}

	for _, gen := range genNumbersFromResp {
		if gen != genNumberFromCtx {
			level.Debug(logger).Log("msg", fmt.Sprintf("inconsistency in results cache gen numbers %s (GEN-FROM-RESPONSE) != %s (GEN-FROM-STORE), not caching the response", gen, genNumberFromCtx))
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
	expr = promql.PreprocessExpr(expr, timestamp.Time(r.GetStart()), timestamp.Time(r.GetEnd()))

	end := r.GetEnd()
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

func (s resultsCache) handleMiss(ctx context.Context, r Request, maxCacheTime int64) (Response, []Extent, error) {
	response, err := s.next.Do(ctx, r)
	if err != nil {
		return nil, nil, err
	}

	if !s.shouldCacheResponse(ctx, r, response, maxCacheTime) {
		return response, []Extent{}, nil
	}

	extent, err := toExtent(ctx, r, s.extractor.ResponseWithoutHeaders(response))
	if err != nil {
		return nil, nil, err
	}

	extents := []Extent{
		extent,
	}
	return response, extents, nil
}

func (s resultsCache) handleHit(ctx context.Context, r Request, extents []Extent, maxCacheTime int64) (Response, []Extent, error) {
	var (
		reqResps []RequestResponse
		err      error
	)
	sp, ctx := opentracing.StartSpanFromContext(ctx, "handleHit")
	defer sp.Finish()
	log := spanlogger.FromContext(ctx)
	defer log.Finish()

	requests, responses, err := s.partition(r, extents)
	if err != nil {
		return nil, nil, err
	}
	if len(requests) == 0 {
		response, err := s.merger.MergeResponse(responses...)
		// No downstream requests so no need to write back to the cache.
		return response, nil, err
	}

	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}
	reqResps, err = DoRequests(ctx, s.next, requests, s.parallelismForReq(ctx, tenantIDs, r))

	if err != nil {
		return nil, nil, err
	}

	for _, reqResp := range reqResps {
		responses = append(responses, reqResp.Response)
		if !s.shouldCacheResponse(ctx, r, reqResp.Response, maxCacheTime) {
			continue
		}
		extent, err := toExtent(ctx, reqResp.Request, s.extractor.ResponseWithoutHeaders(reqResp.Response))
		if err != nil {
			return nil, nil, err
		}
		extents = append(extents, extent)
	}
	sort.Slice(extents, func(i, j int) bool {
		if extents[i].Start == extents[j].Start {
			// as an optimization, for two extents starts at the same time, we
			// put bigger extent at the front of the slice, which helps
			// to reduce the amount of merge we have to do later.
			return extents[i].End > extents[j].End
		}

		return extents[i].Start < extents[j].Start
	})

	// Merge any extents - potentially overlapping
	accumulator, err := newAccumulator(extents[0])
	if err != nil {
		return nil, nil, err
	}
	mergedExtents := make([]Extent, 0, len(extents))

	for i := 1; i < len(extents); i++ {
		if accumulator.End+r.GetStep() < extents[i].Start {
			mergedExtents, err = merge(mergedExtents, accumulator)
			if err != nil {
				return nil, nil, err
			}
			accumulator, err = newAccumulator(extents[i])
			if err != nil {
				return nil, nil, err
			}
			continue
		}

		if accumulator.End >= extents[i].End {
			continue
		}

		accumulator.TraceId = jaegerTraceID(ctx)
		accumulator.End = extents[i].End
		currentRes, err := extents[i].toResponse()
		if err != nil {
			return nil, nil, err
		}
		merged, err := s.merger.MergeResponse(accumulator.Response, currentRes)
		if err != nil {
			return nil, nil, err
		}
		accumulator.Response = merged
	}

	mergedExtents, err = merge(mergedExtents, accumulator)
	if err != nil {
		return nil, nil, err
	}

	response, err := s.merger.MergeResponse(responses...)
	return response, mergedExtents, err
}

type accumulator struct {
	Response
	Extent
}

func merge(extents []Extent, acc *accumulator) ([]Extent, error) {
	any, err := types.MarshalAny(acc.Response)
	if err != nil {
		return nil, err
	}
	return append(extents, Extent{
		Start:    acc.Extent.Start,
		End:      acc.Extent.End,
		Response: any,
		TraceId:  acc.Extent.TraceId,
	}), nil
}

func newAccumulator(base Extent) (*accumulator, error) {
	res, err := base.toResponse()
	if err != nil {
		return nil, err
	}
	return &accumulator{
		Response: res,
		Extent:   base,
	}, nil
}

func toExtent(ctx context.Context, req Request, res Response) (Extent, error) {
	any, err := types.MarshalAny(res)
	if err != nil {
		return Extent{}, err
	}
	return Extent{
		Start:    req.GetStart(),
		End:      req.GetEnd(),
		Response: any,
		TraceId:  jaegerTraceID(ctx),
	}, nil
}

// partition calculates the required requests to satisfy req given the cached data.
// extents must be in order by start time.
func (s resultsCache) partition(req Request, extents []Extent) ([]Request, []Response, error) {
	var requests []Request
	var cachedResponses []Response
	start := req.GetStart()

	for _, extent := range extents {
		// If there is no overlap, ignore this extent.
		if extent.GetEnd() < start || extent.Start > req.GetEnd() {
			continue
		}

		// If this extent is tiny and request is not tiny, discard it: more efficient to do a few larger queries.
		// Hopefully tiny request can make tiny extent into not-so-tiny extent.

		// However if the step is large enough, the split_query_by_interval middleware would generate a query with same start and end.
		// For example, if the step size is more than 12h and the interval is 24h.
		// This means the extent's start and end time would be same, even if the timerange covers several hours.
		if (req.GetStart() != req.GetEnd()) && (req.GetEnd()-req.GetStart() > s.minCacheExtent) && (extent.End-extent.Start < s.minCacheExtent) {
			continue
		}

		// If there is a bit missing at the front, make a request for that.
		if start < extent.Start {
			r := req.WithStartEnd(start, extent.Start)
			requests = append(requests, r)
		}
		res, err := extent.toResponse()
		if err != nil {
			return nil, nil, err
		}
		// extract the overlap from the cached extent.
		cachedResponses = append(cachedResponses, s.extractor.Extract(start, req.GetEnd(), res, extent.GetStart(), extent.GetEnd()))
		start = extent.End
	}

	// Lastly, make a request for any data missing at the end.
	if start < req.GetEnd() {
		r := req.WithStartEnd(start, req.GetEnd())
		requests = append(requests, r)
	}

	// If start and end are the same (valid in promql), start == req.GetEnd() and we won't do the query.
	// But we should only do the request if we don't have a valid cached response for it.
	if req.GetStart() == req.GetEnd() && len(cachedResponses) == 0 {
		requests = append(requests, req)
	}

	return requests, cachedResponses, nil
}

func (s resultsCache) filterRecentExtents(req Request, maxCacheFreshness time.Duration, extents []Extent) ([]Extent, error) {
	step := math.Max64(1, req.GetStep())
	maxCacheTime := (int64(model.Now().Add(-maxCacheFreshness)) / step) * step
	for i := range extents {
		// Never cache data for the latest freshness period.
		if extents[i].End > maxCacheTime {
			extents[i].End = maxCacheTime
			res, err := extents[i].toResponse()
			if err != nil {
				return nil, err
			}
			extracted := s.extractor.Extract(extents[i].GetStart(), maxCacheTime, res, extents[i].GetStart(), extents[i].GetEnd())
			any, err := types.MarshalAny(extracted)
			if err != nil {
				return nil, err
			}
			extents[i].Response = any
		}
	}
	return extents, nil
}

func (s resultsCache) get(ctx context.Context, key string) ([]Extent, bool) {
	found, bufs, _, _ := s.cache.Fetch(ctx, []string{cache.HashKey(key)})
	if len(found) != 1 {
		return nil, false
	}

	var resp CachedResponse
	sp, ctx := opentracing.StartSpanFromContext(ctx, "unmarshal-extent") //nolint:ineffassign,staticcheck
	defer sp.Finish()
	log := spanlogger.FromContext(ctx)
	defer log.Finish()

	log.LogFields(otlog.Int("bytes", len(bufs[0])))

	if err := proto.Unmarshal(bufs[0], &resp); err != nil {
		level.Error(log).Log("msg", "error unmarshalling cached value", "err", err)
		log.Error(err)
		return nil, false
	}

	if resp.Key != key {
		return nil, false
	}

	// Refreshes the cache if it contains an old proto schema.
	for _, e := range resp.Extents {
		if e.Response == nil {
			return nil, false
		}
	}

	return resp.Extents, true
}

func (s resultsCache) put(ctx context.Context, key string, extents []Extent) {
	buf, err := proto.Marshal(&CachedResponse{
		Key:     key,
		Extents: extents,
	})
	if err != nil {
		level.Error(s.logger).Log("msg", "error marshalling cached value", "err", err)
		return
	}

	_ = s.cache.Store(ctx, []string{cache.HashKey(key)}, [][]byte{buf})
}

func jaegerTraceID(ctx context.Context) string {
	span := opentracing.SpanFromContext(ctx)
	if span == nil {
		return ""
	}

	spanContext, ok := span.Context().(jaeger.SpanContext)
	if !ok {
		return ""
	}

	return spanContext.TraceID().String()
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

func (e *Extent) toResponse() (Response, error) {
	msg, err := types.EmptyAny(e.Response)
	if err != nil {
		return nil, err
	}

	if err := types.UnmarshalAny(e.Response, msg); err != nil {
		return nil, err
	}

	resp, ok := msg.(Response)
	if !ok {
		return nil, fmt.Errorf("bad cached type")
	}
	return resp, nil
}
