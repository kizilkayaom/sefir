-- +goose Up
CREATE TABLE drafts (
    draft_id UUID DEFAULT gen_random_uuid() NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    contact_id UUID NOT NULL REFERENCES contacts(id),
    recipient_email TEXT,
    subject TEXT NOT NULL,
    body TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now() NOT NULL,
    PRIMARY KEY (draft_id, version),
    CHECK (
    subject ~ '[^[:space:]]'
    AND body ~ '[^[:space:]]'
)
);

-- +goose Down
DROP TABLE drafts;