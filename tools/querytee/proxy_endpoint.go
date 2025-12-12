package querytee

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/util/server"
	"github.com/grafana/loki/v3/tools/querytee/comparator"
	"github.com/grafana/loki/v3/tools/querytee/goldfish"
)

// contextKey is used for storing values in context
type contextKey int

const (
	// originalHTTPHeadersKey stores the original HTTP headers in context
	originalHTTPHeadersKey contextKey = iota
)

type ResponsesComparator interface {
	Compare(expected, actual []byte, queryEvaluationTime time.Time) (*ComparisonSummary, error)
}

type ComparisonSummary struct {
	skipped        bool
	missingMetrics int
}

type ProxyEndpoint struct {
	backends   []*ProxyBackend
	metrics    *ProxyMetrics
	logger     log.Logger
	comparator comparator.ResponsesComparator

	instrumentCompares bool

	// Whether for this endpoint there's a preferred backend configured.
	hasPreferredBackend bool

	// The route name used to track metrics.
	routeName string

	// Goldfish manager for query sampling and comparison
	goldfishManager goldfish.Manager

	// Handler for processing requests using the middleware pattern.
	// When set, ServeHTTP uses this instead of the legacy executeBackendRequests.
	queryHandler http.Handler
}

func NewProxyEndpoint(
	backends []*ProxyBackend,
	routeName string,
	metrics *ProxyMetrics,
	logger log.Logger,
	comparator comparator.ResponsesComparator,
	instrumentCompares bool,
) *ProxyEndpoint {
	hasPreferredBackend := false
	for _, backend := range backends {
		if backend.preferred {
			hasPreferredBackend = true
			break
		}
	}

	return &ProxyEndpoint{
		backends:            backends,
		routeName:           routeName,
		metrics:             metrics,
		logger:              logger,
		comparator:          comparator,
		hasPreferredBackend: hasPreferredBackend,
		instrumentCompares:  instrumentCompares,
	}
}

// WithGoldfish adds Goldfish manager to the endpoint.
func (p *ProxyEndpoint) WithGoldfish(manager goldfish.Manager) *ProxyEndpoint {
	p.goldfishManager = manager
	return p
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
	// tenant := extractTenant(r)
	tenantID, _, err := tenant.ExtractTenantIDFromHTTPRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Determine if we should sample this query
	shouldSample := false
	if p.goldfishManager != nil {
		shouldSample = p.goldfishManager.ShouldSample(tenantID)
		level.Debug(p.logger).Log(
			"msg", "goldfish sampling decision",
			"tenant", tenantID,
			"sampled", shouldSample,
			"path", r.URL.Path)
	}

	// Send the same request to all backends.
	resCh := make(chan *BackendResponse, len(p.backends))
	go p.executeBackendRequests(r, resCh, shouldSample)

	// Wait for the first response that's feasible to be sent back to the client.
	downstreamRes := p.waitBackendResponseForDownstream(resCh)

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

	p.metrics.responsesTotal.WithLabelValues(downstreamRes.backend.name, r.Method, p.routeName, detectIssuer(r)).Inc()
}

func (p *ProxyEndpoint) executeBackendRequests(r *http.Request, resCh chan *BackendResponse, goldfishSample bool) {
	var (
		wg                  = sync.WaitGroup{}
		err                 error
		body                []byte
		expectedResponseIdx int
		responses           = make([]*BackendResponse, len(p.backends))
		query               = r.URL.RawQuery
		issuer              = detectIssuer(r)
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

	wg.Add(len(p.backends))
	for i, b := range p.backends {
		go func(idx int, backend *ProxyBackend) {
			defer wg.Done()
			var (
				bodyReader io.ReadCloser
				lvl        = level.Debug
			)
			if len(body) > 0 {
				bodyReader = io.NopCloser(bytes.NewReader(body))
			}

			if backend.filter != nil && !backend.filter.Match([]byte(r.URL.String())) {
				lvl(p.logger).Log("msg", "Skipping non-preferred backend", "path", r.URL.Path, "query", query, "backend", backend.name)
				return
			}

			res := backend.ForwardRequest(r, bodyReader)

			// Log with a level based on the backend response.
			if !res.succeeded() {
				lvl = level.Warn
			}

			lvl(p.logger).Log("msg", "Backend response", "path", r.URL.Path, "query", query, "backend", backend.name, "status", res.status, "elapsed", res.duration)
			p.metrics.requestDuration.WithLabelValues(
				res.backend.name,
				r.Method,
				p.routeName,
				strconv.FormatInt(int64(res.statusCode()), 10),
				issuer,
			).Observe(res.duration.Seconds())

			// Keep track of the response if required.
			if p.comparator != nil {
				if backend.preferred {
					expectedResponseIdx = idx
				}
				responses[idx] = res
			}

			resCh <- res
		}(i, b)
	}

	// Wait until all backend requests completed.
	wg.Wait()
	close(resCh)

	// Compare responses.
	if p.comparator != nil {
		expectedResponse := responses[expectedResponseIdx]
		for i := range responses {
			if i == expectedResponseIdx {
				continue
			}
			actualResponse := responses[i]

			result := comparisonSuccess
			summary, err := p.compareResponses(expectedResponse, actualResponse, time.Now().UTC())
			if err != nil {
				level.Error(p.logger).Log("msg", "response comparison failed",
					"backend-name", p.backends[i].name,
					"route-name", p.routeName,
					"query", r.URL.RawQuery, "err", err)
				result = comparisonFailed
			} else if summary != nil && summary.Skipped {
				result = comparisonSkipped
			}

			if p.instrumentCompares && summary != nil {
				p.metrics.missingMetrics.WithLabelValues(p.backends[i].name, p.routeName, result, issuer).Observe(float64(summary.MissingMetrics))
			}
			p.metrics.responsesComparedTotal.WithLabelValues(p.backends[i].name, p.routeName, result, issuer).Inc()
		}
	}

	// Process with Goldfish if enabled and sampled
	if goldfishSample && p.goldfishManager != nil && len(responses) >= 2 {
		// Use preferred backend as Cell A, first non-preferred as Cell B
		var cellAResp, cellBResp *BackendResponse

		// Find preferred backend response
		for _, resp := range responses {
			if resp != nil && resp.backend.preferred {
				cellAResp = resp
				break
			}
		}

		// Find first non-preferred backend response
		for _, resp := range responses {
			if resp != nil && !resp.backend.preferred {
				cellBResp = resp
				break
			}
		}

		if cellAResp != nil && cellBResp != nil {
			tenantID, _, _ := tenant.ExtractTenantIDFromHTTPRequest(r)
			level.Info(p.logger).Log("msg", "Processing query with Goldfish",
				"tenant", tenantID,
				"query", r.URL.Query().Get("query"),
				"cellA_backend", cellAResp.backend.name,
				"cellA_status", cellAResp.status,
				"cellB_backend", cellBResp.backend.name,
				"cellB_status", cellBResp.status)
			go p.processWithGoldfish(r, cellAResp, cellBResp)
		} else {
			level.Warn(p.logger).Log("msg", "Unable to process query with Goldfish: missing backend responses")
		}
	}
}

func (p *ProxyEndpoint) waitBackendResponseForDownstream(resCh chan *BackendResponse) *BackendResponse {
	var (
		responses                 = make([]*BackendResponse, 0, len(p.backends))
		preferredResponseReceived = false
	)

	for res := range resCh {
		// If the response is successful we can immediately return it if:
		// - There's no preferred backend configured
		// - Or this response is from the preferred backend
		// - Or the preferred backend response has already been received and wasn't successful
		if res.succeeded() && (!p.hasPreferredBackend || res.backend.preferred || preferredResponseReceived) {
			return res
		}

		// If we received a non successful response from the preferred backend, then we can
		// return the first successful response received so far (if any).
		if res.backend.preferred && !res.succeeded() {
			preferredResponseReceived = true

			for _, prevRes := range responses {
				if prevRes.succeeded() {
					return prevRes
				}
			}
		}

		// Otherwise we keep track of it for later.
		responses = append(responses, res)
	}

	// No successful response, so let's pick the first one.
	return responses[0]
}

func (p *ProxyEndpoint) compareResponses(expectedResponse, actualResponse *BackendResponse, queryEvalTime time.Time) (*comparator.ComparisonSummary, error) {
	if expectedResponse.err != nil {
		return &comparator.ComparisonSummary{Skipped: true}, nil
	}

	if actualResponse.err != nil {
		return nil, fmt.Errorf("skipped comparison of response because the request to the secondary backend failed: %w", actualResponse.err)
	}

	// compare response body only if we get a 200
	if expectedResponse.status != 200 {
		return &comparator.ComparisonSummary{Skipped: true}, nil
	}

	if actualResponse.status != 200 {
		return nil, fmt.Errorf("skipped comparison of response because we got status code %d from secondary backend's response", actualResponse.status)
	}

	if expectedResponse.status != actualResponse.status {
		return nil, fmt.Errorf("expected status code %d but got %d", expectedResponse.status, actualResponse.status)
	}

	return p.comparator.Compare(expectedResponse.body, actualResponse.body, queryEvalTime)
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

// processWithGoldfish sends the query and responses to Goldfish for comparison
func (p *ProxyEndpoint) processWithGoldfish(r *http.Request, cellAResp, cellBResp *BackendResponse) {
	cellAGoldfishResp := &goldfish.BackendResponse{
		BackendName: cellAResp.backend.name,
		Status:      cellAResp.status,
		Body:        cellAResp.body,
		Duration:    cellAResp.duration,
		TraceID:     cellAResp.traceID,
		SpanID:      cellAResp.spanID,
	}

	cellBGoldfishResp := &goldfish.BackendResponse{
		BackendName: cellBResp.backend.name,
		Status:      cellBResp.status,
		Body:        cellBResp.body,
		Duration:    cellBResp.duration,
		TraceID:     cellBResp.traceID,
		SpanID:      cellBResp.spanID,
	}

	p.goldfishManager.SendToGoldfish(r, cellAGoldfishResp, cellBGoldfishResp)
}
