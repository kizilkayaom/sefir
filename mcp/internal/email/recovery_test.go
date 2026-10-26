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

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRecoverStaleOutboxEntriesMarksFoundMessageSent(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	config, err := pgxpool.ParseConfig(databaseURL)
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
	const rfc822MessageID = "<sefir.recovery@example.com>"
	const gmailMessageID = "gmail-recovered-123"
	var outboxID string
	err = pool.QueryRow(ctx, `
		INSERT INTO outbox (
			approval_id, state, attempts, claimed_at,
			rfc822_message_id, last_error
		)
		VALUES (gen_random_uuid(), 'sending', 1, $1, $2, 'previous send interrupted')
		RETURNING outbox_id::text
	`, cutoff.Add(-10*time.Minute), rfc822MessageID).Scan(&outboxID)
	if err != nil {
		t.Fatalf("insert stale outbox entry: %v", err)
	}

	requests := 0
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			if req.Method != http.MethodGet {
				t.Errorf("request method = %q, want GET", req.Method)
			}
			if req.URL.Scheme != "https" || req.URL.Host != "gmail.googleapis.com" {
				t.Errorf("request origin = %s://%s, want https://gmail.googleapis.com", req.URL.Scheme, req.URL.Host)
			}
			const wantPath = "/gmail/v1/users/me/messages"
			if req.URL.Path != wantPath {
				t.Errorf("request path = %q, want %q", req.URL.Path, wantPath)
			}
			query := req.URL.Query()
			if got, want := query.Get("q"), "rfc822msgid:"+rfc822MessageID; got != want {
				t.Errorf("q = %q, want %q", got, want)
			}
			if got := query.Get("labelIds"); got != "SENT" {
				t.Errorf("labelIds = %q, want SENT", got)
			}
			if got := query.Get("maxResults"); got != "2" {
				t.Errorf("maxResults = %q, want 2", got)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"messages":[{"id":"gmail-recovered-123"}]}`)),
				Request:    req,
			}, nil
		}),
	}

	entries, err := RecoverStaleOutboxEntries(ctx, pool, client, cutoff, 10)
	if err != nil {
		t.Fatalf("recover stale outbox entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.OutboxID != outboxID {
		t.Errorf("outbox ID = %q, want %q", entry.OutboxID, outboxID)
	}
	if entry.State != "sent" {
		t.Errorf("state = %q, want sent", entry.State)
	}
	if entry.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", entry.Attempts)
	}
	if entry.GmailMessageID == nil {
		t.Error("Gmail message ID is nil")
	} else if *entry.GmailMessageID != gmailMessageID {
		t.Errorf("Gmail message ID = %q, want %q", *entry.GmailMessageID, gmailMessageID)
	}
	if entry.LastError != nil {
		t.Errorf("last error = %q, want nil", *entry.LastError)
	}
	if requests != 1 {
		t.Errorf("request count = %d, want 1", requests)
	}

	var savedState string
	var savedAttempts int
	var savedGmailMessageID, savedLastError *string
	err = pool.QueryRow(ctx, `
		SELECT state, attempts, gmail_message_id, last_error
		FROM outbox
		WHERE outbox_id = $1
	`, outboxID).Scan(&savedState, &savedAttempts, &savedGmailMessageID, &savedLastError)
	if err != nil {
		t.Fatalf("read recovered outbox entry: %v", err)
	}
	if savedState != "sent" {
		t.Errorf("saved state = %q, want sent", savedState)
	}
	if savedAttempts != 1 {
		t.Errorf("saved attempts = %d, want 1", savedAttempts)
	}
	if savedGmailMessageID == nil {
		t.Error("saved Gmail message ID is nil")
	} else if *savedGmailMessageID != gmailMessageID {
		t.Errorf("saved Gmail message ID = %q, want %q", *savedGmailMessageID, gmailMessageID)
	}
	if savedLastError != nil {
		t.Errorf("saved last error = %q, want nil", *savedLastError)
	}

	entries, err = RecoverStaleOutboxEntries(ctx, pool, client, cutoff, 10)
	if err != nil {
		t.Fatalf("recover stale outbox entries again: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entry count after recovery = %d, want 0", len(entries))
	}
	if requests != 1 {
		t.Errorf("total request count = %d, want 1", requests)
	}
}

func TestRecoverStaleOutboxEntriesLeavesUnmatchedMessageUnchanged(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	config, err := pgxpool.ParseConfig(databaseURL)
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
	claimedAt := cutoff.Add(-10 * time.Minute)
	const rfc822MessageID = "<sefir.unmatched@example.com>"
	const lastError = "previous send interrupted"
	var outboxID string
	var updatedAt time.Time
	err = pool.QueryRow(ctx, `
		INSERT INTO outbox (
			approval_id, state, attempts, claimed_at,
			rfc822_message_id, last_error
		)
		VALUES (gen_random_uuid(), 'sending', 1, $1, $2, $3)
		RETURNING outbox_id::text, updated_at
	`, claimedAt, rfc822MessageID, lastError).Scan(&outboxID, &updatedAt)
	if err != nil {
		t.Fatalf("insert stale outbox entry: %v", err)
	}

	requests := 0
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			if req.Method != http.MethodGet {
				t.Errorf("request method = %q, want GET", req.Method)
			}
			const wantPath = "/gmail/v1/users/me/messages"
			if req.URL.Path != wantPath {
				t.Errorf("request path = %q, want %q", req.URL.Path, wantPath)
			}
			if got, want := req.URL.Query().Get("q"), "rfc822msgid:"+rfc822MessageID; got != want {
				t.Errorf("q = %q, want %q", got, want)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("{}")),
				Request:    req,
			}, nil
		}),
	}

	for run := 1; run <= 2; run++ {
		entries, err := RecoverStaleOutboxEntries(ctx, pool, client, cutoff, 10)
		if err != nil {
			t.Fatalf("recovery run %d: %v", run, err)
		}
		if len(entries) != 1 {
			t.Fatalf("entry count = %d, want 1", len(entries))
		}
		entry := entries[0]
		if entry.OutboxID != outboxID {
			t.Errorf("outbox ID = %q, want %q", entry.OutboxID, outboxID)
		}
		if entry.State != "sending" {
			t.Errorf("state = %q, want sending", entry.State)
		}
		if entry.Attempts != 1 {
			t.Errorf("attempts = %d, want 1", entry.Attempts)
		}
		if entry.GmailMessageID != nil {
			t.Errorf("Gmail message ID = %q, want nil", *entry.GmailMessageID)
		}
		if entry.LastError == nil || *entry.LastError != lastError {
			t.Errorf("last error must remain %q", lastError)
		}
		if entry.RFC822MessageID == nil || *entry.RFC822MessageID != rfc822MessageID {
			t.Errorf("Message-ID must remain %q", rfc822MessageID)
		}
		if entry.ClaimedAt == nil || !entry.ClaimedAt.Equal(claimedAt) {
			t.Errorf("claim time must remain %s", claimedAt)
		}
		if !entry.UpdatedAt.Equal(updatedAt) {
			t.Errorf("update time = %s, want %s", entry.UpdatedAt, updatedAt)
		}
		if requests != run {
			t.Errorf("request count = %d, want %d", requests, run)
		}

		var savedState, savedMessageID string
		var savedAttempts int
		var savedGmailMessageID, savedLastError *string
		var savedClaimedAt, savedUpdatedAt time.Time
		err = pool.QueryRow(ctx, `
			SELECT state, attempts, rfc822_message_id, gmail_message_id,
				last_error, claimed_at, updated_at
			FROM outbox
			WHERE outbox_id = $1
		`, outboxID).Scan(&savedState, &savedAttempts, &savedMessageID,
			&savedGmailMessageID, &savedLastError, &savedClaimedAt, &savedUpdatedAt)
		if err != nil {
			t.Fatalf("read unmatched outbox entry: %v", err)
		}
		if savedState != "sending" || savedAttempts != 1 {
			t.Errorf("saved state = %q, attempts = %d, want sending and 1", savedState, savedAttempts)
		}
		if savedGmailMessageID != nil {
			t.Errorf("saved Gmail message ID = %q, want nil", *savedGmailMessageID)
		}
		if savedLastError == nil || *savedLastError != lastError {
			t.Errorf("saved last error must remain %q", lastError)
		}
		if savedMessageID != rfc822MessageID {
			t.Errorf("saved Message-ID = %q, want %q", savedMessageID, rfc822MessageID)
		}
		if !savedClaimedAt.Equal(claimedAt) || !savedUpdatedAt.Equal(updatedAt) {
			t.Errorf("saved claim/update times changed: %s, %s", savedClaimedAt, savedUpdatedAt)
		}
	}
}

func TestRecoverStaleOutboxEntriesContinuesAfterFailures(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	config, err := pgxpool.ParseConfig(databaseURL)
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
	failedMessageID := "<sefir.lookup-failed@example.com>"
	foundMessageID := "<sefir.found@example.com>"
	const lastError = "previous send interrupted"
	fixtures := []struct {
		name               string
		messageID          *string
		claimedAt          time.Time
		wantState          string
		wantGmailMessageID string
		outboxID           string
	}{
		{name: "missing Message-ID", claimedAt: cutoff.Add(-30 * time.Minute), wantState: "sending"},
		{name: "lookup failure", messageID: &failedMessageID, claimedAt: cutoff.Add(-20 * time.Minute), wantState: "sending"},
		{name: "found message", messageID: &foundMessageID, claimedAt: cutoff.Add(-10 * time.Minute), wantState: "sent", wantGmailMessageID: "gmail-recovered-123"},
	}
	for i := range fixtures {
		fixture := &fixtures[i]
		err := pool.QueryRow(ctx, `
			INSERT INTO outbox (
				approval_id, state, attempts, claimed_at,
				rfc822_message_id, last_error
			)
			VALUES (gen_random_uuid(), 'sending', 1, $1, $2, $3)
			RETURNING outbox_id::text
		`, fixture.claimedAt, fixture.messageID, lastError).Scan(&fixture.outboxID)
		if err != nil {
			t.Fatalf("insert %s outbox entry: %v", fixture.name, err)
		}
	}

	lookupErr := errors.New("lookup unavailable")
	requests := 0
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			if req.Method != http.MethodGet {
				t.Errorf("request method = %q, want GET", req.Method)
			}
			const wantPath = "/gmail/v1/users/me/messages"
			if req.URL.Path != wantPath {
				t.Errorf("request path = %q, want %q", req.URL.Path, wantPath)
			}
			switch req.URL.Query().Get("q") {
			case "rfc822msgid:" + failedMessageID:
				return nil, lookupErr
			case "rfc822msgid:" + foundMessageID:
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"messages":[{"id":"gmail-recovered-123"}]}`)),
					Request:    req,
				}, nil
			default:
				t.Errorf("unexpected lookup query: %q", req.URL.Query().Get("q"))
				return nil, errors.New("unexpected Gmail lookup")
			}
		}),
	}

	entries, err := RecoverStaleOutboxEntries(ctx, pool, client, cutoff, 10)
	if !errors.Is(err, lookupErr) {
		t.Fatalf("recovery error = %v, want it to wrap %v", err, lookupErr)
	}
	for _, want := range []string{fixtures[0].outboxID, "missing its Message-ID", fixtures[1].outboxID} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("recovery error = %v, want it to contain %q", err, want)
		}
	}
	if len(entries) != len(fixtures) {
		t.Fatalf("entry count = %d, want %d", len(entries), len(fixtures))
	}
	if requests != 2 {
		t.Errorf("request count = %d, want 2", requests)
	}

	for i, fixture := range fixtures {
		entry := entries[i]
		if entry.OutboxID != fixture.outboxID {
			t.Errorf("%s: outbox ID = %q, want %q", fixture.name, entry.OutboxID, fixture.outboxID)
		}
		if entry.State != fixture.wantState || entry.Attempts != 1 {
			t.Errorf("%s: state = %q, attempts = %d, want %q and 1", fixture.name, entry.State, entry.Attempts, fixture.wantState)
		}
		if fixture.wantGmailMessageID == "" {
			if entry.GmailMessageID != nil {
				t.Errorf("%s: Gmail message ID = %q, want nil", fixture.name, *entry.GmailMessageID)
			}
			if entry.LastError == nil || *entry.LastError != lastError {
				t.Errorf("%s: last error must remain %q", fixture.name, lastError)
			}
		} else {
			if entry.GmailMessageID == nil || *entry.GmailMessageID != fixture.wantGmailMessageID {
				t.Errorf("%s: Gmail message ID must be %q", fixture.name, fixture.wantGmailMessageID)
			}
			if entry.LastError != nil {
				t.Errorf("%s: last error = %q, want nil", fixture.name, *entry.LastError)
			}
		}

		var savedState string
		var savedAttempts int
		var savedGmailMessageID, savedLastError *string
		err := pool.QueryRow(ctx, `
			SELECT state, attempts, gmail_message_id, last_error
			FROM outbox
			WHERE outbox_id = $1
		`, fixture.outboxID).Scan(&savedState, &savedAttempts, &savedGmailMessageID, &savedLastError)
		if err != nil {
			t.Fatalf("read %s outbox entry: %v", fixture.name, err)
		}
		if savedState != fixture.wantState || savedAttempts != 1 {
			t.Errorf("%s: saved state = %q, attempts = %d, want %q and 1", fixture.name, savedState, savedAttempts, fixture.wantState)
		}
		if fixture.wantGmailMessageID == "" {
			if savedGmailMessageID != nil {
				t.Errorf("%s: saved Gmail message ID = %q, want nil", fixture.name, *savedGmailMessageID)
			}
			if savedLastError == nil || *savedLastError != lastError {
				t.Errorf("%s: saved last error must remain %q", fixture.name, lastError)
			}
		} else {
			if savedGmailMessageID == nil || *savedGmailMessageID != fixture.wantGmailMessageID {
				t.Errorf("%s: saved Gmail message ID must be %q", fixture.name, fixture.wantGmailMessageID)
			}
			if savedLastError != nil {
				t.Errorf("%s: saved last error = %q, want nil", fixture.name, *savedLastError)
			}
		}
	}
}

func TestRecoverStaleOutboxEntriesContinuesAfterSaveFailure(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	config, err := pgxpool.ParseConfig(databaseURL)
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
			LIKE public.outbox INCLUDING ALL,
			CONSTRAINT reject_recovered_message
				CHECK (gmail_message_id IS DISTINCT FROM 'gmail-blocked-123')
		)
	`)
	if err != nil {
		t.Fatalf("create temporary outbox table: %v", err)
	}

	cutoff := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	const lastError = "previous send interrupted"
	fixtures := []struct {
		name               string
		messageID          string
		claimedAt          time.Time
		responseBody       string
		wantState          string
		wantGmailMessageID string
		outboxID           string
	}{
		{
			name:         "save failure",
			messageID:    "<sefir.save-failed@example.com>",
			claimedAt:    cutoff.Add(-20 * time.Minute),
			responseBody: `{"messages":[{"id":"gmail-blocked-123"}]}`,
			wantState:    "sending",
		},
		{
			name:               "successful save",
			messageID:          "<sefir.saved@example.com>",
			claimedAt:          cutoff.Add(-10 * time.Minute),
			responseBody:       `{"messages":[{"id":"gmail-recovered-123"}]}`,
			wantState:          "sent",
			wantGmailMessageID: "gmail-recovered-123",
		},
	}
	for i := range fixtures {
		fixture := &fixtures[i]
		err := pool.QueryRow(ctx, `
			INSERT INTO outbox (
				approval_id, state, attempts, claimed_at,
				rfc822_message_id, last_error
			)
			VALUES (gen_random_uuid(), 'sending', 1, $1, $2, $3)
			RETURNING outbox_id::text
		`, fixture.claimedAt, fixture.messageID, lastError).Scan(&fixture.outboxID)
		if err != nil {
			t.Fatalf("insert %s outbox entry: %v", fixture.name, err)
		}
	}

	requests := 0
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			if req.Method != http.MethodGet {
				t.Errorf("request method = %q, want GET", req.Method)
			}
			const wantPath = "/gmail/v1/users/me/messages"
			if req.URL.Path != wantPath {
				t.Errorf("request path = %q, want %q", req.URL.Path, wantPath)
			}
			for _, fixture := range fixtures {
				if req.URL.Query().Get("q") == "rfc822msgid:"+fixture.messageID {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(fixture.responseBody)),
						Request:    req,
					}, nil
				}
			}
			t.Errorf("unexpected lookup query: %q", req.URL.Query().Get("q"))
			return nil, errors.New("unexpected Gmail lookup")
		}),
	}

	entries, err := RecoverStaleOutboxEntries(ctx, pool, client, cutoff, 10)
	var databaseErr *pgconn.PgError
	if !errors.As(err, &databaseErr) {
		t.Fatalf("recovery error = %v, want a wrapped PostgreSQL error", err)
	}
	if databaseErr.Code != "23514" || databaseErr.ConstraintName != "reject_recovered_message" {
		t.Errorf("database error = %v, want reject_recovered_message check violation", databaseErr)
	}
	for _, want := range []string{fixtures[0].outboxID, "gmail-blocked-123"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("recovery error = %v, want it to contain %q", err, want)
		}
	}
	if len(entries) != len(fixtures) {
		t.Fatalf("entry count = %d, want %d", len(entries), len(fixtures))
	}
	if requests != 2 {
		t.Errorf("request count = %d, want 2", requests)
	}

	for i, fixture := range fixtures {
		entry := entries[i]
		if entry.OutboxID != fixture.outboxID {
			t.Errorf("%s: outbox ID = %q, want %q", fixture.name, entry.OutboxID, fixture.outboxID)
		}
		if entry.State != fixture.wantState || entry.Attempts != 1 {
			t.Errorf("%s: state = %q, attempts = %d, want %q and 1", fixture.name, entry.State, entry.Attempts, fixture.wantState)
		}
		if fixture.wantGmailMessageID == "" {
			if entry.GmailMessageID != nil {
				t.Errorf("%s: Gmail message ID = %q, want nil", fixture.name, *entry.GmailMessageID)
			}
			if entry.LastError == nil || *entry.LastError != lastError {
				t.Errorf("%s: last error must remain %q", fixture.name, lastError)
			}
		} else {
			if entry.GmailMessageID == nil || *entry.GmailMessageID != fixture.wantGmailMessageID {
				t.Errorf("%s: Gmail message ID must be %q", fixture.name, fixture.wantGmailMessageID)
			}
			if entry.LastError != nil {
				t.Errorf("%s: last error = %q, want nil", fixture.name, *entry.LastError)
			}
		}

		var savedState string
		var savedAttempts int
		var savedGmailMessageID, savedLastError *string
		err := pool.QueryRow(ctx, `
			SELECT state, attempts, gmail_message_id, last_error
			FROM outbox
			WHERE outbox_id = $1
		`, fixture.outboxID).Scan(&savedState, &savedAttempts, &savedGmailMessageID, &savedLastError)
		if err != nil {
			t.Fatalf("read %s outbox entry: %v", fixture.name, err)
		}
		if savedState != fixture.wantState || savedAttempts != 1 {
			t.Errorf("%s: saved state = %q, attempts = %d, want %q and 1", fixture.name, savedState, savedAttempts, fixture.wantState)
		}
		if fixture.wantGmailMessageID == "" {
			if savedGmailMessageID != nil {
				t.Errorf("%s: saved Gmail message ID = %q, want nil", fixture.name, *savedGmailMessageID)
			}
			if savedLastError == nil || *savedLastError != lastError {
				t.Errorf("%s: saved last error must remain %q", fixture.name, lastError)
			}
		} else {
			if savedGmailMessageID == nil || *savedGmailMessageID != fixture.wantGmailMessageID {
				t.Errorf("%s: saved Gmail message ID must be %q", fixture.name, fixture.wantGmailMessageID)
			}
			if savedLastError != nil {
				t.Errorf("%s: saved last error = %q, want nil", fixture.name, *savedLastError)
			}
		}
	}
}

func TestRecoverStaleOutboxEntriesHandlesCancellation(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	tests := []struct {
		name               string
		confirmMatch       bool
		wantState          string
		wantGmailMessageID string
	}{
		{name: "during lookup", wantState: "sending"},
		{name: "after confirmed match", confirmMatch: true, wantState: "sent", wantGmailMessageID: "gmail-recovered-123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := pgxpool.ParseConfig(databaseURL)
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
			const lastError = "previous send interrupted"
			fixtures := []struct {
				name               string
				messageID          string
				claimedAt          time.Time
				wantState          string
				wantGmailMessageID string
				outboxID           string
			}{
				{
					name:               "first entry",
					messageID:          "<sefir.first@example.com>",
					claimedAt:          cutoff.Add(-20 * time.Minute),
					wantState:          tt.wantState,
					wantGmailMessageID: tt.wantGmailMessageID,
				},
				{
					name:      "unprocessed entry",
					messageID: "<sefir.second@example.com>",
					claimedAt: cutoff.Add(-10 * time.Minute),
					wantState: "sending",
				},
			}
			for i := range fixtures {
				fixture := &fixtures[i]
				err := pool.QueryRow(ctx, `
					INSERT INTO outbox (
						approval_id, state, attempts, claimed_at,
						rfc822_message_id, last_error
					)
					VALUES (gen_random_uuid(), 'sending', 1, $1, $2, $3)
					RETURNING outbox_id::text
				`, fixture.claimedAt, fixture.messageID, lastError).Scan(&fixture.outboxID)
				if err != nil {
					t.Fatalf("insert %s outbox entry: %v", fixture.name, err)
				}
			}

			recoveryCtx, cancelRecovery := context.WithCancel(ctx)
			defer cancelRecovery()
			requests := 0
			client := &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					requests++
					if req.Method != http.MethodGet {
						t.Errorf("request method = %q, want GET", req.Method)
					}
					const wantPath = "/gmail/v1/users/me/messages"
					if req.URL.Path != wantPath {
						t.Errorf("request path = %q, want %q", req.URL.Path, wantPath)
					}
					if got, want := req.URL.Query().Get("q"), "rfc822msgid:"+fixtures[0].messageID; got != want {
						t.Errorf("q = %q, want %q", got, want)
					}
					cancelRecovery()
					if !tt.confirmMatch {
						return nil, req.Context().Err()
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"messages":[{"id":"gmail-recovered-123"}]}`)),
						Request:    req,
					}, nil
				}),
			}

			entries, err := RecoverStaleOutboxEntries(recoveryCtx, pool, client, cutoff, 10)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("recovery error = %v, want context.Canceled", err)
			}
			if len(entries) != len(fixtures) {
				t.Fatalf("entry count = %d, want %d", len(entries), len(fixtures))
			}
			if requests != 1 {
				t.Errorf("request count = %d, want 1", requests)
			}

			for i, fixture := range fixtures {
				entry := entries[i]
				if entry.OutboxID != fixture.outboxID {
					t.Errorf("%s: outbox ID = %q, want %q", fixture.name, entry.OutboxID, fixture.outboxID)
				}
				if entry.State != fixture.wantState || entry.Attempts != 1 {
					t.Errorf("%s: state = %q, attempts = %d, want %q and 1", fixture.name, entry.State, entry.Attempts, fixture.wantState)
				}
				if fixture.wantGmailMessageID == "" {
					if entry.GmailMessageID != nil {
						t.Errorf("%s: Gmail message ID = %q, want nil", fixture.name, *entry.GmailMessageID)
					}
					if entry.LastError == nil || *entry.LastError != lastError {
						t.Errorf("%s: last error must remain %q", fixture.name, lastError)
					}
				} else {
					if entry.GmailMessageID == nil || *entry.GmailMessageID != fixture.wantGmailMessageID {
						t.Errorf("%s: Gmail message ID must be %q", fixture.name, fixture.wantGmailMessageID)
					}
					if entry.LastError != nil {
						t.Errorf("%s: last error = %q, want nil", fixture.name, *entry.LastError)
					}
				}

				var savedState string
				var savedAttempts int
				var savedGmailMessageID, savedLastError *string
				err := pool.QueryRow(ctx, `
					SELECT state, attempts, gmail_message_id, last_error
					FROM outbox
					WHERE outbox_id = $1
				`, fixture.outboxID).Scan(&savedState, &savedAttempts, &savedGmailMessageID, &savedLastError)
				if err != nil {
					t.Fatalf("read %s outbox entry: %v", fixture.name, err)
				}
				if savedState != fixture.wantState || savedAttempts != 1 {
					t.Errorf("%s: saved state = %q, attempts = %d, want %q and 1", fixture.name, savedState, savedAttempts, fixture.wantState)
				}
				if fixture.wantGmailMessageID == "" {
					if savedGmailMessageID != nil {
						t.Errorf("%s: saved Gmail message ID = %q, want nil", fixture.name, *savedGmailMessageID)
					}
					if savedLastError == nil || *savedLastError != lastError {
						t.Errorf("%s: saved last error must remain %q", fixture.name, lastError)
					}
				} else {
					if savedGmailMessageID == nil || *savedGmailMessageID != fixture.wantGmailMessageID {
						t.Errorf("%s: saved Gmail message ID must be %q", fixture.name, fixture.wantGmailMessageID)
					}
					if savedLastError != nil {
						t.Errorf("%s: saved last error = %q, want nil", fixture.name, *savedLastError)
					}
				}
			}
		})
	}
}

func TestRecoverStaleOutboxEntriesRejectsInvalidArguments(t *testing.T) {
	cutoff := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		nilPool   bool
		nilClient bool
		cutoff    time.Time
		limit     int
		wantError string
	}{
		{name: "nil pool", nilPool: true, cutoff: cutoff, limit: 10, wantError: "database pool is required"},
		{name: "nil client", nilClient: true, cutoff: cutoff, limit: 10, wantError: "HTTP client is required"},
		{name: "zero cutoff", limit: 10, wantError: "list stale outbox entries: claimed_before is required"},
		{name: "zero limit", cutoff: cutoff, limit: 0, wantError: "list stale outbox entries: limit must be between 1 and 100"},
		{name: "negative limit", cutoff: cutoff, limit: -1, wantError: "list stale outbox entries: limit must be between 1 and 100"},
		{name: "limit too large", cutoff: cutoff, limit: 101, wantError: "list stale outbox entries: limit must be between 1 and 100"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := &pgxpool.Pool{}
			if tt.nilPool {
				pool = nil
			}
			requests := 0
			client := &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					requests++
					return nil, errors.New("unexpected Gmail request")
				}),
			}
			if tt.nilClient {
				client = nil
			}

			entries, err := RecoverStaleOutboxEntries(context.Background(), pool, client, tt.cutoff, tt.limit)
			if err == nil {
				t.Fatal("recovery error = nil, want an error for the invalid argument")
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Errorf("recovery error = %v, want it to contain %q", err, tt.wantError)
			}
			if entries != nil {
				t.Errorf("entries = %+v, want nil", entries)
			}
			if requests != 0 {
				t.Errorf("request count = %d, want 0", requests)
			}
		})
	}
}
