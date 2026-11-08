package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const LegacyAccountID = "00000000-0000-4000-8000-000000000001"

var accountIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func GmailTokenPath(root, accountID string) (string, error) {
	accountID = strings.ToLower(accountID)
	if !accountIDPattern.MatchString(accountID) {
		return "", errors.New("valid account ID is required")
	}
	if accountID == LegacyAccountID {
		return filepath.Join(root, "token-gmail.json"), nil
	}
	return filepath.Join(root, ".sefir", "accounts", accountID, "token-gmail.json"), nil
}

func RemoveAccountFiles(root, accountID string) error {
	path, err := GmailTokenPath(root, accountID)
	if err != nil {
		return err
	}
	if !strings.EqualFold(accountID, LegacyAccountID) {
		return os.RemoveAll(filepath.Dir(path))
	}
	paths := []string{path}
	paths = append(paths, filepath.Join(root, ".sefir", "owner.json"))
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
