-- +goose Up
CREATE TABLE accounts (
    account_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username TEXT NOT NULL,
    password_salt BYTEA,
    password_hash BYTEA,
    google_subject TEXT UNIQUE,
    google_email TEXT,
    legacy_reserved BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((password_salt IS NULL AND password_hash IS NULL) OR
           (password_salt IS NOT NULL AND password_hash IS NOT NULL AND
            octet_length(password_salt) = 32 AND octet_length(password_hash) = 32)),
    CHECK ((google_subject IS NULL) = (google_email IS NULL))
);
CREATE UNIQUE INDEX accounts_username_unique ON accounts (lower(username));
INSERT INTO accounts (account_id, username, legacy_reserved)
VALUES ('00000000-0000-4000-8000-000000000001', '__legacy_owner__', true);
ALTER TABLE contacts ADD COLUMN account_id UUID NOT NULL
    DEFAULT '00000000-0000-4000-8000-000000000001' REFERENCES accounts(account_id);
CREATE INDEX contacts_account_created ON contacts (account_id, created_at DESC, id DESC);

-- +goose Down
ALTER TABLE contacts DROP COLUMN account_id;
DROP TABLE accounts;
