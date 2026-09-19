-- +goose Up
-- Add indexes

-- Composite index for filtering queries with WHERE clauses (includes user column)
CREATE INDEX idx_sampled_queries_filter_composite ON sampled_queries(
    tenant_id,
    user,
    correlation_id,
    sampled_at DESC
);

CREATE INDEX idx_sampled_queries_filter_user ON sampled_queries(
    user,
    sampled_at DESC
);

CREATE INDEX idx_sampled_queries_filter_tenant ON sampled_queries(
    tenant_id,
    sampled_at DESC
);

CREATE INDEX idx_sampled_queries_filter_user_and_tenant ON sampled_queries(
    user,
    tenant_id,
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

-- +goose Down

-- Drop all the new indexes
DROP INDEX idx_sampled_queries_cell_b_engine_filter ON sampled_queries;
DROP INDEX idx_sampled_queries_cell_a_engine_filter ON sampled_queries;
DROP INDEX idx_sampled_queries_engine_filter_composite ON sampled_queries;
DROP INDEX idx_sampled_queries_filter_user_and_tenant ON sampled_queries;
DROP INDEX idx_sampled_queries_filter_tenant ON sampled_queries;
DROP INDEX idx_sampled_queries_filter_user ON sampled_queries;
DROP INDEX idx_sampled_queries_filter_composite ON sampled_queries;
