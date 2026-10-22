package email

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

func LoadOAuthConfig(credentialsPath, redirectURL string) (*oauth2.Config, error) {
	if strings.TrimSpace(credentialsPath) == "" || strings.TrimSpace(redirectURL) == "" {
		return nil, errors.New("credentials path and redirect URL are required")
	}

	data, err := os.ReadFile(credentialsPath)
	if err != nil {
		return nil, err
	}
	config, err := google.ConfigFromJSON(
		data,
		"https://www.googleapis.com/auth/gmail.send",
		"https://www.googleapis.com/auth/gmail.readonly",
	)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(config.ClientID) == "" || strings.TrimSpace(config.ClientSecret) == "" {
		return nil, errors.New("Client ID and secret are required")
	}

	config.RedirectURL = redirectURL
	return config, nil
}
