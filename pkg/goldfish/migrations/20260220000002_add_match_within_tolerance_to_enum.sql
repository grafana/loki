-- +goose Up
-- Add 'match_within_tolerance' to comparison_status ENUM on sampled_queries.
-- The application sets this when responses differ by hash but match within
-- the configured value tolerance (e.g. floating-point drift).

ALTER TABLE sampled_queries
MODIFY COLUMN comparison_status ENUM('match', 'mismatch', 'error', 'partial', 'match_within_tolerance') NOT NULL DEFAULT 'mismatch';

-- +goose Down
-- Revert to original ENUM. Map any match_within_tolerance rows to 'match' so the column change succeeds.

UPDATE sampled_queries SET comparison_status = 'match' WHERE comparison_status = 'match_within_tolerance';

ALTER TABLE sampled_queries
MODIFY COLUMN comparison_status ENUM('match', 'mismatch', 'error', 'partial') NOT NULL DEFAULT 'mismatch';
