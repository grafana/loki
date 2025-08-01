package goldfish

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql" // MySQL driver
)

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

	// Initialize schema
	if err := initMySQLSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
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
			cell_a_used_new_engine, cell_b_used_new_engine,
			sampled_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		sample.CellAUsedNewEngine,
		sample.CellBUsedNewEngine,
		sample.SampledAt,
	)

	return err
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

// initMySQLSchema creates the necessary tables if they don't exist
func initMySQLSchema(db *sql.DB) error {
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS sampled_queries (
			correlation_id VARCHAR(36) PRIMARY KEY,
			tenant_id VARCHAR(255) NOT NULL,
			query TEXT NOT NULL,
			query_type VARCHAR(50) NOT NULL,
			start_time TIMESTAMP,
			end_time TIMESTAMP,
			step_duration BIGINT,

			-- Performance statistics
			cell_a_exec_time_ms BIGINT,
			cell_b_exec_time_ms BIGINT,
			cell_a_queue_time_ms BIGINT,
			cell_b_queue_time_ms BIGINT,
			cell_a_bytes_processed BIGINT,
			cell_b_bytes_processed BIGINT,
			cell_a_lines_processed BIGINT,
			cell_b_lines_processed BIGINT,
			cell_a_bytes_per_second BIGINT,
			cell_b_bytes_per_second BIGINT,
			cell_a_lines_per_second BIGINT,
			cell_b_lines_per_second BIGINT,
			cell_a_entries_returned BIGINT,
			cell_b_entries_returned BIGINT,
			cell_a_splits BIGINT,
			cell_b_splits BIGINT,
			cell_a_shards BIGINT,
			cell_b_shards BIGINT,

			-- Response metadata without sensitive content
			cell_a_response_hash VARCHAR(64),
			cell_b_response_hash VARCHAR(64),
			cell_a_response_size BIGINT,
			cell_b_response_size BIGINT,
			cell_a_status_code INTEGER,
			cell_b_status_code INTEGER,

			-- Query engine version tracking
			cell_a_used_new_engine BOOLEAN DEFAULT FALSE,
			cell_b_used_new_engine BOOLEAN DEFAULT FALSE,

			sampled_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS comparison_outcomes (
			correlation_id VARCHAR(36) PRIMARY KEY,
			comparison_status VARCHAR(50) NOT NULL,
			difference_details JSON,
			performance_metrics JSON,
			compared_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (correlation_id) REFERENCES sampled_queries(correlation_id)
		)`,

		`CREATE INDEX idx_sampled_queries_tenant ON sampled_queries(tenant_id)`,
		`CREATE INDEX idx_sampled_queries_time ON sampled_queries(sampled_at)`,
		`CREATE INDEX idx_comparison_status ON comparison_outcomes(comparison_status)`,
	}

	for i, schema := range schemas {
		if _, err := db.Exec(schema); err != nil {
			// MySQL might return an error if index already exists, which is OK
			if i >= 2 && strings.Contains(err.Error(), "Duplicate key name") {
				continue
			}
			return fmt.Errorf("failed to execute schema statement %d: %w", i+1, err)
		}
	}

	return nil
}

