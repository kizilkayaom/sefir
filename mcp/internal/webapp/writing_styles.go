package webapp

import (
	"context"
	"errors"
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

func (s PostgresStore) WritingStyles(
	ctx context.Context,
	limit, offset int,
) ([]WritingStyleProfile, error) {
	accountID, err := contextAccountID(ctx)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 101 || offset < 0 || offset > 1000000 {
		return nil, errors.New("limit must be between 1 and 101 and offset between 0 and 1000000")
	}

	rows, err := s.Pool.Query(ctx, `
		SELECT p.profile_id::text, p.name, p.language, p.content, p.format_version,
			p.revision, p.created_at, p.updated_at
		FROM writing_style_profiles p
		JOIN accounts a ON a.account_id=p.account_id
		WHERE p.account_id=$1 AND NOT a.legacy_reserved AND a.deletion_requested_at IS NULL
		ORDER BY p.created_at DESC, p.profile_id DESC
		LIMIT $2 OFFSET $3`, accountID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByName[WritingStyleProfile])
}

func (s PostgresStore) DeleteWritingStyle(ctx context.Context, profileID string) error {
	accountID, err := contextAccountID(ctx)
	if err != nil {
		return err
	}
	result, err := s.Pool.Exec(ctx, `
		DELETE FROM writing_style_profiles AS p
		USING accounts AS a
		WHERE p.account_id=$1 AND p.profile_id=$2 AND a.account_id=p.account_id
			AND NOT a.legacy_reserved AND a.deletion_requested_at IS NULL`, accountID, profileID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
