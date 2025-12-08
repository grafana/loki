-- +goose Up
-- Add race winner tracking columns to sampled_queries table
-- These columns track which backend (cell) returned first when racing is enabled

ALTER TABLE sampled_queries
ADD COLUMN cell_a_won BOOLEAN NOT NULL DEFAULT FALSE,
ADD COLUMN cell_b_won BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
-- Remove race winner tracking columns

ALTER TABLE sampled_queries 
DROP COLUMN cell_a_won,
DROP COLUMN cell_b_won;

