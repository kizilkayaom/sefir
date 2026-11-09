-- +goose Up
ALTER TABLE drafts
ADD COLUMN preset_id TEXT,
ADD COLUMN preset_revision INTEGER,
ADD CONSTRAINT drafts_writing_style_check CHECK (
    (preset_id IS NULL AND preset_revision IS NULL)
    OR (
        preset_id IS NOT NULL
        AND preset_revision IS NOT NULL
        AND preset_id ~ '[^[:space:]]'
        AND preset_revision > 0
    )
);
-- +goose Down
ALTER TABLE drafts
DROP CONSTRAINT drafts_writing_style_check,
DROP COLUMN preset_id,
DROP COLUMN preset_revision;
