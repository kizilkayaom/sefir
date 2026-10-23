package email

import (
	"context"
	"errors"
	"golang.org/x/oauth2"
	"net/http"
	"time"
)

func NewGmailClient(ctx context.Context, config *oauth2.Config, tokenPath string) (*http.Client, error) {
	if config == nil {
		return nil, errors.New("Invalid oauth configuration")
	}

	token, err := LoadOAuthToken(tokenPath)
	if err != nil {
		return nil, err
	}

	baseClient := &http.Client{Timeout: 30 * time.Second}
	authCtx := context.WithValue(ctx, oauth2.HTTPClient, baseClient)

	return config.Client(authCtx, token), nil
}
