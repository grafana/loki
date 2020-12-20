package api

import (
	"net/http"

	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/chunk/purger"
	"github.com/cortexproject/cortex/pkg/querier/queryrange"
)

// middleware for setting cache gen header to let consumer of response know all previous responses could be invalid due to delete operation
func getHTTPCacheGenNumberHeaderSetterMiddleware(cacheGenNumbersLoader *purger.TombstonesLoader) middleware.Interface {
	return middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, err := user.ExtractOrgID(r.Context())
			if err != nil {
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}

			cacheGenNumber := cacheGenNumbersLoader.GetResultsCacheGenNumber(userID)

			w.Header().Set(queryrange.ResultsCacheGenNumberHeaderName, cacheGenNumber)
			next.ServeHTTP(w, r)
		})
	})
}
