package querytee

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/v3/tools/querytee/goldfish"
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
	comparator ResponsesComparator

	instrumentCompares bool

	// Whether for this endpoint there's a preferred backend configured.
	hasPreferredBackend bool

	// The route name used to track metrics.
	routeName string

	// Goldfish manager for query sampling and comparison
	goldfishManager *goldfish.Manager
}

func NewProxyEndpoint(backends []*ProxyBackend, routeName string, metrics *ProxyMetrics, logger log.Logger, comparator ResponsesComparator, instrumentCompares bool) *ProxyEndpoint {
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
func (p *ProxyEndpoint) WithGoldfish(manager *goldfish.Manager) *ProxyEndpoint {
	p.goldfishManager = manager
	return p
}

func (p *ProxyEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract tenant for sampling decision
	tenant := extractTenant(r)

	// Determine if we should sample this query
	shouldSample := false
	if p.goldfishManager != nil {
		shouldSample = p.goldfishManager.ShouldSample(tenant)
		level.Debug(p.logger).Log("msg", "Goldfish sampling decision", "tenant", tenant, "sampled", shouldSample, "path", r.URL.Path)
	}

	// Send the same request to all backends.
	resCh := make(chan *backendResponse, len(p.backends))
	go p.executeBackendRequests(r, resCh, shouldSample)

	// Wait for the first response that's feasible to be sent back to the client.
	downstreamRes := p.waitBackendResponseForDownstream(resCh)

	if downstreamRes.err != nil {
		http.Error(w, downstreamRes.err.Error(), http.StatusInternalServerError)
	} else {
		w.WriteHeader(downstreamRes.status)
		if _, err := w.Write(downstreamRes.body); err != nil {
			level.Warn(p.logger).Log("msg", "Unable to write response", "err", err)
		}
	}

	p.metrics.responsesTotal.WithLabelValues(downstreamRes.backend.name, r.Method, p.routeName, detectIssuer(r)).Inc()
}

func (p *ProxyEndpoint) executeBackendRequests(r *http.Request, resCh chan *backendResponse, goldfishSample bool) {
	var (
		wg                  = sync.WaitGroup{}
		err                 error
		body                []byte
		expectedResponseIdx int
		responses           = make([]*backendResponse, len(p.backends))
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
				start      = time.Now()
				lvl        = level.Debug
			)
			if len(body) > 0 {
				bodyReader = io.NopCloser(bytes.NewReader(body))
			}

			if backend.filter != nil && !backend.filter.Match([]byte(r.URL.String())) {
				lvl(p.logger).Log("msg", "Skipping non-preferred backend", "path", r.URL.Path, "query", query, "backend", backend.name)
				return
			}

			status, body, err := backend.ForwardRequest(r, bodyReader)
			elapsed := time.Since(start)

			res := &backendResponse{
				backend:  backend,
				status:   status,
				body:     body,
				err:      err,
				duration: elapsed,
			}

			// Log with a level based on the backend response.
			if !res.succeeded() {
				lvl = level.Warn
			}

			lvl(p.logger).Log("msg", "Backend response", "path", r.URL.Path, "query", query, "backend", backend.name, "status", status, "elapsed", elapsed)
			p.metrics.requestDuration.WithLabelValues(
				res.backend.name,
				r.Method,
				p.routeName,
				strconv.Itoa(res.statusCode()),
				issuer,
			).Observe(elapsed.Seconds())

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
			} else if summary != nil && summary.skipped {
				result = comparisonSkipped
			}

			if p.instrumentCompares && summary != nil {
				p.metrics.missingMetrics.WithLabelValues(p.backends[i].name, p.routeName, result, issuer).Observe(float64(summary.missingMetrics))
			}
			p.metrics.responsesComparedTotal.WithLabelValues(p.backends[i].name, p.routeName, result, issuer).Inc()
		}
	}

	// Process with Goldfish if enabled and sampled
	if goldfishSample && p.goldfishManager != nil && len(responses) >= 2 {
		// Use preferred backend as Cell A, first non-preferred as Cell B
		var cellAResp, cellBResp *backendResponse

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
			level.Info(p.logger).Log("msg", "Processing query with Goldfish",
				"tenant", extractTenant(r),
				"query", r.URL.Query().Get("query"),
				"cellA_backend", cellAResp.backend.name,
				"cellA_status", cellAResp.status,
				"cellB_backend", cellBResp.backend.name,
				"cellB_status", cellBResp.status)
			go p.processWithGoldfish(r, cellAResp, cellBResp)
		}
	}
}

func (p *ProxyEndpoint) waitBackendResponseForDownstream(resCh chan *backendResponse) *backendResponse {
	var (
		responses                 = make([]*backendResponse, 0, len(p.backends))
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

func (p *ProxyEndpoint) compareResponses(expectedResponse, actualResponse *backendResponse, queryEvalTime time.Time) (*ComparisonSummary, error) {
	if expectedResponse.err != nil {
		return &ComparisonSummary{skipped: true}, nil
	}

	if actualResponse.err != nil {
		return nil, fmt.Errorf("skipped comparison of response because the request to the secondary backend failed: %w", actualResponse.err)
	}

	// compare response body only if we get a 200
	if expectedResponse.status != 200 {
		return &ComparisonSummary{skipped: true}, nil
	}

	if actualResponse.status != 200 {
		return nil, fmt.Errorf("skipped comparison of response because we got status code %d from secondary backend's response", actualResponse.status)
	}

	if expectedResponse.status != actualResponse.status {
		return nil, fmt.Errorf("expected status code %d but got %d", expectedResponse.status, actualResponse.status)
	}

	return p.comparator.Compare(expectedResponse.body, actualResponse.body, queryEvalTime)
}

type backendResponse struct {
	backend  *ProxyBackend
	status   int
	body     []byte
	err      error
	duration time.Duration
}

func (r *backendResponse) succeeded() bool {
	if r.err != nil {
		return false
	}

	// We consider the response successful if it's a 2xx or 4xx (but not 429).
	return (r.status >= 200 && r.status < 300) || (r.status >= 400 && r.status < 500 && r.status != 429)
}

func (r *backendResponse) statusCode() int {
	if r.err != nil || r.status <= 0 {
		return 500
	}

	return r.status
}

func detectIssuer(r *http.Request) string {
	if strings.HasPrefix(r.Header.Get("User-Agent"), "loki-canary") {
		return canaryIssuer
	}
	return unknownIssuer
}

// extractTenant extracts the tenant ID from the X-Scope-OrgID header.
// Returns "anonymous" if no tenant header is present.
func extractTenant(r *http.Request) string {
	tenant := r.Header.Get("X-Scope-OrgID")
	if tenant == "" {
		return "anonymous"
	}
	return tenant
}

// processWithGoldfish sends the query and responses to Goldfish for comparison
func (p *ProxyEndpoint) processWithGoldfish(r *http.Request, cellAResp, cellBResp *backendResponse) {
	// Use a detached context with timeout since this runs async
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Capture response data with actual durations
	cellAData, err := goldfish.CaptureResponse(&http.Response{
		StatusCode: cellAResp.status,
		Body:       io.NopCloser(bytes.NewReader(cellAResp.body)),
	}, cellAResp.duration)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to capture cell A response", "err", err)
		return
	}

	cellBData, err := goldfish.CaptureResponse(&http.Response{
		StatusCode: cellBResp.status,
		Body:       io.NopCloser(bytes.NewReader(cellBResp.body)),
	}, cellBResp.duration)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to capture cell B response", "err", err)
		return
	}

	p.goldfishManager.ProcessQueryPair(ctx, r, cellAData, cellBData)
}
