package goldfish

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/google/uuid"
	"github.com/grafana/loki/v3/pkg/goldfish"
	"github.com/grafana/loki/v3/tools/querytee/comparator"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

const (
	unknownUser      = "unknown"
	unknownQueryType = "unknown"
)

// Manager defines the interface for Goldfish manager operations.
type Manager interface {
	// ComparisonMinAge returns the minimum age of data to send to goldfish for comparison.
	ComparisonMinAge() time.Duration
	// ComparisonStartDate returns the configured start date for comparisons.
	ComparisonStartDate() time.Time
	// ShouldSample determines if a query should be sampled based on tenant configuration.
	ShouldSample(tenantID string) bool
	// SendToGoldfish sends backend responses to Goldfish for comparison.
	SendToGoldfish(httpReq *http.Request, cellAResp, cellBResp *BackendResponse)
	// Close closes the manager and its dependent storage connections.
	Close() error
}

// Manager coordinates Goldfish sampling and comparison operations.
// It handles query sampling decisions, response comparison, persistence, and storage of results.
type manager struct {
	config      Config
	sampler     *Sampler
	storage     goldfish.Storage
	resultStore ResultStore
	logger      log.Logger
	metrics     *metrics
	comparator  comparator.ResponsesComparator
}

type metrics struct {
	sampledQueries     prometheus.Counter
	comparisonResults  *prometheus.CounterVec
	samplingDecisions  *prometheus.CounterVec
	storageOperations  *prometheus.CounterVec
	comparisonDuration prometheus.Histogram
}

// NewManager creates a new Goldfish manager with the provided configuration.
// Returns an error if the configuration is invalid.
func NewManager(config Config, comparator comparator.ResponsesComparator, storage goldfish.Storage, resultStore ResultStore, logger log.Logger, registerer prometheus.Registerer) (Manager, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	m := &manager{
		config:      config,
		sampler:     NewSampler(config.SamplingConfig),
		storage:     storage,
		resultStore: resultStore,
		logger:      logger,
		metrics: &metrics{
			sampledQueries: promauto.With(registerer).NewCounter(prometheus.CounterOpts{
				Name: "goldfish_sampled_queries_total",
				Help: "Total number of queries sampled by Goldfish",
			}),
			comparisonResults: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
				Name: "goldfish_comparison_results_total",
				Help: "Total number of comparison results by status",
			}, []string{"status"}),
			samplingDecisions: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
				Name: "goldfish_sampling_decisions_total",
				Help: "Total number of sampling decisions",
			}, []string{"tenant", "sampled"}),
			storageOperations: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
				Name: "goldfish_storage_operations_total",
				Help: "Total number of storage operations",
			}, []string{"operation", "status"}),
			comparisonDuration: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
				Name:    "goldfish_comparison_duration_seconds",
				Help:    "Duration of response comparisons",
				Buckets: prometheus.DefBuckets,
			}),
		},
		comparator: comparator,
	}

	return m, nil
}

// ComparsionMinAge returns the minimum age of data to send to goldfish for comparison.
func (m *manager) ComparisonMinAge() time.Duration {
	return m.config.ComparisonMinAge
}

func (m *manager) ComparisonStartDate() time.Time {
	return time.Time(m.config.ComparisonStartDate)
}

// ShouldSample determines if a query should be sampled based on tenant configuration.
// Returns false if Goldfish is disabled or if the tenant should not be sampled.
func (m *manager) ShouldSample(tenantID string) bool {
	if !m.config.Enabled {
		return false
	}

	sampled := m.sampler.ShouldSample(tenantID)
	m.metrics.samplingDecisions.WithLabelValues(tenantID, fmt.Sprintf("%v", sampled)).Inc()
	level.Debug(m.logger).Log("msg", "Goldfish sampling check", "tenant", tenantID, "sampled", sampled, "default_rate", m.config.SamplingConfig.DefaultRate)
	return sampled
}

type BackendResponse struct {
	BackendName string
	Status      int
	Body        []byte
	Duration    time.Duration
	TraceID     string
	SpanID      string
}

func (m *manager) SendToGoldfish(httpReq *http.Request, cellAResp, cellBResp *BackendResponse) {
	if !m.config.Enabled {
		return
	}

	cellAData, err := CaptureResponse(&http.Response{
		StatusCode: cellAResp.Status,
		Body:       io.NopCloser(bytes.NewReader(cellAResp.Body)),
	}, cellAResp.Duration, cellAResp.TraceID, cellAResp.SpanID, m.logger)
	if err != nil {
		level.Error(m.logger).Log("msg", "failed to capture cell A response", "err", err)
		return
	}
	cellAData.BackendName = cellAResp.BackendName

	cellBData, err := CaptureResponse(&http.Response{
		StatusCode: cellBResp.Status,
		Body:       io.NopCloser(bytes.NewReader(cellBResp.Body)),
	}, cellBResp.Duration, cellBResp.TraceID, cellBResp.SpanID, m.logger)
	if err != nil {
		level.Error(m.logger).Log("msg", "failed to capture cell B response", "err", err)
		return
	}
	cellBData.BackendName = cellBResp.BackendName

	m.processQueryPair(httpReq, cellAData, cellBData)
}

// processQueryPair processes a sampled query pair from both cells.
// It extracts performance statistics, compares responses, persists raw payloads when configured, and stores metadata/results.
func (m *manager) processQueryPair(req *http.Request, cellAResp, cellBResp *ResponseData) {
	// Use a detached context with timeout since this runs async
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	correlationID := uuid.New().String()
	tenantID := extractTenant(req)
	queryType := getQueryType(req.URL.Path)

	level.Info(m.logger).Log("msg", "Processing query pair in Goldfish",
		"correlation_id", correlationID,
		"tenant", tenantID,
		"query_type", queryType)

	startTime := parseTime(req.URL.Query().Get("start"))
	endTime := parseTime(req.URL.Query().Get("end"))

	// If we couldn't parse the times, use current time as a fallback for MySQL compatibility
	if startTime.IsZero() {
		startTime = time.Now()
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}

	sampledAt := time.Now()

	sample := &goldfish.QuerySample{
		CorrelationID:      correlationID,
		TenantID:           tenantID,
		User:               ExtractUserFromQueryTags(req),
		IsLogsDrilldown:    isLogsDrilldownRequest(req),
		Query:              req.URL.Query().Get("query"),
		QueryType:          queryType,
		StartTime:          startTime,
		EndTime:            endTime,
		Step:               parseDuration(req.URL.Query().Get("step")),
		CellAStats:         cellAResp.Stats,
		CellBStats:         cellBResp.Stats,
		CellAResponseHash:  cellAResp.Hash,
		CellBResponseHash:  cellBResp.Hash,
		CellAResponseSize:  cellAResp.Size,
		CellBResponseSize:  cellBResp.Size,
		CellAStatusCode:    cellAResp.StatusCode,
		CellBStatusCode:    cellBResp.StatusCode,
		CellATraceID:       cellAResp.TraceID,
		CellBTraceID:       cellBResp.TraceID,
		CellASpanID:        cellAResp.SpanID,
		CellBSpanID:        cellBResp.SpanID,
		CellAUsedNewEngine: cellAResp.UsedNewEngine,
		CellBUsedNewEngine: cellBResp.UsedNewEngine,
		SampledAt:          sampledAt,
	}

	m.metrics.sampledQueries.Inc()

	comparisonStart := time.Now()
	result := CompareResponses(sample, cellAResp, cellBResp, m.config.PerformanceTolerance, m.comparator, m.logger)
	m.metrics.comparisonDuration.Observe(time.Since(comparisonStart).Seconds())
	m.metrics.comparisonResults.WithLabelValues(string(result.ComparisonStatus)).Inc()

	// Persist raw payloads when configured
	var persistedA, persistedB *StoredResult
	if m.resultStore != nil {
		persistedA, persistedB = m.persistResultPayloads(ctx, sample, cellAResp, cellBResp, result)
		if persistedA != nil {
			sample.CellAResultURI = persistedA.URI
			sample.CellAResultSize = persistedA.Size
			sample.CellAResultCompression = persistedA.Compression
		}
		if persistedB != nil {
			sample.CellBResultURI = persistedB.URI
			sample.CellBResultSize = persistedB.Size
			sample.CellBResultCompression = persistedB.Compression
		}
	}

	// Track whether the sample was stored successfully
	sampleStored := false

	if m.storage != nil {
		if err := m.storage.StoreQuerySample(ctx, sample, &result); err != nil {
			level.Error(m.logger).Log("msg", "failed to store query sample", "correlation_id", correlationID, "err", err)
			m.metrics.storageOperations.WithLabelValues("store_sample", "error").Inc()
		} else {
			m.metrics.storageOperations.WithLabelValues("store_sample", "success").Inc()
			sampleStored = true
		}
	}

	// Log user extraction debug info
	user := sample.User
	if user != "" && user != unknownUser {
		level.Info(m.logger).Log("msg", "captured user info", "correlation_id", correlationID, "user", user)
	}

	// Log comparison results with stats
	logLevel := level.Info
	if result.ComparisonStatus != goldfish.ComparisonStatusMatch {
		logLevel = level.Warn
	}

	logFields := []interface{}{
		"msg", "query comparison completed",
		"correlation_id", correlationID,
		"tenant", sample.TenantID,
		"query_type", sample.QueryType,
		"comparison_status", result.ComparisonStatus,
		"cell_a_exec_time_ms", sample.CellAStats.ExecTimeMs,
		"cell_b_exec_time_ms", sample.CellBStats.ExecTimeMs,
		"cell_a_bytes_processed", sample.CellAStats.BytesProcessed,
		"cell_b_bytes_processed", sample.CellBStats.BytesProcessed,
		"cell_a_entries_returned", sample.CellAStats.TotalEntriesReturned,
		"cell_b_entries_returned", sample.CellBStats.TotalEntriesReturned,
	}

	if persistedA != nil {
		logFields = append(logFields, "cell_a_result_uri", persistedA.URI, "cell_a_result_size", persistedA.Size)
	}
	if persistedB != nil {
		logFields = append(logFields, "cell_b_result_uri", persistedB.URI, "cell_b_result_size", persistedB.Size)
	}

	// Add performance ratios if available
	if result.PerformanceMetrics.QueryTimeRatio > 0 {
		logFields = append(logFields, "query_time_ratio", result.PerformanceMetrics.QueryTimeRatio)
	}

	// Add difference summary if there are any
	if len(result.DifferenceDetails) > 0 {
		perfDiffs := 0
		contentDiffs := 0
		for key := range result.DifferenceDetails {
			switch key {
			case "content_hash", "status_code", "entries_returned", "bytes_processed", "lines_processed":
				contentDiffs++
			case "exec_time_variance":
				perfDiffs++
			}
		}
		if contentDiffs > 0 {
			logFields = append(logFields, "content_differences", contentDiffs)
		}
		if perfDiffs > 0 {
			logFields = append(logFields, "performance_variances", perfDiffs)
		}
	}

	logLevel(m.logger).Log(logFields...)

	// Log specific performance differences if significant
	if execTimeVar, ok := result.DifferenceDetails["exec_time_variance"]; ok {
		if variance, ok := execTimeVar.(map[string]any); ok {
			if ratio, ok := variance["ratio"].(float64); ok && (ratio > 2.0 || ratio < 0.5) {
				level.Info(m.logger).Log(
					"msg", "significant execution time difference detected",
					"correlation_id", correlationID,
					"cell_a_ms", variance["cell_a_ms"],
					"cell_b_ms", variance["cell_b_ms"],
					"ratio", ratio,
				)
			}
		}
	}

	// Store comparison result only if the sample was stored successfully
	if m.storage != nil && sampleStored {
		if err := m.storage.StoreComparisonResult(ctx, &result); err != nil {
			level.Error(m.logger).Log("msg", "failed to store comparison result", "correlation_id", correlationID, "err", err)
			m.metrics.storageOperations.WithLabelValues("store_result", "error").Inc()
		} else {
			m.metrics.storageOperations.WithLabelValues("store_result", "success").Inc()
		}
	}
}

func (m *manager) persistResultPayloads(ctx context.Context, sample *goldfish.QuerySample, cellAResp, cellBResp *ResponseData, comparison goldfish.ComparisonResult) (*StoredResult, *StoredResult) {
	if !m.shouldPersistResults(comparison) {
		return nil, nil
	}

	persistSingle := func(cellLabel string, resp *ResponseData, hash string, statusCode int) *StoredResult {
		if resp == nil {
			return nil
		}

		stored, err := m.resultStore.Store(ctx, resp.Body, StoreOptions{
			CorrelationID: sample.CorrelationID,
			CellLabel:     cellLabel,
			BackendName:   resp.BackendName,
			TenantID:      sample.TenantID,
			QueryType:     sample.QueryType,
			Hash:          hash,
			StatusCode:    statusCode,
			Timestamp:     sample.SampledAt,
			ContentType:   "application/json",
		})
		if err != nil {
			level.Error(m.logger).Log("msg", "failed to persist query payload", "correlation_id", sample.CorrelationID, "cell", cellLabel, "err", err)
			m.metrics.storageOperations.WithLabelValues("store_payload", "error").Inc()
			return nil
		}

		m.metrics.storageOperations.WithLabelValues("store_payload", "success").Inc()
		level.Info(m.logger).Log("msg", "persisted query payload", "correlation_id", sample.CorrelationID, "cell", cellLabel, "uri", stored.URI, "compressed_size", stored.Size)
		return stored
	}

	var storedA, storedB *StoredResult
	if m.resultStore != nil {
		storedA = persistSingle("cell-a", cellAResp, sample.CellAResponseHash, sample.CellAStatusCode)
		storedB = persistSingle("cell-b", cellBResp, sample.CellBResponseHash, sample.CellBStatusCode)
	}

	return storedA, storedB
}

func (m *manager) shouldPersistResults(result goldfish.ComparisonResult) bool {
	if m.resultStore == nil {
		return false
	}

	switch m.config.ResultsStorage.Mode {
	case ResultsPersistenceModeAll:
		return true
	case ResultsPersistenceModeMismatchOnly:
		return result.ComparisonStatus != goldfish.ComparisonStatusMatch
	default:
		return false
	}
}

// Close closes the manager and its dependent storage connections.
// Should be called when the manager is no longer needed to properly clean up resources.
func (m *manager) Close() error {
	var errs []error

	if m.resultStore != nil {
		if err := m.resultStore.Close(context.Background()); err != nil {
			errs = append(errs, err)
		}
	}

	if m.storage != nil {
		if err := m.storage.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		return errors.Join(errs...)
	}
}

// ResponseData contains response data from a backend cell including performance statistics.
// Used to pass response information from QueryTee to Goldfish for comparison.
type ResponseData struct {
	Body          []byte
	StatusCode    int
	Duration      time.Duration
	Stats         goldfish.QueryStats
	Hash          string
	Size          int64
	UsedNewEngine bool
	TraceID       string
	SpanID        string
	BackendName   string
}

// CaptureResponse captures response data for comparison including trace ID and span ID
func CaptureResponse(resp *http.Response, duration time.Duration, traceID, spanID string, logger log.Logger) (*ResponseData, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Replace the body so it can be read again
	resp.Body = io.NopCloser(bytes.NewReader(body))

	// Extract statistics if this is a successful response
	var stats goldfish.QueryStats
	var hash string
	var size int64
	var usedNewEngine bool

	if resp.StatusCode == 200 {
		extractor := NewStatsExtractor()
		stats, hash, size, usedNewEngine, err = extractor.ExtractResponseData(body, duration.Milliseconds())
		if err != nil {
			// Log error but don't fail the capture
			level.Warn(logger).Log("msg", "failed to extract response statistics", "err", err)
		}
	} else {
		size = int64(len(body))
	}

	return &ResponseData{
		Body:          body,
		StatusCode:    resp.StatusCode,
		Duration:      duration,
		Stats:         stats,
		Hash:          hash,
		Size:          size,
		UsedNewEngine: usedNewEngine,
		TraceID:       traceID,
		SpanID:        spanID,
	}, nil
}

// Helper functions

func extractTenant(r *http.Request) string {
	tenant := r.Header.Get("X-Scope-OrgID")
	if tenant == "" {
		return "fake"
	}
	return tenant
}

func getQueryType(path string) string {
	switch path {
	case "/loki/api/v1/query_range":
		return "query_range"
	case "/loki/api/v1/query":
		return "query"
	case "/loki/api/v1/series":
		return "series"
	case "/loki/api/v1/labels":
		return "labels"
	case "/loki/api/v1/label":
		return "label_values"
	default:
		return unknownQueryType
	}
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}

	// Try parsing as nanosecond Unix timestamp (Loki format)
	if ns, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.Unix(0, ns)
	}

	// Try parsing as RFC3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}

	// Try parsing as Unix seconds
	if sec, err := strconv.ParseInt(s, 10, 64); err == nil && sec < 1e10 {
		return time.Unix(sec, 0)
	}

	return time.Time{}
}

func parseDuration(s string) time.Duration {
	if s == "" {
		return 0
	}
	d, _ := time.ParseDuration(s)
	return d
}

func ExtractUserFromQueryTags(req *http.Request) string {
	// Also check for X-Grafana-User header directly
	tags := httpreq.ExtractQueryTagsFromHTTP(req)
	grafanaUser := req.Header.Get("X-Grafana-User")
	kvs := httpreq.TagsToKeyValues(tags)

	// Iterate through key-value pairs (keys at even indices, values at odd)
	for i := 0; i < len(kvs); i += 2 {
		if i+1 < len(kvs) {
			key, keyOK := kvs[i].(string)
			value, valueOK := kvs[i+1].(string)
			if keyOK && valueOK && key == "user" {
				return value
			}
		}
	}

	// Fallback to X-Grafana-User if not found in query tags
	if grafanaUser != "" {
		return grafanaUser
	}

	return unknownUser
}

// isLogsDrilldownRequest checks if the request comes from Logs Drilldown by examining the X-Query-Tags header
func isLogsDrilldownRequest(req *http.Request) bool {
	tags := httpreq.ExtractQueryTagsFromHTTP(req)
	kvs := httpreq.TagsToKeyValues(tags)

	// Iterate through key-value pairs (keys at even indices, values at odd)
	for i := 0; i < len(kvs); i += 2 {
		if i+1 < len(kvs) {
			key, keyOK := kvs[i].(string)
			value, valueOK := kvs[i+1].(string)
			if keyOK && valueOK && key == "source" && strings.EqualFold(value, constants.LogsDrilldownAppName) {
				return true
			}
		}
	}
	return false
}
