package goldfish

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
)

// CloudSQLStorage implements Storage for Google Cloud SQL
type CloudSQLStorage struct {
	db     *sql.DB
	config StorageConfig
}

// NewCloudSQLStorage creates a new CloudSQL storage backend
func NewCloudSQLStorage(config StorageConfig, password string) (*CloudSQLStorage, error) {
	if password == "" {
		return nil, fmt.Errorf("CloudSQL password must be provided via GOLDFISH_DB_PASSWORD environment variable")
	}

	// Build DSN for CloudSQL proxy connection
	// The proxy handles SSL/TLS, so we use sslmode=disable
	dsn := fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=disable",
		config.CloudSQLHost,
		config.CloudSQLPort,
		config.CloudSQLDatabase,
		config.CloudSQLUser,
		password,
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(config.MaxConnections)
	db.SetMaxIdleConns(config.MaxConnections / 2)
	db.SetConnMaxIdleTime(time.Duration(config.MaxIdleTime) * time.Second)

	// Initialize schema
	if err := initSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return &CloudSQLStorage{
		db:     db,
		config: config,
	}, nil
}

// StoreQuerySample stores a sampled query with performance statistics
func (s *CloudSQLStorage) StoreQuerySample(ctx context.Context, sample *QuerySample) error {
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
			sampled_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29)
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
		sample.SampledAt,
	)

	return err
}

// StoreComparisonResult stores a comparison result
func (s *CloudSQLStorage) StoreComparisonResult(ctx context.Context, result *ComparisonResult) error {
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
		) VALUES ($1, $2, $3, $4, $5)
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
func (s *CloudSQLStorage) Close() error {
	return s.db.Close()
}

// initSchema creates the necessary tables if they don't exist
func initSchema(db *sql.DB) error {
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

			sampled_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS comparison_outcomes (
			correlation_id VARCHAR(36) PRIMARY KEY,
			comparison_status VARCHAR(50) NOT NULL,
			difference_details JSONB,
			performance_metrics JSONB,
			compared_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (correlation_id) REFERENCES sampled_queries(correlation_id)
		)`,

		`CREATE INDEX IF NOT EXISTS idx_sampled_queries_tenant ON sampled_queries(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sampled_queries_time ON sampled_queries(sampled_at)`,
		`CREATE INDEX IF NOT EXISTS idx_comparison_status ON comparison_outcomes(comparison_status)`,
	}

	for _, schema := range schemas {
		if _, err := db.Exec(schema); err != nil {
			return err
		}
	}

	return nil
}
