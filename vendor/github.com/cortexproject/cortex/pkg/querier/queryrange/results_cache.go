package queryrange

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/common/model"
	"github.com/uber/jaeger-client-go"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/chunk/cache"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
)

// ResultsCacheConfig is the config for the results cache.
type ResultsCacheConfig struct {
	CacheConfig       cache.Config  `yaml:"cache"`
	MaxCacheFreshness time.Duration `yaml:"max_freshness"`
	SplitInterval     time.Duration `yaml:"cache_split_interval"`
}

// RegisterFlags registers flags.
func (cfg *ResultsCacheConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.CacheConfig.RegisterFlagsWithPrefix("frontend.", "", f)
	f.DurationVar(&cfg.MaxCacheFreshness, "frontend.max-cache-freshness", 1*time.Minute, "Most recent allowed cacheable result, to prevent caching very recent results that might still be in flux.")
	f.DurationVar(&cfg.SplitInterval, "frontend.cache-split-interval", 24*time.Hour, "The maximum interval expected for each request, results will be cached per single interval.")
}

// Extractor is used by the cache to extract a subset of a response from a cache entry.
type Extractor interface {
	// Extract extracts a subset of a response from the `start` and `end` timestamps in milliseconds in the `from` response.
	Extract(start, end int64, from Response) Response
}

// ExtractorFunc is a sugar syntax for declaring an Extractor from a function.
type ExtractorFunc func(start, end int64, from Response) Response

// Extract calls the `ExtractorFunc` to implements an `Extractor`.
func (e ExtractorFunc) Extract(start, end int64, from Response) Response {
	return e(start, end, from)
}

// PrometheusResponseExtractor is an `Extractor` for a Prometheus query range response.
var PrometheusResponseExtractor = ExtractorFunc(func(start, end int64, from Response) Response {
	promRes := from.(*PrometheusResponse)
	return &PrometheusResponse{
		Status: statusSuccess,
		Data: PrometheusData{
			ResultType: promRes.Data.ResultType,
			Result:     extractMatrix(start, end, promRes.Data.Result),
		},
	}
})

type resultsCache struct {
	logger log.Logger
	cfg    ResultsCacheConfig
	next   Handler
	cache  cache.Cache
	limits Limits

	extractor Extractor
	merger    Merger
}

// NewResultsCacheMiddleware creates results cache middleware from config.
// The middleware cache result using a unique cache key for a given request (step,query,user) and interval.
// The cache assumes that each request length (end-start) is below or equal the interval.
// Each request starting from within the same interval will hit the same cache entry.
// If the cache doesn't have the entire duration of the request cached, it will query the uncached parts and append them to the cache entries.
// see `generateKey`.
func NewResultsCacheMiddleware(logger log.Logger, cfg ResultsCacheConfig, limits Limits, merger Merger, extractor Extractor) (Middleware, cache.Cache, error) {
	c, err := cache.New(cfg.CacheConfig)
	if err != nil {
		return nil, nil, err
	}

	return MiddlewareFunc(func(next Handler) Handler {
		return &resultsCache{
			logger:    logger,
			cfg:       cfg,
			next:      next,
			cache:     c,
			limits:    limits,
			merger:    merger,
			extractor: extractor,
		}
	}), c, nil
}

func (s resultsCache) Do(ctx context.Context, r Request) (Response, error) {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	var (
		key      = generateKey(userID, r, s.cfg.SplitInterval)
		extents  []Extent
		response Response
	)

	maxCacheTime := int64(model.Now().Add(-s.cfg.MaxCacheFreshness))
	if r.GetStart() > maxCacheTime {
		return s.next.Do(ctx, r)
	}

	cached, ok := s.get(ctx, key)
	if ok {
		response, extents, err = s.handleHit(ctx, r, cached)
	} else {
		response, extents, err = s.handleMiss(ctx, r)
	}

	if err == nil && len(extents) > 0 {
		extents, err := s.filterRecentExtents(r, extents)
		if err != nil {
			return nil, err
		}
		s.put(ctx, key, extents)
	}

	return response, err
}

func (s resultsCache) handleMiss(ctx context.Context, r Request) (Response, []Extent, error) {
	response, err := s.next.Do(ctx, r)
	if err != nil {
		return nil, nil, err
	}

	extent, err := toExtent(ctx, r, response)
	if err != nil {
		return nil, nil, err
	}

	extents := []Extent{
		extent,
	}
	return response, extents, nil
}

func (s resultsCache) handleHit(ctx context.Context, r Request, extents []Extent) (Response, []Extent, error) {
	var (
		reqResps []RequestResponse
		err      error
	)
	log, ctx := spanlogger.New(ctx, "handleHit")
	defer log.Finish()

	requests, responses, err := partition(r, extents, s.extractor)
	if err != nil {
		return nil, nil, err
	}
	if len(requests) == 0 {
		response, err := s.merger.MergeResponse(responses...)
		// No downstream requests so no need to write back to the cache.
		return response, nil, err
	}

	reqResps, err = DoRequests(ctx, s.next, requests, s.limits)
	if err != nil {
		return nil, nil, err
	}

	for _, reqResp := range reqResps {
		responses = append(responses, reqResp.Response)
		extent, err := toExtent(ctx, reqResp.Request, reqResp.Response)
		if err != nil {
			return nil, nil, err
		}
		extents = append(extents, extent)
	}
	sort.Slice(extents, func(i, j int) bool {
		return extents[i].Start < extents[j].Start
	})

	// Merge any extents - they're guaranteed not to overlap.
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
func partition(req Request, extents []Extent, extractor Extractor) ([]Request, []Response, error) {
	var requests []Request
	var cachedResponses []Response
	start := req.GetStart()

	for _, extent := range extents {
		// If there is no overlap, ignore this extent.
		if extent.GetEnd() < start || extent.Start > req.GetEnd() {
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
		cachedResponses = append(cachedResponses, extractor.Extract(start, req.GetEnd(), res))
		start = extent.End
	}

	if start < req.GetEnd() {
		r := req.WithStartEnd(start, req.GetEnd())
		requests = append(requests, r)
	}

	return requests, cachedResponses, nil
}

func (s resultsCache) filterRecentExtents(req Request, extents []Extent) ([]Extent, error) {
	maxCacheTime := (int64(model.Now().Add(-s.cfg.MaxCacheFreshness)) / req.GetStep()) * req.GetStep()
	for i := range extents {
		// Never cache data for the latest freshness period.
		if extents[i].End > maxCacheTime {
			extents[i].End = maxCacheTime
			res, err := extents[i].toResponse()
			if err != nil {
				return nil, err
			}
			extracted := s.extractor.Extract(extents[i].Start, maxCacheTime, res)
			any, err := types.MarshalAny(extracted)
			if err != nil {
				return nil, err
			}
			extents[i].Response = any
		}
	}
	return extents, nil
}

// generateKey generates a cache key based on the userID, Request and interval.
func generateKey(userID string, r Request, interval time.Duration) string {
	currentInterval := r.GetStart() / int64(interval/time.Millisecond)
	return fmt.Sprintf("%s:%s:%d:%d", userID, r.GetQuery(), r.GetStep(), currentInterval)
}

func (s resultsCache) get(ctx context.Context, key string) ([]Extent, bool) {
	found, bufs, _ := s.cache.Fetch(ctx, []string{cache.HashKey(key)})
	if len(found) != 1 {
		return nil, false
	}

	var resp CachedResponse
	sp, _ := opentracing.StartSpanFromContext(ctx, "unmarshal-extent")
	defer sp.Finish()

	sp.LogFields(otlog.Int("bytes", len(bufs[0])))

	if err := proto.Unmarshal(bufs[0], &resp); err != nil {
		level.Error(s.logger).Log("msg", "error unmarshalling cached value", "err", err)
		sp.LogFields(otlog.Error(err))
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

	s.cache.Store(ctx, []string{cache.HashKey(key)}, [][]byte{buf})
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
		Samples: make([]client.Sample, 0, len(stream.Samples)),
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
