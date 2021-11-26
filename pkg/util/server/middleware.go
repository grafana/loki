package server

import (
	"context"
	"net/http"
	"regexp"

	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/middleware"
)

// NOTE(kavi): Why new type?
// Our linter won't allow to use basic types like string to be used as key in context.
type ctxKey string

var (
	QueryTagsHTTPHeader ctxKey = "X-Query-Tags"
	safeQueryTags              = regexp.MustCompile("[^a-zA-Z0-9-=, ]+") // only alpha-numeric, ' ', ',', '=' and `-`

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

func ExtractQueryTagsMiddleware() middleware.Interface {
	return middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := req.Context()
			tags := req.Header.Get(string(QueryTagsHTTPHeader))
			tags = safeQueryTags.ReplaceAllString(tags, "")

			if tags != "" {
				ctx = context.WithValue(ctx, QueryTagsHTTPHeader, tags)
				req = req.WithContext(ctx)
			}
			next.ServeHTTP(w, req)
		})
	})
}
