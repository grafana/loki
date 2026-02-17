-- +goose Up
-- Add match_within_tolerance column to sampled_queries and comparison_outcomes tables

ALTER TABLE sampled_queries
ADD COLUMN match_within_tolerance BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE comparison_outcomes
ADD COLUMN match_within_tolerance BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
-- Remove match_within_tolerance column

ALTER TABLE comparison_outcomes DROP COLUMN match_within_tolerance;
ALTER TABLE sampled_queries DROP COLUMN match_within_tolerance;
