package email

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type GmailSendResult struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
}

type GmailAPIError struct {
	StatusCode int
	Message    string
}

func SendGmailMessage(
	ctx context.Context,
	client *http.Client,
	rawMessage []byte,
) (GmailSendResult, error) {
	if client == nil {
		return GmailSendResult{}, errors.New("Invalid client")
	}

	if len(rawMessage) == 0 {
		return GmailSendResult{}, errors.New("Raw message cannot be empty")
	}

	payload, err := json.Marshal(map[string]string{
		"raw": base64.RawURLEncoding.EncodeToString(rawMessage),
	})

	if err != nil {
		return GmailSendResult{}, err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://gmail.googleapis.com/gmail/v1/users/me/messages/send",
		bytes.NewReader(payload),
	)
	if err != nil {
		return GmailSendResult{}, err
	}

	req.Header.Set("Content-Type", "application/json")

	response, err := client.Do(req)
	if err != nil {
		return GmailSendResult{}, err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusUnauthorized {
		return GmailSendResult{}, ErrGmailReconnectRequired
	}

	if response.StatusCode != http.StatusOK {
		var apiError struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}

		err := json.NewDecoder(response.Body).Decode(&apiError)
		if err == nil && apiError.Error.Message != "" {
			return GmailSendResult{}, &GmailAPIError{
				StatusCode: response.StatusCode,
				Message:    apiError.Error.Message,
			}
		}

		return GmailSendResult{}, &GmailAPIError{
			StatusCode: response.StatusCode,
		}

	}

	var result GmailSendResult

	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return GmailSendResult{}, err
	}

	if strings.TrimSpace(result.ID) == "" {
		return GmailSendResult{}, errors.New("Gmail send response is missing a message ID")
	}

	return result, nil
}

func (e *GmailAPIError) Error() string {
	if strings.TrimSpace(e.Message) != "" {
		return fmt.Sprintf("Gmail send request failed: HTTP %d: %s", e.StatusCode, e.Message)
	} else {
		return fmt.Sprintf("Gmail send request failed: HTTP %d", e.StatusCode)
	}
}
