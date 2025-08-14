package querytee

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"time"

	"github.com/grafana/dskit/tracing"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// ProxyBackend holds the information of a single backend.
type ProxyBackend struct {
	name     string
	endpoint *url.URL
	client   *http.Client
	timeout  time.Duration

	// Whether this is the preferred backend from which picking up
	// the response and sending it back to the client.
	preferred bool

	// Only process requests that match the filter.
	filter *regexp.Regexp
}

// NewProxyBackend makes a new ProxyBackend
func NewProxyBackend(name string, endpoint *url.URL, timeout time.Duration, preferred bool) *ProxyBackend {
	return &ProxyBackend{
		name:      name,
		endpoint:  endpoint,
		timeout:   timeout,
		preferred: preferred,
		client: &http.Client{
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("the query-tee proxy does not follow redirects")
			},
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100, // see https://github.com/golang/go/issues/13801
				IdleConnTimeout:     90 * time.Second,
				DisableCompression:  true,
			},
		},
	}
}

func (b *ProxyBackend) WithFilter(f *regexp.Regexp) *ProxyBackend {
	b.filter = f
	return b
}

func (b *ProxyBackend) ForwardRequest(orig *http.Request, body io.ReadCloser) *BackendResponse {
	start := time.Now()
	req, span := b.createBackendRequest(orig, body)
	defer span.Finish()

	// Extract trace ID and span ID from the context
	traceID, spanID, _ := tracing.ExtractTraceSpanID(req.Context())

	status, responseBody, err := b.doBackendRequest(req)
	duration := time.Since(start)

	// Set span status based on response
	if err != nil {
		span.SetTag("error", err.Error())
	}
	span.SetTag("http.status_code", status)
	span.SetTag("duration.ms", duration.Milliseconds())

	return &BackendResponse{
		backend:  b,
		status:   status,
		body:     responseBody,
		err:      err,
		duration: duration,
		traceID:  traceID,
		spanID:   spanID,
	}
}

func (b *ProxyBackend) createBackendRequest(orig *http.Request, body io.ReadCloser) (*http.Request, *tracing.Span) {
	// Create a child span directly from the original context to preserve parent-child relationship
	span, spanCtx := tracing.StartSpanFromContext(orig.Context(), "querytee.backend.request")
	span.SetTag("backend.name", b.name)
	span.SetTag("backend.preferred", b.preferred)
	span.SetTag("backend.endpoint", b.endpoint.String())

	req := orig.Clone(spanCtx)
	req.Body = body
	// RequestURI can't be set on a cloned request. It's only for handlers.
	req.RequestURI = ""
	// Replace the endpoint with the backend one.
	req.URL.Scheme = b.endpoint.Scheme
	req.URL.Host = b.endpoint.Host

	// Prepend the endpoint path to the request path.
	req.URL.Path = path.Join(b.endpoint.Path, req.URL.Path)

	// Set the correct host header for the backend
	req.Header.Set("Host", b.endpoint.Host)

	// Replace the auth:
	// - If the endpoint has user and password, use it.
	// - If the endpoint has user only, keep it and use the request password (if any).
	// - If the endpoint has no user and no password, use the request auth (if any).
	clientUser, clientPass, clientAuth := orig.BasicAuth()
	endpointUser := b.endpoint.User.Username()
	endpointPass, _ := b.endpoint.User.Password()

	req.Header.Del("Authorization")
	if endpointUser != "" && endpointPass != "" {
		req.SetBasicAuth(endpointUser, endpointPass)
	} else if endpointUser != "" {
		req.SetBasicAuth(endpointUser, clientPass)
	} else if clientAuth {
		req.SetBasicAuth(clientUser, clientPass)
	}

	// Remove Accept-Encoding header to avoid sending compressed responses
	req.Header.Del("Accept-Encoding")

	return req, span
}

func (b *ProxyBackend) doBackendRequest(req *http.Request) (int, []byte, error) {
	// Explicitly inject trace context into HTTP headers before creating isolated context.
	// This ensures trace propagation works even with context isolation.
	b.injectTraceHeaders(req)

	// Create an isolated context for the HTTP request timeout.
	// We use context.WithoutCancel to prevent sibling cancellation, then add our own timeout.
	// Note: context.WithoutCancel breaks trace propagation, so we inject headers explicitly above.
	isolatedCtx := context.WithoutCancel(req.Context())
	ctx, cancel := context.WithTimeout(isolatedCtx, b.timeout)
	defer cancel()

	// Execute the request.
	res, err := b.client.Do(req.WithContext(ctx))
	if err != nil {
		return 0, nil, errors.Wrap(err, "executing backend request")
	}

	// Read the entire response body.
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, nil, errors.Wrap(err, "reading backend response")
	}

	return res.StatusCode, body, nil
}

// injectTraceHeaders explicitly injects trace context into HTTP headers.
// This is necessary because context.WithoutCancel breaks the normal trace propagation.
func (b *ProxyBackend) injectTraceHeaders(req *http.Request) {
	ctx := req.Context()

	// First, try OpenTracing if it's registered (dskit might be using this)
	if opentracing.IsGlobalTracerRegistered() {
		if span := opentracing.SpanFromContext(ctx); span != nil {
			tracer := opentracing.GlobalTracer()
			// Inject the span context into the HTTP headers
			_ = tracer.Inject(span.Context(), opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier(req.Header))
			return
		}
	}

	// Otherwise, use OpenTelemetry propagation
	propagator := otel.GetTextMapPropagator()
	propagator.Inject(ctx, propagation.HeaderCarrier(req.Header))
}
