package middleware

import (
	"fmt"
	"net/http"

	"github.com/opentracing-contrib/go-stdlib/nethttp"
	"github.com/opentracing/opentracing-go"
	jaeger "github.com/uber/jaeger-client-go"
	"golang.org/x/net/context"
)

// Dummy dependency to enforce that we have a nethttp version newer
// than the one which implements Websockets. (No semver on nethttp)
var _ = nethttp.MWURLTagFunc

// Tracer is a middleware which traces incoming requests.
type Tracer struct {
	RouteMatcher RouteMatcher
}

// Wrap implements Interface
func (t Tracer) Wrap(next http.Handler) http.Handler {
	opMatcher := nethttp.OperationNameFunc(func(r *http.Request) string {
		op := getRouteName(t.RouteMatcher, r)
		if op == "" {
			return "HTTP " + r.Method
		}

		return fmt.Sprintf("HTTP %s - %s", r.Method, op)
	})

	return nethttp.Middleware(opentracing.GlobalTracer(), next, opMatcher)
}

// ExtractTraceID extracts the trace id, if any from the context.
func ExtractTraceID(ctx context.Context) (string, bool) {
	sp := opentracing.SpanFromContext(ctx)
	if sp == nil {
		return "", false
	}
	sctx, ok := sp.Context().(jaeger.SpanContext)
	if !ok {
		return "", false
	}

	return sctx.TraceID().String(), true
}
