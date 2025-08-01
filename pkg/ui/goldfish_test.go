package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-kit/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoldfishQueriesHandler_AcceptsOutcomeParameter(t *testing.T) {
	// Create mock database
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Create a service with goldfish enabled and mock DB
	service := &Service{
		cfg: Config{
			Goldfish: GoldfishConfig{
				Enable: true,
			},
		},
		logger:     log.NewNopLogger(),
		goldfishDB: db,
	}

	// Mock the count query
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM \(.*\) as computed_results`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	// Mock the main query
	mockTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT \* FROM \(.*\) as computed_results ORDER BY sampled_at DESC LIMIT \? OFFSET \?`).
		WithArgs(20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"correlation_id", "tenant_id", "query", "query_type", "start_time", "end_time", "step_duration",
			"cell_a_exec_time_ms", "cell_b_exec_time_ms", "cell_a_queue_time_ms", "cell_b_queue_time_ms",
			"cell_a_bytes_processed", "cell_b_bytes_processed", "cell_a_lines_processed", "cell_b_lines_processed",
			"cell_a_bytes_per_second", "cell_b_bytes_per_second", "cell_a_lines_per_second", "cell_b_lines_per_second",
			"cell_a_entries_returned", "cell_b_entries_returned", "cell_a_splits", "cell_b_splits",
			"cell_a_shards", "cell_b_shards", "cell_a_response_hash", "cell_b_response_hash",
			"cell_a_response_size", "cell_b_response_size", "cell_a_status_code", "cell_b_status_code",
			"cell_a_trace_id", "cell_b_trace_id", "sampled_at", "created_at", "comparison_status",
		}).
			AddRow("corr1", "tenant1", "rate(log[5m])", "metric", "2023-01-01T12:00:00Z", "2023-01-01T12:05:00Z", 300000,
				100, 120, 10, 15, 1000, 1100, 50, 55, 100, 110, 5, 6, 25, 30, 1, 2, 3, 4,
				"hash1", "hash1", 500, 550, 200, 200, nil, nil, mockTime, mockTime, "match").
			AddRow("corr2", "tenant2", "sum(rate(log[1m]))", "metric", "2023-01-01T12:00:00Z", "2023-01-01T12:05:00Z", 300000,
				150, 140, 20, 18, 2000, 1900, 100, 95, 200, 190, 10, 9, 50, 45, 2, 1, 5, 4,
				"hash2", "hash3", 800, 750, 200, 200, nil, nil, mockTime, mockTime, "mismatch"))

	// Create HTTP test request with outcome parameter
	req := httptest.NewRequest("GET", "/ui/api/v1/goldfish/queries?outcome=all", nil)
	w := httptest.NewRecorder()

	// Call the handler
	handler := service.goldfishQueriesHandler()
	handler.ServeHTTP(w, req)

	// Assert response - should be successful
	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	// Parse the JSON response
	var response GoldfishAPIResponse
	err = json.Unmarshal(body, &response)
	require.NoError(t, err)

	// Verify the response structure
	assert.Equal(t, 2, response.Total)
	assert.Equal(t, 1, response.Page)
	assert.Equal(t, 20, response.PageSize)
	assert.Len(t, response.Queries, 2)

	// Verify the first query
	query1 := response.Queries[0]
	assert.Equal(t, "corr1", query1.CorrelationID)
	assert.Equal(t, "tenant1", query1.TenantID)
	assert.Equal(t, "rate(log[5m])", query1.Query)
	assert.Equal(t, "match", query1.ComparisonStatus)

	// Verify the second query
	query2 := response.Queries[1]
	assert.Equal(t, "corr2", query2.CorrelationID)
	assert.Equal(t, "tenant2", query2.TenantID)
	assert.Equal(t, "sum(rate(log[1m]))", query2.Query)
	assert.Equal(t, "mismatch", query2.ComparisonStatus)

	// Ensure all expectations were met
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGoldfishQueriesHandler_DefaultsToAllOutcome(t *testing.T) {
	// Create mock database
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Create a service with goldfish enabled and mock DB
	service := &Service{
		cfg: Config{
			Goldfish: GoldfishConfig{
				Enable: true,
			},
		},
		logger:     log.NewNopLogger(),
		goldfishDB: db,
	}

	// Mock the count query (no WHERE clause for "all" outcome)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM \(.*\) as computed_results`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	// Mock the main query (no WHERE clause for "all" outcome)
	mockTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT \* FROM \(.*\) as computed_results ORDER BY sampled_at DESC LIMIT \? OFFSET \?`).
		WithArgs(20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"correlation_id", "tenant_id", "query", "query_type", "start_time", "end_time", "step_duration",
			"cell_a_exec_time_ms", "cell_b_exec_time_ms", "cell_a_queue_time_ms", "cell_b_queue_time_ms",
			"cell_a_bytes_processed", "cell_b_bytes_processed", "cell_a_lines_processed", "cell_b_lines_processed",
			"cell_a_bytes_per_second", "cell_b_bytes_per_second", "cell_a_lines_per_second", "cell_b_lines_per_second",
			"cell_a_entries_returned", "cell_b_entries_returned", "cell_a_splits", "cell_b_splits",
			"cell_a_shards", "cell_b_shards", "cell_a_response_hash", "cell_b_response_hash",
			"cell_a_response_size", "cell_b_response_size", "cell_a_status_code", "cell_b_status_code",
			"cell_a_trace_id", "cell_b_trace_id", "sampled_at", "created_at", "comparison_status",
		}).
			AddRow("corr1", "tenant1", "rate(log[5m])", "metric", "2023-01-01T12:00:00Z", "2023-01-01T12:05:00Z", 300000,
				100, 120, 10, 15, 1000, 1100, 50, 55, 100, 110, 5, 6, 25, 30, 1, 2, 3, 4,
				"hash1", "hash1", 500, 550, 200, 200, nil, nil, mockTime, mockTime, "match").
			AddRow("corr2", "tenant2", "sum(rate(log[1m]))", "metric", "2023-01-01T12:00:00Z", "2023-01-01T12:05:00Z", 300000,
				150, 140, 20, 18, 2000, 1900, 100, 95, 200, 190, 10, 9, 50, 45, 2, 1, 5, 4,
				"hash2", "hash3", 800, 750, 200, 200, nil, nil, mockTime, mockTime, "mismatch").
			AddRow("corr3", "tenant3", "count(log)", "metric", "2023-01-01T12:00:00Z", "2023-01-01T12:05:00Z", 300000,
				200, 180, 30, 25, 3000, 2800, 150, 140, 300, 280, 15, 14, 75, 70, 3, 2, 6, 5,
				"hash4", "hash5", 1200, 1100, 500, 200, nil, nil, mockTime, mockTime, "error"))

	// Create HTTP test request without outcome parameter (should default to "all")
	req := httptest.NewRequest("GET", "/ui/api/v1/goldfish/queries", nil)
	w := httptest.NewRecorder()

	// Call the handler
	handler := service.goldfishQueriesHandler()
	handler.ServeHTTP(w, req)

	// Assert response - should be successful
	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	// Parse the JSON response
	var response GoldfishAPIResponse
	err = json.Unmarshal(body, &response)
	require.NoError(t, err)

	// Verify the response structure
	assert.Equal(t, 3, response.Total)
	assert.Equal(t, 1, response.Page)
	assert.Equal(t, 20, response.PageSize)
	assert.Len(t, response.Queries, 3)

	// Verify we get all outcome types (match, mismatch, error)
	statuses := make([]string, 0, 3)
	for _, query := range response.Queries {
		statuses = append(statuses, query.ComparisonStatus)
	}
	assert.Contains(t, statuses, "match")
	assert.Contains(t, statuses, "mismatch")
	assert.Contains(t, statuses, "error")

	// Ensure all expectations were met
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGoldfishQueriesHandler_FiltersMatchOutcome(t *testing.T) {
	// Create mock database
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Create a service with goldfish enabled and mock DB
	service := &Service{
		cfg: Config{
			Goldfish: GoldfishConfig{
				Enable: true,
			},
		},
		logger:     log.NewNopLogger(),
		goldfishDB: db,
	}

	// Mock the count query with WHERE clause for "match" outcome
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM \(.*\) as computed_results WHERE comparison_status = \?`).
		WithArgs("match").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	// Mock the main query with WHERE clause for "match" outcome
	mockTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT \* FROM \(.*\) as computed_results WHERE comparison_status = \? ORDER BY sampled_at DESC LIMIT \? OFFSET \?`).
		WithArgs("match", 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"correlation_id", "tenant_id", "query", "query_type", "start_time", "end_time", "step_duration",
			"cell_a_exec_time_ms", "cell_b_exec_time_ms", "cell_a_queue_time_ms", "cell_b_queue_time_ms",
			"cell_a_bytes_processed", "cell_b_bytes_processed", "cell_a_lines_processed", "cell_b_lines_processed",
			"cell_a_bytes_per_second", "cell_b_bytes_per_second", "cell_a_lines_per_second", "cell_b_lines_per_second",
			"cell_a_entries_returned", "cell_b_entries_returned", "cell_a_splits", "cell_b_splits",
			"cell_a_shards", "cell_b_shards", "cell_a_response_hash", "cell_b_response_hash",
			"cell_a_response_size", "cell_b_response_size", "cell_a_status_code", "cell_b_status_code",
			"cell_a_trace_id", "cell_b_trace_id", "sampled_at", "created_at", "comparison_status",
		}).
			AddRow("corr1", "tenant1", "rate(log[5m])", "metric", "2023-01-01T12:00:00Z", "2023-01-01T12:05:00Z", 300000,
				100, 120, 10, 15, 1000, 1100, 50, 55, 100, 110, 5, 6, 25, 30, 1, 2, 3, 4,
				"hash1", "hash1", 500, 550, 200, 200, nil, nil, mockTime, mockTime, "match"))

	// Create HTTP test request with outcome=match parameter
	req := httptest.NewRequest("GET", "/ui/api/v1/goldfish/queries?outcome=match", nil)
	w := httptest.NewRecorder()

	// Call the handler
	handler := service.goldfishQueriesHandler()
	handler.ServeHTTP(w, req)

	// Assert response - should be successful
	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	// Parse the JSON response
	var response GoldfishAPIResponse
	err = json.Unmarshal(body, &response)
	require.NoError(t, err)

	// Verify the response structure
	assert.Equal(t, 1, response.Total)
	assert.Equal(t, 1, response.Page)
	assert.Equal(t, 20, response.PageSize)
	assert.Len(t, response.Queries, 1)

	// Verify that only "match" results are returned
	query := response.Queries[0]
	assert.Equal(t, "corr1", query.CorrelationID)
	assert.Equal(t, "tenant1", query.TenantID)
	assert.Equal(t, "rate(log[5m])", query.Query)
	assert.Equal(t, "match", query.ComparisonStatus)

	// Ensure all expectations were met
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGoldfishQueriesHandler_FiltersErrorOutcome(t *testing.T) {
	// Create mock database
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Create a service with goldfish enabled and mock DB
	service := &Service{
		cfg: Config{
			Goldfish: GoldfishConfig{
				Enable: true,
			},
		},
		logger:     log.NewNopLogger(),
		goldfishDB: db,
	}

	// Mock the count query with WHERE clause for "error" outcome
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM \(.*\) as computed_results WHERE comparison_status = \?`).
		WithArgs("error").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	// Mock the main query with WHERE clause for "error" outcome
	mockTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT \* FROM \(.*\) as computed_results WHERE comparison_status = \? ORDER BY sampled_at DESC LIMIT \? OFFSET \?`).
		WithArgs("error", 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"correlation_id", "tenant_id", "query", "query_type", "start_time", "end_time", "step_duration",
			"cell_a_exec_time_ms", "cell_b_exec_time_ms", "cell_a_queue_time_ms", "cell_b_queue_time_ms",
			"cell_a_bytes_processed", "cell_b_bytes_processed", "cell_a_lines_processed", "cell_b_lines_processed",
			"cell_a_bytes_per_second", "cell_b_bytes_per_second", "cell_a_lines_per_second", "cell_b_lines_per_second",
			"cell_a_entries_returned", "cell_b_entries_returned", "cell_a_splits", "cell_b_splits",
			"cell_a_shards", "cell_b_shards", "cell_a_response_hash", "cell_b_response_hash",
			"cell_a_response_size", "cell_b_response_size", "cell_a_status_code", "cell_b_status_code",
			"cell_a_trace_id", "cell_b_trace_id", "sampled_at", "created_at", "comparison_status",
		}).
			AddRow("corr3", "tenant3", "count(log)", "metric", "2023-01-01T12:00:00Z", "2023-01-01T12:05:00Z", 300000,
				200, 180, 30, 25, 3000, 2800, 150, 140, 300, 280, 15, 14, 75, 70, 3, 2, 6, 5,
				"hash4", "hash5", 1200, 1100, 500, 200, nil, nil, mockTime, mockTime, "error"))

	// Create HTTP test request with outcome=error parameter
	req := httptest.NewRequest("GET", "/ui/api/v1/goldfish/queries?outcome=error", nil)
	w := httptest.NewRecorder()

	// Call the handler
	handler := service.goldfishQueriesHandler()
	handler.ServeHTTP(w, req)

	// Assert response - should be successful
	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	// Parse the JSON response
	var response GoldfishAPIResponse
	err = json.Unmarshal(body, &response)
	require.NoError(t, err)

	// Verify the response structure
	assert.Equal(t, 1, response.Total)
	assert.Equal(t, 1, response.Page)
	assert.Equal(t, 20, response.PageSize)
	assert.Len(t, response.Queries, 1)

	// Verify that only "error" results are returned
	query := response.Queries[0]
	assert.Equal(t, "corr3", query.CorrelationID)
	assert.Equal(t, "tenant3", query.TenantID)
	assert.Equal(t, "count(log)", query.Query)
	assert.Equal(t, "error", query.ComparisonStatus)

	// Ensure all expectations were met
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGoldfishQueriesHandler_PaginationAndInputValidation(t *testing.T) {
	// Create mock database
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Create a service with goldfish enabled and mock DB
	service := &Service{
		cfg: Config{
			Goldfish: GoldfishConfig{
				Enable: true,
			},
		},
		logger:     log.NewNopLogger(),
		goldfishDB: db,
	}

	// Mock the count query
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM \(.*\) as computed_results`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	// Mock the main query with custom page size and offset
	mockTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT \* FROM \(.*\) as computed_results ORDER BY sampled_at DESC LIMIT \? OFFSET \?`).
		WithArgs(10, 10). // pageSize=10, offset=10 (page 2)
		WillReturnRows(sqlmock.NewRows([]string{
			"correlation_id", "tenant_id", "query", "query_type", "start_time", "end_time", "step_duration",
			"cell_a_exec_time_ms", "cell_b_exec_time_ms", "cell_a_queue_time_ms", "cell_b_queue_time_ms",
			"cell_a_bytes_processed", "cell_b_bytes_processed", "cell_a_lines_processed", "cell_b_lines_processed",
			"cell_a_bytes_per_second", "cell_b_bytes_per_second", "cell_a_lines_per_second", "cell_b_lines_per_second",
			"cell_a_entries_returned", "cell_b_entries_returned", "cell_a_splits", "cell_b_splits",
			"cell_a_shards", "cell_b_shards", "cell_a_response_hash", "cell_b_response_hash",
			"cell_a_response_size", "cell_b_response_size", "cell_a_status_code", "cell_b_status_code",
			"cell_a_trace_id", "cell_b_trace_id", "sampled_at", "created_at", "comparison_status",
		}).
			AddRow("corr1", "tenant1", "rate(log[5m])", "metric", "2023-01-01T12:00:00Z", "2023-01-01T12:05:00Z", 300000,
				100, 120, 10, 15, 1000, 1100, 50, 55, 100, 110, 5, 6, 25, 30, 1, 2, 3, 4,
				"hash1", "hash1", 500, 550, 200, 200, nil, nil, mockTime, mockTime, "match"))

	// Create HTTP test request with custom pagination and invalid outcome (should default to "all")
	req := httptest.NewRequest("GET", "/ui/api/v1/goldfish/queries?page=2&pageSize=10&outcome=invalid_outcome", nil)
	w := httptest.NewRecorder()

	// Call the handler
	handler := service.goldfishQueriesHandler()
	handler.ServeHTTP(w, req)

	// Assert response - should be successful
	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	// Parse the JSON response
	var response GoldfishAPIResponse
	err = json.Unmarshal(body, &response)
	require.NoError(t, err)

	// Verify pagination parameters
	assert.Equal(t, 1, response.Total)
	assert.Equal(t, 2, response.Page)
	assert.Equal(t, 10, response.PageSize)
	assert.Len(t, response.Queries, 1)

	// Ensure all expectations were met
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGoldfishQueriesHandler_ReturnsTraceIDs(t *testing.T) {
	// Create mock database
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Create a service with goldfish enabled and mock DB
	service := &Service{
		cfg: Config{
			Goldfish: GoldfishConfig{
				Enable: true,
			},
		},
		logger:     log.NewNopLogger(),
		goldfishDB: db,
	}

	// Mock the count query
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM \(.*\) as computed_results`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	// Mock the main query - includes trace ID columns
	mockTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	traceID1 := "abc123def456"
	traceID2 := "xyz789uvw012"

	mock.ExpectQuery(`SELECT \* FROM \(.*\) as computed_results ORDER BY sampled_at DESC LIMIT \? OFFSET \?`).
		WithArgs(20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"correlation_id", "tenant_id", "query", "query_type", "start_time", "end_time", "step_duration",
			"cell_a_exec_time_ms", "cell_b_exec_time_ms", "cell_a_queue_time_ms", "cell_b_queue_time_ms",
			"cell_a_bytes_processed", "cell_b_bytes_processed", "cell_a_lines_processed", "cell_b_lines_processed",
			"cell_a_bytes_per_second", "cell_b_bytes_per_second", "cell_a_lines_per_second", "cell_b_lines_per_second",
			"cell_a_entries_returned", "cell_b_entries_returned", "cell_a_splits", "cell_b_splits",
			"cell_a_shards", "cell_b_shards", "cell_a_response_hash", "cell_b_response_hash",
			"cell_a_response_size", "cell_b_response_size", "cell_a_status_code", "cell_b_status_code",
			"cell_a_trace_id", "cell_b_trace_id", "sampled_at", "created_at", "comparison_status",
		}).
			AddRow("corr1", "tenant1", "rate(log[5m])", "metric", "2023-01-01T12:00:00Z", "2023-01-01T12:05:00Z", 300000,
				100, 120, 10, 15, 1000, 1100, 50, 55, 100, 110, 5, 6, 25, 30, 1, 2, 3, 4,
				"hash1", "hash1", 500, 550, 200, 200, &traceID1, &traceID2, mockTime, mockTime, "match").
			AddRow("corr2", "tenant2", "sum(rate(log[1m]))", "metric", "2023-01-01T12:00:00Z", "2023-01-01T12:05:00Z", 300000,
				150, 140, 20, 18, 2000, 1900, 100, 95, 200, 190, 10, 9, 50, 45, 2, 1, 5, 4,
				"hash2", "hash3", 800, 750, 200, 200, nil, nil, mockTime, mockTime, "mismatch"))

	// Create HTTP test request
	req := httptest.NewRequest("GET", "/ui/api/v1/goldfish/queries", nil)
	w := httptest.NewRecorder()

	// Call the handler
	handler := service.goldfishQueriesHandler()
	handler.ServeHTTP(w, req)

	// Assert response - should be successful
	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	if resp.StatusCode != http.StatusOK {
		t.Logf("Response body: %s", string(body))
	}
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Parse the JSON response
	var response GoldfishAPIResponse
	err = json.Unmarshal(body, &response)
	require.NoError(t, err)

	// Verify the response structure
	assert.Equal(t, 2, response.Total)
	assert.Len(t, response.Queries, 2)

	// TEST THE BEHAVIOR: Verify the first query has trace IDs
	query1 := response.Queries[0]
	assert.Equal(t, "corr1", query1.CorrelationID)
	assert.NotNil(t, query1.CellATraceID, "Expected CellATraceID to be present")
	assert.Equal(t, "abc123def456", *query1.CellATraceID, "Expected CellATraceID to match")
	assert.NotNil(t, query1.CellBTraceID, "Expected CellBTraceID to be present")
	assert.Equal(t, "xyz789uvw012", *query1.CellBTraceID, "Expected CellBTraceID to match")

	// TEST THE BEHAVIOR: Verify the second query has null trace IDs
	query2 := response.Queries[1]
	assert.Equal(t, "corr2", query2.CorrelationID)
	assert.Nil(t, query2.CellATraceID, "Expected CellATraceID to be null")
	assert.Nil(t, query2.CellBTraceID, "Expected CellBTraceID to be null")

	// Ensure all expectations were met
	assert.NoError(t, mock.ExpectationsWereMet())
}
