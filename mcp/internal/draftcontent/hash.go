package draftcontent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func Hash(recipientEmail *string, subject, body string) (string, error) {
	return HashWithHTML(recipientEmail, subject, body, "")
}

func HashWithHTML(recipientEmail *string, subject, body, bodyHTML string) (string, error) {
	type DraftHash struct {
		RecipientEmail *string `json:"recipient_email"`
		Subject        string  `json:"subject"`
		Body           string  `json:"body"`
		BodyHTML       string  `json:"body_html,omitempty"`
	}

	payload := DraftHash{
		RecipientEmail: recipientEmail,
		Subject:        subject,
		Body:           body,
		BodyHTML:       bodyHTML,
	}
	serial, err := json.Marshal(payload)

	if err != nil {
		return "", err
	}

	digest := sha256.Sum256(serial)
	return hex.EncodeToString(digest[:]), nil
}
