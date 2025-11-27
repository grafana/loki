package queryrange

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

// LogResultCacheMetrics is the metrics wrapper used in log result cache.
type LogResultCacheMetrics struct {
	CacheHit  prometheus.Counter
	CacheMiss prometheus.Counter
}

// NewLogResultCacheMetrics creates metrics to be used in log result cache.
func NewLogResultCacheMetrics(registerer prometheus.Registerer) *LogResultCacheMetrics {
	return &LogResultCacheMetrics{
		CacheHit: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "query_frontend_log_result_cache_hit_total",
		}),
		CacheMiss: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "query_frontend_log_result_cache_miss_total",
		}),
	}
}

// NewLogResultCache creates a new log result cache middleware.
// Currently it only caches empty filter queries, this is because those are usually easily and freely cacheable.
// Log hits are difficult to handle because of the limit query parameter and the size of the response.
// In the future it could be extended to cache non-empty query results.
// see https://docs.google.com/document/d/1_mACOpxdWZ5K0cIedaja5gzMbv-m0lUVazqZd2O4mEU/edit
func NewLogResultCache(logger log.Logger, limits Limits, cache cache.Cache, shouldCache queryrangebase.ShouldCacheFn,
	transformer UserIDTransformer, metrics *LogResultCacheMetrics) queryrangebase.Middleware {
	if metrics == nil {
		metrics = NewLogResultCacheMetrics(nil)
	}
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return &logResultCache{
			next:        next,
			limits:      limits,
			cache:       cache,
			logger:      logger,
			shouldCache: shouldCache,
			transformer: transformer,
			metrics:     metrics,
		}
	})
}

type logResultCache struct {
	next        queryrangebase.Handler
	limits      Limits
	cache       cache.Cache
	shouldCache queryrangebase.ShouldCacheFn
	transformer UserIDTransformer

	metrics *LogResultCacheMetrics
	logger  log.Logger
}

func (l *logResultCache) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	ctx, sp := tracer.Start(ctx, "logResultCache.Do")
	defer sp.End()

	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}

	if l.shouldCache != nil && !l.shouldCache(ctx, req) {
		return l.next.Do(ctx, req)
	}

	cacheFreshnessCapture := func(id string) time.Duration { return l.limits.MaxCacheFreshness(ctx, id) }
	maxCacheFreshness := validation.MaxDurationPerTenant(tenantIDs, cacheFreshnessCapture)
	maxCacheTime := int64(model.Now().Add(-maxCacheFreshness))
	if req.GetEnd().UnixMilli() > maxCacheTime {
		return l.next.Do(ctx, req)
	}

	lokiReq, ok := req.(*LokiRequest)
	if !ok {
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "invalid request type %T", req)
	}

	interval := validation.SmallestPositiveNonZeroDurationPerTenant(tenantIDs, l.limits.QuerySplitDuration)
	// skip caching by if interval is unset
	// skip caching when limit is 0 as it would get registerted as empty result in the cache even if that time range contains log lines.
	if interval == 0 || lokiReq.Limit == 0 {
		return l.next.Do(ctx, req)
	}
	// The first subquery might not be aligned.
	alignedStart := time.Unix(0, lokiReq.GetStartTs().UnixNano()-(lokiReq.GetStartTs().UnixNano()%interval.Nanoseconds()))
	// generate the cache key based on query, tenant and start time.

	transformedTenantIDs := tenantIDs
	if l.transformer != nil {
		transformedTenantIDs = make([]string, 0, len(tenantIDs))

		for _, tenantID := range tenantIDs {
			transformedTenantIDs = append(transformedTenantIDs, l.transformer(ctx, tenantID))
		}
	}

	cacheKey := fmt.Sprintf("log:%s:%s:%d:%d", tenant.JoinTenantIDs(transformedTenantIDs), req.GetQuery(), interval.Nanoseconds(), alignedStart.UnixNano()/(interval.Nanoseconds()))
	if httpreq.ExtractHeader(ctx, httpreq.LokiDisablePipelineWrappersHeader) == "true" {
		cacheKey = "pipeline-disabled:" + cacheKey
	}

	// Check if caching of non-empty responses is enabled
	// Use the most restrictive setting across tenants (all must be enabled)
	storeNonEmptyEnabled := true
	for _, tenantID := range tenantIDs {
		if !l.limits.LogResultCacheStoreNonEmptyResponse(ctx, tenantID) {
			storeNonEmptyEnabled = false
			break
		}
	}

	// Use cache key for empty results (backward compatible) and :resp suffix for small non-empty responses
	// Include limit in response cache key to ensure we only use cached responses with the same limit
	responseCacheKey := fmt.Sprintf("%s:resp:%d", cacheKey, lokiReq.Limit)

	// Determine which cache keys to check
	cacheKeys := []string{cache.HashKey(cacheKey)}
	if storeNonEmptyEnabled {
		cacheKeys = append(cacheKeys, cache.HashKey(responseCacheKey))
	}

	_, buffs, _, err := l.cache.Fetch(ctx, cacheKeys)
	if err != nil {
		level.Warn(l.logger).Log("msg", "error fetching cache", "err", err, "cacheKey", cacheKey, "responseKey", responseCacheKey)
		return l.next.Do(ctx, req)
	}
	// we expect one or two keys to be found or missing.
	if len(buffs) > len(cacheKeys) {
		level.Warn(l.logger).Log("msg", "unexpected length of cache return values", "buffs", len(buffs), "expected", len(cacheKeys))
		return l.next.Do(ctx, req)
	}

	cacheKeyHit := len(buffs) > 0 && len(buffs[0]) > 0
	responseHit := storeNonEmptyEnabled && len(buffs) > 1 && len(buffs[1]) > 0

	if responseHit {
		// Response key hit - we have a cached response (small non-empty result)
		// We also need the request for time range, so check cache key
		if !cacheKeyHit {
			// Response hit but cache key miss - this shouldn't happen, but handle gracefully
			level.Warn(l.logger).Log("msg", "response cache hit without cache key hit", "responseKey", responseCacheKey)
			return l.next.Do(ctx, req)
		}

		var cachedResponse LokiResponse
		err = proto.Unmarshal(buffs[1], &cachedResponse)
		if err != nil {
			level.Warn(l.logger).Log("msg", "error unmarshalling response from cache", "err", err)
			return l.next.Do(ctx, req)
		}

		var cachedRequest LokiRequest
		err = proto.Unmarshal(buffs[0], &cachedRequest)
		if err != nil {
			level.Warn(l.logger).Log("msg", "error unmarshalling request from cache", "err", err)
			return l.next.Do(ctx, req)
		}

		return l.handleResponseHit(ctx, cacheKey, responseCacheKey, &cachedResponse, &cachedRequest, lokiReq)
	}

	if cacheKeyHit {
		// Cache key hit - empty result (backward compatible)
		var cachedRequest LokiRequest
		err = proto.Unmarshal(buffs[0], &cachedRequest)
		if err != nil {
			level.Warn(l.logger).Log("msg", "error unmarshalling request from cache", "err", err)
			return l.next.Do(ctx, req)
		}

		return l.handleEmptyResponseHit(ctx, cacheKey, &cachedRequest, lokiReq)
	}

	// cache miss
	return l.handleMiss(ctx, cacheKey, responseCacheKey, lokiReq)
}

func (l *logResultCache) handleMiss(ctx context.Context, cacheKey, responseCacheKey string, req *LokiRequest) (queryrangebase.Response, error) {
	l.metrics.CacheMiss.Inc()
	level.Debug(l.logger).Log("msg", "cache miss", "cacheKey", cacheKey, "responseKey", responseCacheKey)
	resp, err := l.next.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	lokiRes, ok := resp.(*LokiResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T", resp)
	}

	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return resp, nil
	}

	// Check if response is empty - cache as request using base key (backward compatibility)
	if isEmpty(lokiRes) {
		data, err := proto.Marshal(req)
		if err != nil {
			level.Warn(l.logger).Log("msg", "error marshalling request", "err", err)
			return resp, nil
		}
		// cache the request (empty result) using cache key
		err = l.cache.Store(ctx, []string{cache.HashKey(cacheKey)}, [][]byte{data})
		if err != nil {
			level.Warn(l.logger).Log("msg", "error storing cache", "err", err)
		}
		return resp, nil
	}

	// Check if caching of non-empty responses is enabled
	// Use the most restrictive setting across tenants (all must be enabled)
	storeNonEmptyCapture := func(id string) bool { return l.limits.LogResultCacheStoreNonEmptyResponse(ctx, id) }
	storeNonEmptyEnabled := validation.AllTruePerTenant(tenantIDs, storeNonEmptyCapture)

	if storeNonEmptyEnabled {
		// Check if response is small enough to cache
		// Use the minimum (most restrictive) limit across tenants
		// 0 means unlimited
		maxCacheSizeCapture := func(id string) int { return l.limits.LogResultCacheMaxResponseSize(ctx, id) }
		maxCacheSize := validation.SmallestPositiveNonZeroIntPerTenant(tenantIDs, maxCacheSizeCapture)

		shouldCache := true
		if maxCacheSize > 0 {
			// Check size only if at least one tenant has a non-zero limit
			responseSize := proto.Size(lokiRes)
			shouldCache = responseSize <= maxCacheSize
		}
		// If no tenant has a limit (all are 0), cache regardless of size (unlimited)

		if shouldCache {
			// Cache both the request (for time range, using cache key) and the response (using :resp key)
			requestData, err := proto.Marshal(req)
			if err != nil {
				level.Warn(l.logger).Log("msg", "error marshalling request", "err", err)
				return resp, nil
			}
			responseData, err := proto.Marshal(lokiRes)
			if err != nil {
				level.Warn(l.logger).Log("msg", "error marshalling response", "err", err)
				return resp, nil
			}
			// Store both keys: cache key for request, :resp key for response
			err = l.cache.Store(ctx, []string{cache.HashKey(cacheKey), cache.HashKey(responseCacheKey)}, [][]byte{requestData, responseData})
			if err != nil {
				level.Warn(l.logger).Log("msg", "error storing cache", "err", err)
			}
			return resp, nil
		}
	}

	// Response is not empty and too large to cache
	return resp, nil
}

func (l *logResultCache) handleEmptyResponseHit(ctx context.Context, cacheKey string, cachedRequest *LokiRequest, lokiReq *LokiRequest) (queryrangebase.Response, error) {
	l.metrics.CacheHit.Inc()
	// we start with an empty response
	result := emptyResponse(lokiReq)
	// if the request is the same and cover the whole time range,
	// we can just return the cached result.
	if cachedRequest.StartTs.UnixNano() <= lokiReq.StartTs.UnixNano() && cachedRequest.EndTs.UnixNano() >= lokiReq.EndTs.UnixNano() {
		return result, nil
	}

	updateCache := false
	// if the query does not overlap cached interval, do not try to fill the gap since it requires extending the queries beyond what is requested in the query.
	// Extending the queries beyond what is requested could result in empty responses due to response limit set in the queries.
	if !overlap(lokiReq.StartTs, lokiReq.EndTs, cachedRequest.StartTs, cachedRequest.EndTs) {
		resp, err := l.next.Do(ctx, lokiReq)
		if err != nil {
			return nil, err
		}
		result = resp.(*LokiResponse)

		// if the response is empty and the query is larger than what is cached, update the cache
		if isEmpty(result) && (lokiReq.EndTs.UnixNano()-lokiReq.StartTs.UnixNano() > cachedRequest.EndTs.UnixNano()-cachedRequest.StartTs.UnixNano()) {
			cachedRequest = cachedRequest.WithStartEnd(lokiReq.GetStartTs(), lokiReq.GetEndTs()).(*LokiRequest)
			updateCache = true
		}
	} else {
		// we could be missing data at the start and the end.
		// so we're going to fetch what is missing.
		var (
			startRequest, endRequest *LokiRequest
			startResp, endResp       *LokiResponse
		)
		g, ctx := errgroup.WithContext(ctx)

		// if we're missing data at the start, start fetching from the start to the cached start.
		if lokiReq.GetStartTs().Before(cachedRequest.GetStartTs()) {
			g.Go(func() error {
				startRequest = lokiReq.WithStartEnd(lokiReq.GetStartTs(), cachedRequest.GetStartTs()).(*LokiRequest)
				resp, err := l.next.Do(ctx, startRequest)
				if err != nil {
					return err
				}
				var ok bool
				startResp, ok = resp.(*LokiResponse)
				if !ok {
					return fmt.Errorf("unexpected response type %T", resp)
				}
				return nil
			})
		}

		// if we're missing data at the end, start fetching from the cached end to the end.
		if lokiReq.GetEndTs().After(cachedRequest.GetEndTs()) {
			g.Go(func() error {
				endRequest = lokiReq.WithStartEnd(cachedRequest.GetEndTs(), lokiReq.GetEndTs()).(*LokiRequest)
				resp, err := l.next.Do(ctx, endRequest)
				if err != nil {
					return err
				}
				var ok bool
				endResp, ok = resp.(*LokiResponse)
				if !ok {
					return fmt.Errorf("unexpected response type %T", resp)
				}
				return nil
			})
		}

		if err := g.Wait(); err != nil {
			return nil, err
		}

		// if we have data at the start, we need to merge it with the cached data if it's empty and update the cache.
		// If it's not empty only merge the response.
		if startResp != nil {
			if isEmpty(startResp) {
				cachedRequest = cachedRequest.WithStartEnd(startRequest.GetStartTs(), cachedRequest.GetEndTs()).(*LokiRequest)
				updateCache = true
			} else {
				if startResp.Status != loghttp.QueryStatusSuccess {
					return startResp, nil
				}
				result = mergeLokiResponse(startResp, result)
			}
		}

		// if we have data at the end, we need to merge it with the cached data if it's empty and update the cache.
		// If it's not empty only merge the response.
		if endResp != nil {
			if isEmpty(endResp) {
				cachedRequest = cachedRequest.WithStartEnd(cachedRequest.GetStartTs(), endRequest.GetEndTs()).(*LokiRequest)
				updateCache = true
			} else {
				if endResp.Status != loghttp.QueryStatusSuccess {
					return endResp, nil
				}
				result = mergeLokiResponse(endResp, result)
			}
		}
	}

	// we need to update the cache since we fetched more either at the end or the start and it was empty.
	if updateCache {
		data, err := proto.Marshal(cachedRequest)
		if err != nil {
			level.Warn(l.logger).Log("msg", "error marshalling request", "err", err)
			return result, err
		}
		// cache the result
		err = l.cache.Store(ctx, []string{cache.HashKey(cacheKey)}, [][]byte{data})
		if err != nil {
			level.Warn(l.logger).Log("msg", "error storing cache", "err", err)
		}
	}
	return result, nil
}

func (l *logResultCache) handleResponseHit(ctx context.Context, cacheKey, responseCacheKey string, cachedResponse *LokiResponse, cachedRequest *LokiRequest, lokiReq *LokiRequest) (queryrangebase.Response, error) {
	l.metrics.CacheHit.Inc()
	// Check if the cached response covers the entire requested time range
	if cachedRequest.StartTs.UnixNano() <= lokiReq.StartTs.UnixNano() && cachedRequest.EndTs.UnixNano() >= lokiReq.EndTs.UnixNano() {
		// Extract the relevant portion if needed
		if cachedRequest.StartTs.UnixNano() < lokiReq.StartTs.UnixNano() || cachedRequest.EndTs.UnixNano() > lokiReq.EndTs.UnixNano() {
			extracted := extractLokiResponse(lokiReq.GetStartTs(), lokiReq.GetEndTs(), cachedResponse)
			return extracted, nil
		}
		return cachedResponse, nil
	}

	// The cached response doesn't fully cover the request, need to fetch missing parts
	// Check if there's any overlap
	if !overlap(lokiReq.StartTs, lokiReq.EndTs, cachedRequest.StartTs, cachedRequest.EndTs) {
		// No overlap - fetch the entire request
		resp, err := l.next.Do(ctx, lokiReq)
		if err != nil {
			return nil, err
		}
		return resp, nil
	}

	// There's overlap - extract the overlapping part and fetch missing parts
	var (
		startRequest, endRequest *LokiRequest
		startResp, endResp       *LokiResponse
	)
	g, ctx := errgroup.WithContext(ctx)

	// If we're missing data at the start, fetch from the start to the cached start
	if lokiReq.GetStartTs().Before(cachedRequest.GetStartTs()) {
		g.Go(func() error {
			startRequest = lokiReq.WithStartEnd(lokiReq.GetStartTs(), cachedRequest.GetStartTs()).(*LokiRequest)
			resp, err := l.next.Do(ctx, startRequest)
			if err != nil {
				return err
			}
			var ok bool
			startResp, ok = resp.(*LokiResponse)
			if !ok {
				return fmt.Errorf("unexpected response type %T", resp)
			}
			return nil
		})
	}

	// If we're missing data at the end, fetch from the cached end to the end
	if lokiReq.GetEndTs().After(cachedRequest.GetEndTs()) {
		g.Go(func() error {
			endRequest = lokiReq.WithStartEnd(cachedRequest.GetEndTs(), lokiReq.GetEndTs()).(*LokiRequest)
			resp, err := l.next.Do(ctx, endRequest)
			if err != nil {
				return err
			}
			var ok bool
			endResp, ok = resp.(*LokiResponse)
			if !ok {
				return fmt.Errorf("unexpected response type %T", resp)
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Extract the overlapping portion from the cached response
	overlapStart := lokiReq.GetStartTs()
	if cachedRequest.GetStartTs().After(overlapStart) {
		overlapStart = cachedRequest.GetStartTs()
	}
	overlapEnd := lokiReq.GetEndTs()
	if cachedRequest.GetEndTs().Before(overlapEnd) {
		overlapEnd = cachedRequest.GetEndTs()
	}
	extractedCached := extractLokiResponse(overlapStart, overlapEnd, cachedResponse)

	// Merge responses: start (if any) + cached overlap + end (if any)
	var result *LokiResponse
	if startResp != nil {
		if startResp.Status != loghttp.QueryStatusSuccess {
			return startResp, nil
		}
		result = startResp
		if extractedCached != nil {
			result = mergeLokiResponse(result, extractedCached)
		}
	} else {
		result = extractedCached
	}

	if endResp != nil {
		if endResp.Status != loghttp.QueryStatusSuccess {
			return endResp, nil
		}
		if result != nil {
			result = mergeLokiResponse(result, endResp)
		} else {
			result = endResp
		}
	}

	if result == nil {
		// Fallback to empty response if somehow we have nothing
		result = emptyResponse(lokiReq)
	}

	// Try to update cache if the merged response is still cacheable and covers a larger range
	// Note: We don't need to check if the feature is enabled here because we're only in this function
	// if we already have a cached response, which means the feature was enabled when it was cached.
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err == nil {
		maxCacheSizeCapture := func(id string) int { return l.limits.LogResultCacheMaxResponseSize(ctx, id) }
		maxCacheSize := validation.SmallestPositiveNonZeroIntPerTenant(tenantIDs, maxCacheSizeCapture)

		shouldCache := true
		if maxCacheSize > 0 {
			responseSize := proto.Size(result)
			shouldCache = responseSize <= maxCacheSize
		}

		// Update cache if the merged response is cacheable and covers a larger range
		if shouldCache {
			newCachedStart := lokiReq.GetStartTs()
			newCachedEnd := lokiReq.GetEndTs()

			// Only update if we're extending the cached range
			if newCachedStart.Before(cachedRequest.GetStartTs()) || newCachedEnd.After(cachedRequest.GetEndTs()) {
				newCachedRequest := cachedRequest.WithStartEnd(newCachedStart, newCachedEnd).(*LokiRequest)
				requestData, err := proto.Marshal(newCachedRequest)
				if err != nil {
					level.Warn(l.logger).Log("msg", "error marshalling request for cache update", "err", err)
				} else {
					responseData, err := proto.Marshal(result)
					if err != nil {
						level.Warn(l.logger).Log("msg", "error marshalling response for cache update", "err", err)
					} else {
						err = l.cache.Store(ctx, []string{cache.HashKey(cacheKey), cache.HashKey(responseCacheKey)}, [][]byte{requestData, responseData})
						if err != nil {
							level.Warn(l.logger).Log("msg", "error updating cache", "err", err)
						}
					}
				}
			}
		}
	}

	return result, nil
}

// extractLokiResponse extracts response with interval [start, end)
func extractLokiResponse(start, end time.Time, r *LokiResponse) *LokiResponse {
	extractedResp := LokiResponse{
		Status:     r.Status,
		Direction:  r.Direction,
		Limit:      r.Limit,
		Version:    r.Version,
		ErrorType:  r.ErrorType,
		Error:      r.Error,
		Statistics: r.Statistics,
		Data: LokiData{
			ResultType: r.Data.ResultType,
			Result:     []logproto.Stream{},
		},
	}
	for _, stream := range r.Data.Result {
		if stream.Entries[0].Timestamp.After(end) || stream.Entries[len(stream.Entries)-1].Timestamp.Before(start) {
			continue
		}

		extractedStream := logproto.Stream{
			Labels:  stream.Labels,
			Entries: []logproto.Entry{},
			Hash:    stream.Hash,
		}
		for _, entry := range stream.Entries {
			if entry.Timestamp.Before(start) || entry.Timestamp.After(end) || entry.Timestamp.Equal(end) {
				continue
			}

			extractedStream.Entries = append(extractedStream.Entries, entry)
		}

		extractedResp.Data.Result = append(extractedResp.Data.Result, extractedStream)
	}

	return &extractedResp
}

func isEmpty(lokiRes *LokiResponse) bool {
	return lokiRes.Status == loghttp.QueryStatusSuccess && len(lokiRes.Data.Result) == 0
}

func emptyResponse(lokiReq *LokiRequest) *LokiResponse {
	return &LokiResponse{
		Status:     loghttp.QueryStatusSuccess,
		Statistics: stats.Result{},
		Direction:  lokiReq.Direction,
		Limit:      lokiReq.Limit,
		Version:    uint32(loghttp.GetVersion(lokiReq.Path)),
		Data: LokiData{
			ResultType: loghttp.ResultTypeStream,
			Result:     []logproto.Stream{},
		},
	}
}

func overlap(aFrom, aThrough, bFrom, bThrough time.Time) bool {
	if aFrom.After(bThrough) || bFrom.After(aThrough) {
		return false
	}

	return true
}
