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

func TestWritingStylesRequiresAccount(t *testing.T) {
	store := PostgresStore{}
	for _, ctx := range []context.Context{
		context.Background(),
		withAccount(context.Background(), Account{ID: "not-a-uuid"}),
	} {
		profiles, err := store.WritingStyles(ctx, 25, 0)
		if err == nil || err.Error() != "authenticated account is required" || len(profiles) != 0 {
			t.Fatalf("account validation: %+v %v", profiles, err)
		}
	}
}

func TestWritingStylesRejectsInvalidPagination(t *testing.T) {
	store := PostgresStore{}
	ctx := withAccount(context.Background(), Account{ID: testID})
	for _, test := range []struct {
		limit  int
		offset int
	}{
		{0, 0},
		{-1, 0},
		{102, 0},
		{1, -1},
		{1, 1000001},
	} {
		profiles, err := store.WritingStyles(ctx, test.limit, test.offset)
		if err == nil || len(profiles) != 0 {
			t.Fatalf("accepted limit=%d offset=%d: %+v %v", test.limit, test.offset, profiles, err)
		}
	}
}

func TestPostgresWritingStyles(t *testing.T) {
	pool := accountTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, "CREATE TEMP TABLE writing_style_profiles (LIKE public.writing_style_profiles INCLUDING ALL)"); err != nil {
		t.Fatal(err)
	}
	ids := make(map[string]string)
	for _, name := range []string{"owner", "other", "reserved", "deleting", "empty"} {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO accounts (username, legacy_reserved, deletion_requested_at)
			VALUES ($1, $2, CASE WHEN $3::boolean THEN now() ELSE NULL END)
			RETURNING account_id::text`, name, name == "reserved", name == "deleting").Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[name] = id
	}
	created := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	want := make(map[string]WritingStyleProfile)
	for i, seed := range []struct {
		name      string
		account   string
		id        string
		createdAt time.Time
	}{
		{"first", "owner", "20000000-0000-4000-8000-000000000001", created},
		{"second", "owner", "10000000-0000-4000-8000-000000000001", created},
		{"old", "owner", "f0000000-0000-4000-8000-000000000001", created.Add(-time.Hour)},
		{"other", "other", "40000000-0000-4000-8000-000000000001", created.Add(time.Hour)},
		{"reserved", "reserved", "50000000-0000-4000-8000-000000000001", created},
		{"deleting", "deleting", "60000000-0000-4000-8000-000000000001", created},
	} {
		profile := WritingStyleProfile{
			ProfileID: seed.id, Name: seed.name, Language: "tr", Content: "## Style\nDoğrudan yaz: " + seed.name,
			FormatVersion: 1, Revision: i + 1, CreatedAt: seed.createdAt, UpdatedAt: seed.createdAt.Add(time.Minute),
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO writing_style_profiles (profile_id, account_id, name, language, content,
				format_version, revision, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, profile.ProfileID, ids[seed.account],
			profile.Name, profile.Language, profile.Content, profile.FormatVersion,
			profile.Revision, profile.CreatedAt, profile.UpdatedAt); err != nil {
			t.Fatal(err)
		}
		want[seed.name] = profile
	}
	store := PostgresStore{Pool: pool}
	ids["missing"] = testID
	for _, test := range []struct {
		name    string
		account string
		limit   int
		offset  int
		want    []string
	}{
		{"all profiles", "owner", 25, 0, []string{"first", "second", "old"}},
		{"first page", "owner", 2, 0, []string{"first", "second"}},
		{"second page", "owner", 2, 2, []string{"old"}},
		{"single row", "owner", 1, 1, []string{"second"}},
		{"past last page", "owner", 2, 3, nil},
		{"maximum offset", "owner", 1, 1000000, nil},
		{"other account", "other", 25, 0, []string{"other"}},
		{"reserved account", "reserved", 25, 0, nil},
		{"deleting account", "deleting", 25, 0, nil},
		{"missing account", "missing", 25, 0, nil},
		{"empty account", "empty", 25, 0, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			profiles, err := store.WritingStyles(withAccount(ctx, Account{ID: ids[test.account]}), test.limit, test.offset)
			if err != nil || len(profiles) != len(test.want) {
				t.Fatalf("listed %d profiles, want %d: %v", len(profiles), len(test.want), err)
			}
			for i, name := range test.want {
				got := profiles[i]
				got.CreatedAt = got.CreatedAt.UTC()
				got.UpdatedAt = got.UpdatedAt.UTC()
				if got != want[name] {
					t.Fatalf("profile %d: %+v, want %+v", i, profiles[i], want[name])
				}
			}
		})
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO writing_style_profiles (account_id, name, language, content, created_at)
		SELECT $1, 'Extra ' || n::text, 'en', 'Use clear language.', $2::timestamptz - n * interval '1 second'
		FROM generate_series(1, 99) AS n`, ids["owner"], created.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	ownerCtx := withAccount(ctx, Account{ID: ids["owner"]})
	profiles, err := store.WritingStyles(ownerCtx, 101, 0)
	if err != nil || len(profiles) != 101 || profiles[0].ProfileID != want["first"].ProfileID {
		t.Fatalf("maximum limit did not return the extra pagination row: %d %v", len(profiles), err)
	}
	profiles, err = store.WritingStyles(ownerCtx, 101, 101)
	if err != nil || len(profiles) != 1 || profiles[0].Name != "Extra 99" {
		t.Fatalf("last page: %+v %v", profiles, err)
	}
	if err := (PostgresAccounts{Pool: pool}).RequestDeletion(ctx, ids["owner"]); err != nil {
		t.Fatal(err)
	}
	if profiles, err := store.WritingStyles(ownerCtx, 101, 0); err != nil || len(profiles) != 0 {
		t.Fatalf("stale account context exposed deleting account's profiles: %d %v", len(profiles), err)
	}
	profiles, err = store.WritingStyles(withAccount(ctx, Account{ID: ids["other"]}), 25, 0)
	if err != nil || len(profiles) != 1 || profiles[0].ProfileID != want["other"].ProfileID {
		t.Fatalf("other account's profiles changed: %+v %v", profiles, err)
	}
}

func TestWritingStyleRequiresAccount(t *testing.T) {
	store := PostgresStore{}
	for _, ctx := range []context.Context{
		context.Background(),
		withAccount(context.Background(), Account{ID: "not-a-uuid"}),
	} {
		profile, err := store.WritingStyle(ctx, testID)
		if err == nil || err.Error() != "authenticated account is required" {
			t.Fatalf("account validation error = %v", err)
		}
		if profile != (WritingStyleProfile{}) {
			t.Fatalf("unauthenticated request returned a profile: %+v", profile)
		}
	}
}

func TestPostgresWritingStyle(t *testing.T) {
	pool := accountTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, "CREATE TEMP TABLE writing_style_profiles (LIKE public.writing_style_profiles INCLUDING ALL)"); err != nil {
		t.Fatal(err)
	}
	ids := make(map[string]string)
	want := make(map[string]WritingStyleProfile)
	created := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	for i, name := range []string{"owner", "other", "reserved", "deleting"} {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO accounts (username, legacy_reserved, deletion_requested_at)
			VALUES ($1, $2, CASE WHEN $3::boolean THEN now() ELSE NULL END)
			RETURNING account_id::text`, name, name == "reserved", name == "deleting").Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[name] = id
		profile := WritingStyleProfile{
			Name: name, Language: "tr", Content: "\n## Style\nDoğrudan yaz: " + name + "\n",
			FormatVersion: 1, Revision: i + 2, CreatedAt: created.Add(time.Duration(i) * time.Hour),
		}
		profile.UpdatedAt = profile.CreatedAt.Add(time.Minute)
		if err := pool.QueryRow(ctx, `
			INSERT INTO writing_style_profiles (account_id, name, language, content,
				format_version, revision, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING profile_id::text`, id, profile.Name, profile.Language, profile.Content,
			profile.FormatVersion, profile.Revision, profile.CreatedAt, profile.UpdatedAt).Scan(&profile.ProfileID); err != nil {
			t.Fatal(err)
		}
		want[name] = profile
	}
	store := PostgresStore{Pool: pool}
	for _, test := range []struct {
		name      string
		account   string
		profileID string
		want      WritingStyleProfile
		wantErr   error
	}{
		{"owner profile", "owner", want["owner"].ProfileID, want["owner"], nil},
		{"other account's own profile", "other", want["other"].ProfileID, want["other"], nil},
		{"owner cannot read other profile", "owner", want["other"].ProfileID, WritingStyleProfile{}, pgx.ErrNoRows},
		{"other cannot read owner profile", "other", want["owner"].ProfileID, WritingStyleProfile{}, pgx.ErrNoRows},
		{"missing profile", "owner", testID, WritingStyleProfile{}, pgx.ErrNoRows},
		{"reserved account", "reserved", want["reserved"].ProfileID, WritingStyleProfile{}, pgx.ErrNoRows},
		{"deleting account", "deleting", want["deleting"].ProfileID, WritingStyleProfile{}, pgx.ErrNoRows},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := store.WritingStyle(withAccount(ctx, Account{ID: ids[test.account]}), test.profileID)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("lookup error = %v, want %v", err, test.wantErr)
			}
			if err != nil {
				if got != (WritingStyleProfile{}) {
					t.Fatalf("failed lookup returned a profile: %+v", got)
				}
				return
			}
			got.CreatedAt = got.CreatedAt.UTC()
			got.UpdatedAt = got.UpdatedAt.UTC()
			if got != test.want {
				t.Fatalf("profile = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestUpdateWritingStyleRequiresAccount(t *testing.T) {
	store := PostgresStore{}
	for _, ctx := range []context.Context{
		context.Background(),
		withAccount(context.Background(), Account{ID: "not-a-uuid"}),
	} {
		saved, err := store.UpdateWritingStyle(ctx, testID, 1, writingstyle.Profile{})
		if err == nil || err.Error() != "authenticated account is required" {
			t.Fatalf("account validation error = %v", err)
		}
		if saved != (WritingStyleProfile{}) {
			t.Fatalf("unauthenticated request returned a profile: %+v", saved)
		}
	}
}

func TestUpdateWritingStyleRejectsInvalidRevision(t *testing.T) {
	store := PostgresStore{}
	ctx := withAccount(context.Background(), Account{ID: testID})
	profile := writingstyle.Profile{
		FormatVersion: 1, Name: "Warm and direct", Language: "en", Content: "Use short sentences.",
	}
	for _, revision := range []int{-1, 0, 2147483647} {
		saved, err := store.UpdateWritingStyle(ctx, testID, revision, profile)
		if err == nil || err.Error() != "expected revision must be between 1 and 2147483646" {
			t.Fatalf("revision %d validation error = %v", revision, err)
		}
		if saved != (WritingStyleProfile{}) {
			t.Fatalf("invalid revision %d returned a profile: %+v", revision, saved)
		}
	}
}

func TestPostgresUpdateWritingStyle(t *testing.T) {
	pool := accountTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, "CREATE TEMP TABLE writing_style_profiles (LIKE public.writing_style_profiles INCLUDING ALL)"); err != nil {
		t.Fatal(err)
	}
	var accountID string
	if err := pool.QueryRow(ctx, "INSERT INTO accounts (username) VALUES ('owner') RETURNING account_id::text").Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	original := WritingStyleProfile{
		Name: "Original style", Language: "en", Content: "Keep the opening brief.",
		FormatVersion: 1, Revision: 3, CreatedAt: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
	}
	original.UpdatedAt = original.CreatedAt.Add(time.Minute)
	if err := pool.QueryRow(ctx, `
		INSERT INTO writing_style_profiles (account_id, name, language, content,
			format_version, revision, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING profile_id::text`, accountID, original.Name, original.Language, original.Content,
		original.FormatVersion, original.Revision, original.CreatedAt, original.UpdatedAt).Scan(&original.ProfileID); err != nil {
		t.Fatal(err)
	}
	store := PostgresStore{Pool: pool}
	accountCtx := withAccount(ctx, Account{ID: accountID})
	profile := writingstyle.Profile{
		FormatVersion: 1, Name: "Sıcak ve doğrudan", Language: "tr",
		Content: "\n## Style\nKısa cümleler kullan.\n\n    Preserve indentation.\n",
	}
	saved, err := store.UpdateWritingStyle(accountCtx, original.ProfileID, original.Revision, profile)
	if err != nil {
		t.Fatalf("update writing style: %v", err)
	}
	if saved.ProfileID != original.ProfileID || saved.Name != profile.Name ||
		saved.Language != profile.Language || saved.Content != profile.Content ||
		saved.FormatVersion != profile.FormatVersion || saved.Revision != 4 {
		t.Fatalf("updated profile = %+v", saved)
	}
	if !saved.CreatedAt.Equal(original.CreatedAt) || !saved.UpdatedAt.After(original.UpdatedAt) {
		t.Fatalf("unexpected timestamps: created=%v updated=%v", saved.CreatedAt, saved.UpdatedAt)
	}
	saved.CreatedAt = saved.CreatedAt.UTC()
	saved.UpdatedAt = saved.UpdatedAt.UTC()
	stored, err := store.WritingStyle(accountCtx, original.ProfileID)
	if err != nil {
		t.Fatalf("read updated profile: %v", err)
	}
	stored.CreatedAt = stored.CreatedAt.UTC()
	stored.UpdatedAt = stored.UpdatedAt.UTC()
	if stored != saved {
		t.Fatalf("stored profile = %+v, want %+v", stored, saved)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM writing_style_profiles").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("profile count = %d, want 1", count)
	}

	stale := writingstyle.Profile{
		FormatVersion: 1, Name: "Stale edit", Language: "en", Content: "This must not replace the saved content.",
	}
	rejected, err := store.UpdateWritingStyle(accountCtx, original.ProfileID, original.Revision, stale)
	if !errors.Is(err, ErrWritingStyleConflict) {
		t.Fatalf("stale update error = %v, want ErrWritingStyleConflict", err)
	}
	if rejected != (WritingStyleProfile{}) {
		t.Fatalf("stale update returned a profile: %+v", rejected)
	}
	stored, err = store.WritingStyle(accountCtx, original.ProfileID)
	if err != nil {
		t.Fatalf("read profile after stale update: %v", err)
	}
	stored.CreatedAt = stored.CreatedAt.UTC()
	stored.UpdatedAt = stored.UpdatedAt.UTC()
	if stored != saved {
		t.Fatalf("stale update changed the profile: %+v, want %+v", stored, saved)
	}
}

func TestPostgresUpdateWritingStyleAccountIsolation(t *testing.T) {
	pool := accountTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, "CREATE TEMP TABLE writing_style_profiles (LIKE public.writing_style_profiles INCLUDING ALL)"); err != nil {
		t.Fatal(err)
	}
	ids := make(map[string]string)
	profiles := make(map[string]WritingStyleProfile)
	for _, name := range []string{"owner", "other", "reserved", "deleting"} {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO accounts (username, legacy_reserved, deletion_requested_at)
			VALUES ($1, $2, CASE WHEN $3::boolean THEN now() ELSE NULL END)
			RETURNING account_id::text`, name, name == "reserved", name == "deleting").Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[name] = id
		profile := WritingStyleProfile{
			Name: name + " style", Language: "en", Content: "Keep it clear.",
			FormatVersion: 1, Revision: 3, CreatedAt: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC),
		}
		profile.UpdatedAt = profile.CreatedAt.Add(time.Minute)
		if err := pool.QueryRow(ctx, `
			INSERT INTO writing_style_profiles (account_id, name, language, content,
				format_version, revision, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING profile_id::text`, id, profile.Name, profile.Language, profile.Content,
			profile.FormatVersion, profile.Revision, profile.CreatedAt, profile.UpdatedAt).Scan(&profile.ProfileID); err != nil {
			t.Fatal(err)
		}
		profiles[name] = profile
	}
	store := PostgresStore{Pool: pool}
	update := writingstyle.Profile{
		FormatVersion: 1, Name: "Changed style", Language: "tr", Content: "Use short sentences.",
	}
	for _, test := range []struct {
		name             string
		account          string
		profileID        string
		expectedRevision int
	}{
		{"other account's profile", "owner", profiles["other"].ProfileID, 3},
		{"other account's profile with wrong revision", "owner", profiles["other"].ProfileID, 2},
		{"missing profile", "owner", testID, 3},
		{"reserved account", "reserved", profiles["reserved"].ProfileID, 3},
		{"deleting account", "deleting", profiles["deleting"].ProfileID, 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			saved, err := store.UpdateWritingStyle(withAccount(ctx, Account{ID: ids[test.account]}),
				test.profileID, test.expectedRevision, update)
			if !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("update error = %v, want pgx.ErrNoRows", err)
			}
			if saved != (WritingStyleProfile{}) {
				t.Fatalf("rejected update returned a profile: %+v", saved)
			}
			var count int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM writing_style_profiles").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != len(profiles) {
				t.Fatalf("profile count = %d, want %d", count, len(profiles))
			}
			for name, want := range profiles {
				var accountID string
				var stored WritingStyleProfile
				if err := pool.QueryRow(ctx, `
					SELECT account_id::text, profile_id::text, name, language, content,
						format_version, revision, created_at, updated_at
					FROM writing_style_profiles WHERE profile_id=$1`, want.ProfileID).Scan(
					&accountID, &stored.ProfileID, &stored.Name, &stored.Language, &stored.Content,
					&stored.FormatVersion, &stored.Revision, &stored.CreatedAt, &stored.UpdatedAt,
				); err != nil {
					t.Fatalf("read %s profile: %v", name, err)
				}
				if accountID != ids[name] {
					t.Fatalf("%s profile account = %s, want %s", name, accountID, ids[name])
				}
				stored.CreatedAt = stored.CreatedAt.UTC()
				stored.UpdatedAt = stored.UpdatedAt.UTC()
				if stored != want {
					t.Fatalf("%s profile changed: %+v, want %+v", name, stored, want)
				}
			}
		})
	}
}

func TestDeleteWritingStyleRequiresAccount(t *testing.T) {
	store := PostgresStore{}
	for _, ctx := range []context.Context{
		context.Background(),
		withAccount(context.Background(), Account{ID: "not-a-uuid"}),
	} {
		err := store.DeleteWritingStyle(ctx, testID)
		if err == nil || err.Error() != "authenticated account is required" {
			t.Fatalf("account validation error = %v", err)
		}
	}
}

func TestPostgresDeleteWritingStyle(t *testing.T) {
	pool := accountTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, "CREATE TEMP TABLE writing_style_profiles (LIKE public.writing_style_profiles INCLUDING ALL)"); err != nil {
		t.Fatal(err)
	}
	ids := make(map[string]string)
	for _, name := range []string{"owner", "other", "reserved", "deleting"} {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO accounts (username, legacy_reserved, deletion_requested_at)
			VALUES ($1, $2, CASE WHEN $3::boolean THEN now() ELSE NULL END)
			RETURNING account_id::text`, name, name == "reserved", name == "deleting").Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[name] = id
	}
	profiles := make(map[string]string)
	remaining := make(map[string]struct{ accountID, name string })
	for _, seed := range []struct{ name, account string }{
		{"target", "owner"},
		{"kept", "owner"},
		{"other", "other"},
		{"reserved", "reserved"},
		{"deleting", "deleting"},
	} {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO writing_style_profiles (account_id, name, content)
			VALUES ($1, $2, 'Keep it clear.')
			RETURNING profile_id::text`, ids[seed.account], seed.name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		profiles[seed.name] = id
		remaining[id] = struct{ accountID, name string }{ids[seed.account], seed.name}
	}
	store := PostgresStore{Pool: pool}
	for _, test := range []struct {
		name      string
		account   string
		profileID string
		wantErr   error
	}{
		{"own profile", "owner", profiles["target"], nil},
		{"already deleted", "owner", profiles["target"], pgx.ErrNoRows},
		{"other account's profile", "owner", profiles["other"], pgx.ErrNoRows},
		{"missing profile", "owner", testID, pgx.ErrNoRows},
		{"reserved account", "reserved", profiles["reserved"], pgx.ErrNoRows},
		{"deleting account", "deleting", profiles["deleting"], pgx.ErrNoRows},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := store.DeleteWritingStyle(withAccount(ctx, Account{ID: ids[test.account]}), test.profileID)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("delete error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr == nil {
				delete(remaining, test.profileID)
			}
			var count int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM writing_style_profiles").Scan(&count); err != nil || count != len(remaining) {
				t.Fatalf("remaining profiles = %d, want %d: %v", count, len(remaining), err)
			}
			for id, want := range remaining {
				var accountID, name, content string
				if err := pool.QueryRow(ctx, `
					SELECT account_id::text, name, content
					FROM writing_style_profiles WHERE profile_id=$1`, id).Scan(&accountID, &name, &content); err != nil {
					t.Fatalf("read remaining profile %s: %v", id, err)
				}
				if accountID != want.accountID || name != want.name || content != "Keep it clear." {
					t.Fatalf("remaining profile %s changed: account=%s name=%q content=%q", id, accountID, name, content)
				}
			}
		})
	}
}
