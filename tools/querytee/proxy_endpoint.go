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
	resCh := make(chan *BackendResponse, len(p.backends))
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

func (p *ProxyEndpoint) executeBackendRequests(r *http.Request, resCh chan *BackendResponse, goldfishSample bool) {
	// Check if query splitting is needed for goldfish comparison
	if p.goldfishManager != nil && p.goldfishManager.ComparisonMinAge() > 0 && goldfishSample {
		minAge := p.goldfishManager.ComparisonMinAge()

		// Check if query is entirely recent (skip goldfish)
		if isQueryEntirelyRecent(r, minAge) {
			level.Debug(p.logger).Log("msg", "query is entirely recent, skipping goldfish comparison")
			goldfishSample = false
		} else {
			// Check if query needs splitting
			needsSplit, splitPoint, step, err := shouldSplitQuery(r, minAge)
			if err == nil && needsSplit {
				level.Info(p.logger).Log(
					"msg", "splitting query for goldfish comparison",
					"split_point", splitPoint,
					"step", step,
					"min_age", minAge,
					"query", r.URL.RawQuery,
				)

				// Execute split query logic
				p.executeSplitQuery(r, resCh, splitPoint, step, goldfishSample)
				return
			}
		}
	}

	var (
		body                []byte
		expectedResponseIdx int
		responses           = make([]*BackendResponse, len(p.backends))
		query               = r.URL.RawQuery
		issuer              = detectIssuer(r)
	)

	level.Debug(p.logger).Log("msg", "Received request", "path", r.URL.Path, "query", query)

	body = p.readAndRestoreRequestBody(r)

	var wg = sync.WaitGroup{}
	p.executeBackendRequestsParallel(backendRequestConfig{
		backends:             p.backends,
		request:              r,
		body:                 body,
		responses:            responses,
		preferredResponseIdx: &expectedResponseIdx,
		resCh:                resCh,
		issuer:               issuer,
		trackForComparison:   p.comparator != nil || (p.goldfishManager != nil && goldfishSample),
	}, &wg)
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

	if goldfishSample {
		p.processResponsesWithGoldfish(r, responses)
	}
}

// executeSplitQuery handles queries that need to be split based on data age.
// It executes the old part of the query to all backends for comparison, and the recent
// part only to the preferred backend, then concatenates the results.
func (p *ProxyEndpoint) executeSplitQuery(
	r *http.Request,
	resCh chan *BackendResponse,
	splitPoint time.Time,
	step time.Duration,
	goldfishSample bool) {
	issuer := detectIssuer(r)
	oldQuery, recentQuery, err := createSplitRequests(r, splitPoint, step)
	if err != nil {
		level.Error(p.logger).Log("msg", "Failed to create split requests", "err", err)
		// Fall back to normal execution without goldfish comparison
		p.executeBackendRequests(r, resCh, false)
		return
	}

	level.Debug(p.logger).Log("msg", "executing split query",
		"old_query", oldQuery.URL.RawQuery,
		"recent_query", recentQuery.URL.RawQuery,
		"split_point", splitPoint,
		"step", step)

	// Find preferred backend for recent query
	var preferredBackend *ProxyBackend
	for _, b := range p.backends {
		if b.preferred {
			preferredBackend = b
			break
		}
	}

	if preferredBackend == nil {
		level.Error(p.logger).Log(
			"msg", "no preferred backend found for recent query. must have a preferred backend when splitting queries.",
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"issuer", issuer,
		)
		resCh <- &BackendResponse{
			err:    fmt.Errorf("no preferred backend found for recent query. must have a preferred backend when splitting queries."),
			status: 500,
		}
		close(resCh)
		return
	}

	// Execute old query to all backends (for comparison)
	var (
		oldResponses         = make([]*BackendResponse, len(p.backends))
		preferredResponseIdx int
		wg                   = sync.WaitGroup{}
	)

	body := p.readAndRestoreRequestBody(oldQuery)
	p.executeBackendRequestsParallel(backendRequestConfig{
		backends:             p.backends,
		request:              oldQuery,
		body:                 body,
		responses:            oldResponses,
		preferredResponseIdx: &preferredResponseIdx,
		issuer:               issuer,
		trackForComparison:   true,
	}, &wg)

	// Execute recent query to preferred backend only
	recentResponses := make([]*BackendResponse, 1) // Only one backend (preferred)
	recentBody := p.readAndRestoreRequestBody(recentQuery)
	p.executeBackendRequestsParallel(backendRequestConfig{
		backends:           []*ProxyBackend{preferredBackend},
		request:            recentQuery,
		body:               recentBody,
		responses:          recentResponses,
		issuer:             issuer,
		trackForComparison: true,
	}, &wg)
	wg.Wait()

	recentRes := recentResponses[0]
	if recentRes == nil || !recentRes.succeeded() {
		level.Error(p.logger).Log(
			"msg", "recent query was not executed or failed",
			"backend", preferredBackend.name,
			"path", recentQuery.URL.Path,
			"issuer", issuer,
		)
		resCh <- &BackendResponse{
			err:    fmt.Errorf("recent query to preferred backend was not executed or failed"),
			status: 500,
		}
		close(resCh)
		return
	}

	lvl := level.Debug
	if !recentRes.succeeded() {
		lvl = level.Warn
	}
	lvl(p.logger).Log(
		"msg", "backend response (recent query)",
		"backend", preferredBackend.name,
		"status", recentRes.status,
		"elapsed", recentRes.duration,
		"issuer", issuer,
	)
	p.metrics.requestDuration.WithLabelValues(
		recentRes.backend.name,
		recentQuery.Method,
		p.routeName,
		strconv.Itoa(recentRes.statusCode()),
		issuer,
	).Observe(recentRes.duration.Seconds())

	preferredOldRes := oldResponses[preferredResponseIdx]
	if preferredOldRes == nil || !preferredOldRes.succeeded() {
		level.Warn(p.logger).Log(
			"msg", "failed to get successful responses from preferred backend for both old and recent queries",
			"issuer", issuer,
		)
		// Return whichever succeeded, or old if both failed
		if preferredOldRes != nil {
			resCh <- preferredOldRes
		} else {
			resCh <- recentRes
		}
		close(resCh)
		return
	}

	preferredConcatenatedResp, err := concatenateResponses(preferredOldRes, recentRes)
	if err != nil {
		level.Error(p.logger).Log("msg", "Failed to concatenate responses", "err", err)
		// Fall back to old response
		resCh <- preferredOldRes
		close(resCh)
		return
	}
	resCh <- preferredConcatenatedResp

	for i, resp := range oldResponses {
		if i == preferredResponseIdx {
			continue
		}

		concatenatedResp, err := concatenateResponses(resp, recentRes)
		if err != nil {
			level.Error(p.logger).Log("msg", "Failed to concatenate responses", "err", err)
			continue
		}
		resCh <- concatenatedResp

		if p.comparator != nil {
			result := comparisonSuccess
			summary, err := p.compareResponses(preferredConcatenatedResp, concatenatedResp, time.Now().UTC())
			if err != nil {
				level.Error(p.logger).Log("msg", "response comparison failed",
					"backend-name", p.backends[i].name,
					"route-name", p.routeName,
					"query", r.URL.RawQuery,
					"issuer", issuer,
					"err", err,
				)
				result = comparisonFailed
			} else if summary != nil && summary.skipped {
				result = comparisonSkipped
			}

			if p.instrumentCompares && summary != nil {
				p.metrics.missingMetrics.WithLabelValues(
					p.backends[i].name,
					p.routeName,
					result,
					issuer,
				).Observe(float64(summary.missingMetrics))
			}
			p.metrics.responsesComparedTotal.WithLabelValues(
				p.backends[i].name,
				p.routeName,
				result,
				issuer,
			).Inc()
		}
	}
	close(resCh)

	if goldfishSample {
		p.processResponsesWithGoldfish(oldQuery, oldResponses)
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

func (p *ProxyEndpoint) compareResponses(expectedResponse, actualResponse *BackendResponse, queryEvalTime time.Time) (*ComparisonSummary, error) {
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

type BackendResponse struct {
	backend  *ProxyBackend
	status   int
	body     []byte
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

// processResponsesWithGoldfish attempts to process responses with Goldfish if enabled and responses are available.
func (p *ProxyEndpoint) processResponsesWithGoldfish(r *http.Request, responses []*BackendResponse) {
	if p.goldfishManager == nil || len(responses) < 2 {
		return
	}

	// Find Cell A (preferred) and Cell B (first non-preferred) responses
	var cellAResp, cellBResp *BackendResponse

	for _, resp := range responses {
		if resp == nil {
			continue
		}
		if resp.backend.preferred {
			cellAResp = resp
		} else if cellBResp == nil {
			cellBResp = resp
		}
		// Break early if we have both
		if cellAResp != nil && cellBResp != nil {
			break
		}
	}

	if cellAResp == nil || cellBResp == nil {
		return
	}

	level.Info(p.logger).Log("msg", "processing responses with Goldfish",
		"tenant", extractTenant(r),
		"query", r.URL.Query().Get("query"),
		"cellA_backend", cellAResp.backend.name,
		"cellA_status", cellAResp.status,
		"cellB_backend", cellBResp.backend.name,
		"cellB_status", cellBResp.status)
	go p.processWithGoldfish(r, cellAResp, cellBResp)
}

// processWithGoldfish sends the query and responses to Goldfish for comparison
func (p *ProxyEndpoint) processWithGoldfish(r *http.Request, cellAResp, cellBResp *BackendResponse) {
	// Use a detached context with timeout since this runs async
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Capture response data with actual durations, trace IDs, and span IDs
	cellAData, err := goldfish.CaptureResponse(&http.Response{
		StatusCode: cellAResp.status,
		Body:       io.NopCloser(bytes.NewReader(cellAResp.body)),
	}, cellAResp.duration, cellAResp.traceID, cellAResp.spanID)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to capture cell A response", "err", err)
		return
	}
	cellAData.BackendName = cellAResp.backend.name

	cellBData, err := goldfish.CaptureResponse(&http.Response{
		StatusCode: cellBResp.status,
		Body:       io.NopCloser(bytes.NewReader(cellBResp.body)),
	}, cellBResp.duration, cellBResp.traceID, cellBResp.spanID)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to capture cell B response", "err", err)
		return
	}
	cellBData.BackendName = cellBResp.backend.name

	p.goldfishManager.ProcessQueryPair(ctx, r, cellAData, cellBData)
}

// readAndRestoreRequestBody reads the request body, restores it for future reads,
// and parses the form. Returns the body bytes or nil if there's no body.
func (p *ProxyEndpoint) readAndRestoreRequestBody(r *http.Request) []byte {
	if r.Body == nil {
		return nil
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		level.Warn(p.logger).Log("msg", "Unable to read request body", "err", err)
		return nil
	}

	if err := r.Body.Close(); err != nil {
		level.Warn(p.logger).Log("msg", "Unable to close request body", "err", err)
	}

	// Restore the body for future reads
	r.Body = io.NopCloser(bytes.NewReader(body))

	// Parse form for query encoding
	if err := r.ParseForm(); err != nil {
		level.Warn(p.logger).Log("msg", "Unable to parse form", "err", err)
	}

	return body
}

// backendRequestConfig holds configuration for executing backend requests.
type backendRequestConfig struct {
	backends             []*ProxyBackend
	request              *http.Request
	body                 []byte
	responses            []*BackendResponse
	preferredResponseIdx *int
	resCh                chan *BackendResponse
	issuer               string
	trackForComparison   bool
}

// executeBackendRequestsParallel executes a request to all backends in parallel.
// It handles body cloning, filtering, metrics, and response collection.
func (p *ProxyEndpoint) executeBackendRequestsParallel(cfg backendRequestConfig, wg *sync.WaitGroup) {
	wg.Add(len(cfg.backends))

	for i, b := range cfg.backends {
		go func(idx int, backend *ProxyBackend) {
			defer wg.Done()

			var bodyReader io.ReadCloser
			if len(cfg.body) > 0 {
				bodyReader = io.NopCloser(bytes.NewReader(cfg.body))
			}

			if backend.filter != nil && !backend.filter.Match([]byte(cfg.request.URL.String())) {
				level.Debug(p.logger).Log("msg", "Skipping non-preferred backend", "path", cfg.request.URL.Path, "backend", backend.name)
				return
			}

			res := backend.ForwardRequest(cfg.request, bodyReader)

			lvl := level.Debug
			if !res.succeeded() {
				lvl = level.Warn
			}

			lvl(p.logger).Log("msg", "backend response", "backend", backend.name, "status", res.status, "elapsed", res.duration)

			p.metrics.requestDuration.WithLabelValues(
				res.backend.name,
				cfg.request.Method,
				p.routeName,
				strconv.Itoa(res.statusCode()),
				cfg.issuer,
			).Observe(res.duration.Seconds())

			// Keep track of the response for comparison if required
			if cfg.trackForComparison {
				if backend.preferred && cfg.preferredResponseIdx != nil {
					*cfg.preferredResponseIdx = idx
				}
				if cfg.responses != nil {
					cfg.responses[idx] = res
				}
			}

			// Send to channel if provided
			if cfg.resCh != nil {
				cfg.resCh <- res
			}
		}(i, b)
	}
}
