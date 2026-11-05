package webapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func accountPurgeTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := accountTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, query := range []string{
		"ALTER TABLE pg_temp.contacts ADD FOREIGN KEY (account_id) REFERENCES pg_temp.accounts(account_id)",
		"ALTER TABLE pg_temp.drafts ADD FOREIGN KEY (contact_id) REFERENCES pg_temp.contacts(id)",
		"ALTER TABLE pg_temp.approvals ADD FOREIGN KEY (draft_id, version) REFERENCES pg_temp.drafts(draft_id, version)",
		"ALTER TABLE pg_temp.outbox ADD FOREIGN KEY (approval_id) REFERENCES pg_temp.approvals(approval_id)",
	} {
		if _, err := pool.Exec(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	return pool
}

type purgeAccountFixture struct {
	accountID  string
	contactID  string
	draftID    string
	version    int
	approvalID string
	outboxID   string
}

func seedPurgeAccount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, username string, version int, state string) purgeAccountFixture {
	t.Helper()
	value := purgeAccountFixture{draftID: testID, version: version}
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (username, deletion_requested_at)
		VALUES ($1, now()) RETURNING account_id::text`, username).Scan(&value.accountID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO contacts (name, status, account_id)
		VALUES ($1, 'confirmed', $2) RETURNING id::text`, username+" contact", value.accountID).Scan(&value.contactID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO drafts (draft_id, version, contact_id, subject, body, content_hash)
		VALUES ($1, $2, $3, 'Test subject', 'Test body', repeat('a', 64))`, value.draftID, version, value.contactID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO approvals (draft_id, version, content_hash, approved_by)
		VALUES ($1, $2, repeat('a', 64), $3) RETURNING approval_id::text`, value.draftID, version, username).Scan(&value.approvalID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO outbox (approval_id, state)
		VALUES ($1, $2) RETURNING outbox_id::text`, value.approvalID, state).Scan(&value.outboxID); err != nil {
		t.Fatal(err)
	}
	return value
}

func assertPurgeAccountRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, value purgeAccountFixture, want bool) {
	t.Helper()
	checks := []struct {
		name  string
		query string
		args  []any
	}{
		{"account", "SELECT EXISTS (SELECT 1 FROM accounts WHERE account_id=$1)", []any{value.accountID}},
		{"contact", "SELECT EXISTS (SELECT 1 FROM contacts WHERE id=$1)", []any{value.contactID}},
		{"draft", "SELECT EXISTS (SELECT 1 FROM drafts WHERE draft_id=$1 AND version=$2)", []any{value.draftID, value.version}},
		{"approval", "SELECT EXISTS (SELECT 1 FROM approvals WHERE approval_id=$1)", []any{value.approvalID}},
		{"outbox", "SELECT EXISTS (SELECT 1 FROM outbox WHERE outbox_id=$1)", []any{value.outboxID}},
	}
	for _, check := range checks {
		var exists bool
		if err := pool.QueryRow(ctx, check.query, check.args...).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists != want {
			t.Fatalf("account %s: %s exists=%t, want %t", value.accountID, check.name, exists, want)
		}
	}
}

func TestPostgresPurgeAccountRejectsIneligibleAccounts(t *testing.T) {
	pool := accountPurgeTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	accounts := PostgresAccounts{Pool: pool}
	requestedAt := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name      string
		reserved  bool
		requested *time.Time
	}{
		{name: "active"},
		{name: "reserved", reserved: true},
		{name: "reserved-pending", reserved: true, requested: &requestedAt},
	} {
		t.Run(test.name, func(t *testing.T) {
			var accountID string
			if err := pool.QueryRow(ctx, `
				INSERT INTO accounts (username, legacy_reserved, deletion_requested_at)
				VALUES ($1, $2, $3) RETURNING account_id::text`, test.name, test.reserved, test.requested).Scan(&accountID); err != nil {
				t.Fatal(err)
			}
			if complete, err := accounts.PurgeAccount(ctx, accountID); err == nil || complete {
				t.Fatalf("ineligible account purge: complete=%t, error=%v", complete, err)
			}
			var exists bool
			if err := pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM accounts WHERE account_id=$1)", accountID).Scan(&exists); err != nil {
				t.Fatal(err)
			}
			if !exists {
				t.Fatal("ineligible account was removed")
			}
		})
	}
	if complete, err := accounts.PurgeAccount(ctx, testID); err != nil || !complete {
		t.Fatalf("missing account purge: complete=%t, error=%v", complete, err)
	}
	if complete, err := accounts.PurgeAccount(ctx, "invalid-uuid"); err == nil || complete {
		t.Fatalf("invalid account ID: complete=%t, error=%v", complete, err)
	}
}

func TestPostgresPurgeAccountWaitsForDeliveryAndPreservesOtherAccount(t *testing.T) {
	pool := accountPurgeTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	accounts := PostgresAccounts{Pool: pool}
	target := seedPurgeAccount(t, ctx, pool, "target", 1, "sending")
	other := seedPurgeAccount(t, ctx, pool, "other", 2, "sending")
	if _, err := pool.Exec(ctx, "UPDATE accounts SET deletion_requested_at=NULL WHERE account_id=$1", other.accountID); err != nil {
		t.Fatal(err)
	}
	if complete, err := accounts.PurgeAccount(ctx, target.accountID); err != nil || complete {
		t.Fatalf("sending account purge: complete=%t, error=%v", complete, err)
	}
	assertPurgeAccountRows(t, ctx, pool, target, true)
	assertPurgeAccountRows(t, ctx, pool, other, true)
	if _, err := pool.Exec(ctx, "UPDATE outbox SET state='sent' WHERE outbox_id=$1", target.outboxID); err != nil {
		t.Fatal(err)
	}
	if complete, err := accounts.PurgeAccount(ctx, target.accountID); err != nil || !complete {
		t.Fatalf("eligible account purge: complete=%t, error=%v", complete, err)
	}
	assertPurgeAccountRows(t, ctx, pool, target, false)
	assertPurgeAccountRows(t, ctx, pool, other, true)
	if complete, err := accounts.PurgeAccount(ctx, target.accountID); err != nil || !complete {
		t.Fatalf("repeated account purge: complete=%t, error=%v", complete, err)
	}
	assertPurgeAccountRows(t, ctx, pool, other, true)
}

func TestPostgresPurgeAccountRollsBackOnFailure(t *testing.T) {
	pool := accountPurgeTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	accounts := PostgresAccounts{Pool: pool}
	target := seedPurgeAccount(t, ctx, pool, "target", 1, "pending")
	if _, err := pool.Exec(ctx, "CREATE TEMP TABLE purge_blockers (contact_id UUID REFERENCES pg_temp.contacts(id))"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO purge_blockers (contact_id) VALUES ($1)", target.contactID); err != nil {
		t.Fatal(err)
	}
	complete, err := accounts.PurgeAccount(ctx, target.accountID)
	var constraint *pgconn.PgError
	if complete || !errors.As(err, &constraint) || constraint.Code != "23503" {
		t.Fatalf("expected foreign key failure: complete=%t, error=%v", complete, err)
	}
	assertPurgeAccountRows(t, ctx, pool, target, true)
	if _, err := pool.Exec(ctx, "DELETE FROM purge_blockers WHERE contact_id=$1", target.contactID); err != nil {
		t.Fatal(err)
	}
	if complete, err := accounts.PurgeAccount(ctx, target.accountID); err != nil || !complete {
		t.Fatalf("retry after database failure: complete=%t, error=%v", complete, err)
	}
	assertPurgeAccountRows(t, ctx, pool, target, false)
}

func TestPostgresDeletionWorkflowIDs(t *testing.T) {
	pool := accountPurgeTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	accounts := PostgresAccounts{Pool: pool}

	ids, err := accounts.DeletionWorkflowIDs(ctx, testID)
	if err != nil || len(ids) != 0 {
		t.Fatalf("missing account workflows: %v, error: %v", ids, err)
	}

	target := seedPurgeAccount(t, ctx, pool, "target", 10, "pending")
	other := seedPurgeAccount(t, ctx, pool, "other", 3, "pending")
	for _, version := range []int{2, 1} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO drafts (draft_id, version, contact_id, subject, body, content_hash)
			VALUES ($1, $2, $3, 'Test subject', 'Test body', repeat('a', 64))`, target.draftID, version, target.contactID); err != nil {
			t.Fatal(err)
		}
	}

	ids, err = accounts.DeletionWorkflowIDs(ctx, target.accountID)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "approval-" + target.draftID + "-v"
	want := []string{prefix + "1", prefix + "2", prefix + "10"}
	if len(ids) != len(want) {
		t.Fatalf("expected target workflows %v, got %v", want, ids)
	}
	for i, id := range want {
		if ids[i] != id {
			t.Fatalf("expected target workflows %v, got %v", want, ids)
		}
	}

	ids, err = accounts.DeletionWorkflowIDs(ctx, other.accountID)
	if err != nil || len(ids) != 1 || ids[0] != "approval-"+other.draftID+"-v3" {
		t.Fatalf("other account workflows: %v, error: %v", ids, err)
	}

	requestedAt := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name      string
		requested *time.Time
		reserved  bool
	}{
		{name: "active"},
		{name: "reserved", requested: &requestedAt, reserved: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, `
				UPDATE accounts SET deletion_requested_at=$2, legacy_reserved=$3
				WHERE account_id=$1`, other.accountID, test.requested, test.reserved); err != nil {
				t.Fatal(err)
			}
			ids, err := accounts.DeletionWorkflowIDs(ctx, other.accountID)
			if err != nil || len(ids) != 0 {
				t.Fatalf("ineligible account workflows: %v, error: %v", ids, err)
			}
		})
	}
}
