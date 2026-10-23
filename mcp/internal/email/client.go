package email

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

type GmailProfile struct {
	EmailAddress string `json:"emailAddress"`
}

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
	source := &savingTokenSource{
		source:    config.TokenSource(authCtx, token),
		tokenPath: tokenPath,
	}
	return oauth2.NewClient(authCtx, source), nil
}

func GetGmailProfile(ctx context.Context, client *http.Client) (GmailProfile, error) {
	if client == nil {
		return GmailProfile{}, errors.New("Invalid http client")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://gmail.googleapis.com/gmail/v1/users/me/profile", nil)
	if err != nil {
		return GmailProfile{}, err
	}

	response, err := client.Do(req)
	if err != nil {
		return GmailProfile{}, err
	}

	defer response.Body.Close()

	if response.StatusCode == http.StatusUnauthorized {
		return GmailProfile{}, ErrGmailReconnectRequired
	}

	if response.StatusCode != http.StatusOK {
		var apiError struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}

		err := json.NewDecoder(response.Body).Decode(&apiError)
		if err == nil && apiError.Error.Message != "" {
			return GmailProfile{}, fmt.Errorf(
				"Gmail profile request failed: HTTP %d: %s",
				response.StatusCode,
				apiError.Error.Message,
			)
		}

		return GmailProfile{}, fmt.Errorf(
			"Gmail profile request failed: HTTP %d",
			response.StatusCode,
		)
	}

	var profile GmailProfile
	if err := json.NewDecoder(response.Body).Decode(&profile); err != nil {
		return GmailProfile{}, err
	}

	if strings.TrimSpace(profile.EmailAddress) == "" {
		return GmailProfile{}, errors.New("Invalid email address")
	}

	return profile, nil
}
