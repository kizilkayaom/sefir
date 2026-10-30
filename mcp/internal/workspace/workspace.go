package workspace

import (
	"errors"
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
