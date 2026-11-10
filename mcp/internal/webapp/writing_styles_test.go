package webapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/kizilkayaom/sefir/mcp/internal/writingstyle"
)

func TestCreateWritingStyleRequiresAccount(t *testing.T) {
	store := PostgresStore{}
	for _, ctx := range []context.Context{
		context.Background(),
		withAccount(context.Background(), Account{ID: "not-a-uuid"}),
	} {
		saved, err := store.CreateWritingStyle(ctx, writingstyle.Profile{})
		if err == nil || err.Error() != "authenticated account is required" {
			t.Fatalf("account validation error = %v", err)
		}
		if saved != (WritingStyleProfile{}) {
			t.Fatalf("unauthenticated request returned a profile: %+v", saved)
		}
	}
}

func TestPostgresCreateWritingStyle(t *testing.T) {
	pool := accountTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, query := range []string{
		"CREATE TEMP TABLE writing_style_profiles (LIKE public.writing_style_profiles INCLUDING ALL)",
		"ALTER TABLE pg_temp.writing_style_profiles ADD FOREIGN KEY (account_id) REFERENCES pg_temp.accounts(account_id) ON DELETE CASCADE",
	} {
		if _, err := pool.Exec(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	ids := make(map[string]string)
	for _, account := range []struct {
		name     string
		reserved bool
		deleting bool
	}{
		{name: "owner"},
		{name: "other"},
		{name: "reserved", reserved: true},
		{name: "deleting", deleting: true},
	} {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO accounts (username, legacy_reserved, deletion_requested_at)
			VALUES ($1, $2, CASE WHEN $3::boolean THEN now() ELSE NULL END)
			RETURNING account_id::text`, account.name, account.reserved, account.deleting).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[account.name] = id
	}
	profile, err := writingstyle.ParseProfile("style.md", []byte("---\nformat_version: 1\nname: Warm and direct\nlanguage: en\n---\n## Style\nDon't exaggerate.\n\n---\n\n    Preserve example indentation.\n"))
	if err != nil {
		t.Fatal(err)
	}
	store := PostgresStore{Pool: pool}
	created := make(map[string]WritingStyleProfile)
	for _, name := range []string{"owner", "other"} {
		accountCtx := withAccount(ctx, Account{ID: ids[name]})
		saved, err := store.CreateWritingStyle(accountCtx, profile)
		if err != nil {
			t.Fatalf("create %s profile: %v", name, err)
		}
		if !uuidPattern.MatchString(saved.ProfileID) || saved.Revision != 1 ||
			saved.CreatedAt.IsZero() || !saved.CreatedAt.Equal(saved.UpdatedAt) {
			t.Fatalf("database defaults were not returned: %+v", saved)
		}
		if saved.Name != profile.Name || saved.Language != profile.Language ||
			saved.Content != profile.Content || saved.FormatVersion != profile.FormatVersion {
			t.Fatalf("saved profile lost parsed values: %+v", saved)
		}
		var owner string
		var stored WritingStyleProfile
		if err := pool.QueryRow(ctx, `
			SELECT account_id::text, profile_id::text, name, language, content, format_version,
				revision, created_at, updated_at
			FROM writing_style_profiles WHERE profile_id=$1`, saved.ProfileID,
		).Scan(&owner, &stored.ProfileID, &stored.Name, &stored.Language, &stored.Content,
			&stored.FormatVersion, &stored.Revision, &stored.CreatedAt, &stored.UpdatedAt); err != nil {
			t.Fatalf("read committed profile: %v", err)
		}
		if owner != ids[name] || stored != saved {
			t.Fatalf("stored profile differs from result or belongs to another account: %s %+v", owner, stored)
		}
		created[name] = saved
	}
	if created["owner"].ProfileID == created["other"].ProfileID {
		t.Fatal("different accounts shared a profile ID")
	}

	for _, id := range []string{ids["reserved"], ids["deleting"], testID} {
		saved, err := store.CreateWritingStyle(withAccount(ctx, Account{ID: id}), profile)
		if !errors.Is(err, pgx.ErrNoRows) || saved != (WritingStyleProfile{}) {
			t.Fatalf("ineligible account created a profile: %+v %v", saved, err)
		}
	}
	invalid := profile
	invalid.FormatVersion = 2
	ownerCtx := withAccount(ctx, Account{ID: ids["owner"]})
	saved, err := store.CreateWritingStyle(ownerCtx, invalid)
	var constraint *pgconn.PgError
	if !errors.As(err, &constraint) || constraint.Code != "23514" || saved != (WritingStyleProfile{}) {
		t.Fatalf("invalid insert did not return a constraint error and empty profile: %+v %v", saved, err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM writing_style_profiles").Scan(&count); err != nil || count != 2 {
		t.Fatalf("failed writes changed stored profiles or left the transaction open: %d %v", count, err)
	}
	if _, err := store.CreateWritingStyle(ownerCtx, profile); err != nil {
		t.Fatalf("valid insert after rollback failed: %v", err)
	}
	if err := (PostgresAccounts{Pool: pool}).RequestDeletion(ctx, ids["owner"]); err != nil {
		t.Fatal(err)
	}
	if saved, err := store.CreateWritingStyle(ownerCtx, profile); !errors.Is(err, pgx.ErrNoRows) || saved != (WritingStyleProfile{}) {
		t.Fatalf("stale account context bypassed deletion check: %+v %v", saved, err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM accounts WHERE account_id=$1", ids["owner"]); err != nil {
		t.Fatal(err)
	}
	var remainingID string
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM writing_style_profiles").Scan(&count); err != nil || count != 1 {
		t.Fatalf("account deletion did not remove only its own profiles: %d %v", count, err)
	}
	if err := pool.QueryRow(ctx, "SELECT profile_id::text FROM writing_style_profiles").Scan(&remainingID); err != nil || remainingID != created["other"].ProfileID {
		t.Fatalf("other account's profile changed: %s %v", remainingID, err)
	}
}
