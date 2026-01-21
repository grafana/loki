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

const (
	// Headers to ignore in PropagateAllHeadersMiddleware to avoid security or encoding issues.
	AuthorizationHeader  = "Authorization"
	AcceptHeader         = "Accept"
	AcceptEncodingHeader = "Accept-Encoding"
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
			// Filter out ignored headers
			filtered := make(http.Header, len(req.Header))
			for k, v := range req.Header {
				if _, ignored := ignoreSet[http.CanonicalHeaderKey(k)]; ignored {
					continue
				}
				filtered[k] = v
			}

			ctx := InjectAllHeaders(req.Context(), filtered)
			next.ServeHTTP(w, req.WithContext(ctx))
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

// InjectAllHeaders adds all headers from the provided http.Header to the context's header map.
func InjectAllHeaders(ctx context.Context, h http.Header) context.Context {
	if len(h) == 0 {
		return ctx
	}
	headers, ok := ctx.Value(headerContextKey{}).(http.Header)
	if !ok {
		headers = make(http.Header, len(h))
		ctx = context.WithValue(ctx, headerContextKey{}, headers)
	}
	for k, values := range h {
		for _, v := range values {
			headers.Add(k, v)
		}
	}
	return ctx
}

// InjectActorPath adds the actor path header to context.
func InjectActorPath(ctx context.Context, value string) context.Context {
	return InjectHeader(ctx, LokiActorPathHeader, value)
}
