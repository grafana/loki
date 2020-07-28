package queryrange

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk/cache"
	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/common/model"
	"github.com/uber/jaeger-client-go"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"
)

type lokiResultsCache struct {
	logger   log.Logger
	cfg      queryrange.ResultsCacheConfig
	next     queryrange.Handler
	cache    cache.Cache
	limits   Limits
	splitter queryrange.CacheSplitter
	merger   queryrange.Merger
}

// NewLokiResultsCacheMiddleware is a cache implementation for "series" and "label" requests
// this code is mostly taken from cortex's result_cache middleware with only few changes.
// Main idea to implement custom cache middleware is that with cortex's cache middleware
// code, we can't correctly cache "series" and "label" responses since they doesn't have a
// time range associtated with it. This custom code addresses the limitation.
func NewLokiResultsCacheMiddleware(
	logger log.Logger,
	cfg queryrange.ResultsCacheConfig,
	splitter queryrange.CacheSplitter,
	limits Limits,
	merger queryrange.Merger,
) (queryrange.Middleware, cache.Cache, error) {
	c, err := cache.New(cfg.CacheConfig)
	if err != nil {
		return nil, nil, err
	}

	return queryrange.MiddlewareFunc(func(next queryrange.Handler) queryrange.Handler {
		return &lokiResultsCache{
			logger:   logger,
			cfg:      cfg,
			next:     next,
			cache:    c,
			limits:   limits,
			merger:   merger,
			splitter: splitter,
		}
	}), c, nil
}

func (s lokiResultsCache) Do(ctx context.Context, r queryrange.Request) (queryrange.Response, error) {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	var (
		key      = s.splitter.GenerateCacheKey(userID, r)
		extents  []queryrange.Extent
		response queryrange.Response
	)

	// check if cache freshness value is provided in legacy config
	maxCacheFreshness := s.cfg.LegacyMaxCacheFreshness
	if maxCacheFreshness == time.Duration(0) {
		maxCacheFreshness = s.limits.MaxCacheFreshness(userID)
	}
	maxCacheTime := int64(model.Now().Add(-maxCacheFreshness))
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
		s.put(ctx, key, extents)
	}

	return response, err
}

func (s lokiResultsCache) handleHit(ctx context.Context, r queryrange.Request, extents []queryrange.Extent) (queryrange.Response, []queryrange.Extent, error) {
	var (
		reqResps []queryrange.RequestResponse
		err      error
	)
	log, ctx := spanlogger.New(ctx, "handleHit")
	defer log.Finish()

	requests, responses, err := partition(r, extents)
	if err != nil {
		return nil, nil, err
	}
	if len(requests) == 0 {
		response, err := s.merger.MergeResponse(responses...)
		// No downstream requests so no need to write back to the cache.
		return response, nil, err
	}

	reqResps, err = queryrange.DoRequests(ctx, s.next, requests, s.limits)
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

	// Merge any extents, they're guaranteed not to overlap.
	accumulator, err := newAccumulator(extents[0])
	if err != nil {
		return nil, nil, err
	}
	mergedExtents := make([]queryrange.Extent, 0, len(extents))

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
		currentRes, err := toResponse(extents[i])
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

// partition calculates the required requests to satisfy req given the cached data.
func partition(req queryrange.Request, extents []queryrange.Extent) ([]queryrange.Request, []queryrange.Response, error) {
	var requests []queryrange.Request
	var cachedResponses []queryrange.Response
	start := req.GetStart()

	for _, extent := range extents {
		// consider the extent only if it is within the request range
		if start <= extent.GetStart() && req.GetEnd() >= extent.GetEnd() {
			// If there is a bit missing at the front, make a request for that.
			if start < extent.Start {
				r := req.WithStartEnd(start, extent.Start)
				requests = append(requests, r)
			}

			res, err := toResponse(extent)
			if err != nil {
				return nil, nil, err
			}

			// extract the overlap from the cached extent.
			cachedResponses = append(cachedResponses, res)
			start = extent.End
		}
	}

	if start < req.GetEnd() {
		r := req.WithStartEnd(start, req.GetEnd())
		requests = append(requests, r)
	}

	return requests, cachedResponses, nil
}

func (s lokiResultsCache) handleMiss(ctx context.Context, r queryrange.Request) (queryrange.Response, []queryrange.Extent, error) {
	response, err := s.next.Do(ctx, r)
	if err != nil {
		return nil, nil, err
	}

	extent, err := toExtent(ctx, r, response)
	if err != nil {
		return nil, nil, err
	}

	extents := []queryrange.Extent{
		extent,
	}
	return response, extents, nil
}

type accumulator struct {
	queryrange.Response
	queryrange.Extent
}

func merge(extents []queryrange.Extent, acc *accumulator) ([]queryrange.Extent, error) {
	any, err := types.MarshalAny(acc.Response)
	if err != nil {
		return nil, err
	}
	return append(extents, queryrange.Extent{
		Start:    acc.Extent.Start,
		End:      acc.Extent.End,
		Response: any,
		TraceId:  acc.Extent.TraceId,
	}), nil
}

func newAccumulator(base queryrange.Extent) (*accumulator, error) {
	res, err := toResponse(base)
	if err != nil {
		return nil, err
	}
	return &accumulator{
		Response: res,
		Extent:   base,
	}, nil
}

func toExtent(ctx context.Context, req queryrange.Request, res queryrange.Response) (queryrange.Extent, error) {
	any, err := types.MarshalAny(res)
	if err != nil {
		return queryrange.Extent{}, err
	}
	return queryrange.Extent{
		Start:    req.GetStart(),
		End:      req.GetEnd(),
		Response: any,
		TraceId:  jaegerTraceID(ctx),
	}, nil
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

func (s lokiResultsCache) get(ctx context.Context, key string) ([]queryrange.Extent, bool) {
	found, bufs, _ := s.cache.Fetch(ctx, []string{cache.HashKey(key)})
	if len(found) != 1 {
		return nil, false
	}

	var resp queryrange.CachedResponse
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

func (s lokiResultsCache) put(ctx context.Context, key string, extents []queryrange.Extent) {
	buf, err := proto.Marshal(&queryrange.CachedResponse{
		Key:     key,
		Extents: extents,
	})
	if err != nil {
		level.Error(s.logger).Log("msg", "error marshalling cached value", "err", err)
		return
	}

	s.cache.Store(ctx, []string{cache.HashKey(key)}, [][]byte{buf})
}

func toResponse(e queryrange.Extent) (queryrange.Response, error) {
	msg, err := types.EmptyAny(e.Response)
	if err != nil {
		return nil, err
	}

	if err := types.UnmarshalAny(e.Response, msg); err != nil {
		return nil, err
	}

	resp, ok := msg.(queryrange.Response)
	if !ok {
		return nil, fmt.Errorf("bad cached type")
	}
	return resp, nil
}
