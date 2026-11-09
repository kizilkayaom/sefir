package catalogue

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kizilkayaom/sefir/mcp/internal/draftcontent"
)

type Draft struct {
	DraftID        string    `json:"draft_id"`
	Version        int       `json:"version"`
	ContactID      string    `json:"contact_id"`
	RecipientEmail *string   `json:"recipient_email"`
	Subject        string    `json:"subject"`
	Body           string    `json:"body"`
	BodyHTML       string    `json:"body_html,omitempty"`
	ContentHash    string    `json:"content_hash"`
	CreatedAt      time.Time `json:"created_at"`
}

type CreateDraftInput struct {
	ContactID      string  `json:"contact_id"`
	Subject        string  `json:"subject"`
	Body           string  `json:"body"`
	BodyHTML       string  `json:"body_html,omitempty"`
	PresetID       *string `json:"preset_id,omitempty"`
	PresetRevision *int    `json:"preset_revision,omitempty"`
}

func CreateDraft(
	ctx context.Context,
	pool *pgxpool.Pool,
	input CreateDraftInput,
) (Draft, error) {
	input.ContactID = strings.TrimSpace(input.ContactID)
	var err error
	input.Subject, input.Body, input.BodyHTML, err = draftcontent.Prepare(input.Subject, input.Body, input.BodyHTML)
	if err != nil {
		return Draft{}, err
	}
	if input.ContactID == "" {
		return Draft{}, errors.New("contact_id is required")
	}
	if (input.PresetID == nil) != (input.PresetRevision == nil) {
		return Draft{}, errors.New("preset_id and preset_revision must be provided together")
	}
	if input.PresetID != nil {
		if strings.TrimSpace(*input.PresetID) == "" {
			return Draft{}, errors.New("preset_id must not be blank")
		}
		if *input.PresetRevision < 1 {
			return Draft{}, errors.New("preset_revision must be at least 1")
		}
	}

	contact, err := GetConfirmedContact(ctx, pool, input.ContactID)
	if err != nil {
		return Draft{}, err
	}

	contentHash, err := draftcontent.HashWithHTML(contact.Email, input.Subject, input.Body, input.BodyHTML)
	if err != nil {
		return Draft{}, err
	}

	const query = `
		INSERT INTO drafts (contact_id, recipient_email, subject, body, body_html, content_hash, preset_id, preset_revision)
		SELECT id, email, $3, $4, $5, $6, $7, $8
		FROM contacts
		WHERE id = $1
			AND status = 'confirmed'
			AND email IS NOT DISTINCT FROM $2
		RETURNING draft_id::text, version, contact_id::text, recipient_email,
			subject, body, body_html, content_hash, created_at
	`

	var draft Draft
	err = pool.QueryRow(
		ctx, query, contact.ID, contact.Email, input.Subject, input.Body, input.BodyHTML, contentHash,
		input.PresetID, input.PresetRevision,
	).Scan(
		&draft.DraftID,
		&draft.Version,
		&draft.ContactID,
		&draft.RecipientEmail,
		&draft.Subject,
		&draft.Body,
		&draft.BodyHTML,
		&draft.ContentHash,
		&draft.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Draft{}, errors.New("contact changed before the draft could be saved")
	}
	if err != nil {
		return Draft{}, err
	}

	return draft, nil
}

func GetDraft(
	ctx context.Context,
	pool *pgxpool.Pool,
	draftID string,
	version int,
) (Draft, error) {
	draftID = strings.TrimSpace(draftID)
	if draftID == "" {
		return Draft{}, errors.New("draft_id is required")
	}
	if version < 1 {
		return Draft{}, errors.New("version must be at least 1")
	}
	const query = `
	SELECT draft_id::text, version, contact_id::text, recipient_email,
       subject, body, body_html, content_hash, created_at
	FROM drafts
	WHERE draft_id = $1 AND version = $2
	`
	var draft Draft
	err := pool.QueryRow(ctx, query, draftID, version).Scan(
		&draft.DraftID,
		&draft.Version,
		&draft.ContactID,
		&draft.RecipientEmail,
		&draft.Subject,
		&draft.Body,
		&draft.BodyHTML,
		&draft.ContentHash,
		&draft.CreatedAt,
	)
	if err != nil {
		return Draft{}, err
	}

	return draft, nil
}
