package email

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kizilkayaom/sefir/mcp/internal/catalogue"
)

func TestApproveDraftRejectsPendingDeletion(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "sefir_test" {
		t.Fatal("test must use sefir_test")
	}
	config.MaxConns = 1
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, table := range []string{"accounts", "contacts", "drafts", "approvals", "outbox"} {
		if _, err := pool.Exec(ctx, "CREATE TEMP TABLE "+table+" (LIKE public."+table+" INCLUDING ALL)"); err != nil {
			t.Fatal(err)
		}
	}
	accountID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if _, err := pool.Exec(ctx, "INSERT INTO accounts (account_id, username) VALUES ($1, 'pending-owner')", accountID); err != nil {
		t.Fatal(err)
	}
	var contactID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO contacts (name, email, status, account_id)
		VALUES ('Recipient', 'recipient@example.com', 'confirmed', $1)
		RETURNING id::text`, accountID).Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	draft, err := catalogue.CreateDraft(ctx, pool, catalogue.CreateDraftInput{
		ContactID: contactID, Subject: "Test", Body: "Test body",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := pool.Exec(ctx, "UPDATE accounts SET deletion_requested_at=now() WHERE account_id=$1", accountID)
	if err != nil {
		t.Fatal(err)
	}
	if result.RowsAffected() != 1 {
		t.Fatalf("marked %d accounts for deletion, want 1", result.RowsAffected())
	}
	approval, err := ApproveDraft(ctx, pool, ApproveDraftInput{
		ApprovalID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		DraftID:    draft.DraftID, Version: draft.Version, ContentHash: draft.ContentHash, ApprovedBy: "pending-owner",
	})
	if err == nil || !strings.Contains(err.Error(), "account is pending deletion") {
		t.Fatalf("expected approval to reject an account pending deletion, got %v", err)
	}
	if approval != (ApprovalResult{}) {
		t.Fatalf("account pending deletion returned an approval: %+v", approval)
	}
	var approvalCount, outboxCount int
	if err := pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM approvals), (SELECT count(*) FROM outbox)
	`).Scan(&approvalCount, &outboxCount); err != nil {
		t.Fatal(err)
	}
	if approvalCount != 0 || outboxCount != 0 {
		t.Fatalf("rejected approval created records: approvals %d, outbox %d", approvalCount, outboxCount)
	}
}

func TestApproveDraftRejectsRepeatedApprovalDuringDeletion(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "sefir_test" {
		t.Fatal("test must use sefir_test")
	}
	config.MaxConns = 1
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, table := range []string{"accounts", "contacts", "drafts", "approvals", "outbox"} {
		if _, err := pool.Exec(ctx, "CREATE TEMP TABLE "+table+" (LIKE public."+table+" INCLUDING ALL)"); err != nil {
			t.Fatal(err)
		}
	}
	accountID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if _, err := pool.Exec(ctx, "INSERT INTO accounts (account_id, username) VALUES ($1, 'pending-owner')", accountID); err != nil {
		t.Fatal(err)
	}
	var contactID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO contacts (name, email, status, account_id)
		VALUES ('Recipient', 'recipient@example.com', 'confirmed', $1)
		RETURNING id::text`, accountID).Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	draft, err := catalogue.CreateDraft(ctx, pool, catalogue.CreateDraftInput{
		ContactID: contactID, Subject: "Test", Body: "Test body",
	})
	if err != nil {
		t.Fatal(err)
	}
	input := ApproveDraftInput{
		ApprovalID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		DraftID:    draft.DraftID, Version: draft.Version, ContentHash: draft.ContentHash, ApprovedBy: "pending-owner",
	}
	approval, err := ApproveDraft(ctx, pool, input)
	if err != nil {
		t.Fatal(err)
	}
	if approval.ApprovalID != input.ApprovalID || approval.OutboxID == "" || approval.OutboxState != "pending" {
		t.Fatalf("unexpected active account approval: %+v", approval)
	}
	const recordsQuery = `
		SELECT
			(SELECT jsonb_agg(a ORDER BY a.approval_id)::text FROM approvals AS a),
			(SELECT jsonb_agg(o ORDER BY o.outbox_id)::text FROM outbox AS o)
	`
	var approvalsBefore, outboxBefore string
	if err := pool.QueryRow(ctx, recordsQuery).Scan(&approvalsBefore, &outboxBefore); err != nil {
		t.Fatal(err)
	}
	result, err := pool.Exec(ctx, "UPDATE accounts SET deletion_requested_at=now() WHERE account_id=$1", accountID)
	if err != nil {
		t.Fatal(err)
	}
	if result.RowsAffected() != 1 {
		t.Fatalf("marked %d accounts for deletion, want 1", result.RowsAffected())
	}
	approval, err = ApproveDraft(ctx, pool, input)
	if err == nil || !strings.Contains(err.Error(), "account is pending deletion") {
		t.Fatalf("expected repeated approval to reject an account pending deletion, got %v", err)
	}
	if approval != (ApprovalResult{}) {
		t.Fatalf("account pending deletion returned an existing approval: %+v", approval)
	}
	var approvalsAfter, outboxAfter string
	if err := pool.QueryRow(ctx, recordsQuery).Scan(&approvalsAfter, &outboxAfter); err != nil {
		t.Fatal(err)
	}
	if approvalsAfter != approvalsBefore {
		t.Fatalf("rejected approval changed approval records: before %s, after %s", approvalsBefore, approvalsAfter)
	}
	if outboxAfter != outboxBefore {
		t.Fatalf("rejected approval changed outbox records: before %s, after %s", outboxBefore, outboxAfter)
	}
}
