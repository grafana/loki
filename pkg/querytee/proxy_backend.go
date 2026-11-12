package querytee

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/grafana/dskit/tracing"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// BackendRole tells how the proxy uses a backend.
type BackendRole int

const (
	// RoleOther is a backend that is neither the primary nor the secondary. It receives every
	// request on a registered route. The proxy never returns its response and never compares it.
	RoleOther BackendRole = iota
	// RolePrimary is the backend whose response the proxy returns to the client, even when the
	// response is an error. The proxy does not fall back to another backend.
	RolePrimary
	// RoleSecondary is the backend whose responses the proxy compares with the primary's. It
	// receives every write on a registered route. It receives a read only when the selection
	// strategy picks it. The proxy never returns its response. When -proxy.compare-responses is
	// set, the proxy compares its response with the primary's on routes that have a comparator.
	RoleSecondary
)

// String returns the name of the role. It is the value of the backend_alias metric label.
func (r BackendRole) String() string {
	switch r {
	case RolePrimary:
		return "primary"
	case RoleSecondary:
		return "secondary"
	default:
		return "other"
	}
}

// ProxyBackend holds the information of a single backend.
type ProxyBackend struct {
	name     string
	endpoint *url.URL
	client   *http.Client
	timeout  time.Duration
	role     BackendRole
}

// NewProxyBackend makes a new ProxyBackend.
func NewProxyBackend(name string, endpoint *url.URL, timeout time.Duration, role BackendRole) (*ProxyBackend, error) {
	switch role {
	case RoleOther, RolePrimary, RoleSecondary:
	default:
		return nil, errors.Errorf("unknown backend role %d", role)
	}

	return &ProxyBackend{
		name:     name,
		endpoint: endpoint,
		timeout:  timeout,
		role:     role,
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
	}, nil
}

// Alias returns the backend alias label value for metrics. It is "primary", "secondary" or "other".
func (b *ProxyBackend) Alias() string {
	return b.role.String()
}

func (b *ProxyBackend) isPrimary() bool {
	return b.role == RolePrimary
}

func (b *ProxyBackend) isSecondary() bool {
	return b.role == RoleSecondary
}

func (b *ProxyBackend) ForwardRequest(orig *http.Request, body io.ReadCloser) *BackendResponse {
	start := time.Now()
	req, span := b.createBackendRequest(orig, body)
	defer span.Finish()

	// Extract trace ID and span ID from the context
	traceID, spanID, _ := tracing.ExtractTraceSpanID(req.Context())

	status, responseBody, responseHeaders, err := b.doBackendRequest(req)
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
		headers:  responseHeaders,
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
	span.SetTag("backend.role", b.role.String())
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

func (b *ProxyBackend) doBackendRequest(req *http.Request) (int, []byte, http.Header, error) {
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
		return 0, nil, nil, errors.Wrap(err, "executing backend request")
	}

	// Read the entire response body.
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, nil, res.Header, errors.Wrap(err, "reading backend response")
	}

	return res.StatusCode, body, res.Header, nil
}

// injectTraceHeaders explicitly injects trace context into HTTP headers.
// This is necessary because context.WithoutCancel breaks the normal trace propagation.
func (b *ProxyBackend) injectTraceHeaders(req *http.Request) {
	otel.GetTextMapPropagator().Inject(req.Context(), propagation.HeaderCarrier(req.Header))
}

// ProxyBackends is a list of backends.
type ProxyBackends []*ProxyBackend

// CountFunc returns the number of backends for which match returns true.
func (bs ProxyBackends) CountFunc(match func(*ProxyBackend) bool) int {
	var count int
	for _, b := range bs {
		if match(b) {
			count++
		}
	}
	return count
}
