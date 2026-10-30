package catalogue

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func ListDrafts(ctx context.Context, pool *pgxpool.Pool, limit, offset int) ([]Draft, error) {
	if limit < 1 || limit > 101 || offset < 0 {
		return nil, errors.New("invalid pagination")
	}
	rows, err := pool.Query(ctx, `
		SELECT draft_id::text, version, contact_id::text, recipient_email,
			subject, body, body_html, content_hash, created_at
		FROM drafts ORDER BY created_at DESC, draft_id DESC, version DESC
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByName[Draft])
}
