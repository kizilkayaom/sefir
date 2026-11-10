package webapp

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/kizilkayaom/sefir/mcp/internal/writingstyle"
)

type WritingStyleProfile struct {
	ProfileID     string    `json:"profile_id"`
	Name          string    `json:"name"`
	Language      string    `json:"language"`
	Content       string    `json:"content"`
	FormatVersion int       `json:"format_version"`
	Revision      int       `json:"revision"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (s PostgresStore) CreateWritingStyle(
	ctx context.Context,
	profile writingstyle.Profile,
) (WritingStyleProfile, error) {
	accountID, err := contextAccountID(ctx)
	if err != nil {
		return WritingStyleProfile{}, err
	}

	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return WritingStyleProfile{}, err
	}
	defer tx.Rollback(ctx)
	if err := tx.QueryRow(ctx, `
		SELECT account_id::text FROM accounts
		WHERE account_id=$1 AND NOT legacy_reserved AND deletion_requested_at IS NULL
		FOR SHARE`, accountID).Scan(&accountID); err != nil {
		return WritingStyleProfile{}, err
	}

	var saved WritingStyleProfile
	err = tx.QueryRow(ctx, `
		INSERT INTO writing_style_profiles (account_id, name, language, content, format_version)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING profile_id::text, name, language, content, format_version, revision, created_at, updated_at`,
		accountID, profile.Name, profile.Language, profile.Content, profile.FormatVersion,
	).Scan(&saved.ProfileID, &saved.Name, &saved.Language, &saved.Content, &saved.FormatVersion,
		&saved.Revision, &saved.CreatedAt, &saved.UpdatedAt)
	if err != nil {
		return WritingStyleProfile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return WritingStyleProfile{}, err
	}
	return saved, nil
}
