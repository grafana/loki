-- +goose Up
-- Add composite indexes aligned with common Goldfish filters and sort

-- Optimize queries filtering by tenant and ordering by sampled_at DESC
CREATE INDEX idx_sampled_queries_tenant_sampled_at ON sampled_queries(tenant_id, sampled_at DESC, correlation_id);

-- Optimize queries filtering by user and ordering by sampled_at DESC
CREATE INDEX idx_sampled_queries_user_sampled_at ON sampled_queries(user, sampled_at DESC, correlation_id);

-- +goose Down
-- Remove composite indexes

DROP INDEX idx_sampled_queries_user_sampled_at ON sampled_queries;
DROP INDEX idx_sampled_queries_tenant_sampled_at ON sampled_queries;

