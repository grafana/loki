package httpreq

import (
	"context"
	"net/http"
	"regexp"

	"github.com/weaveworks/common/middleware"
)

// NOTE(kavi): Why new type?
// Our linter won't allow to use basic types like string to be used as key in context.
type ctxKey string

var (
	QueryTagsHTTPHeader ctxKey = "X-Query-Tags"
	safeQueryTags              = regexp.MustCompile("[^a-zA-Z0-9-=, ]+") // only alpha-numeric, ' ', ',', '=' and `-`

)

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
