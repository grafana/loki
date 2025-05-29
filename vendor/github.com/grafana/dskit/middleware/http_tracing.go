// Provenance-includes-location: https://github.com/weaveworks/common/blob/main/middleware/http_tracing.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Weaveworks Ltd.

package middleware

import (
	"context"
	"fmt"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/dskit/httpgrpc"

	"github.com/gorilla/mux"
	"github.com/opentracing-contrib/go-stdlib/nethttp"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"google.golang.org/grpc"
)

var tracer = otel.Tracer("dskit/middleware")

// Dummy dependency to enforce that we have a nethttp version newer
// than the one which implements Websockets. (No semver on nethttp)
var _ = nethttp.MWURLTagFunc

// Tracer is a middleware which traces incoming requests.
type Tracer struct {
	SourceIPs *SourceIPExtractor
}

// Wrap implements Interface
func (t Tracer) Wrap(next http.Handler) http.Handler {
	if opentracing.IsGlobalTracerRegistered() {
		return t.wrapWithOpenTracing(next)
	}
	// If no OpenTracing, let's do OTel.
	return t.wrapWithOTel(next)
}

func (t Tracer) wrapWithOpenTracing(next http.Handler) http.Handler {
	// Do OpenTracing when it's registered.
	options := []nethttp.MWOption{
		nethttp.OperationNameFunc(httpOperationName),
		nethttp.MWSpanObserver(func(sp opentracing.Span, r *http.Request) {
			// add a tag with the client's user agent to the span
			userAgent := r.Header.Get("User-Agent")
			if userAgent != "" {
				sp.SetTag("http.user_agent", userAgent)
			}

			// add the content type, useful when query requests are sent as POST
			if ct := r.Header.Get("Content-Type"); ct != "" {
				sp.SetTag("http.content_type", ct)
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

func (t Tracer) wrapWithOTel(next http.Handler) http.Handler {
	tracingMiddleware := otelhttp.NewHandler(next, "http.tracing", otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
		return httpOperationName(r)
	}))

	// Wrap the 'tracingMiddleware' to capture its execution
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if labeler, ok := otelhttp.LabelerFromContext(r.Context()); ok {
			// add a tag with the client's user agent to the span
			userAgent := r.Header.Get("User-Agent")
			if userAgent != "" {
				labeler.Add(attribute.String("http.user_agent", userAgent))
			}

			labeler.Add(attribute.String("http.url", r.URL.Path))
			labeler.Add(attribute.String("http.method", r.Method))

			// add the content type, useful when query requests are sent as POST
			if ct := r.Header.Get("Content-Type"); ct != "" {
				labeler.Add(attribute.String("http.content_type", ct))
			}

			labeler.Add(attribute.String("headers", fmt.Sprintf("%v", r.Header)))
			// add a tag with the client's sourceIPs to the span, if a
			// SourceIPExtractor is given.
			if t.SourceIPs != nil {
				labeler.Add(attribute.String("sourceIPs", t.SourceIPs.Get(r)))
			}
		}

		tracingMiddleware.ServeHTTP(w, r)
	})

	return handler
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

		if opentracing.IsGlobalTracerRegistered() {
			return handleHTTPGRPCRequestWithOpenTracing(ctx, req, httpRequest, router, handler)
		}

		return handleHTTPGRPCRequestWithOTel(ctx, req, httpRequest, router, handler)
	}
}

func handleHTTPGRPCRequestWithOpenTracing(ctx context.Context, req any, httpRequest *http.Request, router *mux.Router, handler grpc.UnaryHandler) (any, error) {
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

func handleHTTPGRPCRequestWithOTel(ctx context.Context, req any, httpRequest *http.Request, router *mux.Router, handler grpc.UnaryHandler) (any, error) {
	// extract relevant span & tag data from request
	method := httpRequest.Method
	routeName := getRouteName(router, httpRequest)
	urlPath := httpRequest.URL.Path
	userAgent := httpRequest.Header.Get("User-Agent")

	parentSpan := trace.SpanFromContext(ctx)
	if parentSpan.SpanContext().IsValid() {
		parentSpan.SetAttributes(attribute.String("http.url", urlPath))
		parentSpan.SetAttributes(attribute.String("http.method", method))
		parentSpan.SetAttributes(attribute.String("http.route", routeName))
		parentSpan.SetAttributes(attribute.String("http.user_agent", userAgent))
	}
	// create and start child HTTP span and set span name and attributes
	childSpanName := httpOperationName(httpRequest)

	startSpanOpts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("component", "net/http"),
			attribute.String("http.method", method),
			attribute.String("http.url", urlPath),
			attribute.String("http.route", routeName),
			attribute.String("http.user_agent", userAgent),
		),
	}

	var childSpan trace.Span
	ctx, childSpan = tracer.Start(ctx, childSpanName, startSpanOpts...)
	defer childSpan.End()

	return handler(ctx, req)
}

func httpOperationName(r *http.Request) string {
	routeName := ExtractRouteName(r.Context())
	return getOperationName(routeName, r)
}

func getOperationName(routeName string, r *http.Request) string {
	if routeName == "" {
		return "HTTP " + r.Method
	}
	return fmt.Sprintf("HTTP %s - %s", r.Method, routeName)
}
