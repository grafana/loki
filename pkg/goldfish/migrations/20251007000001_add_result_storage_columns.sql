-- +goose Up
-- Add columns to persist result storage metadata for Goldfish

ALTER TABLE sampled_queries
  ADD COLUMN cell_a_result_uri TEXT NULL AFTER cell_a_status_code,
  ADD COLUMN cell_b_result_uri TEXT NULL AFTER cell_a_result_uri,
  ADD COLUMN cell_a_result_size_bytes BIGINT NULL AFTER cell_b_result_uri,
  ADD COLUMN cell_b_result_size_bytes BIGINT NULL AFTER cell_a_result_size_bytes,
  ADD COLUMN cell_a_result_compression VARCHAR(32) NULL AFTER cell_b_result_size_bytes,
  ADD COLUMN cell_b_result_compression VARCHAR(32) NULL AFTER cell_a_result_compression;

-- +goose Down
-- Remove result storage metadata columns

ALTER TABLE sampled_queries
  DROP COLUMN cell_b_result_compression,
  DROP COLUMN cell_a_result_compression,
  DROP COLUMN cell_b_result_size_bytes,
  DROP COLUMN cell_a_result_size_bytes,
  DROP COLUMN cell_b_result_uri,
  DROP COLUMN cell_a_result_uri;
