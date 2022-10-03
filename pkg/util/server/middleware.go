package server

import (
	"net/http"

	"github.com/grafana/dskit/tenant"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/middleware"

	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
)

// NewPrepopulateMiddleware creates a middleware which will parse incoming http forms.
// This is important because some endpoints can POST x-www-form-urlencoded bodies instead of GET w/ query strings.
func NewPrepopulateMiddleware() middleware.Interface {
	return middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			err := req.ParseForm()
			if err != nil {
				WriteError(httpgrpc.Errorf(http.StatusBadRequest, err.Error()), w)
				return

			}
			next.ServeHTTP(w, req)
		})
	})
}

func ResponseJSONMiddleware() middleware.Interface {
	return middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "application/json; charset=UTF-8")
			next.ServeHTTP(w, req)
		})
	})
}

// middleware for setting cache gen header to let consumer of response know all previous responses could be invalid due to delete operation
func CacheGenNumberHeaderSetterMiddleware(cacheGenNumbersLoader queryrangebase.CacheGenNumberLoader) middleware.Interface {
	return middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userIDs, err := tenant.TenantIDs(r.Context())
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}

			cacheGenNumber := cacheGenNumbersLoader.GetResultsCacheGenNumber(userIDs)

			w.Header().Set(queryrangebase.ResultsCacheGenNumberHeaderName, cacheGenNumber)
			next.ServeHTTP(w, r)
		})
	})
}
