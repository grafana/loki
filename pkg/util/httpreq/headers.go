package httpreq

import (
	"context"
	"net/http"
	"strings"

	"github.com/grafana/dskit/middleware"
)

type headerContextKey string

var (
	// LokiActorPathHeader is the name of the header e.g. used to enqueue requests in hierarchical queues.
	LokiActorPathHeader               = "X-Loki-Actor-Path"
	LokiDisablePipelineWrappersHeader = "X-Loki-Disable-Pipeline-Wrappers"

	// LokiActorPathDelimiter is the delimiter used to serialise the hierarchy of the actor.
	LokiActorPathDelimiter = "|"
)

func PropagateHeadersMiddleware(headers ...string) middleware.Interface {
	return middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			for _, h := range headers {
				value := req.Header.Get(h)
				if value != "" {
					ctx := req.Context()
					ctx = context.WithValue(ctx, headerContextKey(h), value)
					req = req.WithContext(ctx)
				}
			}
			next.ServeHTTP(w, req)
		})
	})
}

func ExtractHeader(ctx context.Context, name string) string {
	s, _ := ctx.Value(headerContextKey(name)).(string)
	return s
}

func ExtractActorPath(ctx context.Context) []string {
	value := ExtractHeader(ctx, LokiActorPathHeader)
	if value == "" {
		return nil
	}
	return strings.Split(value, LokiActorPathDelimiter)
}

func InjectActorPath(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, headerContextKey(LokiActorPathHeader), value)
}

func InjectHeader(ctx context.Context, key, value string) context.Context {
	return context.WithValue(ctx, headerContextKey(key), value)
}

// allHeadersContextKey is used to store all HTTP headers in context for downstream restoration.
type allHeadersContextKey struct{}

// PropagateAllHeadersMiddleware stores all HTTP headers in context for downstream restoration.
// This is useful when requests go through a codec decode/encode cycle that loses headers.
// Headers in the ignoreList will not be propagated.
func PropagateAllHeadersMiddleware(ignoreList ...string) middleware.Interface {
	ignoreSet := make(map[string]struct{}, len(ignoreList))
	for _, h := range ignoreList {
		ignoreSet[http.CanonicalHeaderKey(h)] = struct{}{}
	}

	return middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := req.Context()
			ctxHeaders, ok := ctx.Value(allHeadersContextKey{}).(http.Header)
			if !ok {
				ctxHeaders = make(http.Header)
				ctx = context.WithValue(ctx, allHeadersContextKey{}, ctxHeaders)
				req = req.WithContext(ctx)
			}

			for k, v := range req.Header {
				// Skip headers in the ignore list
				if _, ignored := ignoreSet[http.CanonicalHeaderKey(k)]; ignored {
					continue
				}
				for _, vv := range v {
					ctxHeaders.Add(k, vv)
				}
			}

			next.ServeHTTP(w, req)
		})
	})
}

// ExtractAllHeaders retrieves all headers stored in context by PropagateAllHeadersMiddleware.
func ExtractAllHeaders(ctx context.Context) http.Header {
	ctxHeaders, ok := ctx.Value(allHeadersContextKey{}).(http.Header)
	if !ok {
		return nil
	}
	return ctxHeaders
}
