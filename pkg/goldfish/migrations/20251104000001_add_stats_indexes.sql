-- +goose Up
-- Add indexes optimized for statistics queries

-- Compound index for time-based filtering on statistics queries
CREATE INDEX idx_sampled_queries_stats_time_engine ON sampled_queries(
    sampled_at DESC,
    cell_b_used_new_engine
);

-- Index for end_time filtering (usesRecentData filter) on statistics queries
CREATE INDEX idx_sampled_queries_stats_end_time ON sampled_queries(
    end_time,
    sampled_at DESC
);

-- +goose Down

-- Drop the statistics indexes
DROP INDEX idx_sampled_queries_stats_end_time ON sampled_queries;
DROP INDEX idx_sampled_queries_stats_time_engine ON sampled_queries;
