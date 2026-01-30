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

const (
	hitKindEmpty    = "empty"
	hitKindNonEmpty = "non-empty"
)

// LogResultCacheMetrics is the metrics wrapper used in log result cache.
type LogResultCacheMetrics struct {
	CacheHit  *prometheus.CounterVec
	CacheMiss prometheus.Counter
}

// NewLogResultCacheMetrics creates metrics to be used in log result cache.
func NewLogResultCacheMetrics(registerer prometheus.Registerer) *LogResultCacheMetrics {
	return &LogResultCacheMetrics{
		CacheHit: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "query_frontend_log_result_cache_hit_total",
		}, []string{"hit_kind"}),
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
	storeNonEmptyCapture := func(id string) bool { return l.limits.LogResultCacheStoreNonEmptyResponse(ctx, id) }
	storeNonEmptyEnabled := validation.AllTruePerTenant(tenantIDs, storeNonEmptyCapture)

	// Use cache key for empty results (backward compatible) and :resp suffix for small non-empty responses
	// Include limit and direction in response cache key to ensure we only use cached responses with the same limit and direction
	responseCacheKey := fmt.Sprintf("%s:resp:%d:%d", cacheKey, lokiReq.Limit, lokiReq.Direction)
	// Marker cache key to indicate a non-empty response exists for ANY limit and ANY direction (without limit or direction suffix)
	markerCacheKey := fmt.Sprintf("%s:resp", cacheKey)

	// Determine which cache keys to check
	cacheKeys := []string{cache.HashKey(cacheKey)}
	if storeNonEmptyEnabled {
		cacheKeys = append(cacheKeys, cache.HashKey(markerCacheKey), cache.HashKey(responseCacheKey))
	}

	_, buffs, _, err := l.cache.Fetch(ctx, cacheKeys)
	if err != nil {
		level.Warn(l.logger).Log("msg", "error fetching cache", "err", err, "cacheKey", cacheKey, "responseKey", responseCacheKey)
		return l.next.Do(ctx, req)
	}

	// Track cache request for positive result cache
	stats.FromContext(ctx).AddCacheEntriesRequested(stats.PositiveResultCache, 1)

	// we expect one, two, or three keys to be found or missing.
	if len(buffs) > len(cacheKeys) {
		level.Warn(l.logger).Log("msg", "unexpected length of cache return values", "buffs", len(buffs), "expected", len(cacheKeys))
		return l.next.Do(ctx, req)
	}

	cacheKeyHit := len(buffs) > 0 && len(buffs[0]) > 0
	markerHit := storeNonEmptyEnabled && len(buffs) > 1 && len(buffs[1]) > 0
	responseHit := storeNonEmptyEnabled && len(buffs) > 2 && len(buffs[2]) > 0

	if responseHit {
		// Response key hit - we have a cached response (small non-empty result)
		// We also need the request for time range, so check cache key
		if !cacheKeyHit {
			// Response hit but cache key miss - this shouldn't happen, but handle gracefully
			level.Warn(l.logger).Log("msg", "response cache hit without cache key hit", "responseKey", responseCacheKey)
			return l.next.Do(ctx, req)
		}

		var cachedResponse LokiResponse
		err = proto.Unmarshal(buffs[2], &cachedResponse)
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

		// Track bytes retrieved for non-empty response cache hit (request and response)
		statsCtx := stats.FromContext(ctx)
		statsCtx.AddCacheBytesRetrieved(stats.PositiveResultCache, len(buffs[0])+len(buffs[2]))

		return l.handleResponseHit(ctx, cacheKey, responseCacheKey, &cachedResponse, &cachedRequest, lokiReq)
	}

	if cacheKeyHit {
		// Unmarshal cached request to check for overlap
		var cachedRequest LokiRequest
		err = proto.Unmarshal(buffs[0], &cachedRequest)
		if err != nil {
			level.Warn(l.logger).Log("msg", "error unmarshalling request from cache", "err", err)
			return l.next.Do(ctx, req)
		}

		// Check if marker exists - if it does, we have a non-empty response cached for a different limit
		// In this case, we cannot treat it as an empty response and must fetch from next handler
		if markerHit && !responseHit {
			// Marker exists but response for this limit doesn't - non-empty response cached for different limit
			// We handle this as a miss so the cache is populated with a non-empty response for this limit.
			return l.handleMiss(ctx, cacheKey, responseCacheKey, lokiReq)
		}

		// Cache key hit - empty result (backward compatible)
		return l.handleEmptyResponseHit(ctx, cacheKey, responseCacheKey, &cachedRequest, lokiReq, storeNonEmptyEnabled)
	}

	// cache miss
	return l.handleMiss(ctx, cacheKey, responseCacheKey, lokiReq)
}

func (l *logResultCache) handleMiss(ctx context.Context, cacheKey, responseCacheKey string, req *LokiRequest) (queryrangebase.Response, error) {
	l.metrics.CacheMiss.Inc()

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

		// Cache if unlimited or response is small enough
		shouldCache := maxCacheSize == 0 || proto.Size(lokiRes) <= maxCacheSize

		if shouldCache {
			// Adjust time range if response hit the limit
			adjStart, adjEnd := adjustCacheTimeRange(req, lokiRes)
			adjustedReq := req.WithStartEnd(adjStart, adjEnd).(*LokiRequest)

			// Cache both the request (for time range, using cache key) and the response (using :resp key)
			requestData, err := proto.Marshal(adjustedReq)
			if err != nil {
				level.Warn(l.logger).Log("msg", "error marshalling request", "err", err)
				return resp, nil
			}
			responseData, err := proto.Marshal(lokiRes)
			if err != nil {
				level.Warn(l.logger).Log("msg", "error marshalling response", "err", err)
				return resp, nil
			}
			// Marker cache key to indicate a non-empty response exists for ANY limit and ANY direction
			markerCacheKey := fmt.Sprintf("%s:resp", cacheKey)
			// Marker value - just needs to exist, use minimal value
			markerValue := []byte{1}
			// Store three keys: cache key for request, marker key, :resp:limit key for response
			err = l.cache.Store(ctx, []string{cache.HashKey(cacheKey), cache.HashKey(markerCacheKey), cache.HashKey(responseCacheKey)}, [][]byte{requestData, markerValue, responseData})
			if err != nil {
				level.Warn(l.logger).Log("msg", "error storing cache", "err", err)
			}

			return resp, nil
		}
	}

	// Response is not empty and too large to cache
	return resp, nil
}

func (l *logResultCache) handleEmptyResponseHit(ctx context.Context, cacheKey, responseCacheKey string, cachedRequest *LokiRequest, lokiReq *LokiRequest, storeNonEmptyEnabled bool) (queryrangebase.Response, error) {
	l.metrics.CacheHit.WithLabelValues(hitKindEmpty).Inc()

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

	// If the merged result is non-empty (e.g., endResp had data), update cache with marker and response cache key
	// This allows future requests to treat it as a non-empty cached response
	// If a future request's end matches no log lines, extractLokiResponse will handle returning an empty response
	if storeNonEmptyEnabled && !isEmpty(result) {
		// Adjust time range if response hit the limit
		adjStart, adjEnd := adjustCacheTimeRange(lokiReq, result)
		newCachedRequest := lokiReq.WithStartEnd(adjStart, adjEnd).(*LokiRequest)
		l.updateCacheWithNonEmptyResponse(ctx, cacheKey, responseCacheKey, newCachedRequest, result)
		return result, nil
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

// updateCacheWithNonEmptyResponse updates the cache with a non-empty response, storing the request,
// marker key, and response cache key. This is used when a previously empty cached response becomes
// non-empty after fetching missing parts.
func (l *logResultCache) updateCacheWithNonEmptyResponse(ctx context.Context, cacheKey, responseCacheKey string, newCachedRequest *LokiRequest, newCachedResponse *LokiResponse) {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return
	}

	maxCacheSizeCapture := func(id string) int { return l.limits.LogResultCacheMaxResponseSize(ctx, id) }
	maxCacheSize := validation.SmallestPositiveNonZeroIntPerTenant(tenantIDs, maxCacheSizeCapture)

	// Cache if unlimited or response is small enough
	shouldCache := maxCacheSize == 0 || proto.Size(newCachedResponse) <= maxCacheSize
	if !shouldCache {
		return
	}

	requestData, reqErr := proto.Marshal(newCachedRequest)
	if reqErr != nil {
		level.Error(l.logger).Log("msg", "error marshalling request for cache update", "err", reqErr)
		return
	}

	responseData, respErr := proto.Marshal(newCachedResponse)
	if respErr != nil {
		level.Error(l.logger).Log("msg", "error marshalling response for cache update", "err", respErr)
		return
	}

	// Marker cache key to indicate a non-empty response exists for ANY limit and ANY direction
	markerCacheKey := fmt.Sprintf("%s:resp", cacheKey)
	// Marker value - just needs to exist, use minimal value
	markerValue := []byte{1}

	// Store three keys: cache key for request, marker key, :resp:limit key for response
	if err := l.cache.Store(ctx, []string{cache.HashKey(cacheKey), cache.HashKey(markerCacheKey), cache.HashKey(responseCacheKey)}, [][]byte{requestData, markerValue, responseData}); err != nil {
		level.Error(l.logger).Log("msg", "error updating cache", "err", err)
		return
	}

	// Track cache store stats for non-empty responses
	statsCtx := stats.FromContext(ctx)
	statsCtx.AddCacheEntriesStored(stats.PositiveResultCache, 1)
	statsCtx.AddCacheBytesSent(stats.PositiveResultCache, len(requestData)+len(markerValue)+len(responseData))
}

func (l *logResultCache) handleResponseHit(ctx context.Context, cacheKey, responseCacheKey string, cachedResponse *LokiResponse, cachedRequest *LokiRequest, lokiReq *LokiRequest) (queryrangebase.Response, error) {
	l.metrics.CacheHit.WithLabelValues(hitKindNonEmpty).Inc()

	// Track cache hit stats
	statsCtx := stats.FromContext(ctx)
	statsCtx.AddCacheEntriesFound(stats.PositiveResultCache, 1)

	// Check if the cached response covers the entire requested time range
	if cachedRequest.StartTs.UnixNano() <= lokiReq.StartTs.UnixNano() && cachedRequest.EndTs.UnixNano() >= lokiReq.EndTs.UnixNano() {
		result := cachedResponse

		// Extract the relevant portion if needed
		if cachedRequest.StartTs.UnixNano() < lokiReq.StartTs.UnixNano() || cachedRequest.EndTs.UnixNano() > lokiReq.EndTs.UnixNano() {
			result = extractLokiResponse(lokiReq.GetStartTs(), lokiReq.GetEndTs(), lokiReq.Direction, cachedResponse)
		}

		// The cached response may have more entries than the limit, so we need to extract only the relevant
		result.Data.Result = mergeOrderedNonOverlappingStreams([]*LokiResponse{result}, lokiReq.Limit, lokiReq.Direction)

		// Full cache hit - track query length served
		statsCtx.AddCacheQueryLengthServed(stats.PositiveResultCache, lokiReq.EndTs.Sub(lokiReq.StartTs))

		return result, nil
	}

	// The cached response doesn't fully cover the request, need to fetch missing parts
	// Check if there's any overlap
	updateCache := false
	var newCachedRequest *LokiRequest
	var newCachedResponse *LokiResponse
	var result queryrangebase.Response

	if !overlap(lokiReq.StartTs, lokiReq.EndTs, cachedRequest.StartTs, cachedRequest.EndTs) {
		// No overlap - fetch the entire request
		resp, err := l.next.Do(ctx, lokiReq)
		if err != nil {
			return nil, err
		}
		lokiResp, ok := resp.(*LokiResponse)
		if !ok {
			return resp, nil
		}

		result = resp

		// if the response is not empty and the timerange is bigger than what is cached, update the cache
		if !isEmpty(lokiResp) && (lokiReq.EndTs.UnixNano()-lokiReq.StartTs.UnixNano() > cachedRequest.EndTs.UnixNano()-cachedRequest.StartTs.UnixNano()) {
			tenantIDs, err := tenant.TenantIDs(ctx)
			if err == nil {
				maxCacheSizeCapture := func(id string) int { return l.limits.LogResultCacheMaxResponseSize(ctx, id) }
				maxCacheSize := validation.SmallestPositiveNonZeroIntPerTenant(tenantIDs, maxCacheSizeCapture)

				// Cache if unlimited or response is small enough
				// Note, if we are in this function, we already know  caching non-empty responses is enabled.
				shouldCache := maxCacheSize == 0 || proto.Size(lokiResp) <= maxCacheSize
				if shouldCache {
					// Adjust time range if response hit the limit
					adjStart, adjEnd := adjustCacheTimeRange(lokiReq, lokiResp)
					newCachedRequest = cachedRequest.WithStartEnd(adjStart, adjEnd).(*LokiRequest)
					newCachedResponse = lokiResp
					updateCache = true
				}
			}
		}
	} else {

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

		// Track query length served from cache (the overlap portion)
		if overlapEnd.After(overlapStart) {
			statsCtx.AddCacheQueryLengthServed(stats.PositiveResultCache, overlapEnd.Sub(overlapStart))
		}

		extractedCached := extractLokiResponse(overlapStart, overlapEnd, lokiReq.Direction, cachedResponse)

		// Build the user-facing response: start (if any) + cached overlap + end (if any)
		// This applies the request limit via mergeLokiResponse
		var lokiResult *LokiResponse
		if startResp != nil {
			if startResp.Status != loghttp.QueryStatusSuccess {
				return startResp, nil
			}
			lokiResult = startResp
			if extractedCached != nil {
				lokiResult = mergeLokiResponse(lokiResult, extractedCached)
			}
		} else {
			lokiResult = extractedCached
		}

		if endResp != nil {
			if endResp.Status != loghttp.QueryStatusSuccess {
				return endResp, nil
			}
			if lokiResult != nil {
				lokiResult = mergeLokiResponse(lokiResult, endResp)
			} else {
				lokiResult = endResp
			}
		}

		if lokiResult == nil {
			// Fallback to empty response if somehow we have nothing
			lokiResult = emptyResponse(lokiReq)
		}

		result = lokiResult

		// Try to update cache if the merged response is still cacheable and covers a larger range
		// Note: We don't need to check if the feature is enabled here because we're only in this function
		// if we already have a cached response, which means the feature was enabled when it was cached.
		if tenantIDs, err := tenant.TenantIDs(ctx); err == nil {
			maxCacheSizeCapture := func(id string) int { return l.limits.LogResultCacheMaxResponseSize(ctx, id) }
			maxCacheSize := validation.SmallestPositiveNonZeroIntPerTenant(tenantIDs, maxCacheSizeCapture)

			// For caching, merge ALL data without limit: startResp + FULL cachedResponse + endResp
			// This ensures we extend the cache with all available entries
			// Collect all non-nil responses to merge
			var toMerge []queryrangebase.Response
			if startResp != nil {
				toMerge = append(toMerge, startResp)
			}
			if cachedResponse != nil {
				toMerge = append(toMerge, cachedResponse)
			}
			if endResp != nil {
				toMerge = append(toMerge, endResp)
			}
			cacheResult := mergeLokiResponseWithLimit(0, toMerge...) // 0 = no limit

			// Cache if unlimited or response is small enough
			// Note, if we are in this function, we already know caching non-empty responses is enabled.
			// cacheResult is always non-nil here since we always have cachedResponse.
			shouldCache := maxCacheSize == 0 || proto.Size(cacheResult) <= maxCacheSize

			// Update cache if the merged response is cacheable and covers a larger range
			if shouldCache {
				// Calculate the extended time range.
				// For cache extension, we can extend the range if the sub-responses didn't hit the limit.
				// The cached response was already validated, so we only need to check startResp and endResp.
				extendedStart := cachedRequest.GetStartTs()
				extendedEnd := cachedRequest.GetEndTs()

				// Extend start if startResp is under limit (meaning we got all entries in that range)
				if startResp != nil && lokiReq.GetStartTs().Before(extendedStart) {
					if responseUnderRequestLimit(startResp, lokiReq.Limit) {
						extendedStart = lokiReq.GetStartTs()
					} else {
						// startResp hit limit - adjust to the actual data bounds
						minTs, _ := getResponseTimeBounds(startResp)
						extendedStart = minTs
					}
				}

				// Extend end if endResp is under limit (meaning we got all entries in that range)
				if endResp != nil && lokiReq.GetEndTs().After(extendedEnd) {
					if responseUnderRequestLimit(endResp, lokiReq.Limit) {
						extendedEnd = lokiReq.GetEndTs()
					} else {
						// endResp hit limit - adjust to the actual data bounds
						_, maxTs := getResponseTimeBounds(endResp)
						extendedEnd = maxTs
					}
				}

				// Only update if we're extending the cached range
				if extendedStart.Before(cachedRequest.GetStartTs()) || extendedEnd.After(cachedRequest.GetEndTs()) {
					newCachedRequest = cachedRequest.WithStartEnd(extendedStart, extendedEnd).(*LokiRequest)
					newCachedResponse = cacheResult
					updateCache = true
				}
			}
		}
	}

	// Update cache if needed
	if updateCache {
		l.updateCacheWithNonEmptyResponse(ctx, cacheKey, responseCacheKey, newCachedRequest, newCachedResponse)
	}

	return result, nil
}

func debugCountEntries(lokiRes *LokiResponse) int {
	if lokiRes == nil {
		return -1
	}

	var totalEntries int
	for _, stream := range lokiRes.Data.Result {
		totalEntries += len(stream.Entries)
	}
	return totalEntries
}

// extractLokiResponse extracts response with interval [start, end)
func extractLokiResponse(start, end time.Time, direction logproto.Direction, r *LokiResponse) *LokiResponse {
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
		if len(stream.Entries) == 0 {
			continue
		}

		// Determine the min and max timestamps in this stream based on entry ordering.
		// For FORWARD direction: entries are sorted oldest to newest (first=min, last=max)
		// For BACKWARD direction: entries are sorted newest to oldest (first=max, last=min)
		var streamMin, streamMax time.Time
		if direction == logproto.FORWARD {
			streamMin = stream.Entries[0].Timestamp
			streamMax = stream.Entries[len(stream.Entries)-1].Timestamp
		} else {
			streamMin = stream.Entries[len(stream.Entries)-1].Timestamp
			streamMax = stream.Entries[0].Timestamp
		}

		// Skip stream if it's entirely outside the [start, end) range
		if streamMin.After(end) || streamMin.Equal(end) || streamMax.Before(start) {
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

		if len(extractedStream.Entries) > 0 {
			extractedResp.Data.Result = append(extractedResp.Data.Result, extractedStream)
		}
	}

	return &extractedResp
}

func isEmpty(lokiRes *LokiResponse) bool {
	return lokiRes.Status == loghttp.QueryStatusSuccess && len(lokiRes.Data.Result) == 0
}

// responseUnderRequestLimit checks if the entries returned in the response is under the request limit.
func responseUnderRequestLimit(lokiRes *LokiResponse, reqLimit uint32) bool {
	var totalEntries uint32
	for _, stream := range lokiRes.Data.Result {
		totalEntries += uint32(len(stream.Entries))
	}
	return totalEntries < reqLimit
}

// getResponseTimeBounds returns the minimum and maximum timestamps across all entries in the response.
func getResponseTimeBounds(resp *LokiResponse) (minTs, maxTs time.Time) {
	first := true
	for _, stream := range resp.Data.Result {
		for _, entry := range stream.Entries {
			if first {
				minTs = entry.Timestamp
				maxTs = entry.Timestamp
				first = false
				continue
			}
			if entry.Timestamp.Before(minTs) {
				minTs = entry.Timestamp
			}
			if entry.Timestamp.After(maxTs) {
				maxTs = entry.Timestamp
			}
		}
	}
	return minTs, maxTs
}

// adjustCacheTimeRange returns the time range to cache based on actual response data.
// If the response hit the limit, the cached range is trimmed to the actual data bounds.
// For FORWARD queries that hit the limit, we cache [start, maxTs] since we may be missing newer entries.
// For BACKWARD queries that hit the limit, we cache [minTs, end] since we may be missing older entries.
func adjustCacheTimeRange(req *LokiRequest, resp *LokiResponse) (start, end time.Time) {
	// If under limit, cache the full requested range
	if responseUnderRequestLimit(resp, req.Limit) {
		return req.GetStartTs(), req.GetEndTs()
	}

	// Find min/max timestamps in response
	minTs, maxTs := getResponseTimeBounds(resp)

	// Adjust based on direction
	if req.Direction == logproto.FORWARD {
		return req.GetStartTs(), maxTs
	}
	return minTs, req.GetEndTs()
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
