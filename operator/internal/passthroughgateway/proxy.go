package passthroughgateway

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
)

var lokiWritePaths = []string{
	"/loki/api/v1/push",
	"/api/prom/push",
	"/otlp/v1/logs",
}

type LokiRouter struct {
	writeProxy    *httputil.ReverseProxy
	readProxy     *httputil.ReverseProxy
	logger        logr.Logger
	defaultTenant string
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// NewLokiRouter creates a new router that directs requests to the appropriate upstream.
func NewLokiRouter(cfg *Config, logger logr.Logger) (*LokiRouter, error) {
	transport, err := newTransport(cfg)
	if err != nil {
		return nil, err
	}

	writeProxy, err := newReverseProxy(cfg.WriteUpstreamEndpoint, transport, logger)
	if err != nil {
		return nil, err
	}

	readProxy, err := newReverseProxy(cfg.ReadUpstreamEndpoint, transport, logger)
	if err != nil {
		return nil, err
	}

	return &LokiRouter{
		writeProxy:    writeProxy,
		readProxy:     readProxy,
		logger:        logger,
		defaultTenant: cfg.DefaultTenant,
	}, nil
}

func newTransport(cfg *Config) (*http.Transport, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	tlsConfig, err := BuildUpstreamTLSConfig(cfg.TLSOptions(), cfg.UpstreamCAFile, cfg.UpstreamCertFile, cfg.UpstreamKeyFile)
	if err != nil {
		return nil, err
	}
	transport.TLSClientConfig = tlsConfig

	return transport, nil
}

func newReverseProxy(upstreamEndpoint string, transport *http.Transport, logger logr.Logger) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(upstreamEndpoint)
	if err != nil {
		return nil, err
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host

			if target.Path != "" && target.Path != "/" {
				req.URL.Path, _ = url.JoinPath(target.Path, req.URL.Path)
			}
		},
		Transport: transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.Error(err, "proxy error", "method", r.Method, "path", r.URL.Path)
			w.WriteHeader(http.StatusBadGateway)
		},
	}

	return proxy, nil
}

func isWritePath(path string) bool {
	return slices.ContainsFunc(lokiWritePaths, func(writePath string) bool {
		return strings.HasPrefix(path, writePath)
	})
}

func (r *LokiRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Header.Get("X-Scope-OrgID") == "" {
		if r.defaultTenant == "" {
			r.logger.Error(nil, "missing required header", "header", "X-Scope-OrgID", "path", req.URL.Path, "method", req.Method)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("Bad request: X-Scope-OrgID header is required"))
			return
		}
		req.Header.Set("X-Scope-OrgID", r.defaultTenant)
	}

	if isWritePath(req.URL.Path) {
		r.writeProxy.ServeHTTP(w, req)
		return
	}
	r.readProxy.ServeHTTP(w, req)
}

// InstrumentedHandler wraps an http.Handler with metrics instrumentation.
func InstrumentedHandler(handler http.Handler, metrics *Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metrics.RequestsInFlight.Inc()
		defer metrics.RequestsInFlight.Dec()

		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		handler.ServeHTTP(wrapped, r)

		route := "read"
		if isWritePath(r.URL.Path) {
			route = "write"
		}

		duration := time.Since(start).Seconds()
		metrics.RequestDuration.WithLabelValues(r.Method, route).Observe(duration)
		metrics.RequestsTotal.WithLabelValues(r.Method, strconv.Itoa(wrapped.statusCode)).Inc()
	})
}
