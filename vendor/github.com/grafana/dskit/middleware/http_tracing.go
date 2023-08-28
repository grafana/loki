// Provenance-includes-location: https://github.com/weaveworks/common/blob/main/middleware/http_tracing.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Weaveworks Ltd.

package middleware

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/opentracing-contrib/go-stdlib/nethttp"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/uber/jaeger-client-go"
)

// Dummy dependency to enforce that we have a nethttp version newer
// than the one which implements Websockets. (No semver on nethttp)
var _ = nethttp.MWURLTagFunc

// Tracer is a middleware which traces incoming requests.
type Tracer struct {
	RouteMatcher RouteMatcher
	SourceIPs    *SourceIPExtractor
}

// Wrap implements Interface
func (t Tracer) Wrap(next http.Handler) http.Handler {
	options := []nethttp.MWOption{
		nethttp.OperationNameFunc(makeHTTPOperationNameFunc(t.RouteMatcher)),
		nethttp.MWSpanObserver(func(sp opentracing.Span, r *http.Request) {
			// add a tag with the client's user agent to the span
			userAgent := r.Header.Get("User-Agent")
			if userAgent != "" {
				sp.SetTag("http.user_agent", userAgent)
			}

			// add a tag with the client's sourceIPs to the span, if a
			// SourceIPExtractor is given.
			if t.SourceIPs != nil {
				sp.SetTag("sourceIPs", t.SourceIPs.Get(r))
			}
		}),
	}

	return nethttp.Middleware(opentracing.GlobalTracer(), next, options...)
}

const httpGRPCHandleMethod = "/httpgrpc.HTTP/Handle"

// HTTPGRPCTracer is a middleware which traces incoming httpgrpc requests.
type HTTPGRPCTracer struct {
	RouteMatcher RouteMatcher
}

// InitHTTPGRPCMiddleware initializes gorilla/mux-compatible HTTP middleware
//
// HTTPGRPCTracer is specific to the server-side handling of HTTP requests which were
// wrapped into gRPC requests and routed through the httpgrpc.HTTP/Handle gRPC.
//
// HTTPGRPCTracer.Wrap must be attached to the same mux.Router assigned to dskit/server.Config.Router
// but it does not need to be attached to dskit/server.Config.HTTPMiddleware.
// dskit/server.Config.HTTPMiddleware is applied to direct HTTP requests not routed through gRPC;
// the server utilizes the default http middleware Tracer.Wrap for those standard http requests.
func InitHTTPGRPCMiddleware(router *mux.Router) *mux.Router {
	middleware := HTTPGRPCTracer{RouteMatcher: router}
	router.Use(middleware.Wrap)
	return router
}

// Wrap creates and decorates server-side tracing spans for httpgrpc requests
//
// The httpgrpc client wraps HTTP requests up into a generic httpgrpc.HTTP/Handle gRPC method.
// The httpgrpc server unwraps httpgrpc.HTTP/Handle gRPC requests into HTTP requests
// and forwards them to its own internal HTTP router.
//
// By default, the server-side tracing spans for the httpgrpc.HTTP/Handle gRPC method
// have no data about the wrapped HTTP request being handled.
//
// HTTPGRPCTracer.Wrap starts a child span with span name and tags following the approach in
// Tracer.Wrap's usage of opentracing-contrib/go-stdlib/nethttp.Middleware
// and attaches the HTTP server span tags to the parent httpgrpc.HTTP/Handle gRPC span, allowing
// tracing tooling to differentiate the HTTP requests represented by the httpgrpc.HTTP/Handle spans.
//
// opentracing-contrib/go-stdlib/nethttp.Middleware could not be used here
// as it does not expose options to access and tag the incoming parent span.
//
// Parent span tagging depends on using a Jaeger tracer for now to check the parent span's
// OperationName(), which is not available on the generic opentracing Tracer interface.
func (hgt HTTPGRPCTracer) Wrap(next http.Handler) http.Handler {
	httpOperationNameFunc := makeHTTPOperationNameFunc(hgt.RouteMatcher)
	fn := func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tracer := opentracing.GlobalTracer()

		// skip spans which were not forwarded from httpgrpc.HTTP/Handle spans;
		// standard http spans started directly from the HTTP server are presumed to
		// already be instrumented by Tracer.Wrap
		parentSpan := opentracing.SpanFromContext(ctx)
		if parentSpan, ok := parentSpan.(*jaeger.Span); ok {
			if parentSpan.OperationName() != httpGRPCHandleMethod {
				next.ServeHTTP(w, r)
				return
			}
		}

		// extract relevant span & tag data from request
		method := r.Method
		matchedRoute := getRouteName(hgt.RouteMatcher, r)
		urlPath := r.URL.Path
		userAgent := r.Header.Get("User-Agent")

		// tag parent httpgrpc.HTTP/Handle server span, if it exists
		if parentSpan != nil {
			parentSpan.SetTag(string(ext.HTTPUrl), urlPath)
			parentSpan.SetTag(string(ext.HTTPMethod), method)
			parentSpan.SetTag("http.route", matchedRoute)
			parentSpan.SetTag("http.user_agent", userAgent)
		}

		// create and start child HTTP span
		// mirroring opentracing-contrib/go-stdlib/nethttp.Middleware span name and tags
		childSpanName := httpOperationNameFunc(r)
		startSpanOpts := []opentracing.StartSpanOption{
			ext.SpanKindRPCServer,
			opentracing.Tag{Key: string(ext.Component), Value: "net/http"},
			opentracing.Tag{Key: string(ext.HTTPUrl), Value: urlPath},
			opentracing.Tag{Key: string(ext.HTTPMethod), Value: method},
			opentracing.Tag{Key: "http.route", Value: matchedRoute},
			opentracing.Tag{Key: "http.user_agent", Value: userAgent},
		}
		if parentSpan != nil {
			startSpanOpts = append(
				startSpanOpts,
				opentracing.SpanReference{
					Type:              opentracing.ChildOfRef,
					ReferencedContext: parentSpan.Context(),
				})
		}

		childSpan := tracer.StartSpan(childSpanName, startSpanOpts...)
		defer childSpan.Finish()

		r = r.WithContext(opentracing.ContextWithSpan(r.Context(), childSpan))
		next.ServeHTTP(w, r)
	}

	return http.HandlerFunc(fn)
}

func makeHTTPOperationNameFunc(routeMatcher RouteMatcher) func(r *http.Request) string {
	return func(r *http.Request) string {
		op := getRouteName(routeMatcher, r)
		if op == "" {
			return "HTTP " + r.Method
		}
		return fmt.Sprintf("HTTP %s - %s", r.Method, op)
	}
}
