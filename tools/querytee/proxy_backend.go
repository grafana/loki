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
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("tools/querytee")

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

// createIsolatedContextWithTracing creates a new context that:
// - Is isolated from parent and sibling cancellation
// - Preserves trace context for distributed tracing
// - Has its own independent cancellation
func createIsolatedContextWithTracing(parent context.Context) (context.Context, context.CancelFunc) {
	// Extract trace context from parent
	spanCtx := trace.SpanFromContext(parent).SpanContext()

	// Create new context with its own cancellation, isolated from parent
	ctx, cancel := context.WithCancel(context.Background())

	// Restore trace context
	if spanCtx.IsValid() {
		ctx = trace.ContextWithSpanContext(ctx, spanCtx)
	}

	return ctx, cancel
}

func (b *ProxyBackend) ForwardRequest(orig *http.Request, body io.ReadCloser) *BackendResponse {
	start := time.Now()
	req, span := b.createBackendRequest(orig, body)
	defer span.End()

	// Extract trace ID and span ID from the child span context
	traceID, _ := tracing.ExtractSampledTraceID(req.Context())
	spanCtx := span.SpanContext()
	spanID := spanCtx.SpanID().String()

	status, responseBody, err := b.doBackendRequest(req)
	duration := time.Since(start)

	// Set span status based on response
	if err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("error", err.Error()))
	}
	span.SetAttributes(
		attribute.Int("http.status_code", status),
		attribute.Int64("duration.ms", duration.Milliseconds()),
	)

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

func (b *ProxyBackend) createBackendRequest(orig *http.Request, body io.ReadCloser) (*http.Request, trace.Span) {
	// Create isolated context that preserves tracing but is isolated from sibling failures
	isolatedCtx, _ := createIsolatedContextWithTracing(orig.Context())

	// Create a child span for this backend request
	spanCtx, span := tracer.Start(isolatedCtx, "querytee.backend.request",
		trace.WithAttributes(
			attribute.String("backend.name", b.name),
			attribute.Bool("backend.preferred", b.preferred),
			attribute.String("backend.endpoint", b.endpoint.String()),
		),
	)

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
	// Honor the read timeout.
	ctx, cancel := context.WithTimeout(req.Context(), b.timeout)
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
