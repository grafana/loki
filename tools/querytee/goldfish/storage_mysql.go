package goldfish

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql" // MySQL driver
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var embedMigrations embed.FS

// MySQLStorage provides common MySQL storage implementation
type MySQLStorage struct {
	db     *sql.DB
	config StorageConfig
}

// NewMySQLStorage creates a new MySQL storage backend with a custom DSN
func NewMySQLStorage(dsn string, config StorageConfig) (*MySQLStorage, error) {
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
	}, nil
}

// StoreQuerySample stores a sampled query with performance statistics
func (s *MySQLStorage) StoreQuerySample(ctx context.Context, sample *QuerySample) error {
	query := `
		INSERT INTO sampled_queries (
			correlation_id, tenant_id, query, query_type,
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
			cell_a_used_new_engine, cell_b_used_new_engine,
			sampled_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		sample.CorrelationID,
		sample.TenantID,
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
		sample.CellAUsedNewEngine,
		sample.CellBUsedNewEngine,
		sample.SampledAt,
	)

	return err
}

// GetTraceIDs retrieves trace IDs for a given correlation ID
// TODO(twhitney): Move this to the query logic used by the UI once we consolidate the two
func (s *MySQLStorage) GetTraceIDs(correlationID string) (cellATraceID, cellBTraceID string, err error) {
	query := "SELECT cell_a_trace_id, cell_b_trace_id FROM sampled_queries WHERE correlation_id = ?"

	err = s.db.QueryRow(query, correlationID).Scan(&cellATraceID, &cellBTraceID)
	if err != nil {
		return "", "", fmt.Errorf("failed to retrieve trace IDs: %w", err)
	}

	return cellATraceID, cellBTraceID, nil
}

// StoreComparisonResult stores a comparison result
func (s *MySQLStorage) StoreComparisonResult(ctx context.Context, result *ComparisonResult) error {
	differenceJSON, err := json.Marshal(result.DifferenceDetails)
	if err != nil {
		return fmt.Errorf("failed to marshal difference details: %w", err)
	}

	metricsJSON, err := json.Marshal(result.PerformanceMetrics)
	if err != nil {
		return fmt.Errorf("failed to marshal performance metrics: %w", err)
	}

	query := `
		INSERT INTO comparison_outcomes (
			correlation_id, comparison_status,
			difference_details, performance_metrics,
			compared_at
		) VALUES (?, ?, ?, ?, ?)
	`

	_, err = s.db.ExecContext(ctx, query,
		result.CorrelationID,
		result.ComparisonStatus,
		differenceJSON,
		metricsJSON,
		result.ComparedAt,
	)

	return err
}

// Close closes the database connection
func (s *MySQLStorage) Close() error {
	return s.db.Close()
}

// GetDB returns the underlying database connection for testing
func (s *MySQLStorage) GetDB() *sql.DB {
	return s.db
}

// runMigrations executes database migrations using embedded goose migrations
func runMigrations(db *sql.DB) error {
	// Set goose dialect to MySQL
	if err := goose.SetDialect("mysql"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	// Set embedded migration provider
	goose.SetBaseFS(embedMigrations)

	// Run migrations from embedded filesystem
	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}
