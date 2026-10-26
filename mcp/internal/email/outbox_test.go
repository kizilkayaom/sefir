package email

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestListStaleOutboxEntries(t *testing.T) {
	URL := os.Getenv("TEST_DATABASE_URL")
	if URL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	config, err := pgxpool.ParseConfig(URL)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}

	if config.ConnConfig.Database != "sefir_test" {
		t.Fatal("test must use sefir_test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open test database pool: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("connect to test database: %v", err)
	}

	_, err = pool.Exec(ctx, `
		CREATE TEMP TABLE outbox (
			LIKE public.outbox INCLUDING ALL
		)
	`)
	if err != nil {
		t.Fatalf("create temporary outbox table: %v", err)
	}

	cutoff := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	entries, err := ListStaleOutboxEntries(ctx, pool, cutoff, 10)
	if err != nil {
		t.Fatalf("list stale outbox entries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entry count = %d, want 0", len(entries))
	}

	existingGmailID := "gmail-existing"
	fixtures := []struct {
		name           string
		state          string
		claimedAt      time.Time
		gmailMessageID *string
	}{
		{
			name:      "newer stale",
			state:     "sending",
			claimedAt: cutoff.Add(-time.Minute),
		},
		{
			name:      "oldest stale",
			state:     "sending",
			claimedAt: cutoff.Add(-10 * time.Minute),
		},
		{
			name:      "recent",
			state:     "sending",
			claimedAt: cutoff.Add(time.Minute),
		},
		{
			name:      "boundary",
			state:     "sending",
			claimedAt: cutoff,
		},
		{
			name:      "pending",
			state:     "pending",
			claimedAt: cutoff.Add(-20 * time.Minute),
		},
		{
			name:           "already recorded",
			state:          "sending",
			claimedAt:      cutoff.Add(-20 * time.Minute),
			gmailMessageID: &existingGmailID,
		},
	}

	fixtureIDs := make(map[string]string, len(fixtures))
	for _, fixture := range fixtures {
		var outboxID string
		err := pool.QueryRow(ctx, `
			INSERT INTO outbox (
				approval_id, state, claimed_at, gmail_message_id
			)
			VALUES (gen_random_uuid(), $1, $2, $3)
			RETURNING outbox_id::text
		`, fixture.state, fixture.claimedAt, fixture.gmailMessageID).Scan(&outboxID)
		if err != nil {
			t.Fatalf("insert %s outbox entry: %v", fixture.name, err)
		}
		fixtureIDs[fixture.name] = outboxID
	}

	entries, err = ListStaleOutboxEntries(ctx, pool, cutoff, 10)
	if err != nil {
		t.Fatalf("list stale outbox entries: %v", err)
	}
	wantIDs := []string{fixtureIDs["oldest stale"], fixtureIDs["newer stale"]}
	if len(entries) != len(wantIDs) {
		t.Fatalf("entry count = %d, want %d; entries = %+v", len(entries), len(wantIDs), entries)
	}
	for i, wantID := range wantIDs {
		if entries[i].OutboxID != wantID {
			t.Errorf("entry %d: outbox ID = %q, want %q", i, entries[i].OutboxID, wantID)
		}
	}

	entries, err = ListStaleOutboxEntries(ctx, pool, cutoff, 1)
	if err != nil {
		t.Fatalf("list stale outbox entries with limit 1: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entry count with limit 1 = %d, want 1", len(entries))
	}
	if entries[0].OutboxID != fixtureIDs["oldest stale"] {
		t.Errorf("limited entry: outbox ID = %q, want %q", entries[0].OutboxID, fixtureIDs["oldest stale"])
	}
}
