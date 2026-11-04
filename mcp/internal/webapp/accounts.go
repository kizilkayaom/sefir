package webapp

import (
	"context"
	"crypto/rand"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kizilkayaom/sefir/mcp/internal/workspace"
)

var ErrAccountConflict = errors.New("username or Google account is already in use")

type Account struct {
	ID string
	ownerCredentials
}

type Accounts interface {
	ByID(context.Context, string) (Account, error)
	ByUsername(context.Context, string) (Account, error)
	ByGoogleSubject(context.Context, string) (Account, error)
	Create(context.Context, ownerCredentials) (Account, error)
	Google(context.Context, GoogleIdentity) (Account, error)
	LinkGoogle(context.Context, string, GoogleIdentity) (Account, error)
	ImportLegacy(context.Context, *ownerCredentials) error
	RequestDeletion(context.Context, string) error
}

type PostgresAccounts struct{ Pool *pgxpool.Pool }

const accountColumns = `account_id::text, username, password_salt, password_hash,
	COALESCE(google_subject, ''), COALESCE(google_email, '')`

func scanAccount(row pgx.Row) (Account, error) {
	var account Account
	account.Version = 1
	err := row.Scan(&account.ID, &account.Username, &account.Salt, &account.Hash, &account.GoogleSubject, &account.GoogleEmail)
	var constraint *pgconn.PgError
	if errors.As(err, &constraint) && constraint.Code == "23505" {
		err = ErrAccountConflict
	}
	return account, err
}

func (a PostgresAccounts) ByID(ctx context.Context, id string) (Account, error) {
	return scanAccount(a.Pool.QueryRow(ctx, "SELECT "+accountColumns+" FROM accounts WHERE account_id=$1 AND NOT legacy_reserved AND deletion_requested_at IS NULL", id))
}

func (a PostgresAccounts) ByUsername(ctx context.Context, username string) (Account, error) {
	return scanAccount(a.Pool.QueryRow(ctx, "SELECT "+accountColumns+" FROM accounts WHERE lower(username)=lower($1) AND NOT legacy_reserved AND deletion_requested_at IS NULL", strings.TrimSpace(username)))
}

func (a PostgresAccounts) ByGoogleSubject(ctx context.Context, subject string) (Account, error) {
	return scanAccount(a.Pool.QueryRow(ctx, "SELECT "+accountColumns+" FROM accounts WHERE google_subject=$1 AND NOT legacy_reserved AND deletion_requested_at IS NULL", subject))
}

func (a PostgresAccounts) Create(ctx context.Context, credentials ownerCredentials) (Account, error) {
	if !credentials.valid() {
		return Account{}, errors.New("invalid account credentials")
	}
	return scanAccount(a.Pool.QueryRow(ctx, `
		INSERT INTO accounts (username, password_salt, password_hash, google_subject, google_email)
		VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,'')) RETURNING `+accountColumns,
		credentials.Username, credentials.Salt, credentials.Hash, credentials.GoogleSubject, credentials.GoogleEmail))
}

var googleUsernameCharacters = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func googleUsername(address string) string {
	name := googleUsernameCharacters.ReplaceAllString(strings.SplitN(address, "@", 2)[0], "-")
	if len(name) > 24 {
		name = name[:24]
	}
	if validateUsername(name) != nil {
		name = "user"
	}
	return name
}

func (a PostgresAccounts) Google(ctx context.Context, identity GoogleIdentity) (Account, error) {
	find := func() (Account, error) {
		return a.ByGoogleSubject(ctx, identity.Subject)
	}
	account, err := find()
	if err == nil || !errors.Is(err, pgx.ErrNoRows) {
		return account, err
	}
	name := googleUsername(identity.Email)
	for i := 0; i < 4; i++ {
		account, err = a.Create(ctx, ownerCredentials{Version: 1, Username: name, GoogleSubject: identity.Subject, GoogleEmail: identity.Email})
		if !errors.Is(err, ErrAccountConflict) {
			return account, err
		}
		if account, err = find(); err == nil || !errors.Is(err, pgx.ErrNoRows) {
			return account, err
		}
		name = googleUsername(identity.Email) + "-" + strings.ToLower(rand.Text()[:6])
	}
	return Account{}, ErrAccountConflict
}

func (a PostgresAccounts) LinkGoogle(ctx context.Context, id string, identity GoogleIdentity) (Account, error) {
	account, err := scanAccount(a.Pool.QueryRow(ctx, `
		UPDATE accounts SET google_subject=$2, google_email=$3
		WHERE account_id=$1 AND NOT legacy_reserved AND (google_subject IS NULL OR google_subject=$2) AND deletion_requested_at IS NULL
		RETURNING `+accountColumns, id, identity.Subject, identity.Email))
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrAccountConflict
	}
	return account, err
}

func (a PostgresAccounts) RequestDeletion(ctx context.Context, accountID string) error {
	result, err := a.Pool.Exec(ctx, `
		UPDATE accounts
		SET deletion_requested_at=COALESCE(deletion_requested_at, now())
		WHERE account_id=$1 AND NOT legacy_reserved`, accountID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (a PostgresAccounts) ImportLegacy(ctx context.Context, owner *ownerCredentials) error {
	if owner == nil {
		return nil
	}
	if !owner.valid() {
		return errors.New("invalid legacy account")
	}
	_, err := a.Pool.Exec(ctx, `
		UPDATE accounts SET username=$2, password_salt=$3, password_hash=$4,
			google_subject=NULLIF($5,''), google_email=NULLIF($6,''), legacy_reserved=false
		WHERE account_id=$1 AND legacy_reserved AND deletion_requested_at IS NULL`,
		workspace.LegacyAccountID, owner.Username, owner.Salt, owner.Hash, owner.GoogleSubject, owner.GoogleEmail)
	return err
}

type accountContextKey struct{}

func withAccount(ctx context.Context, account Account) context.Context {
	return context.WithValue(ctx, accountContextKey{}, account)
}

func contextAccount(ctx context.Context) Account {
	account, _ := ctx.Value(accountContextKey{}).(Account)
	return account
}

func contextAccountID(ctx context.Context) (string, error) {
	id := contextAccount(ctx).ID
	if !uuidPattern.MatchString(id) {
		return "", errors.New("authenticated account is required")
	}
	return id, nil
}

func (a PostgresAccounts) PendingDeletions(ctx context.Context, limit int) ([]string, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("limit must be between 1 and 100")
	}

	query := `
	SELECT account_id::text
	FROM accounts
	WHERE deletion_requested_at IS NOT NULL
	AND NOT legacy_reserved
	ORDER BY deletion_requested_at, account_id
	LIMIT $1
	`

	rows, err := a.Pool.Query(ctx, query, limit)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return ids, nil
}

const sendingOutboxQuery = `
	SELECT EXISTS (
		SELECT 1
		FROM outbox AS o
		JOIN approvals AS ap ON ap.approval_id = o.approval_id
		JOIN drafts AS d
		ON d.draft_id = ap.draft_id AND d.version = ap.version
		JOIN contacts AS c ON c.id = d.contact_id
		WHERE c.account_id = $1
		AND o.state = 'sending'
	)
	`

func (a PostgresAccounts) HasSendingOutbox(ctx context.Context, accountID string) (bool, error) {
	var sending bool
	if err := a.Pool.QueryRow(ctx, sendingOutboxQuery, accountID).Scan(&sending); err != nil {
		return false, err
	}

	return sending, nil
}

func (a PostgresAccounts) PurgeAccount(ctx context.Context, accountID string) (bool, error) {
	tx, err := a.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var deletionRequestedAt *time.Time
	var reserved bool
	err = tx.QueryRow(ctx, `
		SELECT deletion_requested_at, legacy_reserved
		FROM accounts
		WHERE account_id=$1
		FOR UPDATE`, accountID).Scan(&deletionRequestedAt, &reserved)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if reserved || deletionRequestedAt == nil {
		return false, errors.New("account is not eligible for deletion")
	}

	var sending bool
	if err := tx.QueryRow(ctx, sendingOutboxQuery, accountID).Scan(&sending); err != nil {
		return false, err
	}
	if sending {
		return false, nil
	}

	queries := []string{
		`DELETE FROM outbox AS o
		 USING approvals AS ap, drafts AS d, contacts AS c
		 WHERE o.approval_id=ap.approval_id
		   AND ap.draft_id=d.draft_id AND ap.version=d.version
		   AND d.contact_id=c.id AND c.account_id=$1`,
		`DELETE FROM approvals AS ap
		 USING drafts AS d, contacts AS c
		 WHERE ap.draft_id=d.draft_id AND ap.version=d.version
		   AND d.contact_id=c.id AND c.account_id=$1`,
		`DELETE FROM drafts AS d
		 USING contacts AS c
		 WHERE d.contact_id=c.id AND c.account_id=$1`,
		`DELETE FROM contacts WHERE account_id=$1`,
		`DELETE FROM accounts WHERE account_id=$1`,
	}
	for _, query := range queries {
		if _, err := tx.Exec(ctx, query, accountID); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
