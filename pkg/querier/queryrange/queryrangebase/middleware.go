package queryrangebase

import (
	"context"
	"net/http"

	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
)

const (
	// ResultsCacheGenNumberHeaderName holds name of the header we want to set in http response
	ResultsCacheGenNumberHeaderName = "Results-Cache-Gen-Number"
)

func CacheGenNumberHeaderSetterMiddleware(cacheGenNumbersLoader resultscache.CacheGenNumberLoader) middleware.Interface {
	return middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userIDs, err := tenant.TenantIDs(r.Context())
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}

			cacheGenNumber := cacheGenNumbersLoader.GetResultsCacheGenNumber(userIDs)

			w.Header().Set(ResultsCacheGenNumberHeaderName, cacheGenNumber)
			next.ServeHTTP(w, r)
		})
	})
}

func CacheGenNumberContextSetterMiddleware(cacheGenNumbersLoader resultscache.CacheGenNumberLoader) Middleware {
	return MiddlewareFunc(func(next Handler) Handler {
		return HandlerFunc(func(ctx context.Context, req Request) (Response, error) {
			userIDs, err := tenant.TenantIDs(ctx)
			if err != nil {
				return nil, err
			}

			cacheGenNumber := cacheGenNumbersLoader.GetResultsCacheGenNumber(userIDs)

			res, err := next.Do(ctx, req)
			if err != nil {
				return nil, err
			}

			res.SetHeader(ResultsCacheGenNumberHeaderName, cacheGenNumber)
			return res, nil
		})
	})
}
