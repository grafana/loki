// Provenance-includes-location: https://github.com/weaveworks/common/blob/main/middleware/http_tracing.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Weaveworks Ltd.

package middleware

import (
	"context"
	"fmt"
	"net/http"

	"github.com/grafana/dskit/httpgrpc"

	"github.com/gorilla/mux"
	"github.com/opentracing-contrib/go-stdlib/nethttp"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"google.golang.org/grpc"
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

// HTTPGRPCTracingInterceptor adds additional information about the encapsulated HTTP request
// to httpgrpc trace spans.
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
// Note that we cannot do this in the httpgrpc Server implementation, as some applications (eg.
// Mimir's queriers) call Server.Handle() directly, which means we'd attach HTTP-request related
// span tags to whatever parent span is active in the caller, rather than the /httpgrpc.HTTP/Handle
// span created by the tracing middleware for requests that arrive over the network.
func HTTPGRPCTracingInterceptor(router *mux.Router) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		if info.FullMethod != "/httpgrpc.HTTP/Handle" {
			return handler(ctx, req)
		}

		httpgrpcRequest, ok := req.(*httpgrpc.HTTPRequest)
		if !ok {
			return handler(ctx, req)
		}

		httpRequest, err := httpgrpc.ToHTTPRequest(ctx, httpgrpcRequest)
		if err != nil {
			return handler(ctx, req)
		}

		tracer := opentracing.GlobalTracer()
		parentSpan := opentracing.SpanFromContext(ctx)

		// extract relevant span & tag data from request
		method := httpRequest.Method
		routeName := getRouteName(router, httpRequest)
		urlPath := httpRequest.URL.Path
		userAgent := httpRequest.Header.Get("User-Agent")

		// tag parent httpgrpc.HTTP/Handle server span, if it exists
		if parentSpan != nil {
			parentSpan.SetTag(string(ext.HTTPUrl), urlPath)
			parentSpan.SetTag(string(ext.HTTPMethod), method)
			parentSpan.SetTag("http.route", routeName)
			parentSpan.SetTag("http.user_agent", userAgent)
		}

		// create and start child HTTP span
		// mirroring opentracing-contrib/go-stdlib/nethttp.Middleware span name and tags
		childSpanName := getOperationName(routeName, httpRequest)
		startSpanOpts := []opentracing.StartSpanOption{
			ext.SpanKindRPCServer,
			opentracing.Tag{Key: string(ext.Component), Value: "net/http"},
			opentracing.Tag{Key: string(ext.HTTPUrl), Value: urlPath},
			opentracing.Tag{Key: string(ext.HTTPMethod), Value: method},
			opentracing.Tag{Key: "http.route", Value: routeName},
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

		ctx = opentracing.ContextWithSpan(ctx, childSpan)
		return handler(ctx, req)
	}
}

func makeHTTPOperationNameFunc(routeMatcher RouteMatcher) func(r *http.Request) string {
	return func(r *http.Request) string {
		routeName := getRouteName(routeMatcher, r)
		return getOperationName(routeName, r)
	}
}

func getOperationName(routeName string, r *http.Request) string {
	if routeName == "" {
		return "HTTP " + r.Method
	}
	return fmt.Sprintf("HTTP %s - %s", r.Method, routeName)
}
