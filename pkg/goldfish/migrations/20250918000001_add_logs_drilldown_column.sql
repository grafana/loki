-- +goose Up
-- Add is_logs_drilldown column to track queries from Logs Drilldown app

ALTER TABLE sampled_queries 
ADD COLUMN is_logs_drilldown BOOLEAN DEFAULT FALSE AFTER user;

-- Update existing rows to have FALSE instead of NULL
UPDATE sampled_queries SET is_logs_drilldown = FALSE WHERE is_logs_drilldown IS NULL;

-- Add index for efficient filtering by logs drilldown status
CREATE INDEX idx_sampled_queries_logs_drilldown ON sampled_queries(is_logs_drilldown);

-- +goose Down
-- Remove is_logs_drilldown column and index

DROP INDEX idx_sampled_queries_logs_drilldown ON sampled_queries;
ALTER TABLE sampled_queries DROP COLUMN is_logs_drilldown;