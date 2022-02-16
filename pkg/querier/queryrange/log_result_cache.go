package queryrange

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-kit/kit/log/level"
	"github.com/go-kit/log"
	"github.com/golang/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/tenant"
)

func NewLogCacheNoResult(logger log.Logger, limits Limits, cacheConfig cache.Config, reg prometheus.Registerer) (queryrangebase.Middleware, error) {
	// todo test creating multiple cache instances
	c, err := cache.New(cacheConfig, reg, logger)
	if err != nil {
		return nil, err
	}
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return &logCacheNoResult{
			next:   next,
			limits: limits,
			cache:  c,
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
	userid, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	interval := l.limits.QuerySplitDuration(userid)
	// skip caching by if interval is unset
	if interval == 0 {
		return l.next.Do(ctx, req)
	}
	// Generate the correct cache key
	// based on query, tenant
	cacheKey := fmt.Sprintf("nolog:%s:%s:%d", userid, req.GetQuery(), int64(req.GetStart()/(interval.Milliseconds())))

	// todo handle hedge case where start and end is not well aligned.

	// do no cache fresh
	_, buff, _, err := l.cache.Fetch(ctx, []string{cacheKey})
	if err != nil {
		level.Warn(l.logger).Log("msg", "error fetching cache", "err", err)
		return l.next.Do(ctx, req)
	}
	// we expect only one key to be found or missing.
	if len(buff) > 1 {
		level.Warn(l.logger).Log("msg", "unexpected length of cache return values", "buff", len(buff), "missing", len(missing))
		return l.next.Do(ctx, req)
	}

	if len(buff) == 0 {
		// cache miss
		level.Debug(l.logger).Log("msg", "cache miss", "key", cacheKey)
		resp, err := l.next.Do(ctx, req)
		if err != nil {
			return nil, err
		}
		// if we have a stream
		lokiRes, ok := resp.(*LokiResponse)
		if !ok {
			return nil, fmt.Errorf("unexpected response type %T", resp)
		}
		// we have data we don't want to cache.
		if len(lokiRes.Data.Result) != 0 {
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

	// cache hit
	var cachedResquest LokiRequest
	err = proto.Unmarshal(buff[0], &cachedResquest)
	if err != nil {
		level.Warn(l.logger).Log("msg", "error unmarshalling request from cache", "err", err)
		return l.next.Do(ctx, req)
	}

	// todo check if cached request is the same as the current request
	if req.GetStart() == cachedResquest.GetStart() && req.GetEnd() == cachedResquest.GetEnd() {
		return &LokiResponse{
			Status:     loghttp.QueryStatusSuccess,
			Statistics: stats.Result{},
			// todo finish
			Data: LokiData{
				ResultType: loghttp.ResultTypeStream,
				Result:     []logproto.Stream{},
			},
		}, nil
	}

	// 50 / 10 =  5

	// 51 88
	// 51 60, 60 70, 70 80 , 80 88
	// 41 50 | 50 60, 60 70, 70 80 , 80 88
	var foregroundRequest, backgroundRequest *LokiRequest
	if req.GetStart() != cachedResquest.GetStart() {
		foregroundRequest := req.WithStartEnd(req.GetStart(), cachedResquest.GetStart())
		foregroundResp, err := l.next.Do(ctx, foregroundRequest)
		if err != nil {
			return nil, err
		}
	}

	if req.GetEnd() != cachedResquest.GetEnd() {
		backgroundRequest := req.WithStartEnd(cachedResquest.GetEnd(), req.GetEnd())
		backgroundResp, err := l.next.Do(ctx, backgroundRequest)
		if err != nil {
			return nil, err
		}
	}
	if isEmpty(foregroundResp) && isEmpty(backgroundResp) {
		data, err := proto.Marshal(req)
		if err != nil {
			return nil, err
		}
		l.cache.Store(ctx, []string{cacheKey}, [][]byte{data})
	}

	// if one side is not empty extend the cache to this side and return the other side.

	// if both sides are not empty return both sides.
	// But don't touch the cache.
	// inteval 5.
	// first time 51 55
	// 50 51, 51 55, 55 60

	// if we have a stream
	lokiRes, ok := resp.(*LokiResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T", resp)
	}
	// we have data we don't want to cache.
	if len(lokiRes.Data.Result) != 0 {
		return resp, nil
	}

	return nil, nil
}

func isEmpty(resp queryrangebase.Response) (bool, error) {
	return resp.Status == loghttp.QueryStatusSuccess && len(resp.Data.Result) == 0
}
