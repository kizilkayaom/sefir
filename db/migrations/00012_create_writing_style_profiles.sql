-- +goose Up
CREATE TABLE writing_style_profiles (
    profile_id UUID PRIMARY KEY NOT NULL DEFAULT gen_random_uuid(),
    account_id UUID  NOT NULL REFERENCES accounts(account_id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 80),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    language TEXT NOT NULL DEFAULT 'en'
        CHECK (char_length(language) BETWEEN 1 AND 35),
    content TEXT NOT NULL
        CHECK (content ~ '[^[:space:]]' AND octet_length(content) <= 16384),
    format_version INTEGER NOT NULL DEFAULT 1 CHECK (format_version = 1),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX writing_style_profiles_account
ON writing_style_profiles (account_id);

-- +goose Down
DROP TABLE writing_style_profiles;
