package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
)

type failingPresetAccounts struct {
	Accounts
	err error
}

func (a failingPresetAccounts) SetDefaultPreset(context.Context, string, string) (Account, error) {
	return Account{}, a.err
}

func TestDefaultPresetUpdatesOnlyCurrentAccount(t *testing.T) {
	server, _, _, _ := fixture(t)
	cookie, csrf := login(t, server)
	other, err := server.accounts.Google(context.Background(), GoogleIdentity{
		Subject: "other-account", Email: "other@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	response := request(server, "GET", "/api/settings/account", "", cookie, "")
	var settings struct {
		DefaultPresetID string `json:"default_preset_id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || settings.DefaultPresetID != "prof" {
		t.Fatalf("unexpected initial settings: %d %s", response.Code, response.Body.String())
	}

	for _, preset := range []string{"formal", "warm_prof", "technical", "prof"} {
		response = request(server, "PUT", "/api/settings/account/style",
			`{"preset_id":"`+preset+`"}`, cookie, csrf)
		if response.Code != http.StatusOK {
			t.Fatalf("save %s: %d %s", preset, response.Code, response.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result) != 1 || result["default_preset_id"] != preset {
			t.Fatalf("unexpected update response: %s", response.Body.String())
		}
		response = request(server, "GET", "/api/settings/account", "", cookie, "")
		if err := json.Unmarshal(response.Body.Bytes(), &settings); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK || settings.DefaultPresetID != preset {
			t.Fatalf("saved preset was not returned: %d %s", response.Code, response.Body.String())
		}
		unchanged, err := server.accounts.ByID(context.Background(), other.ID)
		if err != nil || unchanged.DefaultPresetID != other.DefaultPresetID {
			t.Fatalf("other account changed: %v", err)
		}
	}
}

func TestDefaultPresetRejectsInvalidInput(t *testing.T) {
	server, _, _, _ := fixture(t)
	cookie, csrf := login(t, server)
	accountID := server.sessions[cookie.Value].AccountID
	tests := []struct {
		body   string
		status int
	}{
		{`{}`, http.StatusUnprocessableEntity},
		{`{"preset_id":""}`, http.StatusUnprocessableEntity},
		{`{"preset_id":"unknown"}`, http.StatusUnprocessableEntity},
		{`{"preset_id":"Formal"}`, http.StatusUnprocessableEntity},
		{`{"preset_id":42}`, http.StatusBadRequest},
		{`{"preset_id":`, http.StatusBadRequest},
		{`{"preset_id":"formal","account_id":"` + testID + `"}`, http.StatusBadRequest},
	}
	for _, test := range tests {
		response := request(server, "PUT", "/api/settings/account/style", test.body, cookie, csrf)
		if response.Code != test.status {
			t.Fatalf("body %s: %d %s", test.body, response.Code, response.Body.String())
		}
		if test.status == http.StatusUnprocessableEntity {
			var result map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result["error"] != "Choose a supported writing style" {
				t.Fatalf("unexpected validation message: %s", response.Body.String())
			}
		}
		account, err := server.accounts.ByID(context.Background(), accountID)
		if err != nil || account.DefaultPresetID != "prof" {
			t.Fatalf("invalid request changed the default: %v", err)
		}
	}
}

func TestDefaultPresetRequiresSessionAndCSRF(t *testing.T) {
	server, _, _, _ := fixture(t)
	cookie, _ := login(t, server)
	accountID := server.sessions[cookie.Value].AccountID
	tests := []struct {
		name   string
		cookie *http.Cookie
		csrf   string
		status int
	}{
		{name: "missing session", status: http.StatusUnauthorized},
		{name: "missing CSRF", cookie: cookie, status: http.StatusForbidden},
		{name: "incorrect CSRF", cookie: cookie, csrf: "wrong-token", status: http.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := request(server, "PUT", "/api/settings/account/style",
				`{"preset_id":"formal"}`, test.cookie, test.csrf)
			if response.Code != test.status {
				t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
			}
			account, err := server.accounts.ByID(context.Background(), accountID)
			if err != nil || account.DefaultPresetID != "prof" {
				t.Fatalf("unauthorized request changed the default: %v", err)
			}
		})
	}
}

func TestDefaultPresetHandlesStoreErrors(t *testing.T) {
	server, _, _, _ := fixture(t)
	cookie, csrf := login(t, server)
	accounts := server.accounts
	accountID := server.sessions[cookie.Value].AccountID
	tests := []struct {
		err     error
		status  int
		message string
	}{
		{fmt.Errorf("validate preset: %w", ErrInvalidPreset), http.StatusUnprocessableEntity, "Choose a supported writing style"},
		{pgx.ErrNoRows, http.StatusNotFound, "Record not found"},
		{errors.New("private database details"), http.StatusServiceUnavailable, "Database operation failed. Please try again."},
		{context.DeadlineExceeded, http.StatusServiceUnavailable, "Database operation failed. Please try again."},
	}
	for _, test := range tests {
		server.accounts = failingPresetAccounts{Accounts: accounts, err: test.err}
		response := request(server, "PUT", "/api/settings/account/style",
			`{"preset_id":"formal"}`, cookie, csrf)
		var result map[string]string
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if response.Code != test.status || len(result) != 1 || result["error"] != test.message {
			t.Fatalf("unexpected error response: %d %s", response.Code, response.Body.String())
		}
		account, err := accounts.ByID(context.Background(), accountID)
		if err != nil || account.DefaultPresetID != "prof" {
			t.Fatalf("failed save changed the default: %v", err)
		}
	}
}
