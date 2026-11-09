package catalogue

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kizilkayaom/sefir/mcp/internal/draftcontent"
)

var ErrDraftConflict = errors.New("a newer draft version exists or the draft has changed")

type EditDraftInput struct {
	RecipientEmail *string `json:"recipient_email,omitempty"`
	Subject        string  `json:"subject"`
	Body           string  `json:"body"`
	BodyHTML       string  `json:"body_html"`
	ContentHash    string  `json:"content_hash"`
}

func SaveDraftVersion(ctx context.Context, pool *pgxpool.Pool, id string, version int, input EditDraftInput) (Draft, error) {
	subject, body, bodyHTML, err := draftcontent.Prepare(input.Subject, input.Body, input.BodyHTML)
	if err != nil {
		return Draft{}, err
	}
	if input.RecipientEmail != nil {
		recipient, err := draftcontent.NormalizeRecipient(*input.RecipientEmail)
		if err != nil {
			return Draft{}, err
		}
		input.RecipientEmail = &recipient
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Draft{}, err
	}
	defer tx.Rollback(ctx)
	var contactID, storedHash string
	var recipient *string
	var presetID *string
	var presetRevision *int
	err = tx.QueryRow(ctx, `
		SELECT contact_id::text, recipient_email, content_hash, preset_id, preset_revision FROM drafts
		WHERE draft_id = $1 AND version = $2 FOR UPDATE`, id, version).Scan(&contactID, &recipient, &storedHash, &presetID, &presetRevision)
	if err != nil {
		return Draft{}, err
	}
	var latest int
	if err := tx.QueryRow(ctx, "SELECT MAX(version) FROM drafts WHERE draft_id = $1", id).Scan(&latest); err != nil {
		return Draft{}, err
	}
	if storedHash != input.ContentHash || latest != version || version >= 2147483647 {
		return Draft{}, ErrDraftConflict
	}
	if input.RecipientEmail != nil {
		recipient = input.RecipientEmail
	}
	hash, err := draftcontent.HashWithHTML(recipient, subject, body, bodyHTML)
	if err != nil {
		return Draft{}, err
	}
	rows, err := tx.Query(ctx, `
		INSERT INTO drafts (draft_id, version, contact_id, recipient_email, subject, body, body_html, content_hash, preset_id, preset_revision)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING draft_id::text, version, contact_id::text, recipient_email,
			subject, body, body_html, content_hash, created_at`,
		id, version+1, contactID, recipient, subject, body, bodyHTML, hash, presetID, presetRevision)
	if err != nil {
		return Draft{}, err
	}
	draft, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[Draft])
	if err != nil {
		var constraint *pgconn.PgError
		if errors.As(err, &constraint) && constraint.Code == "23505" {
			return Draft{}, ErrDraftConflict
		}
		return Draft{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Draft{}, err
	}
	return draft, nil
}
