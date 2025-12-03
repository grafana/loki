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

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/tools/querytee/goldfish"
	"github.com/prometheus/client_golang/prometheus"
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
				level.Debug(p.logger).Log(
					"msg", "splitting query for goldfish comparison",
					"split_point", splitPoint,
					"step", step,
					"min_age", minAge,
					"query", r.URL.RawQuery,
				)

				// Execute split query logic
				p.executeSplitQuery(r, resCh, splitPoint, step, goldfishSample)
				return
			} else {
				level.Debug(p.logger).Log("msg", "query does not need splitting, skipping goldfish comparison")
			}
		}
	}

	var (
		body      []byte
		responses = make([]*BackendResponse, len(p.backends))
		query     = r.URL.RawQuery
		issuer    = detectIssuer(r)
	)

	level.Debug(p.logger).Log(
		"msg", "Received request", 
		"path", r.URL.Path, 
		"query", query,
		"issuer", issuer,
		"user", goldfish.ExtractUserFromQueryTags(r, p.logger),
	)

	body = p.readAndRestoreRequestBody(r)
	_, preferredBackendIdx := p.preferredBackend()

	var wg = sync.WaitGroup{}
	backendExecutor := backendRequestExecutor{
		resCh:           resCh,
		issuer:          issuer,
		logger:          p.logger,
		routeName:       p.routeName,
		requestDuration: p.metrics.requestDuration,
	}

	preferredRespReady := make(chan *BackendResponse, 1)
	notifyOnComplete := map[int]chan *BackendResponse{
		preferredBackendIdx: preferredRespReady,
	}
	backendExecutor.execInParallel(
		p.backends,
		responses,
		r,
		body,
		notifyOnComplete,
		&wg)
	wg.Wait()
	close(resCh)

	// Compare responses.
	if p.comparator != nil {
		expectedResponse := <-preferredRespReady
		for i := range responses {
			actualResponse := responses[i]
			if actualResponse == nil || actualResponse.backend.preferred {
				continue
			}

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

	user := goldfish.ExtractUserFromQueryTags(r, p.logger)
	level.Debug(p.logger).Log("msg", "executing split goldfish query",
		"old_query", oldQuery.URL.RawQuery,
		"recent_query", recentQuery.URL.RawQuery,
		"split_point", splitPoint,
		"step", step,
		"user", user,
	)
	preferredBackend, preferredBackendIdx := p.preferredBackend()

	if preferredBackend == nil {
		level.Error(p.logger).Log(
			"msg", "no preferred backend found for recent query. must have a preferred backend when splitting queries.",
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"issuer", issuer,
			"user", user,
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
		oldResponses    = make([]*BackendResponse, len(p.backends))
		allBackendsWg   = sync.WaitGroup{} // Tracks all backend requests
		recentResponses = make([]*BackendResponse, 1)
		recentWg        = sync.WaitGroup{}
	)

	backendExecutor := backendRequestExecutor{
		issuer:          issuer,
		logger:          p.logger,
		requestDuration: p.metrics.requestDuration,
		routeName:       p.routeName,
	}
	preferredOldRespReady := make(chan *BackendResponse, 1)
	body := p.readAndRestoreRequestBody(oldQuery)
	notifyOnComplete := map[int]chan *BackendResponse{
		preferredBackendIdx: preferredOldRespReady,
	}
	backendExecutor.execInParallel(
		p.backends,
		oldResponses,
		oldQuery,
		body,
		notifyOnComplete,
		&allBackendsWg)

	// Execute recent query to preferred backend only
	recentReady := make(chan *BackendResponse, 1)
	recentBody := p.readAndRestoreRequestBody(recentQuery)
	notifyRecentOnComplete := map[int]chan *BackendResponse{
		0: recentReady,
	}
	backendExecutor.execInParallel(
		[]*ProxyBackend{preferredBackend},
		recentResponses,
		recentQuery,
		recentBody,
		notifyRecentOnComplete,
		&recentWg)

	// Wait for preferred backend's old and recent responses, then return immediately
	// This allows the client to get a response without waiting for other backends
	go func() {
		preferredOldRes := <-preferredOldRespReady
		recentRes := <-recentReady

		level.Debug(p.logger).Log(
			"msg", "backend response (recent query)",
			"backend", preferredBackend.name,
			"status", recentRes.statusCode(),
			"elapsed", recentRes.duration,
			"issuer", issuer,
			"query", recentQuery.URL.RawQuery,
			"user", user,
		)
		p.metrics.requestDuration.WithLabelValues(
			recentRes.backend.name,
			recentQuery.Method,
			p.routeName,
			strconv.Itoa(recentRes.statusCode()),
			issuer,
		).Observe(recentRes.duration.Seconds())

		// no recent split is an error
		if recentRes == nil || !recentRes.succeeded() {
			level.Error(p.logger).Log(
				"msg", "recent query was not executed or failed",
				"backend", preferredBackend.name,
				"path", recentQuery.URL.Path,
				"issuer", issuer,
				"user", user,
			)

			resCh <- &BackendResponse{
				err:    fmt.Errorf("recent query to preferred backend failed and no successful alternatives"),
				status: 500,
			}
			close(resCh)
			return
		}

		// no preferred old split is an error
		// TODO(twhitney): this will no longer be an error when we allow racing
		if preferredOldRes == nil || !preferredOldRes.succeeded() {
			level.Warn(p.logger).Log(
				"msg", "preferred backend old data split failed, but recent succeeded",
				"issuer", issuer,
				"user", user,
			)

			resCh <- &BackendResponse{
				err:    fmt.Errorf("preferred backend old data split failed: %w", err),
				status: 500,
			}
			close(resCh)
			return
		}

		preferredConcatenatedResp, err := concatenateResponses(preferredOldRes, recentRes, r)
		if err != nil {
			level.Error(p.logger).Log(
				"msg", "failed to concatenate preferred backend splits",
				"err", err,
				"backend", preferredBackend.name,
				"query", recentQuery.URL.RawQuery,
				"issuer", issuer,
				"user", user,
			)
			resCh <- &BackendResponse{
				err:    fmt.Errorf("failed to concatenate preferred backend splits: %w", err),
				status: 500,
			}
			close(resCh)
			return
		}

		// send preferred concatenated response immediately
		// TODO(twhitney): this will change when we implement racing
		resCh <- preferredConcatenatedResp
		//TODO(twhitney): do I need to close this channel here? Was that happening in the old executeBackendRequests?
		close(resCh)

		// continue processing other backends in background for comparison and goldfish
		go func() {
			allBackendsWg.Wait()

			// concatenate and compare responses from non-preferred backends
			for i, resp := range oldResponses {
				if resp == nil || resp.backend.preferred {
					continue
				}

				concatenatedResp, err := concatenateResponses(resp, recentRes, r)
				if err != nil {
					level.Warn(p.logger).Log(
						"msg", "failed to concatenate non-preferred backend responses, skipping comparison",
						"err", err,
						"backend", p.backends[i].name,
						"issuer", issuer,
						"user", user,
					)
					continue
				}

				if p.comparator != nil {
					result := comparisonSuccess
					summary, err := p.compareResponses(preferredConcatenatedResp, concatenatedResp, time.Now().UTC())
					if err != nil {
						level.Error(p.logger).Log("msg", "response comparison failed",
							"backend-name", p.backends[i].name,
							"route-name", p.routeName,
							"query", r.URL.RawQuery,
							"issuer", issuer,
							"user", user,
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

			if goldfishSample {
				level.Debug(p.logger).Log(
					"msg", "processing concatenated responses with Goldfish",
					"user", user,
					"issuer", issuer,
					"query", oldQuery.URL.RawQuery,
				)
				p.processResponsesWithGoldfish(oldQuery, oldResponses)
			}
		}()
	}()
}

func (p *ProxyEndpoint) preferredBackend() (*ProxyBackend, int) {
	// Find preferred backend for recent query
	var preferredBackend *ProxyBackend
	preferredBackendIdx := -1
	for i, b := range p.backends {
		if b.preferred {
			preferredBackend = b
			preferredBackendIdx = i
			break
		}
	}
	return preferredBackend, preferredBackendIdx
}

func extractDirection(r *http.Request) logproto.Direction {
	if r == nil {
		return logproto.FORWARD
	}

	// Parse the request to extract direction
	if err := r.ParseForm(); err != nil {
		return logproto.FORWARD
	}

	rangeQuery, err := loghttp.ParseRangeQuery(r)
	if err != nil {
		return logproto.FORWARD
	}

	return rangeQuery.Direction
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

// backendRequestExecutor is used for executing multiple backend requests in parallel and communicating back
// responses in async friendly ways.
type backendRequestExecutor struct {
	resCh           chan *BackendResponse
	issuer          string
	logger          log.Logger
	requestDuration *prometheus.HistogramVec
	routeName       string
}

// execInParallel executes a request to all backends in parallel.
func (ex *backendRequestExecutor) execInParallel(
	backends []*ProxyBackend,
	responses []*BackendResponse,
	request *http.Request,
	body []byte,
	notifyOnComplete map[int]chan *BackendResponse,
	wg *sync.WaitGroup,
) {
	wg.Add(len(backends))

	if len(responses) != len(backends) {
		panic("responses and backends must have the same length")
	}

	for i, b := range backends {
		go func(idx int, backend *ProxyBackend) {
			defer wg.Done()

			var bodyReader io.ReadCloser
			if len(body) > 0 {
				bodyReader = io.NopCloser(bytes.NewReader(body))
			}

			if backend.filter != nil && !backend.filter.Match([]byte(request.URL.String())) {
				level.Debug(ex.logger).Log(
					"msg", "Skipping non-preferred backend",
					"path", request.URL.Path,
					"backend", backend.name,
					"issuer", ex.issuer,
					"user", goldfish.ExtractUserFromQueryTags(request, ex.logger),
				)
				return
			}

			res := backend.ForwardRequest(request, bodyReader)

			lvl := level.Debug
			if !res.succeeded() {
				lvl = level.Warn
			}

			lvl(ex.logger).Log(
				"msg", "backend response",
				"backend", backend.name,
				"status", res.status,
				"elapsed", res.duration,
				"issuer", ex.issuer,
				"user", goldfish.ExtractUserFromQueryTags(request, ex.logger),
			)

			ex.requestDuration.WithLabelValues(
				res.backend.name,
				request.Method,
				ex.routeName,
				strconv.Itoa(res.statusCode()),
				ex.issuer,
			).Observe(res.duration.Seconds())

			// Notify if this backend has a notification channel
			if notifyOnComplete != nil {
				if notifyCh, exists := notifyOnComplete[idx]; exists {
					notifyCh <- res
					close(notifyCh) //only 1 response per backend
				}
			}
			responses[idx] = res

			// Send to channel if provided
			if ex.resCh != nil {
				ex.resCh <- res
			}
		}(i, b)
	}
}
