package webapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestGmailConnectionDisable(t *testing.T) {
	root := t.TempDir()
	tokenDir := filepath.Join(root, "tokens")
	connection := &GmailConnection{
		CredentialsPath: filepath.Join(root, "credentials-gmail.json"),
		TokenPath:       filepath.Join(tokenDir, "token-gmail.json"),
	}
	connection.Disable()
	connection.Disable()

	ctx := context.Background()
	status := connection.Status(ctx)
	if status.Connected || status.Email != "" || status.Message != "Gmail connection is disabled." {
		t.Fatalf("unexpected disabled status: %+v", status)
	}

	authURL, err := connection.Start("test-state", "test-verifier")
	if authURL != "" || !errors.Is(err, errGmailConnectionDisabled) {
		t.Fatalf("expected disabled start, got URL %q and error %v", authURL, err)
	}
	if err := connection.Complete(ctx, "test-code", "test-verifier"); !errors.Is(err, errGmailConnectionDisabled) {
		t.Fatalf("expected disabled completion, got %v", err)
	}
	if _, err := os.Stat(tokenDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected token directory to remain absent, got %v", err)
	}
}
