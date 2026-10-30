package webapp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeGoogleIdentity struct {
	state, nonce, verifier string
	identity               GoogleIdentity
	err                    error
	calls                  int
}

func (g *fakeGoogleIdentity) Start(state, nonce, verifier string) string {
	g.state, g.nonce, g.verifier = state, nonce, verifier
	return "https://accounts.google.com/test"
}

func (g *fakeGoogleIdentity) Complete(_ context.Context, code, nonce, verifier string) (GoogleIdentity, error) {
	g.calls++
	if code != "code" || nonce == "" || verifier == "" || nonce != g.nonce || verifier != g.verifier {
		return GoogleIdentity{}, errors.New("invalid exchange")
	}
	return g.identity, g.err
}

func googleFixture(t *testing.T) (*Server, *fakeGoogleIdentity, Config) {
	t.Helper()
	config := setupConfig(t)
	google := &fakeGoogleIdentity{identity: GoogleIdentity{Subject: "google-subject-1", Email: "owner@example.com"}}
	config.GoogleAuth = google
	server, err := New(config, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return server, google, config
}

func namedCookie(t *testing.T, response *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == name && cookie.MaxAge > 0 {
			return cookie
		}
	}
	t.Fatalf("missing %s cookie: %d %s", name, response.Code, response.Body.String())
	return nil
}

func beginGoogle(t *testing.T, server *Server, mode string, session *http.Cookie, csrf string) *http.Cookie {
	t.Helper()
	path := "/api/auth/google"
	if mode == "link" {
		path = "/api/settings/account/google"
	}
	response := request(server, "POST", path, `{"mode":"`+mode+`"}`, session, csrf)
	if response.Code != 200 {
		t.Fatalf("start: %d %s", response.Code, response.Body.String())
	}
	cookie := namedCookie(t, response, "sefir_google")
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 600 {
		t.Fatal("unprotected OAuth cookie")
	}
	return cookie
}

func googleCallback(server *Server, query string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "http://localhost:8080/oauth/google/sign-in/callback?"+query, nil)
	r.RemoteAddr = "127.0.0.1:12345"
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	return w
}

func TestGoogleStandaloneSignupAndSignin(t *testing.T) {
	for _, mode := range []string{"signup", "signin"} {
		t.Run(mode, func(t *testing.T) {
			server, google, config := googleFixture(t)
			cookie := beginGoogle(t, server, mode, nil, "")
			callback := googleCallback(server, "state="+google.state+"&code=code", cookie)
			if callback.Code != 303 || callback.Header().Get("Location") != "/#drafts" {
				t.Fatal(callback.Code, callback.Header())
			}
			sessionCookie := namedCookie(t, callback, "sefir_session")
			accountID := server.sessions[sessionCookie.Value].AccountID
			account, err := config.Accounts.ByID(context.Background(), accountID)
			if err != nil || account.GoogleSubject != google.identity.Subject || len(account.Hash) != 0 {
				t.Fatal(account.ID, err)
			}
			if got := request(server, "GET", "/api/session", "", sessionCookie, "").Body.String(); !strings.Contains(got, `"password_available":false`) {
				t.Fatal(got)
			}
			replay := googleCallback(server, "state="+google.state+"&code=code", cookie)
			if replay.Header().Get("Location") != "/?auth=expired#signin" || google.calls != 1 {
				t.Fatal("callback was reusable")
			}
			restarted, err := New(config, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			google.identity.Email = "renamed@example.com"
			cookie = beginGoogle(t, restarted, "signin", nil, "")
			callback = googleCallback(restarted, "state="+google.state+"&code=code", cookie)
			sessionCookie = namedCookie(t, callback, "sefir_session")
			if restarted.sessions[sessionCookie.Value].AccountID != accountID {
				t.Fatal("email change created a different account")
			}
			google.identity.Subject = "second-google-account"
			cookie = beginGoogle(t, restarted, "signin", nil, "")
			callback = googleCallback(restarted, "state="+google.state+"&code=code", cookie)
			sessionCookie = namedCookie(t, callback, "sefir_session")
			if restarted.sessions[sessionCookie.Value].AccountID == accountID {
				t.Fatal("different Google identities shared an account")
			}
		})
	}
}

func TestStandaloneGoogleDoesNotClaimPasswordAccount(t *testing.T) {
	server, google, config := googleFixture(t)
	credentials, err := newOwner("Owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	passwordAccount, err := config.Accounts.Create(context.Background(), *credentials)
	if err != nil {
		t.Fatal(err)
	}
	cookie := beginGoogle(t, server, "signin", nil, "")
	callback := googleCallback(server, "state="+google.state+"&code=code", cookie)
	sessionCookie := namedCookie(t, callback, "sefir_session")
	if server.sessions[sessionCookie.Value].AccountID == passwordAccount.ID {
		t.Fatal("Google claimed a matching username")
	}
	unchanged, err := config.Accounts.ByID(context.Background(), passwordAccount.ID)
	if err != nil || unchanged.GoogleSubject != "" || !unchanged.matches("Owner", testPassword) {
		t.Fatal("password account changed")
	}
}

func TestGoogleLinkPreservesPasswordAndRequiresOriginalSession(t *testing.T) {
	server, google, config := googleFixture(t)
	credentials, err := newOwner("Owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	account, err := config.Accounts.Create(context.Background(), *credentials)
	if err != nil {
		t.Fatal(err)
	}
	if got := request(server, "POST", "/api/settings/account/google", "{}", nil, "").Code; got != 401 {
		t.Fatal(got)
	}
	sessionCookie, csrf := login(t, server)
	if got := request(server, "POST", "/api/settings/account/google", "{}", sessionCookie, "").Code; got != 403 {
		t.Fatal(got)
	}
	cookie := beginGoogle(t, server, "link", sessionCookie, csrf)
	callback := googleCallback(server, "state="+google.state+"&code=code", cookie)
	if callback.Header().Get("Location") != "/?auth=expired#settings" {
		t.Fatal(callback.Header())
	}
	cookie = beginGoogle(t, server, "link", sessionCookie, csrf)
	callback = googleCallback(server, "state="+google.state+"&code=code", cookie, sessionCookie)
	if callback.Header().Get("Location") != "/?auth=linked#settings" {
		t.Fatal(callback.Header())
	}
	linked, err := config.Accounts.ByID(context.Background(), account.ID)
	if err != nil || linked.GoogleSubject != google.identity.Subject || !linked.matches("Owner", testPassword) {
		t.Fatal("link changed the account")
	}
	restarted, err := New(config, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sessionCookie, csrf = login(t, restarted)
	cookie = beginGoogle(t, restarted, "link", sessionCookie, csrf)
	google.identity.Subject = "different-account"
	callback = googleCallback(restarted, "state="+google.state+"&code=code", cookie, sessionCookie)
	if callback.Header().Get("Location") != "/?auth=already_linked#settings" {
		t.Fatal("link replaced a different account")
	}
}

func TestGoogleCallbackRejectsInvalidAttempts(t *testing.T) {
	for _, scenario := range []string{"missing cookie", "wrong state", "expired", "cancelled", "exchange failed", "signed out while linking"} {
		t.Run(scenario, func(t *testing.T) {
			server, google, config := googleFixture(t)
			cookie := beginGoogle(t, server, "signup", nil, "")
			query := "state=" + google.state + "&code=code"
			cookies := []*http.Cookie{cookie}
			switch scenario {
			case "missing cookie":
				cookies = nil
			case "wrong state":
				query = "state=wrong&code=code"
			case "expired":
				value := server.googleAttempts[cookie.Value]
				value.Expires = time.Now().Add(-time.Second)
				server.googleAttempts[cookie.Value] = value
			case "cancelled":
				query = "state=" + google.state + "&error=access_denied"
			case "exchange failed":
				google.err = errors.New("failed")
			case "signed out while linking":
				value := server.googleAttempts[cookie.Value]
				value.Mode, value.SessionID = "link", "revoked"
				server.googleAttempts[cookie.Value] = value
			}
			callback := googleCallback(server, query, cookies...)
			if callback.Code != 303 || !strings.Contains(callback.Header().Get("Location"), "auth=") {
				t.Fatal(callback.Code, callback.Header())
			}
			for _, cookie := range callback.Result().Cookies() {
				if cookie.Name == "sefir_session" {
					t.Fatal("invalid attempt created a session")
				}
			}
			if len(config.Accounts.(*memoryAccounts).items) != 0 {
				t.Fatal("invalid attempt created an account")
			}
		})
	}
}

func TestGoogleStartRequiresOriginAndAllowsNewAccounts(t *testing.T) {
	server, _, _ := googleFixture(t)
	r := httptest.NewRequest("POST", "http://localhost:8080/api/auth/google", strings.NewReader(`{"mode":"signin"}`))
	r.Header.Set("Origin", "https://other.example")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	if w.Code != 403 || len(w.Result().Cookies()) != 0 {
		t.Fatal(w.Code)
	}
	beginGoogle(t, server, "signin", nil, "")
	server.config.GoogleAuth = nil
	if got := request(server, "POST", "/api/auth/google", `{"mode":"signup"}`, nil, "").Code; got != 503 {
		t.Fatal(got)
	}
}
