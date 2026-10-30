package draftcontent

import (
	"strings"
	"testing"
)

func TestNormalizeRecipient(t *testing.T) {
	for _, value := range []string{"person@example.com", " person@example.com "} {
		got, err := NormalizeRecipient(value)
		if err != nil || got != "person@example.com" {
			t.Fatalf("recipient = %q, error = %v", got, err)
		}
	}
	for _, value := range []string{
		"", "not-an-email", "Name <person@example.com>",
		"one@example.com, two@example.com", "one@example.com\r\nBcc: two@example.com",
		"person@example.com\n", strings.Repeat("a", 243) + "@example.com",
	} {
		if _, err := NormalizeRecipient(value); err == nil {
			t.Fatalf("accepted invalid recipient %q", value)
		}
	}
}
