-- +goose Up
CREATE EXTENSION vector;

-- +goose Down
DROP EXTENSION vector;