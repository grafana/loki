-- +goose Up
-- Add mismatch_cause column to sampled_queries and comparison_outcomes tables.
-- Stores the cause when comparison_status is 'mismatch'.

ALTER TABLE sampled_queries
ADD COLUMN mismatch_cause VARCHAR(80) DEFAULT NULL;

ALTER TABLE comparison_outcomes
ADD COLUMN mismatch_cause VARCHAR(80) DEFAULT NULL;

-- +goose Down
-- Remove mismatch_cause column

ALTER TABLE comparison_outcomes DROP COLUMN mismatch_cause;
ALTER TABLE sampled_queries DROP COLUMN mismatch_cause;
