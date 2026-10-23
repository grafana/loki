package querytee

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/querytee/comparator"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

// maxLoggedQueryLength cuts the query in the log line of a pair.
const maxLoggedQueryLength = 1000

var errNoPrimaryBackend = errors.New("no primary backend")

// FanOutHandler implements queryrangebase.Handler and fans out requests to multiple backends.
// It sends each request to every backend, except that the secondary backend strategy decides
// whether the secondary backend gets it. It returns the primary backend's response as soon as it is
// ready. When it has a comparator, it compares the secondary backend's response with the primary's
// in the background. It never returns the secondary backend's response.
type FanOutHandler struct {
	backends                 []*ProxyBackend
	backendsWithoutSecondary []*ProxyBackend
	secondaryBackendRouter   *secondaryBackendRouter
	codec                    queryrangebase.Codec
	logger                   log.Logger
	metrics                  *ProxyMetrics
	routeName                string
	comparator               comparator.ResponsesComparator
	reportMissingSeries      bool
}

// FanOutHandlerConfig holds configuration for creating a FanOutHandler.
type FanOutHandlerConfig struct {
	Backends            []*ProxyBackend
	Codec               queryrangebase.Codec
	Logger              log.Logger
	Metrics             *ProxyMetrics
	RouteName           string
	Comparator          comparator.ResponsesComparator
	ReportMissingSeries bool

	// SecondaryBackendStrategy picks the requests that the secondary backend receives. Without a
	// strategy, the secondary backend receives every request.
	SecondaryBackendStrategy SelectionStrategy
}

// NewFanOutHandler creates a new FanOutHandler.
func NewFanOutHandler(cfg FanOutHandlerConfig) *FanOutHandler {
	return &FanOutHandler{
		backends:                 cfg.Backends,
		backendsWithoutSecondary: slices.DeleteFunc(slices.Clone(cfg.Backends), (*ProxyBackend).isSecondary),
		secondaryBackendRouter:   newSecondaryBackendRouter(cfg.SecondaryBackendStrategy, cfg.Metrics, cfg.RouteName, cfg.Logger),
		codec:                    cfg.Codec,
		logger:                   cfg.Logger,
		metrics:                  cfg.Metrics,
		routeName:                cfg.RouteName,
		comparator:               cfg.Comparator,
		reportMissingSeries:      cfg.ReportMissingSeries,
	}
}

// backendResult holds the result from a single backend request.
type backendResult struct {
	backend     *ProxyBackend
	backendResp *BackendResponse
	response    queryrangebase.Response
	err         error
}

// Do implements queryrangebase.Handler. It sends the request to every backend, except that the
// secondary backend strategy decides whether the secondary backend gets it. It returns the primary
// backend's response. In the background, when it has a comparator, it compares the secondary
// backend's response with the primary's.
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

	backends := h.backendsForRequest(ctx, req)
	results := h.makeBackendRequests(ctx, backends, httpReq, body, req, issuer)
	collected := make([]*backendResult, 0, len(backends))

	return h.doWithPrimary(ctx, req, len(backends), results, collected, httpReq)
}

// backendsForRequest returns the backends that get the request. The secondary backend gets it only
// when the secondary backend strategy selects it. The strategy decision is counted in the metrics.
func (h *FanOutHandler) backendsForRequest(ctx context.Context, req queryrangebase.Request) []*ProxyBackend {
	if h.secondaryBackendRouter.shouldRoute(ctx, req) {
		return h.backends
	}
	return h.backendsWithoutSecondary
}

func (h *FanOutHandler) doWithPrimary(ctx context.Context, req queryrangebase.Request, numBackends int, results <-chan *backendResult, collected []*backendResult, httpReq *http.Request) (queryrangebase.Response, error) {
	for i := 0; i < numBackends; i++ {
		result := <-results
		collected = append(collected, result)

		if result.backend.isPrimary() {
			remaining := numBackends - i - 1
			tenantID := tenantLabel(ctx)
			go func() {
				h.collectRemainingAndCompare(req, remaining, httpReq, results, collected, tenantID)
			}()

			// The primary backend failed or returned an error status. The client gets that
			// response. The proxy does not fall back to another backend.
			if result.err != nil {
				level.Warn(h.logger).Log(
					"msg", "primary backend failed",
					"route", h.routeName,
					"backend", result.backend.name,
					"status", result.backendResp.status,
					"err", result.err,
				)
				return &NonDecodableResponse{
					StatusCode: result.backendResp.status,
					Body:       result.backendResp.body,
				}, result.err
			}

			return result.response, nil
		}
	}

	return nil, errNoPrimaryBackend
}

// tenantLabel returns the tenant label of the metrics for the request. A multi-tenant request has
// its tenant IDs joined by "|". It returns "unknown" when the context has no valid tenant.
func tenantLabel(ctx context.Context) string {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil || len(tenantIDs) == 0 {
		return unknownTenant
	}
	return tenant.JoinTenantIDs(tenantIDs)
}

// collectRemainingAndCompare collects the remaining backend results. When the handler has a
// comparator, it also compares the secondary result with the primary one, and records the outcome.
// Call it asynchronously, so it does not block the primary response.
func (h *FanOutHandler) collectRemainingAndCompare(req queryrangebase.Request, remaining int, httpReq *http.Request, results <-chan *backendResult, collected []*backendResult, tenantID string) {
	for range remaining {
		collected = append(collected, <-results)
	}

	var primary, secondary *backendResult
	for _, r := range collected {
		switch {
		case r.backend.isPrimary():
			primary = r
		case r.backend.isSecondary():
			secondary = r
		}
	}
	if h.comparator == nil || primary == nil || secondary == nil {
		return
	}

	issuer := detectIssuer(httpReq)

	result := comparisonMatch
	summary, cause, err := h.compareResponses(primary.backendResp, secondary.backendResp, time.Now().UTC())
	if errors.Is(err, comparator.ErrComparisonMismatch) {
		result = comparisonMismatch
		level.Warn(h.logger).Log("msg", "compared responses were not equal",
			"backend", secondary.backend.name,
			"route", h.routeName,
			"query", httpReq.URL.RawQuery,
			"err", err)
	} else if err != nil {
		result = comparisonFailed
		level.Error(h.logger).Log("msg", "response comparison failed",
			"backend", secondary.backend.name,
			"route", h.routeName,
			"query", httpReq.URL.RawQuery,
			"err", err)
	} else if summary != nil && summary.Skipped {
		result = comparisonSkipped
	}

	if h.reportMissingSeries && summary != nil {
		h.metrics.missingMetrics.WithLabelValues(
			secondary.backend.name, secondary.backend.Alias(), h.routeName, result, issuer,
		).Observe(float64(summary.MissingMetrics))
	}

	h.compareStats(req, primary, secondary, tenantID, result, cause)

	h.metrics.responsesComparedTotal.WithLabelValues(
		secondary.backend.name, secondary.backend.Alias(), h.routeName, result, cause, issuer, tenantID,
	).Inc()
}

// compareStats observes the ratios of the stats of the secondary over the stats of the primary, and
// logs the pair of responses with the stats of each backend.
func (h *FanOutHandler) compareStats(req queryrangebase.Request, primary, secondary *backendResult, tenantID, result, cause string) {
	// statsLogFields returns the log fields of the stats of one backend.
	statsLogFields := func(prefix string, s stats.Result) []any {
		dataobj := s.Querier.Store.Dataobj
		cacheEntriesFound, cacheEntriesRequested := resultCacheEntries(s.Caches)
		return []any{
			prefix + "_bytes_processed", s.Summary.TotalBytesProcessed,
			prefix + "_lines_processed", s.Summary.TotalLinesProcessed,
			prefix + "_exec_time_seconds", s.Summary.ExecTime,
			prefix + "_queue_time_seconds", s.Summary.QueueTime,
			prefix + "_splits", s.Summary.Splits,
			prefix + "_shards", s.Summary.Shards,
			prefix + "_chunk_decompressed_bytes", s.Querier.Store.Chunk.DecompressedBytes,
			prefix + "_dataobj_rows_read", dataobj.PrePredicateDecompressedRows,
			prefix + "_dataobj_decompressed_bytes", dataobj.PrePredicateDecompressedBytes,
			prefix + "_dataobj_post_filter_rows", dataobj.PostFilterRows,
			prefix + "_result_cache_entries_found", cacheEntriesFound,
			prefix + "_result_cache_entries_requested", cacheEntriesRequested,
		}
	}

	primaryStats, hasPrimaryStats := responseStats(primary.response)
	secondaryStats, hasSecondaryStats := responseStats(secondary.response)

	if hasPrimaryStats && hasSecondaryStats {
		h.metrics.observeSecondaryBackendStats(h.routeName, primaryStats, secondaryStats)
	}

	query := req.GetQuery()
	if len(query) > maxLoggedQueryLength {
		query = query[:maxLoggedQueryLength]
	}
	keyvals := []any{
		"msg", "secondary pair",
		"route", h.routeName,
		"tenant", tenantID,
		"result", result,
		"cause", cause,
		"query", query,
		"primary_status", primary.backendResp.status,
		"secondary_status", secondary.backendResp.status,
		"primary_trace_id", primary.backendResp.traceID,
		"secondary_trace_id", secondary.backendResp.traceID,
		"primary_duration", primary.backendResp.duration,
		"secondary_duration", secondary.backendResp.duration,
	}
	if hasPrimaryStats {
		keyvals = append(keyvals, statsLogFields("primary", primaryStats)...)
	}
	if hasSecondaryStats {
		keyvals = append(keyvals, statsLogFields("secondary", secondaryStats)...)
	}
	level.Info(h.logger).Log(keyvals...)
}

func (h *FanOutHandler) compareResponses(primaryResponse, secondaryResponse *BackendResponse, queryEvalTime time.Time) (*comparator.ComparisonSummary, string, error) {
	if primaryResponse.err != nil {
		return &comparator.ComparisonSummary{Skipped: true}, errorCause(primaryResponse), nil
	}

	if secondaryResponse.err != nil {
		return nil, errorCause(secondaryResponse), fmt.Errorf("skipped comparison of response because the request to the secondary backend failed: %w", secondaryResponse.err)
	}

	// compare response body only if we get a 200
	if primaryResponse.status != 200 {
		return &comparator.ComparisonSummary{Skipped: true}, errorCause(primaryResponse), nil
	}

	if secondaryResponse.status != 200 {
		return nil, errorCause(secondaryResponse), fmt.Errorf("skipped comparison of response because we got status code %d from secondary backend's response", secondaryResponse.status)
	}

	if primaryResponse.status != secondaryResponse.status {
		return nil, causeCompareError, fmt.Errorf("expected status code %d but got %d", primaryResponse.status, secondaryResponse.status)
	}

	summary, err := h.comparator.Compare(primaryResponse.body, secondaryResponse.body, queryEvalTime)
	var cause string
	switch {
	case errors.Is(err, comparator.ErrComparisonMismatch):
		if summary != nil {
			cause = summary.MismatchCause
		}
	case err != nil:
		cause = causeCompareError
	}
	return summary, cause, err
}

// executeBackendRequest executes a request to a single backend.
func (h *FanOutHandler) executeBackendRequest(
	ctx context.Context,
	httpReq *http.Request,
	body []byte,
	backend *ProxyBackend,
	req queryrangebase.Request,
) *backendResult {
	// Clone the request for this backend
	clonedReq := httpReq.Clone(ctx)
	if len(body) > 0 {
		clonedReq.Body = io.NopCloser(bytes.NewReader(body))
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

// WithComparator sets the response comparator.
func (h *FanOutHandler) WithComparator(comparator comparator.ResponsesComparator) *FanOutHandler {
	h.comparator = comparator
	return h
}

// makeBackendRequests initiates backend requests and returns a channel for receiving results.
func (h *FanOutHandler) makeBackendRequests(
	ctx context.Context,
	backends []*ProxyBackend,
	httpReq *http.Request,
	body []byte,
	req queryrangebase.Request,
	issuer string,
) chan *backendResult {
	results := make(chan *backendResult, len(backends))

	for i, backend := range backends {
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

// responseStats returns the query stats of a response. It reports false when the response carries
// none.
func responseStats(resp queryrangebase.Response) (stats.Result, bool) {
	switch r := resp.(type) {
	case *queryrange.LokiPromResponse:
		return r.Statistics, true
	case *queryrange.LokiResponse:
		return r.Statistics, true
	default:
		return stats.Result{}, false
	}
}

// errorCause returns why a backend response failed: timeout, transport, http_4xx, http_5xx or
// http_other.
func errorCause(r *BackendResponse) string {
	switch {
	case r.err != nil && r.status == http.StatusGatewayTimeout:
		return "timeout"
	case r.err != nil:
		return "transport"
	case r.status >= 400 && r.status < 500:
		return "http_4xx"
	case r.status >= 500:
		return "http_5xx"
	default:
		return "http_other"
	}
}
