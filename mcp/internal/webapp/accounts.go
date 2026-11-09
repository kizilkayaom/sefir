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
var ErrInvalidPreset = errors.New("unsupported writing style")

type Account struct {
	ID              string
	DefaultPresetID string
	ownerCredentials
}

type DeletionRun struct {
	WorkflowID string
	RunID      string
}

type Accounts interface {
	ByID(context.Context, string) (Account, error)
	ByUsername(context.Context, string) (Account, error)
	ByGoogleSubject(context.Context, string) (Account, error)
	Create(context.Context, ownerCredentials) (Account, error)
	Google(context.Context, GoogleIdentity) (Account, error)
	LinkGoogle(context.Context, string, GoogleIdentity) (Account, error)
	SetDefaultPreset(context.Context, string, string) (Account, error)
	ImportLegacy(context.Context, *ownerCredentials) error
	RequestDeletion(context.Context, string) error
	WithActiveAccount(context.Context, string, func() error) error
}

type PostgresAccounts struct{ Pool *pgxpool.Pool }

func (a PostgresAccounts) WithActiveAccount(ctx context.Context, accountID string, action func() error) error {
	if !uuidPattern.MatchString(accountID) || action == nil {
		return errors.New("valid account ID and action are required")
	}
	tx, err := a.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id string
	if err := tx.QueryRow(ctx, `SELECT account_id::text FROM accounts
		WHERE account_id=$1 AND NOT legacy_reserved AND deletion_requested_at IS NULL
		FOR SHARE`, accountID).Scan(&id); err != nil {
		return err
	}
	if err := action(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const accountColumns = `account_id::text, username, password_salt, password_hash,
	COALESCE(google_subject, ''), COALESCE(google_email, ''), default_preset_id`

func scanAccount(row pgx.Row) (Account, error) {
	var account Account
	account.Version = 1
	err := row.Scan(&account.ID, &account.Username, &account.Salt, &account.Hash, &account.GoogleSubject, &account.GoogleEmail, &account.DefaultPresetID)
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

func (a PostgresAccounts) DeletionWorkflowIDs(ctx context.Context, accountID string) ([]string, error) {
	query := `
	SELECT 'approval-' || d.draft_id::text || '-v' || d.version::text
	FROM drafts AS d
	JOIN contacts AS c ON c.id = d.contact_id
	JOIN accounts AS acc ON acc.account_id = c.account_id
	WHERE acc.account_id = $1
	AND acc.deletion_requested_at IS NOT NULL
	AND NOT acc.legacy_reserved
	ORDER BY d.draft_id, d.version
	`

	rows, err := a.Pool.Query(ctx, query, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	workflowIDs := make([]string, 0)

	for rows.Next() {
		var workflowID string
		if err := rows.Scan(&workflowID); err != nil {
			return nil, err
		}
		workflowIDs = append(workflowIDs, workflowID)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return workflowIDs, nil

}

func (a PostgresAccounts) SaveDeletionRun(ctx context.Context, accountID, workflowID, runID string) error {
	if !uuidPattern.MatchString(accountID) {
		return errors.New("invalid account ID")
	}
	if strings.TrimSpace(workflowID) == "" {
		return errors.New("workflow ID is required")
	}
	if strings.TrimSpace(runID) == "" {
		return errors.New("run ID is required")
	}

	tx, err := a.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var found int
	err = tx.QueryRow(ctx, `
		SELECT 1
		FROM drafts AS d
		JOIN contacts AS c ON c.id = d.contact_id
		JOIN accounts AS acc ON acc.account_id = c.account_id
		WHERE acc.account_id = $1
		AND acc.deletion_requested_at IS NOT NULL
		AND NOT acc.legacy_reserved
		AND ('approval-' || d.draft_id::text || '-v' || d.version::text) = $2
		FOR SHARE OF acc`, accountID, workflowID).Scan(&found)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO account_deletion_runs (account_id, workflow_id, run_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (account_id, workflow_id, run_id) DO NOTHING`, accountID, workflowID, runID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (a PostgresAccounts) DeletionRuns(ctx context.Context, accountID string) ([]DeletionRun, error) {
	if !uuidPattern.MatchString(accountID) {
		return nil, errors.New("invalid account ID")
	}

	rows, err := a.Pool.Query(ctx, `
		SELECT r.workflow_id, r.run_id
		FROM account_deletion_runs AS r
		JOIN accounts AS acc ON acc.account_id = r.account_id
		WHERE acc.account_id = $1
		AND acc.deletion_requested_at IS NOT NULL
		AND NOT acc.legacy_reserved
		ORDER BY r.workflow_id, r.run_id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := make([]DeletionRun, 0)
	for rows.Next() {
		var run DeletionRun
		if err := rows.Scan(&run.WorkflowID, &run.RunID); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return runs, nil
}

func (a PostgresAccounts) SetDefaultPreset(
	ctx context.Context,
	accountID, presetID string,
) (Account, error) {
	if err := validateDefaultPreset(accountID, presetID); err != nil {
		return Account{}, err
	}
	return scanAccount(a.Pool.QueryRow(ctx, `
		UPDATE accounts SET default_preset_id=$2
		WHERE account_id=$1 AND NOT legacy_reserved AND deletion_requested_at IS NULL
		RETURNING `+accountColumns, accountID, presetID))
}

func validateDefaultPreset(accountID, presetID string) error {
	if !uuidPattern.MatchString(accountID) {
		return errors.New("valid account ID is required")
	}
	switch presetID {
	case "prof", "warm_prof", "technical", "formal":
		return nil
	default:
		return ErrInvalidPreset
	}
}
