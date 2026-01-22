package querytee

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/querytee/comparator"
	"github.com/grafana/loki/v3/pkg/querytee/goldfish"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

// FanOutHandler implements queryrangebase.Handler and fans out requests to multiple backends.
// It returns the preferred backend's response as soon as ready, while capturing remaining
// responses for goldfish comparison in the background.
type FanOutHandler struct {
	backends                      []*ProxyBackend
	codec                         queryrangebase.Codec
	goldfishManager               goldfish.Manager
	logger                        log.Logger
	metrics                       *ProxyMetrics
	routeName                     string
	comparator                    comparator.ResponsesComparator
	instrumentCompares            bool
	routingMode                   RoutingMode
	raceTolerance                 time.Duration
	addRoutingDecisionsToWarnings bool
}

// FanOutHandlerConfig holds configuration for creating a FanOutHandler.
type FanOutHandlerConfig struct {
	Backends                      []*ProxyBackend
	Codec                         queryrangebase.Codec
	GoldfishManager               goldfish.Manager
	Logger                        log.Logger
	Metrics                       *ProxyMetrics
	RouteName                     string
	Comparator                    comparator.ResponsesComparator
	InstrumentCompares            bool
	RoutingMode                   RoutingMode
	RaceTolerance                 time.Duration
	AddRoutingDecisionsToWarnings bool
}

// NewFanOutHandler creates a new FanOutHandler.
func NewFanOutHandler(cfg FanOutHandlerConfig) *FanOutHandler {
	return &FanOutHandler{
		backends:                      cfg.Backends,
		codec:                         cfg.Codec,
		goldfishManager:               cfg.GoldfishManager,
		logger:                        cfg.Logger,
		metrics:                       cfg.Metrics,
		routeName:                     cfg.RouteName,
		comparator:                    cfg.Comparator,
		instrumentCompares:            cfg.InstrumentCompares,
		routingMode:                   cfg.RoutingMode,
		raceTolerance:                 cfg.RaceTolerance,
		addRoutingDecisionsToWarnings: cfg.AddRoutingDecisionsToWarnings,
	}
}

// backendResult holds the result from a single backend request.
type backendResult struct {
	backend     *ProxyBackend
	backendResp *BackendResponse
	response    queryrangebase.Response
	err         error
}

// shouldCompare returns true if the given backend result should be compared against the preferred result.
func (h *FanOutHandler) shouldCompare(r *backendResult, preferV1 bool) bool {
	if h.comparator == nil {
		return false
	}

	if r.backend.v1Preferred && preferV1 {
		return true
	}

	if r.backend.v2Preferred && !preferV1 {
		return true
	}

	return false
}

// Do implements queryrangebase.Handler. It fans out the request to all backends, returns the preferred backend's
// response, and captures remaining responses for goldfish comparison in the background.
func (h *FanOutHandler) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	httpReq, err := h.codec.EncodeRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}

	// Preserve original headers lost during codec decode/encode cycle.
	// Only add headers that weren't already set by the codec to avoid duplication.
	if origHeaders := httpreq.ExtractAllHeaders(ctx); origHeaders != nil {
		for k, values := range origHeaders {
			if httpReq.Header.Get(k) == "" {
				for _, v := range values {
					httpReq.Header.Add(k, v)
				}
			}
		}
	}

	issuer := detectIssuer(httpReq)
	user := goldfish.ExtractUserFromQueryTags(httpReq)
	level.Debug(h.logger).Log(
		"msg", "Received request",
		"path", httpReq.URL.Path,
		"query", httpReq.URL.RawQuery,
		"issuer", issuer,
		"user", user,
	)

	// Read body for reuse across backends
	var body []byte
	if httpReq.Body != nil {
		body, err = io.ReadAll(httpReq.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		httpReq.Body.Close()
	}

	// Determine if we should sample this query
	tenants, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to extract tenant IDs: %w", err)
	}
	shouldSample := h.shouldSample(tenants, httpReq)

	results := h.makeBackendRequests(ctx, httpReq, body, req, issuer)
	collected := make([]*backendResult, 0, len(h.backends))

	switch h.routingMode {
	case RoutingModeRace:
		return h.doWithRacing(results, collected, httpReq, shouldSample)
	case RoutingModeV2Preferred:
		return h.doWithPreferred(results, collected, httpReq, shouldSample, false)
	default:
		return h.doWithPreferred(results, collected, httpReq, shouldSample, true)
	}
}

func (h *FanOutHandler) doWithRacing(results <-chan *backendResult, collected []*backendResult, httpReq *http.Request, shouldSample bool) (queryrangebase.Response, error) {
	for i := 0; i < len(h.backends); i++ {
		result := <-results
		collected = append(collected, result)

		//TODO(twhitney): 404s are treated as successful responses, but v2 is missing some metdata endpoints that we should fallback to v1 for
		if result.err == nil && result.backendResp.succeeded() {
			winner := result
			remaining := len(h.backends) - i - 1

			if result.backend.v1Preferred && h.raceTolerance > 0 {
				select {
				case r2 := <-results:
					collected = append(collected, r2)
					if r2.err == nil && r2.backendResp.succeeded() {
						winner = r2
						remaining = len(h.backends) - i - 2
					}
				case <-time.After(h.raceTolerance):
				}
			}

			return h.finishRace(winner, remaining, httpReq, results, collected, shouldSample)
		}
	}

	return h.returnFallback(collected)
}

func (h *FanOutHandler) doWithPreferred(results <-chan *backendResult, collected []*backendResult, httpReq *http.Request, shouldSample bool, preferV1 bool) (queryrangebase.Response, error) {
	for i := 0; i < len(h.backends); i++ {
		result := <-results
		collected = append(collected, result)

		isPreferred := (preferV1 && result.backend.v1Preferred) || (!preferV1 && result.backend.v2Preferred)
		if isPreferred {
			if !result.backendResp.succeeded() {
				continue
			}

			// 404s are treated as successful, but v2 does not implement all metadata endpoints, so in that case fall back to v1
			if !preferV1 && result.backendResp.status == 404 {
				continue
			}

			remaining := len(h.backends) - i - 1
			go func() {
				h.collectRemainingAndCompare(remaining, httpReq, results, collected, shouldSample, preferV1)
			}()

			// when the preferred backends succeeds, but with error, that indicates an invalid request
			// return the error to the client
			if result.err != nil {
				return &NonDecodableResponse{
					StatusCode: result.backendResp.status,
					Body:       result.backendResp.body,
				}, result.err
			}

			if result.response != nil && h.addRoutingDecisionsToWarnings {
				addWarningToResponse(result.response, fmt.Sprintf("used response from preferred backend %s", result.backend.name))
			}

			return result.response, nil
		}
	}

	// if we get here, no preferred backend was found or the preferred backend
	// failed. in this case, return any successful response or an error if all failed
	return h.returnFallback(collected)
}

func (h *FanOutHandler) returnFallback(collected []*backendResult) (queryrangebase.Response, error) {
	for _, result := range collected {
		if result.err == nil && result.backendResp.succeeded() {
			return result.response, nil
		}
	}

	if len(collected) > 0 && collected[0].err != nil {
		result := collected[0]
		return &NonDecodableResponse{
			StatusCode: result.backendResp.status,
			Body:       result.backendResp.body,
		}, result.err
	}
	return nil, fmt.Errorf("all backends failed")
}

// finishRace records the race winner and spawns a goroutine to collect remaining results.
func (h *FanOutHandler) finishRace(winner *backendResult, remaining int, httpReq *http.Request, results <-chan *backendResult, collected []*backendResult, shouldSample bool) (queryrangebase.Response, error) {
	h.metrics.raceWins.WithLabelValues(
		winner.backend.name,
		winner.backend.Alias(),
		h.routeName,
		detectIssuer(httpReq),
	).Inc()

	go func() {
		h.collectRemainingAndCompare(remaining, httpReq, results, collected, shouldSample, true)
	}()

	if winner.response != nil && h.addRoutingDecisionsToWarnings {
		addWarningToResponse(winner.response, fmt.Sprintf("%s backend won the race", winner.backend.name))
	}

	return winner.response, nil
}

// collectRemainingAndCompare collects remaining backend results, performs comparisons,
// and processes goldfish sampling. Should be called asynchronously to not block preferred response from returning.
func (h *FanOutHandler) collectRemainingAndCompare(remaining int, httpReq *http.Request, results <-chan *backendResult, collected []*backendResult, shouldSample bool, preferV1 bool) {
	issuer := detectIssuer(httpReq)
	for range remaining {
		r := <-results
		collected = append(collected, r)
	}

	var preferredResult *backendResult
	for _, r := range collected {
		if r.backend.v1Preferred && preferV1 {
			preferredResult = r
			break
		}

		if r.backend.v2Preferred && !preferV1 {
			preferredResult = r
			break
		}
	}

	for _, r := range collected {
		if h.shouldCompare(r, preferV1) {
			result := comparisonSuccess
			summary, err := h.compareResponses(preferredResult.backendResp, r.backendResp, time.Now().UTC())
			if err != nil {
				level.Error(h.logger).Log("msg", "response comparison failed",
					"backend-name", r.backend.name,
					"route-name", h.routeName,
					"query", httpReq.URL.RawQuery,
					"err", err)
				result = comparisonFailed
			} else if summary != nil && summary.Skipped {
				result = comparisonSkipped
			}

			if h.instrumentCompares && summary != nil {
				h.metrics.missingMetrics.WithLabelValues(
					r.backend.name, r.backend.Alias(), h.routeName, result, issuer,
				).Observe(float64(summary.MissingMetrics))
			}
			h.metrics.responsesComparedTotal.WithLabelValues(
				r.backend.name, r.backend.Alias(), h.routeName, result, issuer,
			).Inc()
		}
	}

	if shouldSample {
		h.processGoldfishComparison(httpReq, preferredResult, collected, preferV1)
	}
}

func (h *FanOutHandler) compareResponses(expectedResponse, actualResponse *BackendResponse, queryEvalTime time.Time) (*comparator.ComparisonSummary, error) {
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

	return h.comparator.Compare(expectedResponse.body, actualResponse.body, queryEvalTime)
}

// executeBackendRequest executes a request to a single backend.
func (h *FanOutHandler) executeBackendRequest(
	ctx context.Context,
	httpReq *http.Request,
	body []byte,
	backend *ProxyBackend,
	req queryrangebase.Request,
) *backendResult {
	start := time.Now()

	// Clone the request for this backend
	clonedReq := httpReq.Clone(ctx)
	if len(body) > 0 {
		clonedReq.Body = io.NopCloser(bytes.NewReader(body))
	}

	// Check filter
	if backend.filter != nil && !backend.filter.Match([]byte(httpReq.URL.String())) {
		level.Debug(h.logger).Log(
			"msg", "Skipping backend due to filter",
			"backend", backend.name,
			"path", httpReq.URL.Path,
		)
		return &backendResult{
			backend: backend,
			backendResp: &BackendResponse{
				duration: time.Since(start),
			},
			err: fmt.Errorf("filtered out"),
		}
	}

	// Forward request to backend
	backendResp := backend.ForwardRequest(clonedReq, clonedReq.Body)

	result := &backendResult{
		backend:     backend,
		backendResp: backendResp,
	}

	if backendResp.err != nil {
		result.err = backendResp.err
		return result
	}

	// Decode the response
	httpResp := &http.Response{
		StatusCode: backendResp.status,
		Header:     backendResp.headers,
		Body:       io.NopCloser(bytes.NewReader(backendResp.body)),
	}

	response, err := h.codec.DecodeResponse(ctx, httpResp, req)
	if err != nil {
		result.err = fmt.Errorf("failed to decode response from %s: %w", backend.name, err)
		return result
	}

	result.response = response
	return result
}

// recordMetrics records request duration metrics.
func (h *FanOutHandler) recordMetrics(result *backendResult, method, issuer string) {
	if h.metrics == nil {
		return
	}

	h.metrics.responsesTotal.WithLabelValues(
		result.backend.name,
		result.backend.Alias(),
		method,
		h.routeName,
		issuer,
	).Inc()

	h.metrics.requestDuration.WithLabelValues(
		result.backend.name,
		result.backend.Alias(),
		method,
		h.routeName,
		strconv.FormatInt(int64(result.backendResp.status), 10),
		issuer,
	).Observe(result.backendResp.duration.Seconds())
}

// processGoldfishComparison processes responses for goldfish comparison.
func (h *FanOutHandler) processGoldfishComparison(httpReq *http.Request, preferredResult *backendResult, results []*backendResult, preferV1 bool) {
	if h.goldfishManager == nil || len(results) < 2 || preferredResult == nil {
		return
	}
	tenantID, _, _ := tenant.ExtractTenantIDFromHTTPRequest(httpReq)

	// Find preferred and non-preferred responses
	for _, r := range results {
		if r.err != nil || (preferV1 && r.backend.v1Preferred) || (!preferV1 && r.backend.v2Preferred) {
			continue
		}
		level.Info(h.logger).Log("msg", "processing responses with Goldfish",
			"tenant", tenantID,
			"query", httpReq.URL.Query().Get("query"),
			"cellA_backend", preferredResult.backend.name,
			"cellA_status", preferredResult.backendResp.status,
			"cellB_backend", r.backend.name,
			"cellB_status", r.backendResp.status,
		)

		// Re-encode responses to capture body bytes for goldfish
		h.sendToGoldfish(httpReq, preferredResult, r)
	}
}

// sendToGoldfish sends the responses to goldfish for comparison.
func (h *FanOutHandler) sendToGoldfish(httpReq *http.Request, cellA, cellB *backendResult) {
	cellAResp := &goldfish.BackendResponse{
		BackendName: cellA.backend.name,
		Status:      cellA.backendResp.status,
		Body:        cellA.backendResp.body,
		Duration:    cellA.backendResp.duration,
		TraceID:     cellA.backendResp.traceID,
		SpanID:      cellA.backendResp.spanID,
	}

	cellBResp := &goldfish.BackendResponse{
		BackendName: cellB.backend.name,
		Status:      cellB.backendResp.status,
		Body:        cellB.backendResp.body,
		Duration:    cellB.backendResp.duration,
		TraceID:     cellB.backendResp.traceID,
		SpanID:      cellB.backendResp.spanID,
	}

	h.goldfishManager.SendToGoldfish(httpReq, cellAResp, cellBResp)
}

// WithMetrics sets metrics for the handler.
func (h *FanOutHandler) WithMetrics(metrics *ProxyMetrics) *FanOutHandler {
	h.metrics = metrics
	return h
}

// WithComparator sets the response comparator.
func (h *FanOutHandler) WithComparator(comparator comparator.ResponsesComparator) *FanOutHandler {
	h.comparator = comparator
	return h
}

// shouldSample determines if a query should be sampled for goldfish comparison.
func (h *FanOutHandler) shouldSample(tenants []string, httpReq *http.Request) bool {
	if h.goldfishManager == nil {
		return false
	}

	for _, tenant := range tenants {
		if h.goldfishManager.ShouldSample(tenant) {
			level.Debug(h.logger).Log(
				"msg", "Goldfish sampling decision",
				"tenant", tenant,
				"sampled", true,
				"path", httpReq.URL.Path)
			return true
		}
	}

	return false
}

// makeBackendRequests initiates backend requests and returns a channel for receiving results.
func (h *FanOutHandler) makeBackendRequests(
	ctx context.Context,
	httpReq *http.Request,
	body []byte,
	req queryrangebase.Request,
	issuer string,
) chan *backendResult {
	results := make(chan *backendResult, len(h.backends))

	for i, backend := range h.backends {
		go func(_ int, b *ProxyBackend) {
			result := h.executeBackendRequest(ctx, httpReq, body, b, req)

			// ensure a valid status code is set in case of error
			if result.err != nil && result.backendResp.status == 0 {
				result.backendResp.status = statusCodeFromError(result.err)
			}

			results <- result

			// Record metrics
			h.recordMetrics(result, httpReq.Method, issuer)
		}(i, backend)
	}

	return results
}

// statusCodeFromError determines the appropriate HTTP status code when a backend request fails.
func statusCodeFromError(err error) int {
	if errors.Is(err, context.Canceled) {
		return 499 // Client Closed Request
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout // 504
	}

	return http.StatusInternalServerError
}
