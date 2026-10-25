package email

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kizilkayaom/sefir/mcp/internal/catalogue"
)

func TestSendOutboxMessageRejectsEmptyOutboxID(t *testing.T) {
	requests := 0
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			if req.Body == nil {
				return nil, errors.New("Invalid request body")
			}
			defer req.Body.Close()
			return nil, errors.New("unexpected Gmail request")
		}),
	}

	pool := &pgxpool.Pool{}
	entry, err := SendOutboxMessage(
		context.Background(), pool, client, "",
		"sender@example.com", "example.com",
	)
	if err == nil {
		t.Fatal("expected an error for an empty outbox ID")
	}

	if !strings.Contains(err.Error(), "outbox_id is required") {
		t.Errorf("delivery error = %q, want it to contain %q", err.Error(), "outbox_id is required")
	}
	if entry != (OutboxEntry{}) {
		t.Errorf("outbox entry = %+v, want an empty entry", entry)
	}
	if requests != 0 {
		t.Errorf("Gmail request count = %d, want 0", requests)
	}
}

func TestSendOutboxMessageSendsOnlyOnce(t *testing.T) {
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
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open test database pool: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("connect to test database: %v", err)
	}

	var contactID string

	err = pool.QueryRow(ctx, `
		INSERT INTO contacts (name, email, status)
		VALUES ('Integration test', 'recipient@example.com', 'confirmed')
		RETURNING id::text
	`).Scan(&contactID)

	if err != nil {
		t.Fatalf("insert test contact: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if _, err := pool.Exec(cleanupCtx, "DELETE FROM contacts WHERE id = $1", contactID); err != nil {
			t.Errorf("delete test contact: %v", err)
		}
	})

	draft, err := catalogue.CreateDraft(ctx, pool, catalogue.CreateDraftInput{
		ContactID: contactID,
		Subject:   "Integration test",
		Body:      "This is a test email.",
	})

	if err != nil {
		t.Fatalf("create test draft: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if _, err := pool.Exec(cleanupCtx,
			"DELETE FROM drafts WHERE draft_id = $1 AND version = $2",
			draft.DraftID, draft.Version,
		); err != nil {
			t.Errorf("delete test draft: %v", err)
		}
	})

	var approvalID string
	if err := pool.QueryRow(ctx, "SELECT gen_random_uuid()::text").Scan(&approvalID); err != nil {
		t.Fatalf("generate test approval ID: %v", err)
	}

	approval, err := ApproveDraft(ctx, pool, ApproveDraftInput{
		ApprovalID:  approvalID,
		DraftID:     draft.DraftID,
		Version:     draft.Version,
		ContentHash: draft.ContentHash,
		ApprovedBy:  "integration-test",
	})
	if err != nil {
		t.Fatalf("approve test draft: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if _, err := pool.Exec(cleanupCtx, "DELETE FROM outbox WHERE approval_id = $1", approvalID); err != nil {
			t.Errorf("delete test outbox entry: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, "DELETE FROM approvals WHERE approval_id = $1", approvalID); err != nil {
			t.Errorf("delete test approval: %v", err)
		}
	})

	if approval.OutboxID == "" {
		t.Fatal("approval is missing its outbox ID")
	}
	if approval.OutboxState != "pending" {
		t.Errorf("outbox state = %q, want %q", approval.OutboxState, "pending")
	}

	requests := 0

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			if req.Body == nil {
				return nil, errors.New("Gmail send request is missing its body")
			}
			defer req.Body.Close()
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					`{"id":"gmail-message-123","threadId":"gmail-thread-456"}`,
				)),
				Request: req,
			}, nil
		}),
	}

	for i := 0; i < 2; i++ {
		entry, err := SendOutboxMessage(
			ctx, pool, client, approval.OutboxID,
			"sender@example.com", "example.com",
		)
		if err != nil {
			t.Fatalf("send call %d: %v", i+1, err)
		}
		if entry.State != "sent" {
			t.Errorf("send call %d: outbox state = %q, want %q", i+1, entry.State, "sent")
		}
		if entry.Attempts != 1 {
			t.Errorf("send call %d: attempts = %d, want 1", i+1, entry.Attempts)
		}
		if entry.GmailMessageID == nil {
			t.Fatalf("send call %d: Gmail message ID is missing", i+1)
		}
		if *entry.GmailMessageID != "gmail-message-123" {
			t.Errorf("send call %d: Gmail message ID = %q, want %q", i+1, *entry.GmailMessageID, "gmail-message-123")
		}
	}
	if requests != 1 {
		t.Errorf("Gmail request count = %d, want 1", requests)
	}
}
