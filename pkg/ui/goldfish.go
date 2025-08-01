package ui

import (
	"database/sql"
	"time"

	"github.com/go-kit/log/level"
)

// Constants for outcome filtering
const (
	outcomeAll      = "all"
	outcomeMatch    = "match"
	outcomeMismatch = "mismatch"
	outcomeError    = "error"

	whereComparisonStatus = "WHERE comparison_status = ?"
)

// SampledQuery represents a sampled query from the database
type SampledQuery struct {
	CorrelationID string `json:"correlationId" db:"correlation_id"`
	TenantID      string `json:"tenantId" db:"tenant_id"`
	Query         string `json:"query" db:"query"`
	QueryType     string `json:"queryType" db:"query_type"`
	StartTime     string `json:"startTime" db:"start_time"`
	EndTime       string `json:"endTime" db:"end_time"`
	StepDuration  *int64 `json:"stepDuration" db:"step_duration"`

	// Performance statistics
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

	// Response metadata
	CellAResponseHash *string `json:"cellAResponseHash" db:"cell_a_response_hash"`
	CellBResponseHash *string `json:"cellBResponseHash" db:"cell_b_response_hash"`
	CellAResponseSize *int64  `json:"cellAResponseSize" db:"cell_a_response_size"`
	CellBResponseSize *int64  `json:"cellBResponseSize" db:"cell_b_response_size"`
	CellAStatusCode   *int    `json:"cellAStatusCode" db:"cell_a_status_code"`
	CellBStatusCode   *int    `json:"cellBStatusCode" db:"cell_b_status_code"`

	// Trace IDs
	CellATraceID *string `json:"cellATraceID" db:"cell_a_trace_id"`
	CellBTraceID *string `json:"cellBTraceID" db:"cell_b_trace_id"`

	SampledAt time.Time `json:"sampledAt" db:"sampled_at"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`

	// Comparison outcome (always computed by backend)
	ComparisonStatus string `json:"comparisonStatus" db:"comparison_status"`
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
func (s *Service) GetSampledQueries(page, pageSize int, outcome string) (*GoldfishAPIResponse, error) {
	if !s.cfg.Goldfish.Enable {
		return nil, ErrGoldfishDisabled
	}

	if s.goldfishDB == nil {
		return nil, ErrGoldfishNotConfigured
	}

	// Validate and sanitize input parameters
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 20 // Default page size
	}

	// Validate outcome parameter against allowed values
	switch outcome {
	case outcomeAll, outcomeMatch, outcomeMismatch, outcomeError:
		// Valid values - proceed
	default:
		outcome = outcomeAll // Default to all if invalid
	}

	offset := (page - 1) * pageSize

	// Build the computed status subquery
	baseQuery := `
		SELECT
			sq.correlation_id, sq.tenant_id, sq.query, sq.query_type, sq.start_time, sq.end_time, sq.step_duration,
			sq.cell_a_exec_time_ms, sq.cell_b_exec_time_ms, sq.cell_a_queue_time_ms, sq.cell_b_queue_time_ms,
			sq.cell_a_bytes_processed, sq.cell_b_bytes_processed, sq.cell_a_lines_processed, sq.cell_b_lines_processed,
			sq.cell_a_bytes_per_second, sq.cell_b_bytes_per_second, sq.cell_a_lines_per_second, sq.cell_b_lines_per_second,
			sq.cell_a_entries_returned, sq.cell_b_entries_returned, sq.cell_a_splits, sq.cell_b_splits,
			sq.cell_a_shards, sq.cell_b_shards, sq.cell_a_response_hash, sq.cell_b_response_hash,
			sq.cell_a_response_size, sq.cell_b_response_size, sq.cell_a_status_code, sq.cell_b_status_code,
			sq.cell_a_trace_id, sq.cell_b_trace_id,
			sq.sampled_at, sq.created_at,
			CASE
				WHEN co.comparison_status IS NOT NULL THEN co.comparison_status
				WHEN (sq.cell_a_status_code NOT BETWEEN 200 AND 299 OR sq.cell_b_status_code NOT BETWEEN 200 AND 299) THEN 'error'
				WHEN sq.cell_a_response_hash = sq.cell_b_response_hash THEN 'match'
				ELSE 'mismatch'
			END as comparison_status
		FROM sampled_queries sq
		LEFT JOIN comparison_outcomes co ON sq.correlation_id = co.correlation_id
	`

	// Build WHERE clause and args based on outcome filter
	var whereClause string
	var countArgs []any
	var queryArgs []any

	switch outcome {
	case outcomeMatch:
		whereClause = whereComparisonStatus
		countArgs = []any{outcomeMatch}
		queryArgs = []any{outcomeMatch}
	case outcomeMismatch:
		whereClause = whereComparisonStatus
		countArgs = []any{outcomeMismatch}
		queryArgs = []any{outcomeMismatch}
	case outcomeError:
		whereClause = whereComparisonStatus
		countArgs = []any{outcomeError}
		queryArgs = []any{outcomeError}
	default:
		// "all" or any other value - no filter
		whereClause = ""
		countArgs = []any{}
		queryArgs = []any{}
	}

	// Get total count with filtering using subquery
	var total int
	countQuery := `
		SELECT COUNT(*)
		FROM (` + baseQuery + `) as computed_results
		` + whereClause

	// Debug logging
	level.Debug(s.logger).Log("ui-component", "goldfish", "msg", "executing count query", "query", countQuery, "args", countArgs)

	err := s.goldfishDB.QueryRow(countQuery, countArgs...).Scan(&total)
	if err != nil {
		level.Error(s.logger).Log("ui-component", "goldfish", "msg", "count query failed", "err", err)
		return nil, err
	}

	level.Debug(s.logger).Log("ui-component", "goldfish", "msg", "count query result", "total", total)

	// Get paginated results with filtering using subquery
	query := `
		SELECT * FROM (` + baseQuery + `) as computed_results
		` + whereClause + `
		ORDER BY sampled_at DESC 
		LIMIT ? OFFSET ?
	`

	// Add pagination parameters to queryArgs
	queryArgs = append(queryArgs, pageSize, offset)

	// Debug logging
	level.Debug(s.logger).Log("ui-component", "goldfish", "msg", "executing main query", "query", query, "args", queryArgs)

	rows, err := s.goldfishDB.Query(query, queryArgs...)
	if err != nil {
		level.Error(s.logger).Log("ui-component", "goldfish", "msg", "main query failed", "err", err)
		return nil, err
	}
	defer rows.Close()

	var queries []SampledQuery
	for rows.Next() {
		var q SampledQuery
		err := rows.Scan(
			&q.CorrelationID, &q.TenantID, &q.Query, &q.QueryType, &q.StartTime, &q.EndTime, &q.StepDuration,
			&q.CellAExecTimeMs, &q.CellBExecTimeMs, &q.CellAQueueTimeMs, &q.CellBQueueTimeMs,
			&q.CellABytesProcessed, &q.CellBBytesProcessed, &q.CellALinesProcessed, &q.CellBLinesProcessed,
			&q.CellABytesPerSecond, &q.CellBBytesPerSecond, &q.CellALinesPerSecond, &q.CellBLinesPerSecond,
			&q.CellAEntriesReturned, &q.CellBEntriesReturned, &q.CellASplits, &q.CellBSplits,
			&q.CellAShards, &q.CellBShards, &q.CellAResponseHash, &q.CellBResponseHash,
			&q.CellAResponseSize, &q.CellBResponseSize, &q.CellAStatusCode, &q.CellBStatusCode,
			&q.CellATraceID, &q.CellBTraceID,
			&q.SampledAt, &q.CreatedAt,
			&q.ComparisonStatus,
		)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &GoldfishAPIResponse{
		Queries:  queries,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// ErrGoldfishDisabled is returned when goldfish feature is disabled
var ErrGoldfishDisabled = sql.ErrNoRows

// ErrGoldfishNotConfigured is returned when goldfish database is not configured
var ErrGoldfishNotConfigured = sql.ErrConnDone
