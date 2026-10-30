package webapp

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestRegistrationClosedByDefault(t *testing.T) {
	config := setupConfig(t)
	config.SignupEnabled = Config{}.SignupEnabled
	server, err := New(config, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := request(server, "GET", "/api/session", "", nil, "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"signup_available":false`) {
		t.Fatalf("session advertises registration: %d %s", response.Code, response.Body.String())
	}
	for _, path := range []string{"/api/signup", "/api/setup", "/api/auth/google"} {
		body := setupBody("NewUser", testPassword, testPassword)
		if path == "/api/auth/google" {
			body = `{"mode":"signup"}`
		}
		response := request(server, "POST", path, body, nil, "")
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), signupClosedMessage) ||
			len(response.Result().Cookies()) != 0 {
			t.Fatalf("%s allowed registration: %d %s", path, response.Code, response.Body.String())
		}
	}
	if len(config.Accounts.(*memoryAccounts).items) != 0 || len(server.sessions) != 0 || len(server.googleAttempts) != 0 {
		t.Fatal("closed registration created an account, session, or Google attempt")
	}
}

func TestClosedRegistrationKeepsExistingSignInAndLinking(t *testing.T) {
	_, google, config := googleFixture(t)
	config.SignupEnabled = false
	config.Owner, config.Password = "Owner", testPassword
	server, err := New(config, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sessionCookie, csrf := login(t, server)
	accountID := server.sessions[sessionCookie.Value].AccountID
	if response := request(server, "GET", "/api/session", "", sessionCookie, ""); !strings.Contains(response.Body.String(), `"signup_available":false`) {
		t.Fatalf("authenticated session advertises registration: %s", response.Body.String())
	}
	cookie := beginGoogle(t, server, "link", sessionCookie, csrf)
	callback := googleCallback(server, "state="+google.state+"&code=code", cookie, sessionCookie)
	if callback.Header().Get("Location") != "/?auth=linked#settings" {
		t.Fatalf("existing account could not link Google: %s", callback.Header().Get("Location"))
	}
	google.identity.Email = "renamed@example.com"
	cookie = beginGoogle(t, server, "signin", nil, "")
	callback = googleCallback(server, "state="+google.state+"&code=code", cookie)
	if callback.Header().Get("Location") != "/#drafts" {
		t.Fatalf("existing Google sign-in failed: %s", callback.Header().Get("Location"))
	}
	signedIn := namedCookie(t, callback, "sefir_session")
	if server.sessions[signedIn.Value].AccountID != accountID || len(config.Accounts.(*memoryAccounts).items) != 1 {
		t.Fatal("existing Google sign-in created a different account")
	}
	login(t, server)
}

func TestClosedRegistrationRejectsNewGoogleIdentity(t *testing.T) {
	for _, scenario := range []string{"signin", "signup already started", "same email", "storage failure"} {
		t.Run(scenario, func(t *testing.T) {
			server, google, config := googleFixture(t)
			accounts := config.Accounts.(*memoryAccounts)
			if scenario == "same email" {
				_, err := accounts.Google(context.Background(), GoogleIdentity{Subject: "other-subject", Email: google.identity.Email})
				if err != nil {
					t.Fatal(err)
				}
			}
			before := len(accounts.items)
			mode := "signin"
			if scenario == "signup already started" {
				mode = "signup"
			} else {
				server.config.SignupEnabled = false
			}
			cookie := beginGoogle(t, server, mode, nil, "")
			server.config.SignupEnabled = false
			reason := "signup_closed"
			if scenario == "storage failure" {
				accounts.err = errors.New("database unavailable")
				reason = "failed"
			}
			callback := googleCallback(server, "state="+google.state+"&code=code", cookie)
			if callback.Code != http.StatusSeeOther || callback.Header().Get("Location") != "/?auth="+reason+"#"+mode {
				t.Fatalf("callback: %d %s", callback.Code, callback.Header().Get("Location"))
			}
			for _, value := range callback.Result().Cookies() {
				if value.Name == "sefir_session" {
					t.Fatal("unregistered identity received a session")
				}
			}
			if len(accounts.items) != before || len(server.sessions) != 0 {
				t.Fatal("Google sign-in bypassed closed registration")
			}
			replay := googleCallback(server, "state="+google.state+"&code=code", cookie)
			if replay.Header().Get("Location") != "/?auth=expired#signin" || google.calls != 1 {
				t.Fatal("rejected Google attempt was reusable")
			}
		})
	}
}
