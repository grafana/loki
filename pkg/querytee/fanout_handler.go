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
	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/querytee/comparator"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

// FanOutHandler implements queryrangebase.Handler and fans out requests to multiple backends.
// It returns the preferred backend's response as soon as ready, while capturing remaining
// responses for comparison in the background.
type FanOutHandler struct {
	backends                      []*ProxyBackend
	codec                         queryrangebase.Codec
	logger                        log.Logger
	metrics                       *ProxyMetrics
	routeName                     string
	comparator                    comparator.ResponsesComparator
	instrumentCompares            bool
	addRoutingDecisionsToWarnings bool
}

// FanOutHandlerConfig holds configuration for creating a FanOutHandler.
type FanOutHandlerConfig struct {
	Backends                      []*ProxyBackend
	Codec                         queryrangebase.Codec
	Logger                        log.Logger
	Metrics                       *ProxyMetrics
	RouteName                     string
	Comparator                    comparator.ResponsesComparator
	InstrumentCompares            bool
	AddRoutingDecisionsToWarnings bool
}

// NewFanOutHandler creates a new FanOutHandler.
func NewFanOutHandler(cfg FanOutHandlerConfig) *FanOutHandler {
	return &FanOutHandler{
		backends:                      cfg.Backends,
		codec:                         cfg.Codec,
		logger:                        cfg.Logger,
		metrics:                       cfg.Metrics,
		routeName:                     cfg.RouteName,
		comparator:                    cfg.Comparator,
		instrumentCompares:            cfg.InstrumentCompares,
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
func (h *FanOutHandler) shouldCompare(r *backendResult) bool {
	return h.comparator != nil && r.backend.v1Preferred
}

// Do implements queryrangebase.Handler. It fans out the request to all backends, returns the preferred backend's
// response, and captures remaining responses for comparison in the background.
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
	level.Debug(h.logger).Log(
		"msg", "Received request",
		"path", httpReq.URL.Path,
		"query", httpReq.URL.RawQuery,
		"issuer", issuer,
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

	results := h.makeBackendRequests(ctx, httpReq, body, req, issuer)
	collected := make([]*backendResult, 0, len(h.backends))

	return h.doWithPreferred(results, collected, httpReq)
}

func (h *FanOutHandler) doWithPreferred(results <-chan *backendResult, collected []*backendResult, httpReq *http.Request) (queryrangebase.Response, error) {
	for i := 0; i < len(h.backends); i++ {
		result := <-results
		collected = append(collected, result)

		if result.backend.v1Preferred {
			if !result.backendResp.succeeded() {
				continue
			}

			remaining := len(h.backends) - i - 1
			go func() {
				h.collectRemainingAndCompare(remaining, httpReq, results, collected)
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

// tenantLabel returns the tenant label of the metrics for the request. A multi-tenant request has
// its tenant IDs joined by "|".
func tenantLabel(httpReq *http.Request) (string, error) {
	_, ctx, err := user.ExtractOrgIDFromHTTPRequest(httpReq)
	if err != nil {
		return "", err
	}

	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return "", err
	}
	return tenant.JoinTenantIDs(tenantIDs), nil
}

// collectRemainingAndCompare collects remaining backend results and performs comparisons.
// Should be called asynchronously to not block preferred response from returning.
func (h *FanOutHandler) collectRemainingAndCompare(remaining int, httpReq *http.Request, results <-chan *backendResult, collected []*backendResult) {
	issuer := detectIssuer(httpReq)
	tenantID, err := tenantLabel(httpReq)
	if err != nil {
		level.Warn(h.logger).Log(
			"msg", "failed to extract tenant id from http request",
			"route-name", h.routeName,
			"err", err,
		)
		tenantID = "unknown"
	}
	for range remaining {
		r := <-results
		collected = append(collected, r)
	}

	var preferredResult *backendResult
	for _, r := range collected {
		if r.backend.v1Preferred {
			preferredResult = r
			break
		}
	}

	for _, r := range collected {
		if h.shouldCompare(r) {
			result := comparisonMatch
			summary, err := h.compareResponses(preferredResult.backendResp, r.backendResp, time.Now().UTC())
			if err != nil {
				level.Error(h.logger).Log("msg", "response comparison failed",
					"backend-name", r.backend.name,
					"route-name", h.routeName,
					"query", httpReq.URL.RawQuery,
					"err", err)
				result = comparisonFailed
				if errors.Is(err, comparator.ErrComparisonMismatch) {
					result = comparisonMismatch
				}
			} else if summary != nil && summary.Skipped {
				result = comparisonSkipped
			}

			if h.instrumentCompares && summary != nil {
				h.metrics.missingMetrics.WithLabelValues(
					r.backend.name, r.backend.Alias(), h.routeName, result, issuer,
				).Observe(float64(summary.MissingMetrics))
			}
			h.metrics.responsesComparedTotal.WithLabelValues(
				r.backend.name, r.backend.Alias(), h.routeName, result, issuer, tenantID,
			).Inc()
		}
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

// addWarningToResponse adds a warning message to the response based on its type.
func addWarningToResponse(resp queryrangebase.Response, warning string) {
	switch r := resp.(type) {
	case *queryrange.LokiResponse:
		r.Warnings = append(r.Warnings, warning)
	case *queryrange.LokiPromResponse:
		if r.Response != nil {
			r.Response.Warnings = append(r.Response.Warnings, warning)
		}
	}
}
