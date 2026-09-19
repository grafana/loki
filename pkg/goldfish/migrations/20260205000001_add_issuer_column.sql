-- +goose Up
-- +goose StatementBegin
ALTER TABLE sampled_queries ADD COLUMN issuer VARCHAR(50) DEFAULT 'unknown';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sampled_queries DROP COLUMN issuer;
-- +goose StatementEnd
