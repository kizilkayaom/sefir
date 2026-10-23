package email

import (
	"encoding/json"
	"errors"
	"golang.org/x/oauth2"
	"os"
	"path/filepath"
	"strings"
)

func SaveOAuthToken(tokenPath string, token *oauth2.Token) error {
	if token == nil || strings.TrimSpace(tokenPath) == "" ||
		token.AccessToken == "" || token.RefreshToken == "" {
		return errors.New("token invalid")
	}

	data, err := json.Marshal(token)
	if err != nil {
		return err
	}

	tempFile, err := os.CreateTemp(filepath.Dir(tokenPath), "token-*.json")

	if err != nil {
		return err
	}

	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	if _, err := tempFile.Write(data); err != nil {
		return err
	}

	if err := tempFile.Sync(); err != nil {
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}

	return os.Rename(tempFile.Name(), tokenPath)
}

func LoadOAuthToken(tokenPath string) (*oauth2.Token, error) {
	if strings.TrimSpace(tokenPath) == "" {
		return nil, errors.New("Invalid token path")
	}
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, err
	}

	var token oauth2.Token

	if err := json.Unmarshal(data, &token); err != nil {
		return nil, err
	}

	if token.AccessToken == "" || token.RefreshToken == "" {
		return nil, errors.New("Token invalid")
	}

	return &token, nil
}
