package ui

import (
	"database/sql"
	"time"
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

	SampledAt time.Time `json:"sampledAt" db:"sampled_at"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
}

// ComparisonOutcome represents a comparison result from the database
type ComparisonOutcome struct {
	CorrelationID      string      `json:"correlationId" db:"correlation_id"`
	ComparisonStatus   string      `json:"comparisonStatus" db:"comparison_status"`
	DifferenceDetails  interface{} `json:"differenceDetails" db:"difference_details"`
	PerformanceMetrics interface{} `json:"performanceMetrics" db:"performance_metrics"`
	ComparedAt         time.Time   `json:"comparedAt" db:"compared_at"`
	CreatedAt          time.Time   `json:"createdAt" db:"created_at"`
}

// GoldfishApiResponse represents the paginated API response
type GoldfishApiResponse struct {
	Queries  []SampledQuery `json:"queries"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
}

// GetSampledQueries retrieves sampled queries from the database with pagination
func (s *Service) GetSampledQueries(page, pageSize int) (*GoldfishApiResponse, error) {
	if !s.cfg.Goldfish.Enable {
		return nil, ErrGoldfishDisabled
	}

	if s.goldfishDB == nil {
		return nil, ErrGoldfishNotConfigured
	}

	offset := (page - 1) * pageSize

	// Get total count
	var total int
	err := s.goldfishDB.QueryRow("SELECT COUNT(*) FROM sampled_queries").Scan(&total)
	if err != nil {
		return nil, err
	}

	// Get paginated results
	query := `
		SELECT 
			correlation_id, tenant_id, query, query_type, start_time, end_time, step_duration,
			cell_a_exec_time_ms, cell_b_exec_time_ms, cell_a_queue_time_ms, cell_b_queue_time_ms,
			cell_a_bytes_processed, cell_b_bytes_processed, cell_a_lines_processed, cell_b_lines_processed,
			cell_a_bytes_per_second, cell_b_bytes_per_second, cell_a_lines_per_second, cell_b_lines_per_second,
			cell_a_entries_returned, cell_b_entries_returned, cell_a_splits, cell_b_splits,
			cell_a_shards, cell_b_shards, cell_a_response_hash, cell_b_response_hash,
			cell_a_response_size, cell_b_response_size, cell_a_status_code, cell_b_status_code,
			sampled_at, created_at
		FROM sampled_queries 
		ORDER BY sampled_at DESC 
		LIMIT ? OFFSET ?
	`

	rows, err := s.goldfishDB.Query(query, pageSize, offset)
	if err != nil {
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
			&q.SampledAt, &q.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &GoldfishApiResponse{
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
