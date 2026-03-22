-- +goose Up
-- Add performance indexes for goldfish query optimization

-- Composite index for the main query pattern (ORDER BY sampled_at DESC with joins)
CREATE INDEX idx_sampled_queries_sampled_at_desc ON sampled_queries(sampled_at DESC, correlation_id);

-- Index for status computation (covers the CASE expression fields)
CREATE INDEX idx_sampled_queries_status_computation ON sampled_queries(
    cell_a_status_code, 
    cell_b_status_code, 
    cell_a_response_hash, 
    cell_b_response_hash
);

-- Covering index for frequently accessed columns (reduces table lookups)
-- Note: TEXT columns (query) cannot be included in covering indexes
CREATE INDEX idx_sampled_queries_covering ON sampled_queries(
    correlation_id,
    sampled_at DESC,
    tenant_id,
    query_type,
    start_time,
    end_time,
    cell_a_status_code,
    cell_b_status_code,
    cell_a_response_hash,
    cell_b_response_hash
);

-- Index for comparison_outcomes join performance
CREATE INDEX idx_comparison_outcomes_correlation_status ON comparison_outcomes(
    correlation_id, 
    comparison_status
);

-- +goose Down
-- Remove performance indexes

DROP INDEX idx_comparison_outcomes_correlation_status ON comparison_outcomes;
DROP INDEX idx_sampled_queries_covering ON sampled_queries;
DROP INDEX idx_sampled_queries_status_computation ON sampled_queries;
DROP INDEX idx_sampled_queries_sampled_at_desc ON sampled_queries;