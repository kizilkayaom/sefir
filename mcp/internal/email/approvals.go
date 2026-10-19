package email

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ApproveDraftInput struct {
	ApprovalID  string `json:"approval_id"`
	DraftID     string `json:"draft_id"`
	Version     int    `json:"version"`
	ContentHash string `json:"content_hash"`
	ApprovedBy  string `json:"approved_by"`
}

type ApprovalResult struct {
	ApprovalID  string    `json:"approval_id"`
	DraftID     string    `json:"draft_id"`
	Version     int       `json:"version"`
	ContentHash string    `json:"content_hash"`
	ApprovedBy  string    `json:"approved_by"`
	ApprovedAt  time.Time `json:"approved_at"`
	OutboxID    string    `json:"outbox_id"`
	OutboxState string    `json:"outbox_state"`
}

func ApproveDraft(
	ctx context.Context,
	pool *pgxpool.Pool,
	input ApproveDraftInput,
) (ApprovalResult, error) {
	input.ApprovalID = strings.TrimSpace(input.ApprovalID)
	input.DraftID = strings.TrimSpace(input.DraftID)
	input.ContentHash = strings.TrimSpace(input.ContentHash)
	input.ApprovedBy = strings.TrimSpace(input.ApprovedBy)

	if input.ApprovalID == "" || input.DraftID == "" ||
		input.ContentHash == "" || input.ApprovedBy == "" {
		return ApprovalResult{}, errors.New("approval_id, draft_id, content_hash, and approved_by are required")
	}

	if input.Version < 1 {
		return ApprovalResult{}, errors.New("version must be at least 1")
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return ApprovalResult{}, err
	}
	defer tx.Rollback(ctx)

	existing, err := getExistingApproval(ctx, tx, input)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ApprovalResult{}, err
	}

	const draftQuery = `
		SELECT content_hash, recipient_email
		FROM drafts
		WHERE draft_id = $1 AND version = $2
		FOR SHARE
	`
	var storedHash string
	var recipientEmail *string
	err = tx.QueryRow(
		ctx, draftQuery, input.DraftID, input.Version,
	).Scan(&storedHash, &recipientEmail)

	if errors.Is(err, pgx.ErrNoRows) {
		return ApprovalResult{}, errors.New("draft version not found")
	}
	if err != nil {
		return ApprovalResult{}, err
	}

	if storedHash != input.ContentHash {
		return ApprovalResult{}, errors.New("content_hash does not match the stored draft")
	}

	if recipientEmail == nil || strings.TrimSpace(*recipientEmail) == "" {
		return ApprovalResult{}, errors.New("draft recipient email is required")
	}

	const insertApprovalQuery = `
		INSERT INTO approvals (
			approval_id, draft_id, version, content_hash, approved_by
		)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (approval_id) DO NOTHING
		RETURNING approval_id::text, draft_id::text, version,
			content_hash, approved_by, approved_at
	`
	var result ApprovalResult
	err = tx.QueryRow(
		ctx, insertApprovalQuery, input.ApprovalID, input.DraftID, input.Version,
		storedHash, input.ApprovedBy,
	).Scan(
		&result.ApprovalID,
		&result.DraftID,
		&result.Version,
		&result.ContentHash,
		&result.ApprovedBy,
		&result.ApprovedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return getExistingApproval(ctx, tx, input)
	}
	if err != nil {
		return ApprovalResult{}, err
	}

	const insertOutboxQuery = `
		INSERT INTO outbox (approval_id)
		VALUES ($1)
		RETURNING outbox_id::text, state
	`
	err = tx.QueryRow(ctx, insertOutboxQuery, result.ApprovalID).Scan(
		&result.OutboxID,
		&result.OutboxState,
	)
	if err != nil {
		return ApprovalResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return ApprovalResult{}, err
	}
	return result, nil
}

func getExistingApproval(
	ctx context.Context,
	tx pgx.Tx,
	input ApproveDraftInput,
) (ApprovalResult, error) {
	const query = `
		SELECT a.approval_id::text, a.draft_id::text, a.version,
			a.content_hash, a.approved_by, a.approved_at,
			o.outbox_id::text, o.state,
			(a.draft_id = $2::uuid AND a.version = $3
				AND a.content_hash = $4
				AND a.approved_by = $5) AS matches_request
		FROM approvals AS a
		LEFT JOIN outbox AS o ON o.approval_id = a.approval_id
		WHERE a.approval_id = $1
	`

	var result ApprovalResult
	var outboxID, outboxState *string
	var matchesRequest bool
	err := tx.QueryRow(
		ctx, query, input.ApprovalID, input.DraftID, input.Version,
		input.ContentHash, input.ApprovedBy,
	).Scan(
		&result.ApprovalID,
		&result.DraftID,
		&result.Version,
		&result.ContentHash,
		&result.ApprovedBy,
		&result.ApprovedAt,
		&outboxID,
		&outboxState,
		&matchesRequest,
	)
	if err != nil {
		return ApprovalResult{}, err
	}
	if !matchesRequest {
		return ApprovalResult{}, errors.New("approval_id is already used for a different request")
	}
	if outboxID == nil || outboxState == nil {
		return ApprovalResult{}, errors.New("approval is missing its outbox entry")
	}
	result.OutboxID = *outboxID
	result.OutboxState = *outboxState
	return result, nil
}
