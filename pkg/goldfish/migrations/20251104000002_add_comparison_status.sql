-- +goose Up
-- Add comparison_status column to sampled_queries table for query-time filtering

ALTER TABLE sampled_queries
ADD COLUMN comparison_status ENUM('match', 'mismatch', 'error', 'partial') NOT NULL DEFAULT 'mismatch';

-- Backfill existing records with computed comparison status
UPDATE sampled_queries
SET comparison_status = CASE
    WHEN cell_a_status_code < 200 OR cell_a_status_code >= 300
         OR cell_b_status_code < 200 OR cell_b_status_code >= 300 THEN 'error'
    WHEN cell_a_response_hash = cell_b_response_hash THEN 'match'
    ELSE 'mismatch'
END;

-- Add index for efficient filtering by comparison status
CREATE INDEX idx_comparison_status ON sampled_queries(comparison_status);

-- +goose Down
-- Remove comparison_status column and index

DROP INDEX idx_comparison_status ON sampled_queries;
ALTER TABLE sampled_queries DROP COLUMN comparison_status;
