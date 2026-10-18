package catalogue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Draft struct {
	DraftID        string    `json:"draft_id"`
	Version        int       `json:"version"`
	ContactID      string    `json:"contact_id"`
	RecipientEmail *string   `json:"recipient_email"`
	Subject        string    `json:"subject"`
	Body           string    `json:"body"`
	ContentHash    string    `json:"content_hash"`
	CreatedAt      time.Time `json:"created_at"`
}

type CreateDraftInput struct {
	ContactID string `json:"contact_id"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
}

func hashDraftContent(recipientEmail *string, subject, body string) (string, error) {
	type DraftHash struct {
		RecipientEmail *string `json:"recipient_email"`
		Subject        string  `json:"subject"`
		Body           string  `json:"body"`
	}

	payload := DraftHash{
		RecipientEmail: recipientEmail,
		Subject:        subject,
		Body:           body,
	}
	serial, err := json.Marshal(payload)

	if err != nil {
		return "", err
	}

	digest := sha256.Sum256(serial)
	return hex.EncodeToString(digest[:]), nil
}

func CreateDraft(
	ctx context.Context,
	pool *pgxpool.Pool,
	input CreateDraftInput,
) (Draft, error) {
	input.ContactID = strings.TrimSpace(input.ContactID)
	input.Subject = strings.TrimSpace(input.Subject)
	input.Body = strings.TrimSpace(input.Body)

	if input.ContactID == "" || input.Subject == "" || input.Body == "" {
		return Draft{}, errors.New("contact_id, subject, and body are required")
	}

	contact, err := GetConfirmedContact(ctx, pool, input.ContactID)
	if err != nil {
		return Draft{}, err
	}

	contentHash, err := hashDraftContent(contact.Email, input.Subject, input.Body)
	if err != nil {
		return Draft{}, err
	}

	const query = `
		INSERT INTO drafts (contact_id, recipient_email, subject, body, content_hash)
		SELECT id, email, $3, $4, $5
		FROM contacts
		WHERE id = $1
			AND status = 'confirmed'
			AND email IS NOT DISTINCT FROM $2
		RETURNING draft_id::text, version, contact_id::text, recipient_email,
			subject, body, content_hash, created_at
	`

	var draft Draft
	err = pool.QueryRow(
		ctx, query, contact.ID, contact.Email, input.Subject, input.Body, contentHash,
	).Scan(
		&draft.DraftID,
		&draft.Version,
		&draft.ContactID,
		&draft.RecipientEmail,
		&draft.Subject,
		&draft.Body,
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
       subject, body, content_hash, created_at
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
		&draft.ContentHash,
		&draft.CreatedAt,
	)
	if err != nil {
		return Draft{}, err
	}

	return draft, nil
}
