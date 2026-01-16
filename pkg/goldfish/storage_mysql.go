package goldfish

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	_ "github.com/go-sql-driver/mysql" // MySQL driver
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var embedMigrations embed.FS

// MySQLStorage provides MySQL storage implementation
type MySQLStorage struct {
	db     *sql.DB
	config StorageConfig
	logger log.Logger
}

// NewMySQLStorage creates a new MySQL storage backend
func NewMySQLStorage(config StorageConfig, logger log.Logger) (*MySQLStorage, error) {
	var dsn string
	password := os.Getenv("GOLDFISH_DB_PASSWORD")

	switch config.Type {
	case "mysql":
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&multiStatements=true",
			config.MySQLUser, password, config.MySQLHost, config.MySQLPort, config.MySQLDatabase)
	case "cloudsql":
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&multiStatements=true",
			config.CloudSQLUser, password, config.CloudSQLHost, config.CloudSQLPort, config.CloudSQLDatabase)
	case "rds":
		dsn = fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true&multiStatements=true",
			config.RDSUser, password, config.RDSEndpoint, config.RDSDatabase)
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", config.Type)
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(config.MaxConnections)
	db.SetMaxIdleConns(config.MaxConnections / 2)
	db.SetConnMaxIdleTime(time.Duration(config.MaxIdleTime) * time.Second)

	// Verify connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Run migrations
	if err := runMigrations(db); err != nil {
		level.Error(logger).Log("msg", "failed to run goldfish migrations", "err", err)
	}

	return &MySQLStorage{
		db:     db,
		config: config,
		logger: logger,
	}, nil
}

// StoreQuerySample stores a sampled query with performance statistics
func (s *MySQLStorage) StoreQuerySample(ctx context.Context, sample *QuerySample, comparison *ComparisonResult) error {
	query := `
		INSERT INTO sampled_queries (
			correlation_id, tenant_id, user, is_logs_drilldown, query, query_type,
			start_time, end_time, step_duration,
			cell_a_exec_time_ms, cell_b_exec_time_ms,
			cell_a_queue_time_ms, cell_b_queue_time_ms,
			cell_a_bytes_processed, cell_b_bytes_processed,
			cell_a_lines_processed, cell_b_lines_processed,
			cell_a_bytes_per_second, cell_b_bytes_per_second,
			cell_a_lines_per_second, cell_b_lines_per_second,
			cell_a_entries_returned, cell_b_entries_returned,
			cell_a_splits, cell_b_splits,
			cell_a_shards, cell_b_shards,
			cell_a_response_hash, cell_b_response_hash,
			cell_a_response_size, cell_b_response_size,
			cell_a_status_code, cell_b_status_code,
			cell_a_result_uri, cell_b_result_uri,
			cell_a_result_size_bytes, cell_b_result_size_bytes,
			cell_a_result_compression, cell_b_result_compression,
			cell_a_trace_id, cell_b_trace_id,
			cell_a_span_id, cell_b_span_id,
			cell_a_used_new_engine, cell_b_used_new_engine,
			sampled_at,
			comparison_status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	// Convert empty span IDs to NULL for database storage
	var cellASpanID, cellBSpanID any
	if sample.CellASpanID != "" {
		cellASpanID = sample.CellASpanID
	}
	if sample.CellBSpanID != "" {
		cellBSpanID = sample.CellBSpanID
	}

	// Prepare nullable result storage metadata
	var cellAResultURI, cellBResultURI any
	var cellAResultSize, cellBResultSize any
	var cellAResultCompression, cellBResultCompression any
	if sample.CellAResultURI != "" {
		cellAResultURI = sample.CellAResultURI
		cellAResultSize = sample.CellAResultSize
		if sample.CellAResultCompression != "" {
			cellAResultCompression = sample.CellAResultCompression
		}
	}
	if sample.CellBResultURI != "" {
		cellBResultURI = sample.CellBResultURI
		cellBResultSize = sample.CellBResultSize
		if sample.CellBResultCompression != "" {
			cellBResultCompression = sample.CellBResultCompression
		}
	}

	_, err := s.db.ExecContext(ctx, query,
		sample.CorrelationID,
		sample.TenantID,
		sample.User,
		sample.IsLogsDrilldown,
		sample.Query,
		sample.QueryType,
		sample.StartTime,
		sample.EndTime,
		sample.Step.Milliseconds(),
		sample.CellAStats.ExecTimeMs,
		sample.CellBStats.ExecTimeMs,
		sample.CellAStats.QueueTimeMs,
		sample.CellBStats.QueueTimeMs,
		sample.CellAStats.BytesProcessed,
		sample.CellBStats.BytesProcessed,
		sample.CellAStats.LinesProcessed,
		sample.CellBStats.LinesProcessed,
		sample.CellAStats.BytesPerSecond,
		sample.CellBStats.BytesPerSecond,
		sample.CellAStats.LinesPerSecond,
		sample.CellBStats.LinesPerSecond,
		sample.CellAStats.TotalEntriesReturned,
		sample.CellBStats.TotalEntriesReturned,
		sample.CellAStats.Splits,
		sample.CellBStats.Splits,
		sample.CellAStats.Shards,
		sample.CellBStats.Shards,
		sample.CellAResponseHash,
		sample.CellBResponseHash,
		sample.CellAResponseSize,
		sample.CellBResponseSize,
		sample.CellAStatusCode,
		sample.CellBStatusCode,
		cellAResultURI,
		cellBResultURI,
		cellAResultSize,
		cellBResultSize,
		cellAResultCompression,
		cellBResultCompression,
		sample.CellATraceID,
		sample.CellBTraceID,
		cellASpanID,
		cellBSpanID,
		sample.CellAUsedNewEngine,
		sample.CellBUsedNewEngine,
		sample.SampledAt,
		comparison.ComparisonStatus,
	)

	return err
}

// StoreComparisonResult stores the outcome of comparing two responses
func (s *MySQLStorage) StoreComparisonResult(ctx context.Context, result *ComparisonResult) error {
	differenceJSON, err := json.Marshal(result.DifferenceDetails)
	if err != nil {
		return fmt.Errorf("failed to marshal difference details: %w", err)
	}

	perfMetricsJSON, err := json.Marshal(result.PerformanceMetrics)
	if err != nil {
		return fmt.Errorf("failed to marshal performance metrics: %w", err)
	}

	query := `
		INSERT INTO comparison_outcomes (
			correlation_id, comparison_status,
			difference_details, performance_metrics,
			compared_at
		) VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			comparison_status = VALUES(comparison_status),
			difference_details = VALUES(difference_details),
			performance_metrics = VALUES(performance_metrics),
			compared_at = VALUES(compared_at)
	`

	_, err = s.db.ExecContext(ctx, query,
		result.CorrelationID,
		result.ComparisonStatus,
		differenceJSON,
		perfMetricsJSON,
		result.ComparedAt,
	)

	return err
}

// GetSampledQueries retrieves sampled queries from the database with pagination and outcome filtering
func (s *MySQLStorage) GetSampledQueries(ctx context.Context, page, pageSize int, filter QueryFilter) (*APIResponse, error) {
	// Validate and sanitize input parameters
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 1000 {
		pageSize = 20 // Default page size
	}

	offset := (page - 1) * pageSize

	// Build WHERE clause for tenant/user/engine/time filters
	whereClause, whereArgs := buildWhereClause(filter)

	query := `
		SELECT
			correlation_id, tenant_id, user, query, query_type, start_time, end_time, step_duration,
			cell_a_exec_time_ms, cell_b_exec_time_ms, cell_a_queue_time_ms, cell_b_queue_time_ms,
			cell_a_bytes_processed, cell_b_bytes_processed, cell_a_lines_processed, cell_b_lines_processed,
			cell_a_bytes_per_second, cell_b_bytes_per_second, cell_a_lines_per_second, cell_b_lines_per_second,
			cell_a_entries_returned, cell_b_entries_returned, cell_a_splits, cell_b_splits,
			cell_a_shards, cell_b_shards, cell_a_response_hash, cell_b_response_hash,
			cell_a_response_size, cell_b_response_size, cell_a_status_code, cell_b_status_code,
			cell_a_result_uri, cell_b_result_uri,
			cell_a_result_size_bytes, cell_b_result_size_bytes,
			cell_a_result_compression, cell_b_result_compression,
			cell_a_trace_id, cell_b_trace_id,
			cell_a_span_id, cell_b_span_id,
			cell_a_used_new_engine, cell_b_used_new_engine,
			sampled_at, created_at,
			comparison_status
		FROM sampled_queries
		` + whereClause + `
		ORDER BY sampled_at DESC
		LIMIT ? OFFSET ?
	`

	// Fetch one extra record to determine if there are more pages
	// We'll fetch pageSize+1 records, then check if we got more than pageSize
	queryArgs := append(whereArgs, pageSize+1, offset)

	// Debug logging
	level.Debug(s.logger).Log("ui-component", "goldfish", "msg", "executing main query", "query", query, "args", queryArgs)

	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		level.Error(s.logger).Log("ui-component", "goldfish", "msg", "main query failed", "err", err)
		return nil, err
	}
	defer rows.Close()

	var queries []QuerySample
	for rows.Next() {
		var q QuerySample
		var stepDurationMs int64
		var createdAt time.Time
		// Use sql.NullString for nullable span ID columns
		var cellASpanID, cellBSpanID sql.NullString
		var cellAResultURI, cellBResultURI sql.NullString
		var cellAResultCompression, cellBResultCompression sql.NullString
		var cellAResultSize, cellBResultSize sql.NullInt64

		err := rows.Scan(
			&q.CorrelationID, &q.TenantID, &q.User, &q.Query, &q.QueryType, &q.StartTime, &q.EndTime, &stepDurationMs,
			&q.CellAStats.ExecTimeMs, &q.CellBStats.ExecTimeMs, &q.CellAStats.QueueTimeMs, &q.CellBStats.QueueTimeMs,
			&q.CellAStats.BytesProcessed, &q.CellBStats.BytesProcessed, &q.CellAStats.LinesProcessed, &q.CellBStats.LinesProcessed,
			&q.CellAStats.BytesPerSecond, &q.CellBStats.BytesPerSecond, &q.CellAStats.LinesPerSecond, &q.CellBStats.LinesPerSecond,
			&q.CellAStats.TotalEntriesReturned, &q.CellBStats.TotalEntriesReturned, &q.CellAStats.Splits, &q.CellBStats.Splits,
			&q.CellAStats.Shards, &q.CellBStats.Shards, &q.CellAResponseHash, &q.CellBResponseHash,
			&q.CellAResponseSize, &q.CellBResponseSize, &q.CellAStatusCode, &q.CellBStatusCode,
			&cellAResultURI, &cellBResultURI,
			&cellAResultSize, &cellBResultSize,
			&cellAResultCompression, &cellBResultCompression,
			&q.CellATraceID, &q.CellBTraceID,
			&cellASpanID, &cellBSpanID,
			&q.CellAUsedNewEngine, &q.CellBUsedNewEngine,
			&q.SampledAt, &createdAt,
			&q.ComparisonStatus,
		)
		if err != nil {
			return nil, err
		}

		// Convert nullable strings to regular strings (empty string if NULL)
		if cellASpanID.Valid {
			q.CellASpanID = cellASpanID.String
		}
		if cellBSpanID.Valid {
			q.CellBSpanID = cellBSpanID.String
		}
		if cellAResultURI.Valid {
			q.CellAResultURI = cellAResultURI.String
		}
		if cellBResultURI.Valid {
			q.CellBResultURI = cellBResultURI.String
		}
		if cellAResultSize.Valid {
			q.CellAResultSize = cellAResultSize.Int64
		}
		if cellBResultSize.Valid {
			q.CellBResultSize = cellBResultSize.Int64
		}
		if cellAResultCompression.Valid {
			q.CellAResultCompression = cellAResultCompression.String
		}
		if cellBResultCompression.Valid {
			q.CellBResultCompression = cellBResultCompression.String
		}

		// Convert step duration from milliseconds to Duration
		q.Step = time.Duration(stepDurationMs) * time.Millisecond

		queries = append(queries, q)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Check if we have more records than requested
	hasMore := false
	if len(queries) > pageSize {
		hasMore = true
		// Trim the result to the requested page size
		queries = queries[:pageSize]
	}

	return &APIResponse{
		Queries:  queries,
		HasMore:  hasMore,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// GetQueryByCorrelationID retrieves a single query sample by correlation ID
func (s *MySQLStorage) GetQueryByCorrelationID(ctx context.Context, correlationID string) (*QuerySample, error) {
	query := `
		SELECT
			correlation_id, tenant_id, user, query, query_type, start_time, end_time, step_duration,
			cell_a_exec_time_ms, cell_b_exec_time_ms, cell_a_queue_time_ms, cell_b_queue_time_ms,
			cell_a_bytes_processed, cell_b_bytes_processed, cell_a_lines_processed, cell_b_lines_processed,
			cell_a_bytes_per_second, cell_b_bytes_per_second, cell_a_lines_per_second, cell_b_lines_per_second,
			cell_a_entries_returned, cell_b_entries_returned, cell_a_splits, cell_b_splits,
			cell_a_shards, cell_b_shards, cell_a_response_hash, cell_b_response_hash,
			cell_a_response_size, cell_b_response_size, cell_a_status_code, cell_b_status_code,
			cell_a_result_uri, cell_b_result_uri,
			cell_a_result_size_bytes, cell_b_result_size_bytes,
			cell_a_result_compression, cell_b_result_compression,
			cell_a_trace_id, cell_b_trace_id,
			cell_a_span_id, cell_b_span_id,
			cell_a_used_new_engine, cell_b_used_new_engine,
			sampled_at, created_at, comparison_status
		FROM sampled_queries
		WHERE correlation_id = ?
	`

	var q QuerySample
	var stepDurationMs int64
	var createdAt time.Time
	var cellASpanID, cellBSpanID sql.NullString
	var cellAResultURI, cellBResultURI sql.NullString
	var cellAResultCompression, cellBResultCompression sql.NullString
	var cellAResultSize, cellBResultSize sql.NullInt64

	err := s.db.QueryRowContext(ctx, query, correlationID).Scan(
		&q.CorrelationID, &q.TenantID, &q.User, &q.Query, &q.QueryType, &q.StartTime, &q.EndTime, &stepDurationMs,
		&q.CellAStats.ExecTimeMs, &q.CellBStats.ExecTimeMs, &q.CellAStats.QueueTimeMs, &q.CellBStats.QueueTimeMs,
		&q.CellAStats.BytesProcessed, &q.CellBStats.BytesProcessed, &q.CellAStats.LinesProcessed, &q.CellBStats.LinesProcessed,
		&q.CellAStats.BytesPerSecond, &q.CellBStats.BytesPerSecond, &q.CellAStats.LinesPerSecond, &q.CellBStats.LinesPerSecond,
		&q.CellAStats.TotalEntriesReturned, &q.CellBStats.TotalEntriesReturned, &q.CellAStats.Splits, &q.CellBStats.Splits,
		&q.CellAStats.Shards, &q.CellBStats.Shards, &q.CellAResponseHash, &q.CellBResponseHash,
		&q.CellAResponseSize, &q.CellBResponseSize, &q.CellAStatusCode, &q.CellBStatusCode,
		&cellAResultURI, &cellBResultURI,
		&cellAResultSize, &cellBResultSize,
		&cellAResultCompression, &cellBResultCompression,
		&q.CellATraceID, &q.CellBTraceID,
		&cellASpanID, &cellBSpanID,
		&q.CellAUsedNewEngine, &q.CellBUsedNewEngine,
		&q.SampledAt, &createdAt, &q.ComparisonStatus,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("query with correlation ID %s not found", correlationID)
		}
		return nil, fmt.Errorf("failed to query by correlation ID: %w", err)
	}

	// Convert nullable strings to regular strings
	if cellASpanID.Valid {
		q.CellASpanID = cellASpanID.String
	}
	if cellBSpanID.Valid {
		q.CellBSpanID = cellBSpanID.String
	}
	if cellAResultURI.Valid {
		q.CellAResultURI = cellAResultURI.String
	}
	if cellBResultURI.Valid {
		q.CellBResultURI = cellBResultURI.String
	}
	if cellAResultSize.Valid {
		q.CellAResultSize = cellAResultSize.Int64
	}
	if cellBResultSize.Valid {
		q.CellBResultSize = cellBResultSize.Int64
	}
	if cellAResultCompression.Valid {
		q.CellAResultCompression = cellAResultCompression.String
	}
	if cellBResultCompression.Valid {
		q.CellBResultCompression = cellBResultCompression.String
	}

	// Convert step duration from milliseconds to Duration
	q.Step = time.Duration(stepDurationMs) * time.Millisecond

	return &q, nil
}

// GetStatistics retrieves aggregated statistics from the database
func (s *MySQLStorage) GetStatistics(ctx context.Context, filter StatsFilter) (*Statistics, error) {
	stats := &Statistics{}

	// Build WHERE clause for time and uses_recent_data filters
	whereClause, whereArgs := buildStatsWhereClause(filter)

	// #nosec G201 nosemgrep: string-formatted-query
	// Safe: buildStatsWhereClause() uses parameterized queries for user inputs
	queriesExecutedQuery := `
		SELECT COUNT(sampled_at) as value
		FROM sampled_queries
		` + whereClause + `
		AND cell_b_used_new_engine = 1
	`
	err := s.db.QueryRowContext(ctx, queriesExecutedQuery, whereArgs...).Scan(&stats.QueriesExecuted)
	if err != nil {
		return nil, fmt.Errorf("failed to get queries executed: %w", err)
	}

	// #nosec G201 nosemgrep: string-formatted-query
	// Safe: buildStatsWhereClause() uses parameterized queries for user inputs
	engineCoverageQuery := `
		SELECT
			CASE
				WHEN COUNT(sampled_at) = 0 THEN 0
				ELSE SUM(cell_b_used_new_engine) / COUNT(sampled_at)
			END as value
		FROM sampled_queries
		` + whereClause
	err = s.db.QueryRowContext(ctx, engineCoverageQuery, whereArgs...).Scan(&stats.EngineCoverage)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine coverage: %w", err)
	}

	// Matching Queries (average of matching hashes for successful queries with new engine)
	// #nosec G201 nosemgrep: string-formatted-query
	// Safe: buildStatsWhereClause() uses parameterized queries for user inputs
	matchingQueriesQuery := `
		SELECT
			COALESCE(AVG(cell_a_response_hash = cell_b_response_hash), 0) as value
		FROM sampled_queries
		` + whereClause + `
		AND cell_a_status_code = 200
		AND cell_b_status_code = 200
		AND cell_b_used_new_engine = 1
	`
	err = s.db.QueryRowContext(ctx, matchingQueriesQuery, whereArgs...).Scan(&stats.MatchingQueries)
	if err != nil {
		return nil, fmt.Errorf("failed to get matching queries: %w", err)
	}

	// Performance Difference (geometric mean of performance ratio for matching queries)
	// Only calculate for queries that match and have valid data
	// #nosec G201 nosemgrep: string-formatted-query
	// Safe: buildStatsWhereClause() uses parameterized queries for user inputs
	perfDifferenceQuery := `
		SELECT
			COALESCE(EXP(AVG(LN(cell_b_exec_time_ms / cell_a_exec_time_ms))) - 1, 0) as value
		FROM sampled_queries
		` + whereClause + `
		AND cell_a_response_hash = cell_b_response_hash
		AND cell_b_used_new_engine = 1
		AND cell_a_exec_time_ms > 0
		AND cell_b_exec_time_ms > 0
		AND (cell_a_bytes_processed > 0 OR cell_a_entries_returned = 0)
		AND (cell_b_bytes_processed > 0 OR cell_b_entries_returned = 0)
	`
	err = s.db.QueryRowContext(ctx, perfDifferenceQuery, whereArgs...).Scan(&stats.PerformanceDifference)
	if err != nil {
		return nil, fmt.Errorf("failed to get performance difference: %w", err)
	}

	return stats, nil
}

// Close closes the storage connection
func (s *MySQLStorage) Close() error {
	return s.db.Close()
}

// buildWhereClause constructs a WHERE clause based on the filter parameters
func buildWhereClause(filter QueryFilter) (string, []any) {
	var conditions []string
	var args []any

	// Add time range filters
	if !filter.From.IsZero() {
		conditions = append(conditions, "sampled_at >= ?")
		args = append(args, filter.From)
	}

	if !filter.To.IsZero() {
		conditions = append(conditions, "sampled_at <= ?")
		args = append(args, filter.To)
	}

	// Add tenant filter
	if filter.Tenant != "" {
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, filter.Tenant)
	}

	// Add user filter
	if filter.User != "" {
		conditions = append(conditions, "user = ?")
		args = append(args, filter.User)
	}

	// Add logs drilldown filter
	if filter.IsLogsDrilldown != nil {
		conditions = append(conditions, "is_logs_drilldown = ?")
		args = append(args, *filter.IsLogsDrilldown)
	}

	// Add new engine filter
	if filter.UsedNewEngine != nil {
		if *filter.UsedNewEngine {
			// Either cell used new engine
			conditions = append(conditions, "(cell_a_used_new_engine = 1 OR cell_b_used_new_engine = 1)")
		} else {
			// Neither cell used new engine
			conditions = append(conditions, "cell_a_used_new_engine = 0 AND cell_b_used_new_engine = 0")
		}
	}

	// Add comparison status filter
	if filter.ComparisonStatus != "" {
		conditions = append(conditions, "comparison_status = ?")
		args = append(args, filter.ComparisonStatus)
	}

	// Combine conditions
	if len(conditions) == 0 {
		return "", nil
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")
	return whereClause, args
}

// buildStatsWhereClause constructs a WHERE clause for statistics queries
func buildStatsWhereClause(filter StatsFilter) (string, []any) {
	var conditions []string
	var args []any

	if !filter.From.IsZero() {
		conditions = append(conditions, "sampled_at >= ?")
		args = append(args, filter.From)
	}

	if !filter.To.IsZero() {
		conditions = append(conditions, "sampled_at <= ?")
		args = append(args, filter.To)
	}

	// When UsesRecentData is false, exclude queries that touch data within the last 3 hours
	if !filter.UsesRecentData {
		conditions = append(conditions, "end_time <= DATE_SUB(NOW(), INTERVAL 3 HOUR)")
	}

	// Combine conditions
	if len(conditions) == 0 {
		return "", nil
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")
	return whereClause, args
}

// runMigrations runs database migrations
func runMigrations(db *sql.DB) error {
	goose.SetBaseFS(embedMigrations)

	if err := goose.SetDialect("mysql"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}
