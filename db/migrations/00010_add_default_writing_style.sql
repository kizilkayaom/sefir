-- +goose Up
ALTER TABLE accounts
ADD COLUMN default_preset_id TEXT NOT NULL DEFAULT 'prof'
CHECK (default_preset_id IN ('prof', 'warm_prof', 'technical', 'formal'));

-- +goose Down
ALTER TABLE accounts
DROP COLUMN default_preset_id;
