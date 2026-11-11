package webapp

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kizilkayaom/sefir/mcp/internal/catalogue"
	"github.com/kizilkayaom/sefir/mcp/internal/writingstyle"
)

type Contact struct {
	ID           string    `json:"id"`
	Name         *string   `json:"name"`
	Email        *string   `json:"email"`
	Organization *string   `json:"organization"`
	Role         *string   `json:"role"`
	Context      *string   `json:"context"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

type ContactInput struct {
	Name         string `json:"name"`
	Email        string `json:"email"`
	Organization string `json:"organization"`
	Role         string `json:"role"`
	Context      string `json:"context"`
	Status       string `json:"status"`
}

type Activity struct {
	OutboxID       string    `json:"outbox_id"`
	DraftID        string    `json:"draft_id"`
	Version        int       `json:"version"`
	Subject        string    `json:"subject"`
	RecipientEmail *string   `json:"recipient_email"`
	ApprovedBy     string    `json:"approved_by"`
	State          string    `json:"state"`
	Attempts       int       `json:"attempts"`
	GmailMessageID *string   `json:"gmail_message_id"`
	LastError      *string   `json:"last_error"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Store interface {
	Drafts(context.Context, int, int) ([]catalogue.Draft, error)
	Draft(context.Context, string, int) (catalogue.Draft, error)
	Contacts(context.Context, int, int) ([]Contact, error)
	SaveContact(context.Context, string, ContactInput) (Contact, error)
	ConfirmedContact(context.Context, string) (catalogue.Contact, error)
	CreateDraft(context.Context, catalogue.CreateDraftInput) (catalogue.Draft, error)
	SaveDraftVersion(context.Context, string, int, catalogue.EditDraftInput) (catalogue.Draft, error)
	Activities(context.Context, int, int) ([]Activity, error)
	DraftActivity(context.Context, string, int) (*Activity, error)
	CreateWritingStyle(context.Context, writingstyle.Profile) (WritingStyleProfile, error)
	WritingStyles(context.Context, int, int) ([]WritingStyleProfile, error)
}

type PostgresStore struct {
	Pool *pgxpool.Pool
}

func (s PostgresStore) Drafts(ctx context.Context, limit, offset int) ([]catalogue.Draft, error) {
	accountID, err := contextAccountID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT d.draft_id::text, d.version, d.contact_id::text, d.recipient_email,
		d.subject, d.body, d.body_html, d.content_hash, d.created_at, d.preset_id, d.preset_revision
		FROM drafts d JOIN contacts c ON c.id=d.contact_id WHERE c.account_id=$3
		ORDER BY d.created_at DESC, d.draft_id DESC, d.version DESC LIMIT $1 OFFSET $2`, limit, offset, accountID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[catalogue.Draft])
}

func (s PostgresStore) Draft(ctx context.Context, id string, version int) (catalogue.Draft, error) {
	accountID, err := contextAccountID(ctx)
	if err != nil {
		return catalogue.Draft{}, err
	}
	var exists bool
	err = s.Pool.QueryRow(ctx, `SELECT true FROM drafts d JOIN contacts c ON c.id=d.contact_id
		WHERE d.draft_id=$1 AND d.version=$2 AND c.account_id=$3`, id, version, accountID).Scan(&exists)
	if err != nil {
		return catalogue.Draft{}, err
	}
	return catalogue.GetDraft(ctx, s.Pool, id, version)
}

func (s PostgresStore) ConfirmedContact(ctx context.Context, id string) (catalogue.Contact, error) {
	if err := s.ownsContact(ctx, id); err != nil {
		return catalogue.Contact{}, err
	}
	return catalogue.GetConfirmedContact(ctx, s.Pool, id)
}

func (s PostgresStore) CreateDraft(ctx context.Context, input catalogue.CreateDraftInput) (catalogue.Draft, error) {
	if err := s.ownsContact(ctx, input.ContactID); err != nil {
		return catalogue.Draft{}, err
	}
	return catalogue.CreateDraft(ctx, s.Pool, input)
}

func (s PostgresStore) SaveDraftVersion(ctx context.Context, id string, version int, input catalogue.EditDraftInput) (catalogue.Draft, error) {
	if _, err := s.Draft(ctx, id, version); err != nil {
		return catalogue.Draft{}, err
	}
	return catalogue.SaveDraftVersion(ctx, s.Pool, id, version, input)
}

func (s PostgresStore) Contacts(ctx context.Context, limit, offset int) ([]Contact, error) {
	accountID, err := contextAccountID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT id::text, name, email, organization, role, context, status, created_at
		FROM contacts WHERE account_id=$3 ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2`, limit, offset, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByName[Contact])
}

func (s PostgresStore) SaveContact(ctx context.Context, id string, input ContactInput) (Contact, error) {
	accountID, err := contextAccountID(ctx)
	if err != nil {
		return Contact{}, err
	}
	args := []any{input.Name, input.Email, input.Organization, input.Role, input.Context, input.Status, accountID}
	query := `
		INSERT INTO contacts (name, email, organization, role, context, status, account_id)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6, $7)
		RETURNING id::text, name, email, organization, role, context, status, created_at`
	if id != "" {
		args = append(args, id)
		query = `
			UPDATE contacts SET name = $1, email = NULLIF($2, ''),
				organization = NULLIF($3, ''), role = NULLIF($4, ''),
				context = NULLIF($5, ''), status = $6
			WHERE id = $8 AND account_id = $7
			RETURNING id::text, name, email, organization, role, context, status, created_at`
	}
	rows, err := s.Pool.Query(ctx, query, args...)
	if err != nil {
		return Contact{}, err
	}
	defer rows.Close()
	return pgx.CollectOneRow(rows, pgx.RowToStructByName[Contact])
}

const activityColumns = `
	SELECT o.outbox_id::text, a.draft_id::text, a.version, d.subject,
		d.recipient_email, a.approved_by, o.state, o.attempts,
		o.gmail_message_id, o.last_error, o.updated_at
	FROM outbox o
	JOIN approvals a USING (approval_id)
	JOIN drafts d ON d.draft_id = a.draft_id AND d.version = a.version
	JOIN contacts c ON c.id = d.contact_id`

func (s PostgresStore) Activities(ctx context.Context, limit, offset int) ([]Activity, error) {
	accountID, err := contextAccountID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, activityColumns+`
		WHERE c.account_id=$3 ORDER BY o.updated_at DESC, o.outbox_id DESC LIMIT $1 OFFSET $2`, limit, offset, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByName[Activity])
}

func (s PostgresStore) DraftActivity(ctx context.Context, id string, version int) (*Activity, error) {
	if _, err := s.Draft(ctx, id, version); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, activityColumns+`
		WHERE a.draft_id = $1 AND a.version = $2
		ORDER BY a.approved_at DESC, a.approval_id DESC LIMIT 1`, id, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entry, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[Activity])
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return &entry, err
}

func (s PostgresStore) ownsContact(ctx context.Context, id string) error {
	accountID, err := contextAccountID(ctx)
	if err != nil {
		return err
	}
	var exists bool
	return s.Pool.QueryRow(ctx, "SELECT true FROM contacts WHERE id=$1 AND account_id=$2", id, accountID).Scan(&exists)
}
