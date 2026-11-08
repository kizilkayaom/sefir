package webapp

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (a PostgresAccounts) PendingDeletionPage(ctx context.Context, afterID string, limit int) ([]string, error) {
	if limit < 1 || limit > 100 || (afterID != "" && !uuidPattern.MatchString(afterID)) {
		return nil, errors.New("valid cursor and limit between 1 and 100 are required")
	}
	rows, err := a.Pool.Query(ctx, `SELECT account_id::text FROM accounts
		WHERE deletion_requested_at IS NOT NULL AND NOT legacy_reserved
		AND ($1::uuid IS NULL OR account_id > $1::uuid)
		ORDER BY account_id LIMIT $2`, nilIfEmpty(afterID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func nilIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (a PostgresAccounts) SettleDeletingOutbox(ctx context.Context, accountID string) (bool, error) {
	if !uuidPattern.MatchString(accountID) {
		return false, errors.New("valid account ID is required")
	}
	tx, err := a.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var ready bool
	err = tx.QueryRow(ctx, `SELECT COALESCE(deletion_requested_at <= now() - interval '2 minutes', false)
		FROM accounts WHERE account_id=$1 AND NOT legacy_reserved FOR UPDATE`, accountID).Scan(&ready)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil || !ready {
		return false, err
	}
	_, err = tx.Exec(ctx, `UPDATE outbox AS o
		SET state='needs_attention', updated_at=now(),
		    last_error='Account deletion: delivery outcome unknown; do not resend'
		FROM approvals AS ap, drafts AS d, contacts AS c
		WHERE o.approval_id=ap.approval_id AND ap.draft_id=d.draft_id AND ap.version=d.version
		AND d.contact_id=c.id AND c.account_id=$1 AND o.state='sending'
		AND COALESCE(o.claimed_at, o.updated_at) <= now() - interval '2 minutes'`, accountID)
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
