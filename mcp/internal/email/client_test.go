package email

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestNewGmailClientRefreshesAndSavesToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token.json")
	token := &oauth2.Token{}
	token.AccessToken = "old_access"
	token.RefreshToken = "test_refresh"
	token.Expiry = time.Now().Add(-time.Hour)
	if err := SaveOAuthToken(path, token); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if r.Method != http.MethodPost {
				t.Errorf("token request method = %q, want POST", r.Method)
				http.Error(w, "Method invalid", http.StatusMethodNotAllowed)
				return
			}
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse token request: %v", err)
				http.Error(w, "Request invalid", http.StatusBadRequest)
				return
			}
			if r.PostForm.Get("grant_type") != "refresh_token" ||
				r.PostForm.Get("refresh_token") != "test_refresh" {
				t.Errorf("unexpected token request: grant_type=%q, refresh_token=%q",
					r.PostForm.Get("grant_type"), r.PostForm.Get("refresh_token"))
				http.Error(w, "Refresh request invalid", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"access_token":"new-access","token_type":"Bearer","expires_in":3600}`)
		case "/profile":
			if got := r.Header.Get("Authorization"); got != "Bearer new-access" {
				t.Errorf("Authorization = %q, want %q", got, "Bearer new-access")
				http.Error(w, "Authorization invalid", http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request path: %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	config := &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: srv.URL + "/token",
		},
	}

	client, err := NewGmailClient(context.Background(), config, path)
	if err != nil {
		t.Fatal(err)
	}

	response, err := client.Get(srv.URL + "/profile")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("profile response status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	savedToken, err := LoadOAuthToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if savedToken.AccessToken != "new-access" {
		t.Errorf("saved access token = %q, want %q", savedToken.AccessToken, "new-access")
	}
	if savedToken.RefreshToken != "test_refresh" {
		t.Errorf("saved refresh token = %q, want %q", savedToken.RefreshToken, "test_refresh")
	}
	if !savedToken.Expiry.After(time.Now()) {
		t.Errorf("saved token expiry = %v, want a future time", savedToken.Expiry)
	}

}

func TestNewGmailClientRequiresReconnect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token.json")
	token := &oauth2.Token{
		AccessToken:  "old_access",
		RefreshToken: "test_refresh",
		Expiry:       time.Now().Add(-time.Hour),
	}
	if err := SaveOAuthToken(path, token); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if r.Method != http.MethodPost {
				t.Errorf("token request method = %q, want POST", r.Method)
				http.Error(w, "Method invalid", http.StatusMethodNotAllowed)
				return
			}
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse token request: %v", err)
				http.Error(w, "Request invalid", http.StatusBadRequest)
				return
			}
			if r.PostForm.Get("grant_type") != "refresh_token" ||
				r.PostForm.Get("refresh_token") != "test_refresh" {
				t.Errorf("unexpected token request: grant_type=%q, refresh_token=%q",
					r.PostForm.Get("grant_type"), r.PostForm.Get("refresh_token"))
				http.Error(w, "Refresh request invalid", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":"invalid_grant"}`)
		case "/profile":
			t.Error("profile request reached the server after refresh was rejected")
			http.Error(w, "Authorization invalid", http.StatusUnauthorized)
		default:
			t.Errorf("unexpected request path: %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	config := &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		Endpoint: oauth2.Endpoint{
			TokenURL: srv.URL + "/token",
		},
	}

	client, err := NewGmailClient(context.Background(), config, path)
	if err != nil {
		t.Fatal(err)
	}

	response, err := client.Get(srv.URL + "/profile")
	if response != nil {
		defer response.Body.Close()
	}
	if !errors.Is(err, ErrGmailReconnectRequired) {
		t.Fatalf("expected reconnect error, got %v", err)
	}

	savedToken, err := LoadOAuthToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if savedToken.AccessToken != token.AccessToken {
		t.Errorf("saved access token = %q, want %q", savedToken.AccessToken, token.AccessToken)
	}
	if savedToken.RefreshToken != token.RefreshToken {
		t.Errorf("saved refresh token = %q, want %q", savedToken.RefreshToken, token.RefreshToken)
	}
	if !savedToken.Expiry.Equal(token.Expiry) {
		t.Errorf("saved token expiry = %v, want %v", savedToken.Expiry, token.Expiry)
	}
}
