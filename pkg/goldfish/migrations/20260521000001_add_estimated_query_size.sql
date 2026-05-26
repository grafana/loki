-- +goose Up
-- Add cell_a_estimated_query_size and cell_b_estimated_query_size columns to sampled_queries table.
-- This is the estimated size of the query as returned by query stats. Defaults to 0 if unknown.

ALTER TABLE sampled_queries
ADD COLUMN cell_a_estimated_query_size BIGINT NOT NULL DEFAULT 0
ADD COLUMN cell_b_estimated_query_size BIGINT NOT NULL DEFAULT 0;

-- +goose Down
-- Remove the cell_a_estimated_query_size and cell_b_estimated_query_size columns

ALTER TABLE sampled_queries DROP COLUMN cell_a_estimated_query_size;
ALTER TABLE sampled_queries DROP COLUMN cell_b_estimated_query_size;
