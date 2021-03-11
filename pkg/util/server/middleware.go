package server

import (
	"net/http"

	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/middleware"
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
