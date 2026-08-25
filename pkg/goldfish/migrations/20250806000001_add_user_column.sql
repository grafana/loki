-- +goose Up
-- Add user column to track who executed the query

ALTER TABLE sampled_queries 
ADD COLUMN user VARCHAR(255) DEFAULT '' AFTER tenant_id;

-- Update existing rows to have empty string instead of NULL
UPDATE sampled_queries SET user = '' WHERE user IS NULL;

-- Add index for efficient filtering by user
CREATE INDEX idx_sampled_queries_user ON sampled_queries(user);

-- +goose Down
-- Remove user column and index

DROP INDEX idx_sampled_queries_user ON sampled_queries;
ALTER TABLE sampled_queries DROP COLUMN user;