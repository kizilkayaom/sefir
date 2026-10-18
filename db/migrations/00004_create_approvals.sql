-- +goose Up
CREATE TABLE approvals (
    approval_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    draft_id UUID NOT NULL,
    version INTEGER NOT NULL,
    content_hash TEXT NOT NULL,
    approved_by TEXT NOT NULL,
    approved_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (draft_id, version)
        REFERENCES drafts (draft_id, version)
);
-- +goose Down
DROP TABLE approvals;
