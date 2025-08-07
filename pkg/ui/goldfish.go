package ui

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/grafana/loki/v3/pkg/goldfish"
)

// Constants for outcome filtering
const (
	outcomeAll      = goldfish.OutcomeAll
	outcomeMatch    = goldfish.OutcomeMatch
	outcomeMismatch = goldfish.OutcomeMismatch
	outcomeError    = goldfish.OutcomeError
)

// SampledQuery represents a sampled query from the database for API responses.
// This is the UI/API representation of goldfish.QuerySample with several important differences:
//
// 1. Time formatting: All time fields use RFC3339 strings instead of time.Time
//   - The frontend expects RFC3339 formatted strings for display
//   - Database columns store timestamps that are scanned into time.Time then formatted
//
// 2. Nullable fields: Uses pointers (*int64, *string) for nullable database columns
//   - The database schema allows NULLs for metrics that might not be available
//   - Go's zero values would be ambiguous (is 0 a real value or NULL?)
//
// 3. Flattened structure: QueryStats fields are flattened into individual columns
//   - Makes the API response simpler for frontend consumption
//   - Matches the database schema which stores stats as individual columns
//
// 4. Database tags: Includes `db:` tags for direct sqlx scanning from queries
//   - The storage layer returns goldfish.QuerySample for internal use
//   - The UI layer queries the database directly for performance
//
// 5. UI-specific fields: Includes trace/logs links generated from configuration
//   - These are computed based on Grafana configuration, not stored
type SampledQuery struct {
	// Core query identification
	CorrelationID string `json:"correlationId" db:"correlation_id"`
	TenantID      string `json:"tenantId" db:"tenant_id"`
	User          string `json:"user" db:"user"`
	Query         string `json:"query" db:"query"`
	QueryType     string `json:"queryType" db:"query_type"`

	// Time range fields - stored as RFC3339 strings for API compatibility
	StartTime    string `json:"startTime" db:"start_time"`       // RFC3339 formatted
	EndTime      string `json:"endTime" db:"end_time"`           // RFC3339 formatted
	StepDuration *int64 `json:"stepDuration" db:"step_duration"` // Step in milliseconds, nullable

	// Performance statistics - flattened from QueryStats for API simplicity
	// All are nullable as some queries might not have complete stats
	CellAExecTimeMs      *int64 `json:"cellAExecTimeMs" db:"cell_a_exec_time_ms"`
	CellBExecTimeMs      *int64 `json:"cellBExecTimeMs" db:"cell_b_exec_time_ms"`
	CellAQueueTimeMs     *int64 `json:"cellAQueueTimeMs" db:"cell_a_queue_time_ms"`
	CellBQueueTimeMs     *int64 `json:"cellBQueueTimeMs" db:"cell_b_queue_time_ms"`
	CellABytesProcessed  *int64 `json:"cellABytesProcessed" db:"cell_a_bytes_processed"`
	CellBBytesProcessed  *int64 `json:"cellBBytesProcessed" db:"cell_b_bytes_processed"`
	CellALinesProcessed  *int64 `json:"cellALinesProcessed" db:"cell_a_lines_processed"`
	CellBLinesProcessed  *int64 `json:"cellBLinesProcessed" db:"cell_b_lines_processed"`
	CellABytesPerSecond  *int64 `json:"cellABytesPerSecond" db:"cell_a_bytes_per_second"`
	CellBBytesPerSecond  *int64 `json:"cellBBytesPerSecond" db:"cell_b_bytes_per_second"`
	CellALinesPerSecond  *int64 `json:"cellALinesPerSecond" db:"cell_a_lines_per_second"`
	CellBLinesPerSecond  *int64 `json:"cellBLinesPerSecond" db:"cell_b_lines_per_second"`
	CellAEntriesReturned *int64 `json:"cellAEntriesReturned" db:"cell_a_entries_returned"`
	CellBEntriesReturned *int64 `json:"cellBEntriesReturned" db:"cell_b_entries_returned"`
	CellASplits          *int64 `json:"cellASplits" db:"cell_a_splits"`
	CellBSplits          *int64 `json:"cellBSplits" db:"cell_b_splits"`
	CellAShards          *int64 `json:"cellAShards" db:"cell_a_shards"`
	CellBShards          *int64 `json:"cellBShards" db:"cell_b_shards"`

	// Response metadata - nullable for error cases
	CellAResponseHash *string `json:"cellAResponseHash" db:"cell_a_response_hash"`
	CellBResponseHash *string `json:"cellBResponseHash" db:"cell_b_response_hash"`
	CellAResponseSize *int64  `json:"cellAResponseSize" db:"cell_a_response_size"`
	CellBResponseSize *int64  `json:"cellBResponseSize" db:"cell_b_response_size"`
	CellAStatusCode   *int    `json:"cellAStatusCode" db:"cell_a_status_code"`
	CellBStatusCode   *int    `json:"cellBStatusCode" db:"cell_b_status_code"`

	// Trace IDs - nullable as not all requests have traces
	CellATraceID *string `json:"cellATraceID" db:"cell_a_trace_id"`
	CellBTraceID *string `json:"cellBTraceID" db:"cell_b_trace_id"`

	// Query engine version tracking
	CellAUsedNewEngine bool `json:"cellAUsedNewEngine" db:"cell_a_used_new_engine"`
	CellBUsedNewEngine bool `json:"cellBUsedNewEngine" db:"cell_b_used_new_engine"`

	// Timestamps - time.Time for database scanning, formatted in JSON marshaling
	SampledAt time.Time `json:"sampledAt" db:"sampled_at"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`

	// Comparison outcome - computed by backend logic
	ComparisonStatus string `json:"comparisonStatus" db:"comparison_status"`

	// UI-only fields - generated based on configuration, not stored in database
	CellATraceLink *string `json:"cellATraceLink,omitempty"`
	CellBTraceLink *string `json:"cellBTraceLink,omitempty"`
	CellALogsLink  *string `json:"cellALogsLink,omitempty"`
	CellBLogsLink  *string `json:"cellBLogsLink,omitempty"`
}

// ComparisonOutcome represents a comparison result from the database
type ComparisonOutcome struct {
	CorrelationID      string    `json:"correlationId" db:"correlation_id"`
	ComparisonStatus   string    `json:"comparisonStatus" db:"comparison_status"`
	DifferenceDetails  any       `json:"differenceDetails" db:"difference_details"`
	PerformanceMetrics any       `json:"performanceMetrics" db:"performance_metrics"`
	ComparedAt         time.Time `json:"comparedAt" db:"compared_at"`
	CreatedAt          time.Time `json:"createdAt" db:"created_at"`
}

// GoldfishAPIResponse represents the paginated API response
type GoldfishAPIResponse struct {
	Queries  []SampledQuery `json:"queries"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
}

// GetSampledQueries retrieves sampled queries from the database with pagination and outcome filtering
func (s *Service) GetSampledQueries(page, pageSize int, filter goldfish.QueryFilter) (*GoldfishAPIResponse, error) {
	if !s.cfg.Goldfish.Enable {
		return nil, ErrGoldfishDisabled
	}

	if s.goldfishStorage == nil {
		return nil, ErrGoldfishNotConfigured
	}

	// Call the storage layer which returns QuerySample
	resp, err := s.goldfishStorage.GetSampledQueries(context.Background(), page, pageSize, filter)
	if err != nil {
		return nil, err
	}

	// Convert from storage types (QuerySample) to UI types (SampledQuery)
	queries := make([]SampledQuery, 0, len(resp.Queries))
	for _, q := range resp.Queries {
		// Create SampledQuery with explicit field mapping
		uiQuery := SampledQuery{
			// Core identification fields
			CorrelationID: q.CorrelationID,
			TenantID:      q.TenantID,
			User:          q.User,
			Query:         q.Query,
			QueryType:     q.QueryType,

			// Time fields - convert time.Time to RFC3339 strings for API
			StartTime:    q.StartTime.Format(time.RFC3339),
			EndTime:      q.EndTime.Format(time.RFC3339),
			StepDuration: int64Ptr(q.Step.Milliseconds()),

			// Timestamps
			SampledAt: q.SampledAt,
			CreatedAt: q.SampledAt, // Using SampledAt as CreatedAt

			// Performance statistics - flatten from QueryStats to individual nullable fields
			CellAExecTimeMs:      &q.CellAStats.ExecTimeMs,
			CellBExecTimeMs:      &q.CellBStats.ExecTimeMs,
			CellAQueueTimeMs:     &q.CellAStats.QueueTimeMs,
			CellBQueueTimeMs:     &q.CellBStats.QueueTimeMs,
			CellABytesProcessed:  &q.CellAStats.BytesProcessed,
			CellBBytesProcessed:  &q.CellBStats.BytesProcessed,
			CellALinesProcessed:  &q.CellAStats.LinesProcessed,
			CellBLinesProcessed:  &q.CellBStats.LinesProcessed,
			CellABytesPerSecond:  &q.CellAStats.BytesPerSecond,
			CellBBytesPerSecond:  &q.CellBStats.BytesPerSecond,
			CellALinesPerSecond:  &q.CellAStats.LinesPerSecond,
			CellBLinesPerSecond:  &q.CellBStats.LinesPerSecond,
			CellAEntriesReturned: &q.CellAStats.TotalEntriesReturned,
			CellBEntriesReturned: &q.CellBStats.TotalEntriesReturned,
			CellASplits:          &q.CellAStats.Splits,
			CellBSplits:          &q.CellBStats.Splits,
			CellAShards:          &q.CellAStats.Shards,
			CellBShards:          &q.CellBStats.Shards,

			// Response metadata - convert to nullable pointers
			CellAResponseHash: strPtr(q.CellAResponseHash),
			CellBResponseHash: strPtr(q.CellBResponseHash),
			CellAResponseSize: &q.CellAResponseSize,
			CellBResponseSize: &q.CellBResponseSize,
			CellAStatusCode:   intPtr(q.CellAStatusCode),
			CellBStatusCode:   intPtr(q.CellBStatusCode),
			CellATraceID:      strPtr(q.CellATraceID),
			CellBTraceID:      strPtr(q.CellBTraceID),
			CellAUsedNewEngine: q.CellAUsedNewEngine,
			CellBUsedNewEngine: q.CellBUsedNewEngine,
		}

		// Determine comparison status based on response codes and hashes
		if q.CellAStatusCode < 200 || q.CellAStatusCode >= 300 || q.CellBStatusCode < 200 || q.CellBStatusCode >= 300 {
			uiQuery.ComparisonStatus = string(goldfish.ComparisonStatusError)
		} else if q.CellAResponseHash == q.CellBResponseHash {
			uiQuery.ComparisonStatus = string(goldfish.ComparisonStatusMatch)
		} else {
			uiQuery.ComparisonStatus = string(goldfish.ComparisonStatusMismatch)
		}

		// Add trace ID explore links if explore is configured
		if s.cfg.Goldfish.GrafanaURL != "" && s.cfg.Goldfish.TracesDatasourceUID != "" {
			if q.CellATraceID != "" {
				link := s.GenerateTraceExploreURL(q.CellATraceID, q.SampledAt)
				uiQuery.CellATraceLink = &link
			}
			if q.CellBTraceID != "" {
				link := s.GenerateTraceExploreURL(q.CellBTraceID, q.SampledAt)
				uiQuery.CellBTraceLink = &link
			}
		}

		// Add logs explore links if logs config is complete
		if s.cfg.Goldfish.GrafanaURL != "" && s.cfg.Goldfish.LogsDatasourceUID != "" &&
			s.cfg.Goldfish.CellANamespace != "" && s.cfg.Goldfish.CellBNamespace != "" {
			if q.CellATraceID != "" {
				link := s.GenerateLogsExploreURL(q.CellATraceID, s.cfg.Goldfish.CellANamespace, q.SampledAt)
				uiQuery.CellALogsLink = &link
			}
			if q.CellBTraceID != "" {
				link := s.GenerateLogsExploreURL(q.CellBTraceID, s.cfg.Goldfish.CellBNamespace, q.SampledAt)
				uiQuery.CellBLogsLink = &link
			}
		}

		queries = append(queries, uiQuery)
	}

	return &GoldfishAPIResponse{
		Queries:  queries,
		Total:    resp.Total,
		Page:     resp.Page,
		PageSize: resp.PageSize,
	}, nil
}

// Helper functions for converting to nullable pointers
func int64Ptr(v int64) *int64 {
	return &v
}

func intPtr(v int) *int {
	return &v
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ErrGoldfishDisabled is returned when goldfish feature is disabled
var ErrGoldfishDisabled = sql.ErrNoRows

// ErrGoldfishNotConfigured is returned when goldfish database is not configured
var ErrGoldfishNotConfigured = sql.ErrConnDone

// GenerateTraceExploreURL generates a Grafana Explore URL for a given trace ID
func (s *Service) GenerateTraceExploreURL(traceID string, sampledAt time.Time) string {
	// Return empty string if configuration is incomplete
	if s.cfg.Goldfish.GrafanaURL == "" || s.cfg.Goldfish.TracesDatasourceUID == "" {
		return ""
	}

	// For Tempo explore, we just need the trace ID, not the full TraceQL query
	// Grafana will construct the query based on the queryType

	// Build the explore state for Tempo
	exploreState := map[string]any{
		"datasource": s.cfg.Goldfish.TracesDatasourceUID,
		"queries": []map[string]any{
			{
				"refId": "A",
				"query": traceID, // Just the trace ID, not the full TraceQL query
				"datasource": map[string]any{
					"type": "tempo",
					"uid":  s.cfg.Goldfish.TracesDatasourceUID,
				},
				"queryType":        "traceql",
				"limit":            20,
				"tableType":        "traces",
				"metricsQueryType": "range",
			},
		},
		"range": map[string]any{
			"from": sampledAt.Add(-5 * time.Minute).UTC().Format(time.RFC3339),
			"to":   sampledAt.Add(5 * time.Minute).UTC().Format(time.RFC3339),
		},
	}

	paneID := "goldfish-explore"
	stateJSON, err := json.Marshal(map[string]any{
		paneID: exploreState,
	})
	if err != nil {
		return ""
	}

	// URL encode the state
	encodedState := url.QueryEscape(string(stateJSON))

	// Build the final URL with schemaVersion
	return fmt.Sprintf("%s/explore?schemaVersion=1&panes=%s", s.cfg.Goldfish.GrafanaURL, encodedState)
}

// GenerateLogsExploreURL generates a Grafana Explore URL for logs related to a trace ID
func (s *Service) GenerateLogsExploreURL(traceID, namespace string, sampledAt time.Time) string {
	// Return empty string if configuration is incomplete
	if s.cfg.Goldfish.GrafanaURL == "" || s.cfg.Goldfish.LogsDatasourceUID == "" {
		return ""
	}

	// Build the LogQL query with the namespace pattern and trace ID filter
	query := fmt.Sprintf(`{job=~"%s/.*quer.*"} |= "%s"`, namespace, traceID)

	// Build the explore state for Loki
	exploreState := map[string]any{
		"datasource": s.cfg.Goldfish.LogsDatasourceUID,
		"queries": []map[string]any{
			{
				"refId": "A",
				"expr":  query, // Loki uses 'expr' instead of 'query'
				"datasource": map[string]any{
					"type": "loki",
					"uid":  s.cfg.Goldfish.LogsDatasourceUID,
				},
			},
		},
		"range": map[string]any{
			"from": sampledAt.Add(-5 * time.Minute).UTC().Format(time.RFC3339),
			"to":   sampledAt.Add(5 * time.Minute).UTC().Format(time.RFC3339),
		},
	}

	paneID := "goldfish-logs-explore"
	stateJSON, err := json.Marshal(map[string]any{
		paneID: exploreState,
	})
	if err != nil {
		return ""
	}

	// URL encode the state
	encodedState := url.QueryEscape(string(stateJSON))

	// Build the final URL with schemaVersion
	return fmt.Sprintf("%s/explore?schemaVersion=1&panes=%s", s.cfg.Goldfish.GrafanaURL, encodedState)
}
