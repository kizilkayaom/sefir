package email

import (
	"errors"
	"strings"
)

func validateMessageIDDomain(domain string) error {
	if domain == "" {
		return errors.New("message ID domain is required")
	}
	if len(domain) > 253 {
		return errors.New("message ID domain must be at most 253 bytes")
	}

	labels := strings.Split(domain, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return errors.New("message ID domain labels must be between 1 and 63 bytes")
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("message ID domain labels must not start or end with a hyphen")
		}

		for _, char := range label {
			isLetter := (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
			isDigit := char >= '0' && char <= '9'
			if !isLetter && !isDigit && char != '-' {
				return errors.New("message ID domain labels must contain only ASCII letters, digits, or hyphens")
			}
		}
	}

	return nil
}
