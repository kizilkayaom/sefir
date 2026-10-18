-- +goose Up
CREATE TABLE outbox (
    outbox_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    approval_id UUID NOT NULL UNIQUE REFERENCES approvals(approval_id),
    state TEXT NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    rfc822_message_id TEXT UNIQUE,
    gmail_message_id TEXT,
    last_error TEXT,
    claimed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (attempts >= 0),
    CHECK (state IN ('pending', 'sending', 'sent', 'failed', 'needs_attention'))
);

-- +goose Down
DROP TABLE outbox;
