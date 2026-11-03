package runtimeconfig

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// provider reads raw bytes from a config source.
type provider interface {
	// Name returns the identifier for this provider (file path or URL), used in error messages and hash keys.
	Name() string
	// Read returns the contents of the config source.
	Read(ctx context.Context) ([]byte, error)
}

// fileProvider reads config from the local filesystem.
type fileProvider struct {
	path string
}

func newFileProvider(path string) *fileProvider {
	return &fileProvider{path: path}
}

func (f *fileProvider) Name() string { return f.path }

func (f *fileProvider) Read(_ context.Context) ([]byte, error) {
	return os.ReadFile(f.path)
}

// sanitizeURLForMetrics drops the parts of a URL that most often carry credentials,
// so what remains can be a metric label value. The path is kept, so a URL that puts a
// secret in a path segment still exposes it.
func sanitizeURLForMetrics(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse runtime config URL: %w", err)
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String(), nil
}

// httpProvider fetches config from an HTTP/HTTPS URL with RED metrics.
type httpProvider struct {
	url             string
	urlForMetrics   string
	client          *http.Client
	requestDuration *prometheus.HistogramVec
}

// newHTTPProvider creates a provider that reads rawURL. urlForMetrics is the label
// value its metrics report, sanitized by the caller, because only the caller can see
// whether two sources sanitize to the same string.
func newHTTPProvider(rawURL, urlForMetrics string, client *http.Client, requestDuration *prometheus.HistogramVec) *httpProvider {
	return &httpProvider{
		url:             rawURL,
		urlForMetrics:   urlForMetrics,
		client:          client,
		requestDuration: requestDuration,
	}
}

// newHTTPRequestDuration creates the shared histogram for all httpProvider instances.
func newHTTPRequestDuration(registerer prometheus.Registerer) *prometheus.HistogramVec {
	return promauto.With(registerer).NewHistogramVec(prometheus.HistogramOpts{
		Name:    "runtime_config_http_request_duration_seconds",
		Help:    "Time spent fetching runtime config from HTTP URLs.",
		Buckets: prometheus.DefBuckets,
		// Use defaults recommended by Prometheus for native histograms.
		NativeHistogramBucketFactor:     1.1,
		NativeHistogramMaxBucketNumber:  100,
		NativeHistogramMinResetDuration: time.Hour,
	}, []string{"url", "status_code"})
}

func (h *httpProvider) Name() string { return h.url }

func (h *httpProvider) Read(ctx context.Context) ([]byte, error) {
	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "dskit-runtimeconfig")

	resp, err := h.client.Do(req)
	if err != nil {
		h.requestDuration.WithLabelValues(h.urlForMetrics, "error").Observe(time.Since(start).Seconds())
		return nil, err
	}
	defer resp.Body.Close()

	statusCode := strconv.Itoa(resp.StatusCode)

	if resp.StatusCode/100 != 2 {
		// Drain body to allow connection reuse.
		_, _ = io.Copy(io.Discard, resp.Body)
		h.requestDuration.WithLabelValues(h.urlForMetrics, statusCode).Observe(time.Since(start).Seconds())
		return nil, &httpError{statusCode: resp.StatusCode, url: h.url}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		h.requestDuration.WithLabelValues(h.urlForMetrics, "error").Observe(time.Since(start).Seconds())
		return nil, fmt.Errorf("read response body: %w", err)
	}

	h.requestDuration.WithLabelValues(h.urlForMetrics, statusCode).Observe(time.Since(start).Seconds())
	return body, nil
}

type httpError struct {
	statusCode int
	url        string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("HTTP %d from %s", e.statusCode, e.url)
}

func isURL(path string) bool {
	return strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")
}
