package email

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OutboxEntry struct {
	OutboxID        string     `json:"outbox_id"`
	ApprovalID      string     `json:"approval_id"`
	State           string     `json:"state"`
	Attempts        int        `json:"attempts"`
	RFC822MessageID *string    `json:"rfc822_message_id"`
	GmailMessageID  *string    `json:"gmail_message_id"`
	LastError       *string    `json:"last_error"`
	ClaimedAt       *time.Time `json:"claimed_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func ClaimOutbox(
	ctx context.Context,
	pool *pgxpool.Pool,
	outboxID string,
) (OutboxEntry, bool, error) {
	outboxID = strings.TrimSpace(outboxID)
	if outboxID == "" {
		return OutboxEntry{}, false, errors.New("outbox_id is required")
	}

	const claimQuery = `
		UPDATE outbox
		SET state = 'sending',
			attempts = attempts + 1,
			claimed_at = now(),
			updated_at = now()
		WHERE outbox_id = $1 AND state = 'pending'
		RETURNING outbox_id::text, approval_id::text, state, attempts,
			rfc822_message_id, gmail_message_id, last_error,
			claimed_at, created_at, updated_at
	`
	entry, err := scanOutboxEntry(pool.QueryRow(ctx, claimQuery, outboxID))
	if err == nil {
		return entry, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return OutboxEntry{}, false, err
	}

	const lookupQuery = `
		SELECT outbox_id::text, approval_id::text, state, attempts,
			rfc822_message_id, gmail_message_id, last_error,
			claimed_at, created_at, updated_at
		FROM outbox
		WHERE outbox_id = $1
	`
	entry, err = scanOutboxEntry(pool.QueryRow(ctx, lookupQuery, outboxID))
	if errors.Is(err, pgx.ErrNoRows) {
		return OutboxEntry{}, false, errors.New("outbox entry not found")
	}
	if err != nil {
		return OutboxEntry{}, false, err
	}
	return entry, false, nil
}

func scanOutboxEntry(row pgx.Row) (OutboxEntry, error) {
	var entry OutboxEntry
	err := row.Scan(
		&entry.OutboxID,
		&entry.ApprovalID,
		&entry.State,
		&entry.Attempts,
		&entry.RFC822MessageID,
		&entry.GmailMessageID,
		&entry.LastError,
		&entry.ClaimedAt,
		&entry.CreatedAt,
		&entry.UpdatedAt,
	)
	if err != nil {
		return OutboxEntry{}, err
	}
	return entry, nil
}
