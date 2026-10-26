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
	messageIDDomain string,
) (OutboxEntry, bool, error) {
	outboxID = strings.TrimSpace(outboxID)
	messageIDDomain = strings.TrimSpace(messageIDDomain)
	if outboxID == "" {
		return OutboxEntry{}, false, errors.New("outbox_id is required")
	}
	if err := validateMessageIDDomain(messageIDDomain); err != nil {
		return OutboxEntry{}, false, err
	}

	const claimQuery = `
		UPDATE outbox
		SET state = 'sending',
			attempts = attempts + 1,
			claimed_at = now(),
			updated_at = now(),
			rfc822_message_id = COALESCE(
				rfc822_message_id,
				'<sefir.' || outbox_id::text || '@' || $2 || '>'
			)
		WHERE outbox_id = $1 AND state = 'pending'
		RETURNING outbox_id::text, approval_id::text, state, attempts,
			rfc822_message_id, gmail_message_id, last_error,
			claimed_at, created_at, updated_at
	`
	entry, err := scanOutboxEntry(pool.QueryRow(ctx, claimQuery, outboxID, messageIDDomain))
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

func MarkOutboxSent(
	ctx context.Context,
	pool *pgxpool.Pool,
	outboxID string,
	gmailMessageID string,
) (OutboxEntry, error) {
	outboxID = strings.TrimSpace(outboxID)
	gmailMessageID = strings.TrimSpace(gmailMessageID)

	if outboxID == "" || gmailMessageID == "" {
		return OutboxEntry{}, errors.New("outbox_id and gmail_message_id are required")
	}

	const updateQuery = `
		UPDATE outbox
		SET state = 'sent',
			gmail_message_id = $2,
			last_error = NULL,
			updated_at = now()
		WHERE outbox_id = $1 AND state = 'sending'
			AND (gmail_message_id IS NULL OR gmail_message_id = $2)
		RETURNING outbox_id::text, approval_id::text, state, attempts,
			rfc822_message_id, gmail_message_id, last_error,
			claimed_at, created_at, updated_at
	`
	entry, err := scanOutboxEntry(pool.QueryRow(ctx, updateQuery, outboxID, gmailMessageID))
	if err == nil {
		return entry, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return OutboxEntry{}, err
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
		return OutboxEntry{}, errors.New("outbox entry not found")
	}
	if err != nil {
		return OutboxEntry{}, err
	}
	if entry.GmailMessageID != nil && *entry.GmailMessageID != gmailMessageID {
		return OutboxEntry{}, errors.New("outbox entry already has a different Gmail message ID")
	}
	if entry.State == "sent" && entry.GmailMessageID != nil && *entry.GmailMessageID == gmailMessageID {
		return entry, nil
	}

	return OutboxEntry{}, errors.New("outbox entry cannot be marked sent from its current state")
}

func MarkOutboxProblem(
	ctx context.Context,
	pool *pgxpool.Pool,
	outboxID string,
	state string,
	errorMessage string,
) (OutboxEntry, error) {
	outboxID = strings.TrimSpace(outboxID)
	state = strings.TrimSpace(state)
	errorMessage = strings.TrimSpace(errorMessage)

	if outboxID == "" || errorMessage == "" {
		return OutboxEntry{}, errors.New("outbox_id and error_message are required")
	}
	if state != "failed" && state != "needs_attention" {
		return OutboxEntry{}, errors.New("state must be failed or needs_attention")
	}

	const updateQuery = `
		UPDATE outbox
		SET state = $2,
			last_error = $3,
			updated_at = now()
		WHERE outbox_id = $1 AND state = 'sending'
			AND gmail_message_id IS NULL
		RETURNING outbox_id::text, approval_id::text, state, attempts,
			rfc822_message_id, gmail_message_id, last_error,
			claimed_at, created_at, updated_at
	`
	entry, err := scanOutboxEntry(pool.QueryRow(ctx, updateQuery, outboxID, state, errorMessage))
	if err == nil {
		return entry, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return OutboxEntry{}, err
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
		return OutboxEntry{}, errors.New("outbox entry not found")
	}
	if err != nil {
		return OutboxEntry{}, err
	}
	if entry.GmailMessageID != nil {
		return OutboxEntry{}, errors.New("outbox entry already has a Gmail message ID")
	}
	if entry.State == state && entry.LastError != nil && *entry.LastError == errorMessage {
		return entry, nil
	}

	return OutboxEntry{}, errors.New("outbox entry cannot be marked with this problem from its current state")
}

func ListStaleOutboxEntries(
	ctx context.Context,
	pool *pgxpool.Pool,
	claimedBefore time.Time,
	limit int,
) ([]OutboxEntry, error) {
	if pool == nil {
		return nil, errors.New("database pool is required")
	}
	if claimedBefore.IsZero() {
		return nil, errors.New("claimed_before is required")
	}
	if limit < 1 || limit > 100 {
		return nil, errors.New("limit must be between 1 and 100")
	}

	const query = `
		SELECT outbox_id::text, approval_id::text, state, attempts,
			rfc822_message_id, gmail_message_id, last_error,
			claimed_at, created_at, updated_at
		FROM outbox
		WHERE state = 'sending'
			AND claimed_at < $1
			AND gmail_message_id IS NULL
		ORDER BY claimed_at ASC, outbox_id ASC
		LIMIT $2
	`
	rows, err := pool.Query(ctx, query, claimedBefore, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]OutboxEntry, 0, limit)
	for rows.Next() {
		entry, err := scanOutboxEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}
