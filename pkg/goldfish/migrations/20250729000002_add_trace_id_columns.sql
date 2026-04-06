-- +goose Up
-- Add trace ID columns to sampled_queries table

ALTER TABLE sampled_queries 
ADD COLUMN cell_a_trace_id VARCHAR(255) DEFAULT NULL,
ADD COLUMN cell_b_trace_id VARCHAR(255) DEFAULT NULL;

-- Add indexes for querying by trace ID
CREATE INDEX idx_sampled_queries_cell_a_trace_id ON sampled_queries(cell_a_trace_id);
CREATE INDEX idx_sampled_queries_cell_b_trace_id ON sampled_queries(cell_b_trace_id);

-- +goose Down
-- Remove trace ID columns and indexes

DROP INDEX idx_sampled_queries_cell_b_trace_id ON sampled_queries;
DROP INDEX idx_sampled_queries_cell_a_trace_id ON sampled_queries;

ALTER TABLE sampled_queries 
DROP COLUMN cell_b_trace_id,
DROP COLUMN cell_a_trace_id;