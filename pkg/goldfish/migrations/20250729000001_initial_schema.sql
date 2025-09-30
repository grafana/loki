-- +goose Up
-- Initial schema for goldfish querytee storage

CREATE TABLE IF NOT EXISTS sampled_queries (
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
);

CREATE TABLE IF NOT EXISTS comparison_outcomes (
	correlation_id VARCHAR(36) PRIMARY KEY,
	comparison_status VARCHAR(50) NOT NULL,
	difference_details JSON,
	performance_metrics JSON,
	compared_at TIMESTAMP NOT NULL,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (correlation_id) REFERENCES sampled_queries(correlation_id)
);

CREATE INDEX idx_sampled_queries_tenant ON sampled_queries(tenant_id);
CREATE INDEX idx_sampled_queries_time ON sampled_queries(sampled_at);
CREATE INDEX idx_comparison_status ON comparison_outcomes(comparison_status);

-- +goose Down
-- Drop tables and indexes in reverse order

DROP INDEX idx_comparison_status ON comparison_outcomes;
DROP INDEX idx_sampled_queries_time ON sampled_queries;
DROP INDEX idx_sampled_queries_tenant ON sampled_queries;

DROP TABLE IF EXISTS comparison_outcomes;
DROP TABLE IF EXISTS sampled_queries;
