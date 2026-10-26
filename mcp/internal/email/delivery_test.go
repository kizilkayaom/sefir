package email

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
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

func TestSendOutboxMessageConcurrentSendsOnlyOnce(t *testing.T) {
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

	var requests atomic.Int32
	gmailStarted := make(chan struct{}, 1)
	releaseGmail := make(chan struct{})

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			count := requests.Add(1)
			if req.Body == nil {
				return nil, errors.New("Gmail send request is missing its body")
			}
			defer req.Body.Close()

			if count == 1 {
				gmailStarted <- struct{}{}
			}
			select {
			case <-releaseGmail:
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					`{"id":"gmail-message-123","threadId":"gmail-thread-456"}`,
				)),
				Request: req,
			}, nil
		}),
	}

	type sendResult struct {
		entry OutboxEntry
		err   error
	}

	firstResult := make(chan sendResult, 1)
	firstDone := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()

		select {
		case <-firstDone:
		case <-timer.C:
			t.Error("first send did not stop after cancellation")
		}
	})

	go func() {
		defer close(firstDone)
		entry, err := SendOutboxMessage(
			ctx, pool, client, approval.OutboxID,
			"sender@example.com", "example.com",
		)
		firstResult <- sendResult{entry: entry, err: err}
	}()

	select {
	case <-gmailStarted:
	case result := <-firstResult:
		t.Fatalf("first send returned before reaching Gmail: entry = %+v, error = %v", result.entry, result.err)
	case <-ctx.Done():
		t.Fatalf("wait for first Gmail request: %v", ctx.Err())
	}

	secondEntry, err := SendOutboxMessage(
		ctx, pool, client, approval.OutboxID,
		"sender@example.com", "example.com",
	)
	if err != nil {
		t.Fatalf("second send: %v", err)
	}
	if secondEntry.OutboxID != approval.OutboxID {
		t.Errorf("second send: outbox ID = %q, want %q", secondEntry.OutboxID, approval.OutboxID)
	}
	if secondEntry.State != "sending" {
		t.Errorf("second send: outbox state = %q, want %q", secondEntry.State, "sending")
	}
	if secondEntry.Attempts != 1 {
		t.Errorf("second send: attempts = %d, want 1", secondEntry.Attempts)
	}
	if secondEntry.GmailMessageID != nil {
		t.Errorf("second send: Gmail message ID = %q, want nil while the first send is paused", *secondEntry.GmailMessageID)
	}
	if count := requests.Load(); count != 1 {
		t.Errorf("Gmail request count while the first send is paused = %d, want 1", count)
	}

	close(releaseGmail)

	var first sendResult
	select {
	case first = <-firstResult:
	case <-ctx.Done():
		t.Fatalf("wait for first send result: %v", ctx.Err())
	}
	if first.err != nil {
		t.Fatalf("first send: %v", first.err)
	}
	if first.entry.OutboxID != approval.OutboxID {
		t.Errorf("first send: outbox ID = %q, want %q", first.entry.OutboxID, approval.OutboxID)
	}
	if first.entry.State != "sent" {
		t.Errorf("first send: outbox state = %q, want %q", first.entry.State, "sent")
	}
	if first.entry.Attempts != 1 {
		t.Errorf("first send: attempts = %d, want 1", first.entry.Attempts)
	}
	if first.entry.GmailMessageID == nil {
		t.Fatal("first send: Gmail message ID is missing")
	}
	if *first.entry.GmailMessageID != "gmail-message-123" {
		t.Errorf("first send: Gmail message ID = %q, want %q", *first.entry.GmailMessageID, "gmail-message-123")
	}

	var state string
	var attempts int
	var gmailMessageID *string
	err = pool.QueryRow(ctx, `
		SELECT state, attempts, gmail_message_id
		FROM outbox
		WHERE outbox_id = $1
	`, approval.OutboxID).Scan(&state, &attempts, &gmailMessageID)
	if err != nil {
		t.Fatalf("read final outbox entry: %v", err)
	}
	if state != "sent" {
		t.Errorf("stored outbox state = %q, want %q", state, "sent")
	}
	if attempts != 1 {
		t.Errorf("stored attempts = %d, want 1", attempts)
	}
	if gmailMessageID == nil {
		t.Fatal("stored Gmail message ID is missing")
	}
	if *gmailMessageID != "gmail-message-123" {
		t.Errorf("stored Gmail message ID = %q, want %q", *gmailMessageID, "gmail-message-123")
	}
	if count := requests.Load(); count != 1 {
		t.Errorf("Gmail request count = %d, want 1", count)
	}
}

func TestSendOutboxMessageRejectedSendIsNotRetried(t *testing.T) {
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
		t.Fatalf("outbox state = %q, want %q", approval.OutboxState, "pending")
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
				StatusCode: http.StatusForbidden,
				Body: io.NopCloser(strings.NewReader(
					`{"error":{"code":403,"message":"permission denied","status":"PERMISSION_DENIED"}}`,
				)),
				Request: req,
			}, nil
		}),
	}

	var savedError string
	for i := 0; i < 2; i++ {
		entry, err := SendOutboxMessage(
			ctx, pool, client, approval.OutboxID,
			"sender@example.com", "example.com",
		)
		if err != nil {
			t.Fatalf("send call %d: %v", i+1, err)
		}
		if entry.OutboxID != approval.OutboxID {
			t.Errorf("send call %d: outbox ID = %q, want %q", i+1, entry.OutboxID, approval.OutboxID)
		}
		if entry.State != "failed" {
			t.Errorf("send call %d: outbox state = %q, want %q", i+1, entry.State, "failed")
		}
		if entry.Attempts != 1 {
			t.Errorf("send call %d: attempts = %d, want 1", i+1, entry.Attempts)
		}
		if entry.GmailMessageID != nil {
			t.Errorf("send call %d: Gmail message ID = %q, want nil", i+1, *entry.GmailMessageID)
		}
		if entry.LastError == nil {
			t.Fatalf("send call %d: last error is missing", i+1)
		}
		if !strings.Contains(*entry.LastError, "HTTP 403") || !strings.Contains(*entry.LastError, "permission denied") {
			t.Errorf("send call %d: last error = %q, want HTTP 403 and permission denied", i+1, *entry.LastError)
		}
		if i == 0 {
			savedError = *entry.LastError
		} else if *entry.LastError != savedError {
			t.Errorf("second send: last error = %q, want original error %q", *entry.LastError, savedError)
		}
		if requests != 1 {
			t.Errorf("send call %d: Gmail request count = %d, want 1", i+1, requests)
		}
	}

	var state string
	var attempts int
	var gmailMessageID *string
	var lastError *string
	err = pool.QueryRow(ctx, `
		SELECT state, attempts, gmail_message_id, last_error
		FROM outbox
		WHERE outbox_id = $1
	`, approval.OutboxID).Scan(&state, &attempts, &gmailMessageID, &lastError)
	if err != nil {
		t.Fatalf("read final outbox entry: %v", err)
	}
	if state != "failed" {
		t.Errorf("stored outbox state = %q, want %q", state, "failed")
	}
	if attempts != 1 {
		t.Errorf("stored attempts = %d, want 1", attempts)
	}
	if gmailMessageID != nil {
		t.Errorf("stored Gmail message ID = %q, want nil", *gmailMessageID)
	}
	if lastError == nil {
		t.Fatal("stored last error is missing")
	}
	if *lastError != savedError {
		t.Errorf("stored last error = %q, want %q", *lastError, savedError)
	}
}

func TestSendOutboxMessageTimeoutIsNotRetried(t *testing.T) {
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
		t.Fatalf("outbox state = %q, want %q", approval.OutboxState, "pending")
	}

	requests := 0
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			if req.Body == nil {
				return nil, errors.New("Gmail send request is missing its body")
			}
			defer req.Body.Close()
			return nil, context.DeadlineExceeded
		}),
	}

	var savedError string
	for i := 0; i < 2; i++ {
		entry, err := SendOutboxMessage(
			ctx, pool, client, approval.OutboxID,
			"sender@example.com", "example.com",
		)
		if err != nil {
			t.Fatalf("send call %d: %v", i+1, err)
		}
		if err := ctx.Err(); err != nil {
			t.Fatalf("send call %d: test context unexpectedly ended: %v", i+1, err)
		}
		if entry.OutboxID != approval.OutboxID {
			t.Errorf("send call %d: outbox ID = %q, want %q", i+1, entry.OutboxID, approval.OutboxID)
		}
		if entry.State != "needs_attention" {
			t.Errorf("send call %d: outbox state = %q, want %q", i+1, entry.State, "needs_attention")
		}
		if entry.Attempts != 1 {
			t.Errorf("send call %d: attempts = %d, want 1", i+1, entry.Attempts)
		}
		if entry.GmailMessageID != nil {
			t.Errorf("send call %d: Gmail message ID = %q, want nil", i+1, *entry.GmailMessageID)
		}
		if entry.LastError == nil {
			t.Fatalf("send call %d: last error is missing", i+1)
		}
		if !strings.Contains(*entry.LastError, context.DeadlineExceeded.Error()) {
			t.Errorf("send call %d: last error = %q, want it to contain %q", i+1, *entry.LastError, context.DeadlineExceeded.Error())
		}
		if i == 0 {
			savedError = *entry.LastError
		} else if *entry.LastError != savedError {
			t.Errorf("second send: last error = %q, want original error %q", *entry.LastError, savedError)
		}
		if requests != 1 {
			t.Errorf("send call %d: Gmail request count = %d, want 1", i+1, requests)
		}
	}

	var state string
	var attempts int
	var gmailMessageID *string
	var lastError *string
	err = pool.QueryRow(ctx, `
		SELECT state, attempts, gmail_message_id, last_error
		FROM outbox
		WHERE outbox_id = $1
	`, approval.OutboxID).Scan(&state, &attempts, &gmailMessageID, &lastError)
	if err != nil {
		t.Fatalf("read final outbox entry: %v", err)
	}
	if state != "needs_attention" {
		t.Errorf("stored outbox state = %q, want %q", state, "needs_attention")
	}
	if attempts != 1 {
		t.Errorf("stored attempts = %d, want 1", attempts)
	}
	if gmailMessageID != nil {
		t.Errorf("stored Gmail message ID = %q, want nil", *gmailMessageID)
	}
	if lastError == nil {
		t.Fatal("stored last error is missing")
	}
	if *lastError != savedError {
		t.Errorf("stored last error = %q, want %q", *lastError, savedError)
	}
}
