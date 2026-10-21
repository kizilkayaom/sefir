package email

import (
	"encoding/base64"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

type MessageInput struct {
	From      string
	Draft     ApprovedDraft
	MessageID string
	Date      time.Time
}

func parseMailbox(value string) (*mail.Address, error) {
	if strings.ContainsAny(value, "\r\n") {
		return nil, errors.New("email address must not contain line breaks")
	}

	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("email address is required")
	}

	address, err := mail.ParseAddress(value)
	if err != nil {
		return nil, err
	}
	return address, nil
}

func BuildMessage(input MessageInput) ([]byte, error) {
	if _, err := parseMailbox(input.From); err != nil {
		return nil, err
	}
	if input.Draft.RecipientEmail == nil {
		return nil, errors.New("recipient email address is required")
	}
	if _, err := parseMailbox(*input.Draft.RecipientEmail); err != nil {
		return nil, err
	}

	if strings.TrimSpace(input.Draft.Subject) == "" {
		return nil, errors.New("subject is required")
	}
	if strings.TrimSpace(input.Draft.Body) == "" {
		return nil, errors.New("body is required")
	}
	if strings.ContainsAny(input.Draft.Subject, "\r\n") {
		return nil, errors.New("subject must not contain line breaks")
	}
	if strings.ContainsAny(input.MessageID, "\r\n") {
		return nil, errors.New("message ID must not contain line breaks")
	}
	if strings.TrimSpace(input.MessageID) == "" {
		return nil, errors.New("message ID is required")
	}
	if input.Date.IsZero() {
		return nil, errors.New("message date is required")
	}
	if !utf8.ValidString(input.Draft.Subject) {
		return nil, errors.New("subject must contain valid UTF-8")
	}
	if !utf8.ValidString(input.Draft.Body) {
		return nil, errors.New("body must contain valid UTF-8")
	}

	return nil, errors.New("message encoding is not implemented yet")
}

func encodeMessageBody(body string) string {
	normalized := strings.ReplaceAll(body, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	normalized = strings.ReplaceAll(normalized, "\n", "\r\n")

	encoded := base64.StdEncoding.EncodeToString([]byte(normalized))
	var builder strings.Builder

	for start := 0; start < len(encoded); start += 76 {
		end := min(start+76, len(encoded))

		builder.WriteString(encoded[start:end])
		builder.WriteString("\r\n")
	}

	return builder.String()

}

func encodeSubject(subject string) string {
	var builder strings.Builder

	for start := 0; start < len(subject); {
		end := min(start+39, len(subject))

		for end < len(subject) && !utf8.RuneStart(subject[end]) {
			end--
		}

		if builder.Len() > 0 {
			builder.WriteString("\r\n ")
		}
		builder.WriteString("=?utf-8?B?")
		builder.WriteString(base64.StdEncoding.EncodeToString([]byte(subject[start:end])))
		builder.WriteString("?=")

		start = end
	}

	return builder.String()
}
