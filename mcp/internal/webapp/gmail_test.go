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

func TestGmailConnectionRemoveToken(t *testing.T) {
	tests := []struct {
		name        string
		createToken bool
		emptyPath   bool
		directory   bool
		wantErr     bool
	}{
		{name: "existing token", createToken: true},
		{name: "missing token"},
		{name: "empty token path", emptyPath: true, wantErr: true},
		{name: "nonempty directory", directory: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			connection := &GmailConnection{
				CredentialsPath: filepath.Join(root, "credentials-gmail.json"),
				TokenPath:       filepath.Join(root, "token-gmail.json"),
			}
			credentials := []byte(`{"client_id":"test-client"}`)
			if err := os.WriteFile(connection.CredentialsPath, credentials, 0600); err != nil {
				t.Fatal(err)
			}
			if tt.createToken {
				if err := os.WriteFile(connection.TokenPath, []byte("test-token"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tt.emptyPath {
				connection.TokenPath = ""
			}
			if tt.directory {
				if err := os.Mkdir(connection.TokenPath, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(connection.TokenPath, "keep.txt"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}

			err := connection.RemoveToken()
			if (err != nil) != tt.wantErr {
				t.Fatalf("RemoveToken() error = %v, want error = %v", err, tt.wantErr)
			}
			authURL, err := connection.Start("test-state", "test-verifier")
			if authURL != "" || !errors.Is(err, errGmailConnectionDisabled) {
				t.Fatalf("expected disabled start, got URL %q and error %v", authURL, err)
			}
			if !tt.wantErr {
				if _, err := os.Stat(connection.TokenPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("expected token to be absent, got %v", err)
				}
				if err := connection.RemoveToken(); err != nil {
					t.Fatalf("repeated token removal failed: %v", err)
				}
			}
			if tt.directory {
				contents, err := os.ReadFile(filepath.Join(connection.TokenPath, "keep.txt"))
				if err != nil {
					t.Fatal(err)
				}
				if string(contents) != "keep" {
					t.Fatalf("directory contents changed: %q", contents)
				}
			}
			contents, err := os.ReadFile(connection.CredentialsPath)
			if err != nil {
				t.Fatalf("read shared credentials: %v", err)
			}
			if string(contents) != string(credentials) {
				t.Fatalf("shared credentials changed: %q", contents)
			}
		})
	}
}
