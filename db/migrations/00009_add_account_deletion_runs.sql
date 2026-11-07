-- +goose Up
CREATE TABLE account_deletion_runs (
    account_id UUID NOT NULL REFERENCES accounts(account_id) ON DELETE CASCADE,
    workflow_id TEXT NOT NULL CHECK (workflow_id <> ''),
    run_id TEXT NOT NULL CHECK (run_id <> ''),
    PRIMARY KEY (account_id, workflow_id, run_id)
);

-- +goose Down
DROP TABLE account_deletion_runs;
