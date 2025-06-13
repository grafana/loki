package goldfish

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Manager coordinates Goldfish sampling and comparison operations.
// It handles query sampling decisions, response comparison, and storage of results.
type Manager struct {
	config  Config
	sampler *Sampler
	storage Storage
	logger  log.Logger
	metrics *metrics
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
func NewManager(config Config, storage Storage, logger log.Logger, registerer prometheus.Registerer) (*Manager, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	m := &Manager{
		config:  config,
		sampler: NewSampler(config.SamplingConfig),
		storage: storage,
		logger:  logger,
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
	}

	return m, nil
}

// ShouldSample determines if a query should be sampled based on tenant configuration.
// Returns false if Goldfish is disabled or if the tenant should not be sampled.
func (m *Manager) ShouldSample(tenantID string) bool {
	if !m.config.Enabled {
		return false
	}

	sampled := m.sampler.ShouldSample(tenantID)
	m.metrics.samplingDecisions.WithLabelValues(tenantID, fmt.Sprintf("%v", sampled)).Inc()
	level.Debug(m.logger).Log("msg", "Goldfish sampling check", "tenant", tenantID, "sampled", sampled, "default_rate", m.config.SamplingConfig.DefaultRate)
	return sampled
}

// ProcessQueryPair processes a sampled query pair from both cells.
// It extracts performance statistics, compares responses, and stores the results asynchronously.
func (m *Manager) ProcessQueryPair(ctx context.Context, req *http.Request, cellAResp, cellBResp *ResponseData) {
	if !m.config.Enabled {
		return
	}

	correlationID := uuid.New().String()
	level.Info(m.logger).Log("msg", "Processing query pair in Goldfish",
		"correlation_id", correlationID,
		"tenant", extractTenant(req),
		"query_type", getQueryType(req.URL.Path))

	// Create query sample with performance statistics
	sample := &QuerySample{
		CorrelationID:     correlationID,
		TenantID:          extractTenant(req),
		Query:             req.URL.Query().Get("query"),
		QueryType:         getQueryType(req.URL.Path),
		StartTime:         parseTime(req.URL.Query().Get("start")),
		EndTime:           parseTime(req.URL.Query().Get("end")),
		Step:              parseDuration(req.URL.Query().Get("step")),
		CellAStats:        cellAResp.Stats,
		CellBStats:        cellBResp.Stats,
		CellAResponseHash: cellAResp.Hash,
		CellBResponseHash: cellBResp.Hash,
		CellAResponseSize: cellAResp.Size,
		CellBResponseSize: cellBResp.Size,
		CellAStatusCode:   cellAResp.StatusCode,
		CellBStatusCode:   cellBResp.StatusCode,
		SampledAt:         time.Now(),
	}

	m.metrics.sampledQueries.Inc()

	// Store the sample if storage is available
	if m.storage != nil {
		if err := m.storage.StoreQuerySample(ctx, sample); err != nil {
			level.Error(m.logger).Log("msg", "failed to store query sample", "correlation_id", correlationID, "err", err)
			m.metrics.storageOperations.WithLabelValues("store_sample", "error").Inc()
		} else {
			m.metrics.storageOperations.WithLabelValues("store_sample", "success").Inc()
		}
	}

	// Compare responses using simplified comparator
	start := time.Now()
	result := CompareResponses(sample)

	m.metrics.comparisonDuration.Observe(time.Since(start).Seconds())

	m.metrics.comparisonResults.WithLabelValues(string(result.ComparisonStatus)).Inc()

	// Log comparison results with stats
	logLevel := level.Info
	if result.ComparisonStatus != ComparisonStatusMatch {
		logLevel = level.Warn
	}

	// Build log fields
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

	// Add performance ratios if available
	if result.PerformanceMetrics.QueryTimeRatio > 0 {
		logFields = append(logFields, "query_time_ratio", result.PerformanceMetrics.QueryTimeRatio)
	}

	// Add difference summary if there are any
	if len(result.DifferenceDetails) > 0 {
		// Count types of differences
		perfDiffs := 0
		contentDiffs := 0
		for key := range result.DifferenceDetails {
			if key == "content_hash" || key == "status_code" || key == "entries_returned" || key == "bytes_processed" || key == "lines_processed" {
				contentDiffs++
			} else if key == "exec_time_variance" {
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

	// Store comparison result if storage is available
	if m.storage != nil {
		if err := m.storage.StoreComparisonResult(ctx, &result); err != nil {
			level.Error(m.logger).Log("msg", "failed to store comparison result", "correlation_id", correlationID, "err", err)
			m.metrics.storageOperations.WithLabelValues("store_result", "error").Inc()
		} else {
			m.metrics.storageOperations.WithLabelValues("store_result", "success").Inc()
		}
	}
}

// Close closes the manager and its storage connections.
// Should be called when the manager is no longer needed to properly clean up resources.
func (m *Manager) Close() error {
	if m.storage != nil {
		return m.storage.Close()
	}
	return nil
}

// ResponseData contains response data from a backend cell including performance statistics.
// Used to pass response information from QueryTee to Goldfish for comparison.
type ResponseData struct {
	Body       []byte
	StatusCode int
	Duration   time.Duration
	Stats      QueryStats
	Hash       string
	Size       int64
}

// CaptureResponse captures response data for comparison
func CaptureResponse(resp *http.Response, duration time.Duration) (*ResponseData, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Replace the body so it can be read again
	resp.Body = io.NopCloser(bytes.NewReader(body))

	// Extract statistics if this is a successful response
	var stats QueryStats
	var hash string
	var size int64

	if resp.StatusCode == 200 {
		extractor := NewStatsExtractor()
		stats, hash, size, err = extractor.ExtractResponseData(body, duration.Milliseconds())
		if err != nil {
			// Log error but don't fail the capture
			level.Warn(log.NewNopLogger()).Log("msg", "failed to extract response statistics", "err", err)
		}
	}

	return &ResponseData{
		Body:       body,
		StatusCode: resp.StatusCode,
		Duration:   duration,
		Stats:      stats,
		Hash:       hash,
		Size:       size,
	}, nil
}

// Helper functions

func extractTenant(r *http.Request) string {
	tenant := r.Header.Get("X-Scope-OrgID")
	if tenant == "" {
		return "anonymous"
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
		return "unknown"
	}
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	// Try parsing as Unix timestamp first
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	// Try other formats
	return time.Time{}
}

func parseDuration(s string) time.Duration {
	if s == "" {
		return 0
	}
	d, _ := time.ParseDuration(s)
	return d
}
