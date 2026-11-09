package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/kizilkayaom/sefir/mcp/internal/catalogue"
)

const testID = "11111111-1111-4111-8111-111111111111"
const testPassword = "letter1!"

type fakeStore struct {
	draft       catalogue.Draft
	err         error
	activity    *Activity
	saved       ContactInput
	draftInput  catalogue.CreateDraftInput
	editInput   catalogue.EditDraftInput
	editID      string
	editVersion int
	limit       int
	offset      int
}

func (f *fakeStore) Drafts(_ context.Context, limit, offset int) ([]catalogue.Draft, error) {
	f.limit, f.offset = limit, offset
	return []catalogue.Draft{f.draft, f.draft}, f.err
}
func (f *fakeStore) Draft(context.Context, string, int) (catalogue.Draft, error) {
	return f.draft, f.err
}
func (f *fakeStore) Contacts(context.Context, int, int) ([]Contact, error) { return nil, f.err }
func (f *fakeStore) SaveContact(_ context.Context, _ string, input ContactInput) (Contact, error) {
	f.saved = input
	return Contact{ID: testID, Name: &input.Name, Status: input.Status}, f.err
}
func (f *fakeStore) ConfirmedContact(context.Context, string) (catalogue.Contact, error) {
	name := "Test contact"
	return catalogue.Contact{ID: testID, Name: &name}, f.err
}
func (f *fakeStore) CreateDraft(_ context.Context, input catalogue.CreateDraftInput) (catalogue.Draft, error) {
	f.draftInput = input
	return f.draft, f.err
}
func (f *fakeStore) SaveDraftVersion(_ context.Context, id string, version int, input catalogue.EditDraftInput) (catalogue.Draft, error) {
	f.editID, f.editVersion, f.editInput = id, version, input
	return f.draft, f.err
}
func (f *fakeStore) Activities(context.Context, int, int) ([]Activity, error) { return nil, f.err }
func (f *fakeStore) DraftActivity(context.Context, string, int) (*Activity, error) {
	return f.activity, f.err
}

type fakeBridge struct {
	calls    []string
	payloads []any
	result   json.RawMessage
	err      error
}

func (f *fakeBridge) Call(_ context.Context, action string, payload any) (json.RawMessage, error) {
	f.calls = append(f.calls, action)
	f.payloads = append(f.payloads, payload)
	if f.result == nil {
		return json.RawMessage(`{"status":"RUNNING"}`), f.err
	}
	return f.result, f.err
}

type fakeGmail struct {
	state     string
	verifier  string
	exchanges int
}

type disconnectedGmail struct{ fakeGmail }

func (*disconnectedGmail) Status(context.Context) GmailStatus {
	return GmailStatus{Message: "Connect Gmail to authorize this account."}
}

func (f *fakeGmail) Status(context.Context) GmailStatus {
	return GmailStatus{Connected: true, Email: "test@example.com"}
}
func (f *fakeGmail) Start(state, verifier string) (string, error) {
	f.state, f.verifier = state, verifier
	return "https://accounts.google.com/test", nil
}
func (f *fakeGmail) Complete(_ context.Context, code, verifier string) error {
	if code != "test-code" || verifier != f.verifier {
		return errors.New("invalid exchange")
	}
	f.exchanges++
	return nil
}

func fixture(t *testing.T) (*Server, *fakeStore, *fakeBridge, *fakeGmail) {
	t.Helper()
	recipient := "test@example.com"
	store := &fakeStore{draft: catalogue.Draft{
		DraftID: testID, Version: 1, ContactID: testID, RecipientEmail: &recipient,
		Subject: "Test draft", Body: "Test body", ContentHash: strings.Repeat("a", 64),
	}}
	bridge, gmail := &fakeBridge{}, &fakeGmail{}
	config := setupConfig(t)
	config.Owner, config.Password = "Owner", testPassword
	server, err := New(config, store, bridge, gmail)
	if err != nil {
		t.Fatal(err)
	}
	return server, store, bridge, gmail
}

func request(server *Server, method, path, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://localhost:8080"+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("Origin", "http://localhost:8080")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	return w
}

func login(t *testing.T, server *Server) (*http.Cookie, string) {
	t.Helper()
	response := request(server, "POST", "/api/login", `{"username":"Owner","password":"`+testPassword+`"}`, nil, "")
	if response.Code != 200 {
		t.Fatalf("login: %d %s", response.Code, response.Body.String())
	}
	var result struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || result.CSRF == "" {
		t.Fatal("missing session protection")
	}
	return cookies[0], result.CSRF
}

func TestSessionProtection(t *testing.T) {
	server, _, bridge, _ := fixture(t)
	for _, path := range []string{"/api/drafts", "/api/contacts", "/api/activity", "/api/settings/gmail"} {
		if got := request(server, "GET", path, "", nil, "").Code; got != 401 {
			t.Fatalf("%s: %d", path, got)
		}
	}
	if got := request(server, "POST", "/api/login", `{"password":"wrong"}`, nil, "").Code; got != 401 {
		t.Fatal(got)
	}
	cookie, csrf := login(t, server)
	if got := request(server, "POST", "/api/logout", "", cookie, "").Code; got != 403 {
		t.Fatal(got)
	}
	if got := request(server, "GET", "/api/session", "", cookie, "").Code; got != 200 {
		t.Fatal(got)
	}
	if got := request(server, "POST", "/api/logout", "", cookie, csrf).Code; got != 200 {
		t.Fatal(got)
	}
	if got := request(server, "GET", "/api/drafts", "", cookie, "").Code; got != 401 {
		t.Fatal(got)
	}
	if len(bridge.calls) != 0 {
		t.Fatal("unauthorized agent call")
	}
}

func TestApprovalRequiresTheAccountsOwnGmailConnection(t *testing.T) {
	server, store, bridge, _ := fixture(t)
	cookie, csrf := login(t, server)
	accountID := server.sessions[cookie.Value].AccountID
	server.config.GmailForAccount = func(id string) (Gmail, error) {
		if id != accountID {
			t.Fatalf("checked another account's Gmail: %s", id)
		}
		return &disconnectedGmail{}, nil
	}
	path := "/api/drafts/" + testID + "/versions/1/approve"
	body := `{"content_hash":"` + store.draft.ContentHash + `"}`
	response := request(server, "POST", path, body, cookie, csrf)
	if response.Code != 422 || !strings.Contains(response.Body.String(), "Connect Gmail in Settings") || len(bridge.calls) != 0 {
		t.Fatalf("approved without own Gmail connection: %d %s", response.Code, response.Body.String())
	}
	store.activity = &Activity{OutboxID: testID, State: "sent"}
	if response := request(server, "POST", path, body, cookie, csrf); response.Code != 200 || len(bridge.calls) != 0 {
		t.Fatalf("existing delivery was not preserved: %d %s", response.Code, response.Body.String())
	}
}

func TestOriginHostExpiryAndLoginRate(t *testing.T) {
	server, _, _, _ := fixture(t)
	r := httptest.NewRequest("POST", "http://localhost:8080/api/login", strings.NewReader(`{"password":"`+testPassword+`"}`))
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	r = httptest.NewRequest("GET", "http://evil.example/api/session", nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	cookie, _ := login(t, server)
	server.mu.Lock()
	value := server.sessions[cookie.Value]
	value.Expires = time.Now().Add(-time.Minute)
	server.sessions[cookie.Value] = value
	server.mu.Unlock()
	if got := request(server, "GET", "/api/drafts", "", cookie, "").Code; got != 401 {
		t.Fatal(got)
	}
	for range 5 {
		request(server, "POST", "/api/login", `{"password":"wrong"}`, nil, "")
	}
	if got := request(server, "POST", "/api/login", `{"password":"wrong"}`, nil, "").Code; got != 429 {
		t.Fatal(got)
	}
}

func TestListsAndValidation(t *testing.T) {
	server, store, _, _ := fixture(t)
	cookie, csrf := login(t, server)
	response := request(server, "GET", "/api/drafts?limit=1&offset=2", "", cookie, "")
	var result struct {
		Items   []catalogue.Draft `json:"items"`
		HasMore bool              `json:"has_more"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || len(result.Items) != 1 || !result.HasMore || store.limit != 2 || store.offset != 2 {
		t.Fatal(response.Body.String())
	}
	for _, path := range []string{"/api/drafts?limit=101", "/api/drafts?offset=-1", "/api/drafts/not-a-uuid/versions/1", "/api/drafts/" + testID + "/versions/0"} {
		if got := request(server, "GET", path, "", cookie, "").Code; got != 400 {
			t.Fatalf("%s: %d", path, got)
		}
	}
	response = request(server, "GET", "/api/contacts", "", cookie, "")
	if !strings.Contains(response.Body.String(), `"items":[]`) {
		t.Fatal(response.Body.String())
	}
	if got := request(server, "POST", "/api/contacts", `{"name":"Name","email":"bad","status":"confirmed"}`, cookie, csrf).Code; got != 422 {
		t.Fatal(got)
	}
	if got := request(server, "POST", "/api/contacts", `{"name":" Name ","email":"valid@example.com","status":"confirmed"}`, cookie, csrf).Code; got != 200 || store.saved.Name != "Name" {
		t.Fatal(got)
	}
	if got := request(server, "POST", "/api/contacts", `{"name":"Name","status":"confirmed","extra":true}`, cookie, csrf).Code; got != 400 {
		t.Fatal(got)
	}
	store.err = errors.New("database secret")
	response = request(server, "GET", "/api/drafts", "", cookie, "")
	if response.Code != 503 || strings.Contains(response.Body.String(), "secret") {
		t.Fatal(response.Body.String())
	}
}

func TestApprovalBindsReviewedDraftAndOwner(t *testing.T) {
	server, store, bridge, _ := fixture(t)
	cookie, csrf := login(t, server)
	path := "/api/drafts/" + testID + "/versions/1/approve"
	if got := request(server, "POST", path, `{"content_hash":"stale"}`, cookie, csrf).Code; got != 409 {
		t.Fatal(got)
	}
	if len(bridge.calls) != 0 {
		t.Fatal("stale draft signaled")
	}
	body := `{"content_hash":"` + store.draft.ContentHash + `"}`
	for range 2 {
		if got := request(server, "POST", path, body, cookie, csrf).Code; got != 202 {
			t.Fatal(got)
		}
	}
	first := bridge.payloads[0].(map[string]any)
	second := bridge.payloads[1].(map[string]any)
	if first["approval_id"] != second["approval_id"] || first["approved_by"] != "Owner" || first["content_hash"] != store.draft.ContentHash {
		t.Fatal("approval identity changed")
	}
	if got := request(server, "POST", path, `{"content_hash":"`+store.draft.ContentHash+`","approved_by":"forged"}`, cookie, csrf).Code; got != 400 {
		t.Fatal(got)
	}
	store.activity = &Activity{State: "sent", OutboxID: testID}
	if got := request(server, "POST", path, body, cookie, csrf).Code; got != 200 {
		t.Fatal(got)
	}
	if len(bridge.calls) != 2 {
		t.Fatal("existing delivery resubmitted")
	}
	store.activity = nil
	store.draft.RecipientEmail = nil
	if got := request(server, "POST", path, body, cookie, csrf).Code; got != 422 {
		t.Fatal(got)
	}
}

func TestGenerationAndUnavailableStatus(t *testing.T) {
	server, store, bridge, _ := fixture(t)
	cookie, csrf := login(t, server)
	body := `{"contact_id":"` + testID + `","cv_text":"Fictional test resume"}`
	store.err = pgx.ErrNoRows
	if got := request(server, "POST", "/api/drafts", body, cookie, csrf).Code; got != 404 {
		t.Fatal(got)
	}
	if len(bridge.calls) != 0 {
		t.Fatal("unconfirmed contact drafted")
	}
	store.err = nil
	bridge.result = json.RawMessage(`{"subject":"Hello","body":"Test body","preset_id":"prof","preset_revision":1}`)
	if got := request(server, "POST", "/api/drafts", body, cookie, csrf).Code; got != 201 {
		t.Fatal(got)
	}
	if store.draftInput.ContactID != testID || store.draftInput.Subject != "Hello" {
		t.Fatal("draft not saved")
	}
	bridge.result = json.RawMessage(`{"subject":"","body":"Test body","preset_id":"prof","preset_revision":1}`)
	if got := request(server, "POST", "/api/drafts", body, cookie, csrf).Code; got != 502 {
		t.Fatal(got)
	}
	bridge.err = errors.New("private upstream message")
	response := request(server, "GET", "/api/drafts/"+testID+"/versions/1/status", "", cookie, "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), "UNAVAILABLE") || strings.Contains(response.Body.String(), "private") {
		t.Fatal(response.Body.String())
	}
}

func TestOAuthRequiresSessionStateAndSingleUse(t *testing.T) {
	server, _, _, gmail := fixture(t)
	cookie, csrf := login(t, server)
	if got := request(server, "POST", "/api/settings/gmail/connect", "", cookie, csrf).Code; got != 200 {
		t.Fatal(got)
	}
	if gmail.state == "" || gmail.verifier == "" {
		t.Fatal("missing state or PKCE")
	}
	path := "/oauth/google/callback?code=test-code&state=" + gmail.state
	if got := request(server, "GET", path, "", nil, "").Code; got != 401 {
		t.Fatal(got)
	}
	if got := request(server, "GET", "/oauth/google/callback?code=test-code&state=wrong", "", cookie, "").Code; got != 400 {
		t.Fatal(got)
	}
	response := request(server, "GET", path, "", cookie, "")
	if response.Code != 303 || response.Header().Get("Location") != "/?gmail=connected#settings" {
		t.Fatal(response.Code)
	}
	if got := request(server, "GET", path, "", cookie, "").Code; got != 400 {
		t.Fatal(got)
	}
	if gmail.exchanges != 1 {
		t.Fatal("OAuth code reused")
	}
}
