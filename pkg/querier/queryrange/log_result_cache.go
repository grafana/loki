package queryrange

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/go-kit/log"
	"github.com/golang/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/httpgrpc"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/tenant"
	"github.com/grafana/loki/pkg/util/validation"
)

func NewLogCacheNoResult(logger log.Logger, limits Limits, cache cache.Cache, reg prometheus.Registerer) (queryrangebase.Middleware, error) {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return &logCacheNoResult{
			next:   next,
			limits: limits,
			cache:  cache,
			logger: logger,
		}
	}), nil
}

type logCacheNoResult struct {
	next   queryrangebase.Handler
	limits Limits
	cache  cache.Cache
	logger log.Logger
}

func (l *logCacheNoResult) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	maxCacheFreshness := validation.MaxDurationPerTenant(tenantIDs, l.limits.MaxCacheFreshness)
	maxCacheTime := int64(model.Now().Add(-maxCacheFreshness))
	if req.GetStart() > maxCacheTime {
		return l.next.Do(ctx, req)
	}

	lokiReq, ok := req.(*LokiRequest)
	if !ok {
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "invalid request type %T", req)
	}

	interval := validation.SmallestPositiveNonZeroDurationPerTenant(tenantIDs, l.limits.QuerySplitDuration)
	// skip caching by if interval is unset
	if interval == 0 {
		return l.next.Do(ctx, req)
	}
	// The first subquery might not be aligned.
	alignedStart := time.Unix(0, lokiReq.GetStartTs().UnixNano()-(lokiReq.GetStartTs().UnixNano()%interval.Nanoseconds()))
	// generate the cache key based on query, tenant and start time.
	cacheKey := fmt.Sprintf("log:%s:%s:%d", tenant.JoinTenantIDs(tenantIDs), req.GetQuery(), alignedStart.UnixNano()/(interval.Nanoseconds()))

	_, buff, _, err := l.cache.Fetch(ctx, []string{cacheKey})
	if err != nil {
		level.Warn(l.logger).Log("msg", "error fetching cache", "err", err)
		return l.next.Do(ctx, req)
	}
	// we expect only one key to be found or missing.
	if len(buff) > 1 {
		level.Warn(l.logger).Log("msg", "unexpected length of cache return values", "buff", len(buff))
		return l.next.Do(ctx, req)
	}

	if len(buff) == 0 {
		return l.handleMiss(ctx, cacheKey, lokiReq)
	}

	// cache hit
	var cachedResquest *LokiRequest
	err = proto.Unmarshal(buff[0], cachedResquest)
	if err != nil {
		level.Warn(l.logger).Log("msg", "error unmarshalling request from cache", "err", err)
		return l.next.Do(ctx, req)
	}
	return l.handleHit(ctx, cacheKey, cachedResquest, lokiReq)
}

func (l *logCacheNoResult) handleMiss(ctx context.Context, cacheKey string, req *LokiRequest) (queryrangebase.Response, error) {
	// cache miss
	level.Debug(l.logger).Log("msg", "cache miss", "key", cacheKey)
	resp, err := l.next.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	lokiRes, ok := resp.(*LokiResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T", resp)
	}
	// we have data we don't want to cache.
	if !isEmpty(lokiRes) {
		return resp, nil
	}
	data, err := proto.Marshal(req)
	if err != nil {
		level.Warn(l.logger).Log("msg", "error marshalling request", "err", err)
		return resp, nil
	}
	// cache the result
	err = l.cache.Store(ctx, []string{cacheKey}, [][]byte{data})
	if err != nil {
		level.Warn(l.logger).Log("msg", "error storing cache", "err", err)
	}
	return resp, nil
}

func (l *logCacheNoResult) handleHit(ctx context.Context, cacheKey string, cachedResquest *LokiRequest, lokiReq *LokiRequest) (queryrangebase.Response, error) {
	// we start with an empty response
	result := &LokiResponse{
		Status:     loghttp.QueryStatusSuccess,
		Statistics: stats.Result{},
		Direction:  cachedResquest.Direction,
		Limit:      cachedResquest.Limit,
		Version:    uint32(loghttp.GetVersion(cachedResquest.Path)),
		Data: LokiData{
			ResultType: loghttp.ResultTypeStream,
			Result:     []logproto.Stream{},
		},
	}
	// if the request is the same and cover the whole time range.
	// we can just return the cached result.
	if lokiReq.GetStartTs().Equal(cachedResquest.GetStartTs()) && lokiReq.GetEndTs().Equal(cachedResquest.GetEndTs()) {
		return result, nil
	}
	// we could be missing data at the start and the end.
	// so we're going to fetch what is missing.
	var (
		startRequest, endRequest *LokiRequest
		startResp, endResp       *LokiResponse
		updateCache              bool
	)
	g, ctx := errgroup.WithContext(ctx)

	if !lokiReq.GetStartTs().Equal(cachedResquest.GetStartTs()) {
		g.Go(func() error {
			startRequest = lokiReq.WithStartEndTime(lokiReq.GetStartTs(), cachedResquest.GetStartTs())
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

	if lokiReq.GetEndTs().Equal(cachedResquest.GetEndTs()) {
		g.Go(func() error {
			endRequest = lokiReq.WithStartEndTime(cachedResquest.GetEndTs(), lokiReq.GetEndTs())
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

	if startResp != nil {
		if isEmpty(startResp) {
			cachedResquest = cachedResquest.WithStartEndTime(startRequest.GetStartTs(), cachedResquest.GetEndTs())
			updateCache = true
		} else {
			if startResp.Status != loghttp.QueryStatusSuccess {
				return startResp, nil
			}
			result = mergeLokiResponse(startResp, result)
		}
	}

	if endResp != nil {
		if isEmpty(endResp) {
			cachedResquest = cachedResquest.WithStartEndTime(cachedResquest.GetStartTs(), endRequest.GetEndTs())
			updateCache = true
		} else {
			if endResp.Status != loghttp.QueryStatusSuccess {
				return endResp, nil
			}
			result = mergeLokiResponse(endResp, result)
		}
	}

	if updateCache {
		data, err := proto.Marshal(cachedResquest)
		if err != nil {
			level.Warn(l.logger).Log("msg", "error marshalling request", "err", err)
			return result, err
		}
		// cache the result
		err = l.cache.Store(ctx, []string{cacheKey}, [][]byte{data})
		if err != nil {
			level.Warn(l.logger).Log("msg", "error storing cache", "err", err)
		}
	}
	return result, nil
}

func isEmpty(lokiRes *LokiResponse) bool {
	return lokiRes.Status == loghttp.QueryStatusSuccess && len(lokiRes.Data.Result) == 0
}
