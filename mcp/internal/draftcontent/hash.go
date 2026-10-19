package draftcontent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func Hash(recipientEmail *string, subject, body string) (string, error) {
	type DraftHash struct {
		RecipientEmail *string `json:"recipient_email"`
		Subject        string  `json:"subject"`
		Body           string  `json:"body"`
	}

	payload := DraftHash{
		RecipientEmail: recipientEmail,
		Subject:        subject,
		Body:           body,
	}
	serial, err := json.Marshal(payload)

	if err != nil {
		return "", err
	}

	digest := sha256.Sum256(serial)
	return hex.EncodeToString(digest[:]), nil
}
