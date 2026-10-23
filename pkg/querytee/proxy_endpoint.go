package querytee

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/util/server"
)

type ProxyEndpoint struct {
	backends []*ProxyBackend
	metrics  *ProxyMetrics
	logger   log.Logger

	// The route name used to track metrics.
	routeName string

	// Handler for processing requests using the middleware pattern.
	// When set, ServeHTTP uses this instead of the legacy executeBackendRequests.
	queryHandler http.Handler
}

func NewProxyEndpoint(
	backends []*ProxyBackend,
	routeName string,
	metrics *ProxyMetrics,
	logger log.Logger,
) *ProxyEndpoint {
	return &ProxyEndpoint{
		backends:  backends,
		routeName: routeName,
		metrics:   metrics,
		logger:    logger,
	}
}

// WithQueryHandler sets the middleware-based query handlers (logs and metrics) for the endpoint.
// When set, ServeHTTP uses this handler instead of the legacy executeBackendRequests.
func (p *ProxyEndpoint) WithQueryHandler(handler http.Handler) *ProxyEndpoint {
	p.queryHandler = handler
	return p
}

func (p *ProxyEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.metrics.requestsTotal.WithLabelValues(r.Method, p.routeName).Inc()

	if p.queryHandler == nil {
		p.serveWrites(w, r)
		return
	}

	p.queryHandler.ServeHTTP(w, r)
}

// serveWrites serves writes without a queryrangebase.Handler, since write requests cannot be decoded into a queryrangebase.Request.
func (p *ProxyEndpoint) serveWrites(w http.ResponseWriter, r *http.Request) {
	// Send the same request to all backends.
	resCh := make(chan *BackendResponse, len(p.backends))
	go p.executeBackendRequests(r, resCh)

	downstreamRes, err := p.waitBackendResponseForDownstream(resCh)
	if err != nil {
		server.WriteError(err, w)
		return
	}

	if downstreamRes.err != nil {
		server.WriteError(downstreamRes.err, w)
	} else {
		// Copy response headers.
		for key, values := range downstreamRes.headers {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}

		w.WriteHeader(downstreamRes.status)
		if _, err := w.Write(downstreamRes.body); err != nil {
			level.Warn(p.logger).Log("msg", "Unable to write response", "err", err)
		}
	}

	p.metrics.responsesTotal.WithLabelValues(downstreamRes.backend.name, downstreamRes.backend.Alias(), r.Method, p.routeName, detectIssuer(r)).Inc()
}

func (p *ProxyEndpoint) executeBackendRequests(r *http.Request, resCh chan *BackendResponse) {
	var (
		wg     = sync.WaitGroup{}
		err    error
		body   []byte
		query  = r.URL.RawQuery
		issuer = detectIssuer(r)
	)

	if r.Body != nil {
		body, err = io.ReadAll(r.Body)
		if err != nil {
			level.Warn(p.logger).Log("msg", "Unable to read request body", "err", err)
			return
		}
		if err := r.Body.Close(); err != nil {
			level.Warn(p.logger).Log("msg", "Unable to close request body", "err", err)
		}

		r.Body = io.NopCloser(bytes.NewReader(body))
		if err := r.ParseForm(); err != nil {
			level.Warn(p.logger).Log("msg", "Unable to parse form", "err", err)
		}
		query = r.Form.Encode()
	}

	level.Debug(p.logger).Log("msg", "Received request", "path", r.URL.Path, "query", query)

	for _, backend := range p.backends {
		wg.Go(func() {
			var (
				bodyReader io.ReadCloser
				lvl        = level.Debug
			)
			if len(body) > 0 {
				bodyReader = io.NopCloser(bytes.NewReader(body))
			}

			res := backend.ForwardRequest(r, bodyReader)

			// Log with a level based on the backend response.
			if !res.succeeded() {
				lvl = level.Warn
			}

			lvl(p.logger).Log("msg", "Backend response", "path", r.URL.Path, "query", query, "backend", backend.name, "status", res.status, "elapsed", res.duration)
			p.metrics.requestDuration.WithLabelValues(
				res.backend.name,
				res.backend.Alias(),
				r.Method,
				p.routeName,
				strconv.FormatInt(int64(res.statusCode()), 10),
				issuer,
			).Observe(res.duration.Seconds())

			resCh <- res
		})
	}

	// Wait until all backend requests completed.
	wg.Wait()
	close(resCh)
}

// waitBackendResponseForDownstream returns the primary backend's response, even when it is an
// error. It does not fall back to another backend. It returns errNoPrimaryBackend when no backend
// is the primary.
func (p *ProxyEndpoint) waitBackendResponseForDownstream(resCh chan *BackendResponse) (*BackendResponse, error) {
	for res := range resCh {
		if res.backend.isPrimary() {
			return res, nil
		}
	}
	return nil, errNoPrimaryBackend
}

type BackendResponse struct {
	backend  *ProxyBackend
	status   int
	body     []byte
	headers  http.Header
	err      error
	duration time.Duration
	traceID  string
	spanID   string
}

func (r *BackendResponse) succeeded() bool {
	if r.err != nil {
		return false
	}

	// We consider the response successful if it's a 2xx or 4xx (but not 429).
	return (r.status >= 200 && r.status < 300) || (r.status >= 400 && r.status < 500 && r.status != 429)
}

func (r *BackendResponse) statusCode() int {
	if r.err != nil || r.status <= 0 {
		return 500
	}

	return r.status
}

// TODO(twhitney): detectIssuer should also detect Grafana as an issuer
func detectIssuer(r *http.Request) string {
	if strings.HasPrefix(r.Header.Get("User-Agent"), "loki-canary") {
		return canaryIssuer
	}
	return unknownIssuer
}
