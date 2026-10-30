-- +goose Up
ALTER TABLE accounts
ADD COLUMN deletion_requested_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE accounts
DROP COLUMN deletion_requested_at;