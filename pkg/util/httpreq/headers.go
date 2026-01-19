package httpreq

import (
	"context"
	"net/http"
	"strings"

	"github.com/grafana/dskit/middleware"
)

type headerContextKey struct{}

var (
	// LokiActorPathHeader is the name of the header e.g. used to enqueue requests in hierarchical queues.
	LokiActorPathHeader               = "X-Loki-Actor-Path"
	LokiDisablePipelineWrappersHeader = "X-Loki-Disable-Pipeline-Wrappers"

	// LokiActorPathDelimiter is the delimiter used to serialise the hierarchy of the actor.
	LokiActorPathDelimiter = "|"
)

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
			ctxHeaders, ok := ctx.Value(headerContextKey{}).(http.Header)
			if !ok {
				ctxHeaders = make(http.Header)
				ctx = context.WithValue(ctx, headerContextKey{}, ctxHeaders)
				req = req.WithContext(ctx)
			}

			for k, v := range req.Header {
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
	ctxHeaders, _ := ctx.Value(headerContextKey{}).(http.Header)
	return ctxHeaders
}

// ExtractHeader retrieves a single header value from context.
func ExtractHeader(ctx context.Context, name string) string {
	if headers := ExtractAllHeaders(ctx); headers != nil {
		return headers.Get(name)
	}
	return ""
}

// ExtractActorPath retrieves the actor path from context.
func ExtractActorPath(ctx context.Context) []string {
	value := ExtractHeader(ctx, LokiActorPathHeader)
	if value == "" {
		return nil
	}
	return strings.Split(value, LokiActorPathDelimiter)
}

// InjectHeader adds a header to the context's header map.
func InjectHeader(ctx context.Context, key, value string) context.Context {
	headers, ok := ctx.Value(headerContextKey{}).(http.Header)
	if !ok {
		headers = make(http.Header)
		ctx = context.WithValue(ctx, headerContextKey{}, headers)
	}
	headers.Set(key, value)
	return ctx
}

// InjectActorPath adds the actor path header to context.
func InjectActorPath(ctx context.Context, value string) context.Context {
	return InjectHeader(ctx, LokiActorPathHeader, value)
}
