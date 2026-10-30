package workspace

import (
	"path/filepath"
	"strings"
	"testing"
)

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
