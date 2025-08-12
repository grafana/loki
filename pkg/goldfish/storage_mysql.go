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
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return &MySQLStorage{
		db:     db,
		config: config,
		logger: logger,
	}, nil
}

// StoreQuerySample stores a sampled query with performance statistics
func (s *MySQLStorage) StoreQuerySample(ctx context.Context, sample *QuerySample) error {
	query := `
		INSERT INTO sampled_queries (
			correlation_id, tenant_id, user, query, query_type,
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
			cell_a_trace_id, cell_b_trace_id,
			cell_a_span_id, cell_b_span_id,
			cell_a_used_new_engine, cell_b_used_new_engine,
			sampled_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		sample.CorrelationID,
		sample.TenantID,
		sample.User,
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
		sample.CellATraceID,
		sample.CellBTraceID,
		sample.CellASpanID,
		sample.CellBSpanID,
		sample.CellAUsedNewEngine,
		sample.CellBUsedNewEngine,
		sample.SampledAt,
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

	// Validate outcome parameter against allowed values
	outcome := filter.Outcome
	switch outcome {
	case OutcomeAll, OutcomeMatch, OutcomeMismatch, OutcomeError:
		// Valid values - proceed
	default:
		outcome = OutcomeAll // Default to all if invalid
	}

	offset := (page - 1) * pageSize

	// Build WHERE clause for tenant/user/engine filters
	whereClause, whereArgs := buildWhereClause(filter)

	// Build HAVING clause based on outcome filter
	// Using HAVING clause to filter on computed status without subqueries
	var havingClause string
	var queryArgs []any

	// Add WHERE args first
	queryArgs = append(queryArgs, whereArgs...)

	if outcome != OutcomeAll {
		havingClause = `HAVING comparison_status = ?`
		queryArgs = append(queryArgs, outcome)
	}

	// Get total count with filtering - optimized without subquery
	var total int
	countQuery := `
		SELECT COUNT(*) FROM (
			SELECT 
				sq.correlation_id,
				CASE
					WHEN co.comparison_status IS NOT NULL THEN co.comparison_status
					WHEN (sq.cell_a_status_code NOT BETWEEN 200 AND 299 OR sq.cell_b_status_code NOT BETWEEN 200 AND 299) THEN 'error'
					WHEN sq.cell_a_response_hash = sq.cell_b_response_hash THEN 'match'
					ELSE 'mismatch'
				END as comparison_status
			FROM sampled_queries sq
			LEFT JOIN comparison_outcomes co ON sq.correlation_id = co.correlation_id
			` + whereClause + `
			` + havingClause + `
		) as filtered
	`

	// Debug logging
	level.Debug(s.logger).Log("ui-component", "goldfish", "msg", "executing count query", "query", countQuery, "args", queryArgs)

	err := s.db.QueryRowContext(ctx, countQuery, queryArgs...).Scan(&total)
	if err != nil {
		level.Error(s.logger).Log("ui-component", "goldfish", "msg", "count query failed", "err", err)
		return nil, err
	}

	level.Debug(s.logger).Log("ui-component", "goldfish", "msg", "count query result", "total", total)

	// Get paginated results - optimized query without nested subqueries
	query := `
		SELECT
			sq.correlation_id, sq.tenant_id, sq.user, sq.query, sq.query_type, sq.start_time, sq.end_time, sq.step_duration,
			sq.cell_a_exec_time_ms, sq.cell_b_exec_time_ms, sq.cell_a_queue_time_ms, sq.cell_b_queue_time_ms,
			sq.cell_a_bytes_processed, sq.cell_b_bytes_processed, sq.cell_a_lines_processed, sq.cell_b_lines_processed,
			sq.cell_a_bytes_per_second, sq.cell_b_bytes_per_second, sq.cell_a_lines_per_second, sq.cell_b_lines_per_second,
			sq.cell_a_entries_returned, sq.cell_b_entries_returned, sq.cell_a_splits, sq.cell_b_splits,
			sq.cell_a_shards, sq.cell_b_shards, sq.cell_a_response_hash, sq.cell_b_response_hash,
			sq.cell_a_response_size, sq.cell_b_response_size, sq.cell_a_status_code, sq.cell_b_status_code,
			sq.cell_a_trace_id, sq.cell_b_trace_id,
			sq.cell_a_span_id, sq.cell_b_span_id,
			sq.sampled_at, sq.created_at,
			CASE
				WHEN co.comparison_status IS NOT NULL THEN co.comparison_status
				WHEN (sq.cell_a_status_code NOT BETWEEN 200 AND 299 OR sq.cell_b_status_code NOT BETWEEN 200 AND 299) THEN 'error'
				WHEN sq.cell_a_response_hash = sq.cell_b_response_hash THEN 'match'
				ELSE 'mismatch'
			END as comparison_status
		FROM sampled_queries sq FORCE INDEX (idx_sampled_queries_sampled_at_desc)
		LEFT JOIN comparison_outcomes co ON sq.correlation_id = co.correlation_id
		` + whereClause + `
		` + havingClause + `
		ORDER BY sq.sampled_at DESC 
		LIMIT ? OFFSET ?
	`

	// Add pagination parameters to queryArgs
	queryArgs = append(queryArgs, pageSize, offset)

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
		var comparisonStatus string
		var createdAt time.Time

		err := rows.Scan(
			&q.CorrelationID, &q.TenantID, &q.User, &q.Query, &q.QueryType, &q.StartTime, &q.EndTime, &stepDurationMs,
			&q.CellAStats.ExecTimeMs, &q.CellBStats.ExecTimeMs, &q.CellAStats.QueueTimeMs, &q.CellBStats.QueueTimeMs,
			&q.CellAStats.BytesProcessed, &q.CellBStats.BytesProcessed, &q.CellAStats.LinesProcessed, &q.CellBStats.LinesProcessed,
			&q.CellAStats.BytesPerSecond, &q.CellBStats.BytesPerSecond, &q.CellAStats.LinesPerSecond, &q.CellBStats.LinesPerSecond,
			&q.CellAStats.TotalEntriesReturned, &q.CellBStats.TotalEntriesReturned, &q.CellAStats.Splits, &q.CellBStats.Splits,
			&q.CellAStats.Shards, &q.CellBStats.Shards, &q.CellAResponseHash, &q.CellBResponseHash,
			&q.CellAResponseSize, &q.CellBResponseSize, &q.CellAStatusCode, &q.CellBStatusCode,
			&q.CellATraceID, &q.CellBTraceID,
			&q.CellASpanID, &q.CellBSpanID,
			&q.SampledAt, &createdAt,
			&comparisonStatus,
		)
		if err != nil {
			return nil, err
		}

		// Convert step duration from milliseconds to Duration
		q.Step = time.Duration(stepDurationMs) * time.Millisecond

		queries = append(queries, q)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &APIResponse{
		Queries:  queries,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// Close closes the storage connection
func (s *MySQLStorage) Close() error {
	return s.db.Close()
}

// buildWhereClause constructs a WHERE clause based on the filter parameters
func buildWhereClause(filter QueryFilter) (string, []any) {
	var conditions []string
	var args []any

	// Add tenant filter
	if filter.Tenant != "" {
		conditions = append(conditions, "sq.tenant_id = ?")
		args = append(args, filter.Tenant)
	}

	// Add user filter
	if filter.User != "" {
		conditions = append(conditions, "sq.user = ?")
		args = append(args, filter.User)
	}

	// Add new engine filter
	if filter.UsedNewEngine != nil {
		if *filter.UsedNewEngine {
			// Either cell used new engine
			conditions = append(conditions, "(sq.cell_a_used_new_engine = 1 OR sq.cell_b_used_new_engine = 1)")
		} else {
			// Neither cell used new engine
			conditions = append(conditions, "sq.cell_a_used_new_engine = 0 AND sq.cell_b_used_new_engine = 0")
		}
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
