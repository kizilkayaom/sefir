package email

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func FindSentGmailMessage(
	ctx context.Context,
	client *http.Client,
	rfc822MessageID string,
) (string, bool, error) {
	if client == nil {
		return "", false, errors.New("HTTP client is required")
	}
	if strings.ContainsAny(rfc822MessageID, "\r\n") {
		return "", false, errors.New("message ID must not contain line breaks")
	}
	rfc822MessageID = strings.TrimSpace(rfc822MessageID)
	if rfc822MessageID == "" {
		return "", false, errors.New("message ID is required")
	}
	if !strings.HasPrefix(rfc822MessageID, "<") || !strings.HasSuffix(rfc822MessageID, ">") {
		return "", false, errors.New("message ID must be enclosed in angle brackets")
	}
	localPart, domain, ok := strings.Cut(rfc822MessageID[1:len(rfc822MessageID)-1], "@")
	if !ok || localPart == "" || domain == "" {
		return "", false, errors.New("message ID must contain a local part and domain separated by @")
	}
	for _, char := range localPart {
		isLetter := (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
		isDigit := char >= '0' && char <= '9'
		if !isLetter && !isDigit && !strings.ContainsRune("._+-", char) {
			return "", false, errors.New("message ID local part contains an unsupported character")
		}
	}
	if err := validateMessageIDDomain(domain); err != nil {
		return "", false, fmt.Errorf("invalid message ID domain: %w", err)
	}

	query := url.Values{}
	query.Set("q", "rfc822msgid:"+rfc822MessageID)
	query.Set("labelIds", "SENT")
	query.Set("maxResults", "2")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://gmail.googleapis.com/gmail/v1/users/me/messages?"+query.Encode(), nil)
	if err != nil {
		return "", false, err
	}
	response, err := client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusUnauthorized {
		return "", false, ErrGmailReconnectRequired
	}
	if response.StatusCode != http.StatusOK {
		var apiError struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(response.Body).Decode(&apiError); err == nil && strings.TrimSpace(apiError.Error.Message) != "" {
			return "", false, fmt.Errorf("Gmail lookup request failed: HTTP %d: %s", response.StatusCode, apiError.Error.Message)
		}
		return "", false, fmt.Errorf("Gmail lookup request failed: HTTP %d", response.StatusCode)
	}

	var result *struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return "", false, fmt.Errorf("decode Gmail lookup response: %w", err)
	}
	if result == nil {
		return "", false, errors.New("Gmail lookup response must be an object")
	}
	if len(result.Messages) > 1 || result.NextPageToken != "" {
		return "", false, errors.New("Gmail message ID lookup is ambiguous: multiple matches or additional results")
	}
	if len(result.Messages) == 0 {
		return "", false, nil
	}

	messageID := strings.TrimSpace(result.Messages[0].ID)
	if messageID == "" {
		return "", false, errors.New("Gmail lookup response is missing a message ID")
	}
	return messageID, true, nil
}
