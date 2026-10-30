-- +goose Up
ALTER TABLE drafts ADD COLUMN body_html TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE drafts DROP COLUMN body_html;
