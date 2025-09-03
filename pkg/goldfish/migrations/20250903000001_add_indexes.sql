-- +goose Up

-- Add indexes
CREATE INDEX idx_sampled_queries_user ON sampled_queries(user);

-- Composite index for the JOIN operation with comparison_outcomes
CREATE INDEX idx_sampled_queries_correlation_composite ON sampled_queries(
    correlation_id,
    cell_a_status_code,
    cell_b_status_code,
    cell_a_response_hash,
    cell_b_response_hash
);

-- Composite index for filtering queries with WHERE clauses
CREATE INDEX idx_sampled_queries_filter_composite ON sampled_queries(
    tenant_id,
    user,
    correlation_id,
    sampled_at DESC
);

-- Index for new engine filtering
CREATE INDEX idx_sampled_queries_engine_filter_composite ON sampled_queries(
    cell_a_used_new_engine,
    cell_b_used_new_engine,
    sampled_at DESC
);
CREATE INDEX idx_sampled_queries_cell_a_engine_filter ON sampled_queries(
    cell_a_used_new_engine
);
CREATE INDEX idx_sampled_queries_cell_b_engine_filter ON sampled_queries(
    cell_b_used_new_engine
);

-- Index for comparison_outcomes join performance
CREATE INDEX idx_comparison_outcomes_correlation_status ON comparison_outcomes(
    correlation_id, 
    comparison_status
);

-- +goose Down

-- Drop all the new indexes
DROP INDEX idx_comparison_outcomes_correlation_status ON comparison_outcomes;
DROP INDEX idx_sampled_queries_cell_b_engine_filter ON sampled_queries;
DROP INDEX idx_sampled_queries_cell_a_engine_filter ON sampled_queries;
DROP INDEX idx_sampled_queries_engine_filter_composite ON sampled_queries;
DROP INDEX idx_sampled_queries_filter_composite ON sampled_queries;
DROP INDEX idx_sampled_queries_correlation_composite ON sampled_queries;
DROP INDEX idx_sampled_queries_user ON sampled_queries;
