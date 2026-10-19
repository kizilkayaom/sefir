package email

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kizilkayaom/sefir/mcp/internal/draftcontent"
)

type ApprovedDraft struct {
	DraftID        string
	Version        int
	RecipientEmail *string
	Subject        string
	Body           string
	ContentHash    string
}

func GetApprovedDraft(
	ctx context.Context,
	pool *pgxpool.Pool,
	outboxID string,
) (ApprovedDraft, error) {
	outboxID = strings.TrimSpace(outboxID)
	if outboxID == "" {
		return ApprovedDraft{}, errors.New("outbox_id is required")
	}

	const query = `
		SELECT d.draft_id::text, d.version, d.recipient_email,
			d.subject, d.body, a.content_hash, d.content_hash
		FROM outbox AS o
		JOIN approvals AS a ON a.approval_id = o.approval_id
		JOIN drafts AS d ON d.draft_id = a.draft_id AND d.version = a.version
		WHERE o.outbox_id = $1
	`

	var draft ApprovedDraft
	var storedHash string
	err := pool.QueryRow(ctx, query, outboxID).Scan(
		&draft.DraftID,
		&draft.Version,
		&draft.RecipientEmail,
		&draft.Subject,
		&draft.Body,
		&draft.ContentHash,
		&storedHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ApprovedDraft{}, errors.New("approved draft not found")
	}
	if err != nil {
		return ApprovedDraft{}, err
	}

	if draft.RecipientEmail == nil || strings.TrimSpace(*draft.RecipientEmail) == "" {
		return ApprovedDraft{}, errors.New("draft recipient email is required")
	}

	contentHash, err := draftcontent.Hash(draft.RecipientEmail, draft.Subject, draft.Body)
	if err != nil {
		return ApprovedDraft{}, err
	}
	if contentHash != draft.ContentHash || contentHash != storedHash {
		return ApprovedDraft{}, errors.New("draft content does not match the stored draft and approval hashes")
	}

	return draft, nil
}
