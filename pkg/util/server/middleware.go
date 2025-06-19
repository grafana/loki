package server

import (
	"net/http"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/middleware"
)

// NewPrepopulateMiddleware creates a middleware which will parse incoming http forms.
// This is important because some endpoints can POST x-www-form-urlencoded bodies instead of GET w/ query strings.
func NewPrepopulateMiddleware() middleware.Interface {
	return middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			err := req.ParseForm()
			if err != nil {
				WriteError(httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error()), w)
				return

			}
			next.ServeHTTP(w, req)
		})
	})
}

// ResponseJSONMiddleware sets the Content-Type header to JSON if it's not set.
func ResponseJSONMiddleware() middleware.Interface {
	return middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req)
			if w.Header().Get("Content-Type") == "" {
				w.Header().Set("Content-Type", "application/json; charset=UTF-8")
			}
		})
	})
}
