package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kizilkayaom/sefir/mcp/internal/catalogue"
	"github.com/kizilkayaom/sefir/mcp/internal/email"
	"github.com/kizilkayaom/sefir/mcp/internal/workspace"
)

func accountTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	for _, table := range []string{"accounts", "contacts", "drafts", "approvals", "outbox"} {
		if _, err := pool.Exec(ctx, "CREATE TEMP TABLE "+table+" (LIKE public."+table+" INCLUDING ALL)"); err != nil {
			t.Fatal(err)
		}
	}
	return pool
}

func TestPostgresAccountsKeepIdentitiesSeparate(t *testing.T) {
	pool := accountTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	accounts := PostgresAccounts{Pool: pool}
	password, err := newOwner("Owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := accounts.Create(ctx, *password)
	if err != nil {
		t.Fatal(err)
	}
	found, err := accounts.ByUsername(ctx, " owner ")
	if err != nil || found.ID != owner.ID || !found.matches("OWNER", testPassword) {
		t.Fatalf("password login: %+v %v", found, err)
	}
	duplicate := *password
	duplicate.Username = "owner"
	if _, err := accounts.Create(ctx, duplicate); !errors.Is(err, ErrAccountConflict) {
		t.Fatalf("case-insensitive duplicate accepted: %v", err)
	}
	identity := GoogleIdentity{Subject: "google-one", Email: "owner@example.com"}
	google, err := accounts.Google(ctx, identity)
	if err != nil || google.ID == owner.ID || len(google.Hash) != 0 || google.Username == owner.Username {
		t.Fatalf("Google claimed a password account: %+v %v", google, err)
	}
	existing, err := accounts.ByGoogleSubject(ctx, identity.Subject)
	if err != nil || existing.ID != google.ID {
		t.Fatalf("existing Google account lookup: %+v %v", existing, err)
	}
	if _, err := accounts.ByGoogleSubject(ctx, "unregistered-subject"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("unregistered Google subject resolved: %v", err)
	}
	identity.Email = "renamed@example.com"
	returning, err := accounts.Google(ctx, identity)
	if err != nil || returning.ID != google.ID {
		t.Fatalf("Google identity changed with email: %+v %v", returning, err)
	}
	other, err := accounts.Google(ctx, GoogleIdentity{Subject: "google-two", Email: "owner@example.com"})
	if err != nil || other.ID == google.ID || other.ID == owner.ID {
		t.Fatalf("different subjects merged: %+v %v", other, err)
	}
	if _, err := accounts.LinkGoogle(ctx, owner.ID, identity); !errors.Is(err, ErrAccountConflict) {
		t.Fatalf("claimed another account's Google identity: %v", err)
	}
	linkedIdentity := GoogleIdentity{Subject: "google-linked", Email: "linked@example.com"}
	linked, err := accounts.LinkGoogle(ctx, owner.ID, linkedIdentity)
	if err != nil || linked.ID != owner.ID || !linked.matches("Owner", testPassword) {
		t.Fatalf("linking lost password or workspace: %+v %v", linked, err)
	}
	returning, err = accounts.Google(ctx, linkedIdentity)
	if err != nil || returning.ID != owner.ID {
		t.Fatalf("linked sign-in did not return original workspace: %+v %v", returning, err)
	}
	if _, err := accounts.LinkGoogle(ctx, owner.ID, GoogleIdentity{Subject: "replacement", Email: "replacement@example.com"}); !errors.Is(err, ErrAccountConflict) {
		t.Fatalf("replaced an existing Google identity: %v", err)
	}
	type result struct {
		account Account
		err     error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			account, err := accounts.Google(ctx, GoogleIdentity{Subject: "concurrent", Email: "same@example.com"})
			results <- result{account, err}
		}()
	}
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.account.ID != second.account.ID {
		t.Fatalf("concurrent Google signup: %+v %+v", first, second)
	}
}

func TestPostgresAccountsHideAccountsPendingDeletion(t *testing.T) {
	pool := accountTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	accounts := PostgresAccounts{Pool: pool}
	credentials, err := newOwner("PendingOwner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := accounts.Create(ctx, *credentials)
	if err != nil {
		t.Fatal(err)
	}
	identity := GoogleIdentity{Subject: "pending-google", Email: "pending@example.com"}
	if _, err := accounts.LinkGoogle(ctx, owner.ID, identity); err != nil {
		t.Fatal(err)
	}
	activeIdentity := GoogleIdentity{Subject: "active-google", Email: "active@example.com"}
	active, err := accounts.Google(ctx, activeIdentity)
	if err != nil {
		t.Fatal(err)
	}
	lookups := []struct {
		name       string
		find       func(context.Context, string) (Account, error)
		pendingKey string
		activeKey  string
	}{
		{"id", accounts.ByID, owner.ID, active.ID},
		{"username", accounts.ByUsername, " pendingowner ", " " + strings.ToUpper(active.Username) + " "},
		{"google_subject", accounts.ByGoogleSubject, identity.Subject, activeIdentity.Subject},
	}
	for _, lookup := range lookups {
		found, err := lookup.find(ctx, lookup.pendingKey)
		if err != nil || found.ID != owner.ID {
			t.Fatalf("%s lookup before deletion: account %q, error %v", lookup.name, found.ID, err)
		}
	}
	if err := accounts.RequestDeletion(ctx, owner.ID); err != nil {
		t.Fatal(err)
	}
	for _, lookup := range lookups {
		t.Run(lookup.name, func(t *testing.T) {
			if _, err := lookup.find(ctx, lookup.pendingKey); !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("account pending deletion remains accessible: %v", err)
			}
			found, err := lookup.find(ctx, lookup.activeKey)
			if err != nil || found.ID != active.ID {
				t.Fatalf("active account lookup: account %q, error %v", found.ID, err)
			}
		})
	}
}

func TestPostgresRequestDeletionPreservesTimestampAndAccountData(t *testing.T) {
	pool := accountTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	accounts := PostgresAccounts{Pool: pool}
	identity := GoogleIdentity{Subject: "deletion-request", Email: "deletion@example.com"}
	account, err := accounts.Google(ctx, identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := accounts.RequestDeletion(ctx, account.ID); err != nil {
		t.Fatal(err)
	}
	var marked bool
	if err := pool.QueryRow(ctx, "SELECT deletion_requested_at IS NOT NULL FROM accounts WHERE account_id=$1", account.ID).Scan(&marked); err != nil {
		t.Fatal(err)
	}
	if !marked {
		t.Fatal("deletion request did not mark the account")
	}
	originalTimestamp := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, "UPDATE accounts SET deletion_requested_at=$2 WHERE account_id=$1", account.ID, originalTimestamp); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := accounts.RequestDeletion(ctx, account.ID); err != nil {
			t.Fatal(err)
		}
	}
	var timestamp time.Time
	var username, subject, email string
	if err := pool.QueryRow(ctx, `
		SELECT deletion_requested_at, username, google_subject, google_email
		FROM accounts WHERE account_id=$1`, account.ID).Scan(&timestamp, &username, &subject, &email); err != nil {
		t.Fatal(err)
	}
	if !timestamp.Equal(originalTimestamp) {
		t.Fatal("repeated deletion request changed the original timestamp")
	}
	if username != account.Username || subject != identity.Subject || email != identity.Email {
		t.Fatal("deletion request changed account identity data")
	}
}

func TestPostgresRequestDeletionRejectsMissingAndReservedAccounts(t *testing.T) {
	pool := accountTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	accounts := PostgresAccounts{Pool: pool}
	if _, err := pool.Exec(ctx, `
		INSERT INTO accounts (account_id, username, legacy_reserved)
		VALUES ($1, '__legacy_owner__', true)`, workspace.LegacyAccountID); err != nil {
		t.Fatal(err)
	}
	for _, accountID := range []string{testID, workspace.LegacyAccountID} {
		if err := accounts.RequestDeletion(ctx, accountID); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("ineligible account: expected pgx.ErrNoRows, got %v", err)
		}
	}
	var unmarked bool
	if err := pool.QueryRow(ctx, "SELECT deletion_requested_at IS NULL FROM accounts WHERE account_id=$1", workspace.LegacyAccountID).Scan(&unmarked); err != nil {
		t.Fatal(err)
	}
	if !unmarked {
		t.Fatal("reserved account was marked for deletion")
	}
	if err := accounts.RequestDeletion(ctx, "invalid-uuid"); err == nil || errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("database error was not preserved: %v", err)
	}
}

func TestPostgresLegacyImportRetainsWorkspace(t *testing.T) {
	pool := accountTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := pool.Exec(ctx, "INSERT INTO accounts (account_id, username, legacy_reserved) VALUES ($1, '__legacy_owner__', true)", workspace.LegacyAccountID)
	if err != nil {
		t.Fatal(err)
	}
	var contactID string
	if err := pool.QueryRow(ctx, "INSERT INTO contacts (name, status) VALUES ('Existing contact', 'confirmed') RETURNING id::text").Scan(&contactID); err != nil {
		t.Fatal(err)
	}
	accounts := PostgresAccounts{Pool: pool}
	google, err := accounts.Google(ctx, GoogleIdentity{Subject: "newcomer", Email: "new@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	store := PostgresStore{Pool: pool}
	if contacts, err := store.Contacts(withAccount(ctx, google), 10, 0); err != nil || len(contacts) != 0 {
		t.Fatalf("signup inherited existing contacts: %+v %v", contacts, err)
	}
	if _, err := accounts.ByID(ctx, workspace.LegacyAccountID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("reserved account can sign in: %v", err)
	}
	credentials, err := newOwner("ExistingOwner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := accounts.ImportLegacy(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	owner, err := accounts.ByUsername(ctx, "ExistingOwner")
	if err != nil || owner.ID != workspace.LegacyAccountID || !owner.matches("ExistingOwner", testPassword) {
		t.Fatalf("legacy import: %+v %v", owner, err)
	}
	if contacts, err := store.Contacts(withAccount(ctx, owner), 10, 0); err != nil || len(contacts) != 1 || contacts[0].ID != contactID {
		t.Fatalf("existing workspace lost: %+v %v", contacts, err)
	}
	credentials.Username = "Replacement"
	if err := accounts.ImportLegacy(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.ByUsername(ctx, "ExistingOwner"); err != nil {
		t.Fatalf("restart overwrote account: %v", err)
	}
}

func readResponse[T any](t *testing.T, response *httptest.ResponseRecorder, status int) T {
	t.Helper()
	if response.Code != status {
		t.Fatalf("HTTP %d: %s", response.Code, response.Body.String())
	}
	var result T
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPrivateWorkspaceHTTP(t *testing.T) {
	pool := accountTestPool(t)
	config := setupConfig(t)
	config.Accounts = PostgresAccounts{Pool: pool}
	mailboxes := map[string]*fakeGmail{}
	config.GmailForAccount = func(id string) (Gmail, error) { return mailboxes[id], nil }
	bridge := &fakeBridge{}
	store := PostgresStore{Pool: pool}
	server, err := New(config, store, bridge, nil)
	if err != nil {
		t.Fatal(err)
	}
	type loginResult struct {
		ID   string `json:"account_id"`
		CSRF string `json:"csrf_token"`
	}
	var cookies []*http.Cookie
	var sessions []loginResult
	var contacts []Contact
	var drafts []catalogue.Draft
	for i, name := range []string{"First", "Second"} {
		response := request(server, "POST", "/api/signup", setupBody(name, testPassword, testPassword), nil, "")
		signedIn := readResponse[loginResult](t, response, 200)
		cookie := namedCookie(t, response, "sefir_session")
		if cookie == nil {
			t.Fatal("missing signup cookie")
		}
		cookies = append(cookies, cookie)
		sessions = append(sessions, signedIn)
		mailboxes[signedIn.ID] = &fakeGmail{}
		for _, path := range []string{"/api/contacts", "/api/drafts", "/api/activity"} {
			empty := readResponse[struct{ Items []json.RawMessage }](t, request(server, "GET", path, "", cookie, ""), 200)
			if len(empty.Items) != 0 {
				t.Fatalf("new workspace contains someone else's data: %s", path)
			}
		}
		body := fmt.Sprintf(`{"name":%q,"email":"recipient@example.com","status":"confirmed"}`, name+" contact")
		contact := readResponse[Contact](t, request(server, "POST", "/api/contacts", body, cookie, signedIn.CSRF), 200)
		contacts = append(contacts, contact)
		body = fmt.Sprintf(`{"contact_id":%q,"subject":%q,"body":"Private body"}`, contact.ID, name+" draft")
		draft := readResponse[catalogue.Draft](t, request(server, "POST", "/api/drafts/manual", body, cookie, signedIn.CSRF), 201)
		drafts = append(drafts, draft)
		readResponse[map[string]string](t, request(server, "POST", "/api/settings/gmail/connect", "", cookie, signedIn.CSRF), 200)
		if mailboxes[signedIn.ID].state == "" {
			t.Fatalf("Gmail connection not started for account %d", i)
		}
	}
	if sessions[0].ID == sessions[1].ID {
		t.Fatal("signups shared an account")
	}
	path := "/api/drafts/" + drafts[0].DraftID + "/versions/1"
	for _, attempt := range []struct{ method, path, body string }{
		{"GET", path, ""},
		{"GET", path + "/status", ""},
		{"POST", path + "/approve", fmt.Sprintf(`{"content_hash":%q}`, drafts[0].ContentHash)},
		{"POST", path + "/edit", fmt.Sprintf(`{"subject":"Overwrite","body":"Changed","content_hash":%q}`, drafts[0].ContentHash)},
		{"PUT", "/api/contacts/" + contacts[0].ID, `{"name":"Overwrite","status":"confirmed"}`},
		{"POST", "/api/drafts", fmt.Sprintf(`{"contact_id":%q,"cv_text":"Private CV"}`, contacts[0].ID)},
		{"POST", "/api/drafts/manual", fmt.Sprintf(`{"contact_id":%q,"subject":"Cross account","body":"Body"}`, contacts[0].ID)},
	} {
		response := request(server, attempt.method, attempt.path, attempt.body, cookies[1], sessions[1].CSRF)
		if response.Code != 404 {
			t.Fatalf("cross-account %s %s: %d %s", attempt.method, attempt.path, response.Code, response.Body.String())
		}
	}
	if len(bridge.calls) != 0 {
		t.Fatalf("cross-account operation reached agent: %v", bridge.calls)
	}
	gmailPath := "/oauth/google/callback?code=test-code&state=" + mailboxes[sessions[0].ID].state
	if response := request(server, "GET", gmailPath, "", cookies[1], ""); response.Code != 400 {
		t.Fatalf("Gmail callback accepted wrong account: %d", response.Code)
	}
	if response := request(server, "GET", gmailPath, "", cookies[0], ""); response.Code != 303 {
		t.Fatalf("Gmail callback failed: %d", response.Code)
	}
	if mailboxes[sessions[0].ID].exchanges != 1 || mailboxes[sessions[1].ID].exchanges != 0 {
		t.Fatal("Gmail authorization was saved to the wrong account")
	}
	readResponse[catalogue.Draft](t, request(server, "GET", path, "", cookies[0], ""), 200)
	readResponse[map[string]json.RawMessage](t, request(server, "POST", path+"/approve",
		fmt.Sprintf(`{"content_hash":%q}`, drafts[0].ContentHash), cookies[0], sessions[0].CSRF), 202)
	if len(bridge.calls) != 1 || bridge.payloads[0].(map[string]any)["approved_by"] != "First" {
		t.Fatalf("approval used wrong identity: %+v", bridge.payloads)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	approved, err := email.ApproveDraft(ctx, pool, email.ApproveDraftInput{
		ApprovalID: testID, DraftID: drafts[0].DraftID, Version: 1, ContentHash: drafts[0].ContentHash, ApprovedBy: "First",
	})
	if err != nil {
		t.Fatal(err)
	}
	ownerID, err := email.OutboxAccountID(ctx, pool, approved.OutboxID)
	if err != nil || ownerID != sessions[0].ID {
		t.Fatalf("delivery belongs to wrong account: %s %v", ownerID, err)
	}
	for i := range cookies {
		visibleContacts := readResponse[struct{ Items []Contact }](t, request(server, "GET", "/api/contacts", "", cookies[i], ""), 200)
		visibleDrafts := readResponse[struct{ Items []catalogue.Draft }](t, request(server, "GET", "/api/drafts", "", cookies[i], ""), 200)
		activities := readResponse[struct{ Items []Activity }](t, request(server, "GET", "/api/activity", "", cookies[i], ""), 200)
		if len(visibleContacts.Items) != 1 || visibleContacts.Items[0].ID != contacts[i].ID ||
			len(visibleDrafts.Items) != 1 || visibleDrafts.Items[0].DraftID != drafts[i].DraftID || len(activities.Items) != 1-i {
			t.Fatalf("workspace %d leaked data", i)
		}
	}
	if _, err := store.Contacts(ctx, 10, 0); err == nil {
		t.Fatal("store allowed missing account context")
	}
	if response := request(server, "POST", "/api/contacts",
		fmt.Sprintf(`{"name":"Injected","status":"confirmed","account_id":%q}`, sessions[0].ID), cookies[1], sessions[1].CSRF); response.Code != 400 {
		t.Fatal("account ID injection was accepted")
	}
	if got := request(server, "POST", "/api/logout", "", cookies[0], sessions[0].CSRF).Code; got != 200 {
		t.Fatal(got)
	}
	if got := request(server, "GET", "/api/contacts", "", cookies[0], "").Code; got != 401 {
		t.Fatal("logged-out session still works")
	}
	if response := request(server, "GET", "/api/session", "", cookies[1], ""); response.Code != 200 || !strings.Contains(response.Body.String(), sessions[1].ID) {
		t.Fatal("logout affected another account")
	}
}

func TestPostgresAccountsRejectGoogleLinkDuringDeletion(t *testing.T) {
	pool := accountTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	accounts := PostgresAccounts{Pool: pool}
	credentials, err := newOwner("PendingOwner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := accounts.Create(ctx, *credentials)
	if err != nil {
		t.Fatal(err)
	}

	result, err := pool.Exec(ctx,
		"UPDATE accounts SET deletion_requested_at=now() WHERE account_id=$1",
		owner.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.RowsAffected() != 1 {
		t.Fatalf("marked %d accounts for deletion, want 1", result.RowsAffected())
	}

	identity := GoogleIdentity{
		Subject: "pending-google",
		Email:   "pending@example.com",
	}

	_, err = accounts.LinkGoogle(ctx, owner.ID, identity)
	if !errors.Is(err, ErrAccountConflict) {
		t.Fatalf("expected ErrAccountConflict, got %v", err)
	}
	var subjectEmpty, emailEmpty bool
	if err := pool.QueryRow(ctx, `
		SELECT google_subject IS NULL, google_email IS NULL
		FROM accounts WHERE account_id=$1`, owner.ID).Scan(&subjectEmpty, &emailEmpty); err != nil {
		t.Fatal(err)
	}
	if !subjectEmpty || !emailEmpty {
		t.Fatal("Google identity was saved to an account pending deletion")
	}
}

func TestPostgresLegacyImportSkipsAccountsPendingDeletion(t *testing.T) {
	pool := accountTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var deletionRequestedAt time.Time
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (account_id, username, legacy_reserved, deletion_requested_at)
		VALUES ($1, '__legacy_owner__', true, now())
		RETURNING deletion_requested_at`, workspace.LegacyAccountID).Scan(&deletionRequestedAt); err != nil {
		t.Fatal(err)
	}
	accounts := PostgresAccounts{Pool: pool}
	credentials, err := newOwner("ImportedOwner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	credentials.GoogleSubject = "legacy-google"
	credentials.GoogleEmail = "legacy@example.com"
	if err := accounts.ImportLegacy(ctx, credentials); err != nil {
		t.Fatal(err)
	}
	var username string
	var reserved, credentialsEmpty bool
	var storedDeletionRequestedAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT username, legacy_reserved,
			password_salt IS NULL AND password_hash IS NULL AND google_subject IS NULL AND google_email IS NULL,
			deletion_requested_at
		FROM accounts WHERE account_id=$1`, workspace.LegacyAccountID).Scan(
		&username, &reserved, &credentialsEmpty, &storedDeletionRequestedAt,
	); err != nil {
		t.Fatal(err)
	}
	if username != "__legacy_owner__" || !reserved {
		t.Fatalf("legacy account was activated during deletion: username %q, reserved %t", username, reserved)
	}
	if !credentialsEmpty {
		t.Fatal("legacy import saved credentials to an account pending deletion")
	}
	if !storedDeletionRequestedAt.Equal(deletionRequestedAt) {
		t.Fatal("legacy import changed the deletion timestamp")
	}
}

func TestPostgresPendingDeletions(t *testing.T) {
	pool := accountTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	accounts := PostgresAccounts{Pool: pool}

	const insertAccount = `
		INSERT INTO accounts (username, legacy_reserved, deletion_requested_at)
		VALUES ($1, $2, $3)
		RETURNING account_id::text
	`

	ids, err := accounts.PendingDeletions(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected no pending accounts, got %v", ids)
	}

	olderTime := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	var newerID, olderID, activeID, reservedID string
	if err := pool.QueryRow(ctx, insertAccount,
		"pending-newer", false, olderTime.Add(time.Hour)).Scan(&newerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, insertAccount,
		"pending-older", false, olderTime).Scan(&olderID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, insertAccount,
		"active", false, nil).Scan(&activeID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, insertAccount,
		"reserved", true, olderTime.Add(-time.Hour)).Scan(&reservedID); err != nil {
		t.Fatal(err)
	}

	ids, err = accounts.PendingDeletions(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != olderID || ids[1] != newerID {
		t.Fatalf("expected pending accounts [%s %s], got %v", olderID, newerID, ids)
	}

	ids, err = accounts.PendingDeletions(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != olderID {
		t.Fatalf("expected only oldest pending account %s, got %v", olderID, ids)
	}

	for _, limit := range []int{-1, 0, 101} {
		if _, err := accounts.PendingDeletions(ctx, limit); err == nil {
			t.Fatalf("invalid limit %d was accepted", limit)
		}
	}
}
