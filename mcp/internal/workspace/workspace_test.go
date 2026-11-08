package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoveAccountFilesIsIsolatedAndRepeatable(t *testing.T) {
	root := t.TempDir()
	firstID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	otherID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	first, _ := GmailTokenPath(root, firstID)
	other, _ := GmailTokenPath(root, otherID)
	legacy, _ := GmailTokenPath(root, LegacyAccountID)
	owner := filepath.Join(root, ".sefir", "owner.json")
	shared := filepath.Join(root, ".sefir", "ai-settings.json")
	unfinishedToken := filepath.Join(filepath.Dir(first), "token-interrupted-write.json")
	for _, path := range []string{first, unfinishedToken, other, legacy, owner, shared} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("synthetic test data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := RemoveAccountFiles(root, firstID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Dir(first)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("account directory remains: %v", err)
	}
	for _, path := range []string{other, legacy, owner, shared} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unrelated file removed: %s", path)
		}
	}
	if err := RemoveAccountFiles(root, "../"); err == nil {
		t.Fatal("invalid ID accepted")
	}
	for range 2 {
		if err := RemoveAccountFiles(root, LegacyAccountID); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{legacy, owner} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("legacy file remains: %s", path)
		}
	}
	for _, path := range []string{other, shared} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unrelated file removed: %s", path)
		}
	}
}

func TestGmailTokenPathsKeepAccountsSeparate(t *testing.T) {
	root := t.TempDir()
	legacy, err := GmailTokenPath(root, LegacyAccountID)
	if err != nil || legacy != filepath.Join(root, "token-gmail.json") {
		t.Fatalf("legacy token path: %q %v", legacy, err)
	}
	firstID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	secondID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	first, err := GmailTokenPath(root, strings.ToUpper(firstID))
	if err != nil || first != filepath.Join(root, ".sefir", "accounts", firstID, "token-gmail.json") {
		t.Fatalf("account token path: %q %v", first, err)
	}
	second, err := GmailTokenPath(root, secondID)
	if err != nil || second == first || second == legacy {
		t.Fatalf("account token paths overlap: %q %v", second, err)
	}
	for _, id := range []string{"", "..", "../" + firstID, firstID + "/../", "not-a-uuid"} {
		if path, err := GmailTokenPath(root, id); err == nil || path != "" {
			t.Fatalf("accepted invalid account %q: %q %v", id, path, err)
		}
	}
}
