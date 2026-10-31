package webapp

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestDeleteAccount(t *testing.T) {
	server, _, _, _ := fixture(t)

	cookie, csrf := login(t, server)
	accountID := server.sessions[cookie.Value].AccountID

	response := request(server, "DELETE", "/api/settings/account",
		`{"confirmation":"DELETE"}`, cookie, csrf)

	if response.Code != http.StatusAccepted {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}

	var result struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "deletion_requested" {
		t.Fatalf("unexpected deletion status: %q", result.Status)
	}

	if !server.accounts.(*memoryAccounts).deletionRequested[accountID] {
		t.Fatal("account was not marked for deletion")
	}
	if _, exists := server.accounts.(*memoryAccounts).items[accountID]; !exists {
		t.Fatal("account record was removed before cleanup")
	}

	sessionCleared, googleCleared := false, false

	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "sefir_session" {
			sessionCleared = cookie.MaxAge < 0
		}
		if cookie.Name == "sefir_google" {
			googleCleared = cookie.MaxAge < 0
		}
	}

	if !sessionCleared || !googleCleared {
		t.Fatal("session and Google cookies were not both cleared")
	}

	protected := request(server, "GET", "/api/settings/account", "", cookie, "")
	if protected.Code != http.StatusUnauthorized {
		t.Fatalf("old session was not rejected: %d %s", protected.Code, protected.Body.String())
	}
}

func TestDeleteAccountRejectsInvalidConfirmation(t *testing.T) {
	bodies := []string{
		`{}`,
		`{"confirmation":""}`,
		`{"confirmation":"delete"}`,
	}
	for _, body := range bodies {
		server, _, _, _ := fixture(t)
		cookie, csrf := login(t, server)
		accountID := server.sessions[cookie.Value].AccountID
		response := request(server, "DELETE", "/api/settings/account", body, cookie, csrf)
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("body %s: unexpected response: %d %s", body, response.Code, response.Body.String())
		}
		accounts := server.accounts.(*memoryAccounts)
		if accounts.deletionRequested[accountID] {
			t.Fatalf("body %s: account was marked for deletion", body)
		}
		if _, exists := accounts.items[accountID]; !exists {
			t.Fatalf("body %s: account record was removed", body)
		}
		if len(response.Result().Cookies()) != 0 {
			t.Fatalf("body %s: rejected deletion changed cookies", body)
		}
		protected := request(server, "GET", "/api/settings/account", "", cookie, "")
		if protected.Code != http.StatusOK {
			t.Fatalf("body %s: session was not preserved: %d %s", body, protected.Code, protected.Body.String())
		}
	}
}

func TestDeleteAccountRequiresSessionAndCSRF(t *testing.T) {
	tests := []struct {
		name        string
		withSession bool
		csrf        string
		status      int
	}{
		{name: "missing session", status: http.StatusUnauthorized},
		{name: "missing CSRF token", withSession: true, status: http.StatusForbidden},
		{name: "incorrect CSRF token", withSession: true, csrf: "wrong-token", status: http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, _, _, _ := fixture(t)
			cookie, _ := login(t, server)
			accountID := server.sessions[cookie.Value].AccountID
			requestCookie := cookie
			if !test.withSession {
				requestCookie = nil
			}
			response := request(server, "DELETE", "/api/settings/account",
				`{"confirmation":"DELETE"}`, requestCookie, test.csrf)
			if response.Code != test.status {
				t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
			}
			accounts := server.accounts.(*memoryAccounts)
			if accounts.deletionRequested[accountID] {
				t.Fatal("account was marked for deletion")
			}
			if _, exists := accounts.items[accountID]; !exists {
				t.Fatal("account record was removed")
			}
			if len(response.Result().Cookies()) != 0 {
				t.Fatal("rejected deletion changed cookies")
			}
			protected := request(server, "GET", "/api/settings/account", "", cookie, "")
			if protected.Code != http.StatusOK {
				t.Fatalf("session was not preserved: %d %s", protected.Code, protected.Body.String())
			}
		})
	}
}
