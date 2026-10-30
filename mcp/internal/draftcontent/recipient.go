package draftcontent

import (
	"errors"
	"net/mail"
	"strings"
)

func NormalizeRecipient(value string) (string, error) {
	if strings.ContainsAny(value, "\r\n") {
		return "", errors.New("enter one recipient email address without line breaks")
	}
	value = strings.TrimSpace(value)
	address, err := mail.ParseAddress(value)
	if err != nil || len(value) > 254 || address.Name != "" || address.Address != value {
		return "", errors.New("enter one valid recipient email address without a display name")
	}
	return value, nil
}
