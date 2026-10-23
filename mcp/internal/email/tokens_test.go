package email

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestSavingTokenSourceRetriesAfterSaveFailure(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "missing", "token.json")
	token := &oauth2.Token{}
	token.AccessToken = "old_access"
	token.RefreshToken = "test_refresh"
	token.Expiry = time.Now().Add(time.Hour)

	source := &savingTokenSource{
		source:    oauth2.StaticTokenSource(token),
		tokenPath: tokenPath,
	}

	got, err := source.Token()
	if err == nil || got != nil {
		t.Fatal("expected a nil token and an error when saving fails")
	}
	if err := os.Mkdir(filepath.Dir(tokenPath), 0700); err != nil {
		t.Fatal(err)
	}

	got, err = source.Token()
	if err != nil {
		t.Fatalf("retry saving token: %v", err)
	}
	if got == nil {
		t.Fatal("expected a token after saving succeeds")
	}

	savedToken, err := LoadOAuthToken(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if savedToken.AccessToken != token.AccessToken {
		t.Errorf("saved access token = %q, want %q", savedToken.AccessToken, token.AccessToken)
	}
	if savedToken.RefreshToken != token.RefreshToken {
		t.Errorf("saved refresh token = %q, want %q", savedToken.RefreshToken, token.RefreshToken)
	}
}
