package email

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kizilkayaom/sefir/mcp/internal/catalogue"
	"github.com/kizilkayaom/sefir/mcp/internal/workspace"
	"golang.org/x/oauth2"
)

func TestWorkspaceDeliveryAndRecoveryUseOwningMailbox(t *testing.T) {
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
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "credentials-gmail.json"),
		[]byte(`{"web":{"client_id":"test-client","client_secret":"test-secret","auth_uri":"https://accounts.google.com/o/oauth2/auth","token_uri":"https://oauth2.googleapis.com/token","redirect_uris":["http://localhost:8080/oauth/google/callback"]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	accountIDs := []string{workspace.LegacyAccountID, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"}
	messageOwners := map[string]string{}
	sends, lookups := map[string]int{}, map[string]int{}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token != "mailbox-0" && token != "mailbox-1" {
			t.Fatalf("unexpected Gmail token %q", token)
		}
		if r.URL.Host != "gmail.googleapis.com" {
			t.Fatalf("unexpected network request: %s", r.URL.Host)
		}
		var body string
		switch r.URL.Path {
		case "/gmail/v1/users/me/profile":
			body = fmt.Sprintf(`{"emailAddress":%q}`, token+"@example.com")
		case "/gmail/v1/users/me/messages/send":
			var input struct{ Raw string }
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			raw, err := base64.RawURLEncoding.DecodeString(input.Raw)
			if err != nil {
				t.Fatal(err)
			}
			message, err := mail.ReadMessage(strings.NewReader(string(raw)))
			if err != nil {
				t.Fatal(err)
			}
			from, err := mail.ParseAddress(message.Header.Get("From"))
			if err != nil || from.Address != token+"@example.com" {
				t.Fatalf("sender does not match mailbox: %s", message.Header.Get("From"))
			}
			sends[token]++
			body = fmt.Sprintf(`{"id":%q}`, "sent-"+token)
		case "/gmail/v1/users/me/messages":
			messageID := strings.TrimPrefix(r.URL.Query().Get("q"), "rfc822msgid:")
			if messageOwners[messageID] != token {
				t.Fatalf("recovery searched another account's mailbox for %s", messageID)
			}
			lookups[token]++
			body = fmt.Sprintf(`{"messages":[{"id":%q}]}`, "recovered-"+token)
		default:
			t.Fatalf("unexpected Gmail path: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	defer func() { http.DefaultTransport = previousTransport }()
	resolve := WorkspaceDeliveryAccounts(pool, root)
	var recoveryIDs []string
	for i, accountID := range accountIDs {
		tokenName := fmt.Sprintf("mailbox-%d", i)
		if _, err := pool.Exec(ctx, "INSERT INTO accounts (account_id, username) VALUES ($1, $2)", accountID, tokenName); err != nil {
			t.Fatal(err)
		}
		if i < 2 {
			path, err := workspace.GmailTokenPath(root, accountID)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := SaveOAuthToken(path, &oauth2.Token{AccessToken: tokenName, RefreshToken: "refresh-" + tokenName, TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
		}
		var contactID string
		if err := pool.QueryRow(ctx, "INSERT INTO contacts (name, email, status, account_id) VALUES ('Recipient', 'recipient@example.com', 'confirmed', $1) RETURNING id::text", accountID).Scan(&contactID); err != nil {
			t.Fatal(err)
		}
		for j := range 2 {
			draft, err := catalogue.CreateDraft(ctx, pool, catalogue.CreateDraftInput{ContactID: contactID, Subject: "Test", Body: "Test body"})
			if err != nil {
				t.Fatal(err)
			}
			approval, err := ApproveDraft(ctx, pool, ApproveDraftInput{
				ApprovalID: fmt.Sprintf("cccccccc-cccc-4ccc-8ccc-%012d", i*2+j),
				DraftID:    draft.DraftID, Version: 1, ContentHash: draft.ContentHash, ApprovedBy: tokenName,
			})
			if err != nil {
				t.Fatal(err)
			}
			if j == 0 {
				account, err := resolve(ctx, approval.OutboxID)
				if i == 2 {
					if err == nil {
						t.Fatal("new account fell back to the legacy Gmail token")
					}
					continue
				}
				if err != nil || account.From != tokenName+"@example.com" {
					t.Fatalf("wrong sending account: %+v %v", account, err)
				}
				entry, err := SendOutboxMessage(ctx, pool, account.Client, approval.OutboxID, account.From, account.MessageIDDomain)
				if err != nil || entry.State != "sent" {
					t.Fatalf("send: %+v %v", entry, err)
				}
				entry, err = SendOutboxMessage(ctx, pool, account.Client, approval.OutboxID, account.From, account.MessageIDDomain)
				if err != nil || entry.State != "sent" || sends[tokenName] != 1 {
					t.Fatalf("retry sent twice: %+v %v", entry, err)
				}
			} else {
				entry, claimed, err := ClaimOutbox(ctx, pool, approval.OutboxID, "example.com")
				if err != nil || !claimed || entry.RFC822MessageID == nil {
					t.Fatalf("claim: %+v %v", entry, err)
				}
				messageOwners[*entry.RFC822MessageID] = tokenName
				recoveryIDs = append(recoveryIDs, entry.OutboxID)
				if _, err := pool.Exec(ctx, "UPDATE outbox SET claimed_at=now()-interval '1 hour' WHERE outbox_id=$1", entry.OutboxID); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	entries, err := RecoverStaleOutboxEntriesWithClients(ctx, pool, func(ctx context.Context, id string) (*http.Client, error) {
		account, err := resolve(ctx, id)
		return account.Client, err
	}, time.Now().Add(-time.Minute), 10)
	if err == nil || !strings.Contains(err.Error(), recoveryIDs[2]) || len(entries) != 3 {
		t.Fatalf("missing account recovery result: %+v %v", entries, err)
	}
	for _, entry := range entries {
		want := "sent"
		if entry.OutboxID == recoveryIDs[2] {
			want = "sending"
		}
		if entry.State != want || entry.Attempts != 1 {
			t.Fatalf("recovery changed delivery incorrectly: %+v", entry)
		}
	}
	if lookups["mailbox-0"] != 1 || lookups["mailbox-1"] != 1 {
		t.Fatalf("recovery did not check both mailboxes: %+v", lookups)
	}
	if _, err := resolve(ctx, "ffffffff-ffff-4fff-8fff-ffffffffffff"); err == nil {
		t.Fatal("resolved a nonexistent outbox entry")
	}
}

func TestWorkspaceDeliveryAccountsRejectsPendingDeletion(t *testing.T) {
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
	approval, err := ApproveDraft(ctx, pool, ApproveDraftInput{
		ApprovalID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		DraftID:    draft.DraftID, Version: draft.Version, ContentHash: draft.ContentHash, ApprovedBy: "pending-owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "credentials-gmail.json"),
		[]byte(`{"web":{"client_id":"test-client","client_secret":"test-secret","auth_uri":"https://accounts.google.com/o/oauth2/auth","token_uri":"https://oauth2.googleapis.com/token","redirect_uris":["http://localhost:8080/oauth/google/callback"]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	tokenPath, err := workspace.GmailTokenPath(root, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := SaveOAuthToken(tokenPath, &oauth2.Token{
		AccessToken: "test-token", RefreshToken: "test-refresh", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	requests := 0
	previousTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Method != http.MethodGet || r.URL.Host != "gmail.googleapis.com" ||
			r.URL.Path != "/gmail/v1/users/me/profile" || r.Header.Get("Authorization") != "Bearer test-token" {
			return nil, errors.New("unexpected Gmail request")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"emailAddress":"sender@example.com"}`)),
			Request:    r,
		}, nil
	})
	defer func() { http.DefaultTransport = previousTransport }()
	resolve := WorkspaceDeliveryAccounts(pool, root)
	account, err := resolve(ctx, approval.OutboxID)
	if err != nil {
		t.Fatal(err)
	}
	if account.Client == nil || account.From != "sender@example.com" || account.MessageIDDomain != "example.com" {
		t.Fatalf("active account resolution: %+v", account)
	}
	if requests != 1 {
		t.Fatalf("active account made %d Gmail requests, want 1", requests)
	}
	result, err := pool.Exec(ctx, "UPDATE accounts SET deletion_requested_at=now() WHERE account_id=$1", accountID)
	if err != nil {
		t.Fatal(err)
	}
	if result.RowsAffected() != 1 {
		t.Fatalf("marked %d accounts for deletion, want 1", result.RowsAffected())
	}
	entry, err := SendOutboxMessage(
		ctx, pool, account.Client, approval.OutboxID,
		account.From, account.MessageIDDomain,
	)
	if err == nil || !strings.Contains(err.Error(), "account is pending deletion") {
		t.Fatalf("expected delivery to reject an account pending deletion, got %v", err)
	}
	if entry != (OutboxEntry{}) {
		t.Fatalf("delivery returned an outbox entry for an account pending deletion: %+v", entry)
	}
	if requests != 1 {
		t.Fatalf("delivery made a Gmail request after deletion was requested: got %d requests, want 1", requests)
	}
	account, err = resolve(ctx, approval.OutboxID)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected pgx.ErrNoRows for an account pending deletion, got %v", err)
	}
	if account != (DeliveryAccount{}) {
		t.Fatalf("account pending deletion returned delivery credentials: %+v", account)
	}
	if requests != 1 {
		t.Fatalf("Gmail request count changed after deletion was requested: got %d, want 1", requests)
	}
	var state string
	var attempts int
	if err := pool.QueryRow(ctx, "SELECT state, attempts FROM outbox WHERE outbox_id=$1", approval.OutboxID).Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "pending" || attempts != 0 {
		t.Fatalf("outbox changed after deletion was requested: state %q, attempts %d", state, attempts)
	}
}
