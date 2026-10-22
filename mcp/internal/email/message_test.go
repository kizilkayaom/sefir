package email

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestBuildMessagePreservesMessageID(t *testing.T) {
	var input MessageInput
	input.From = "sender@example.com"
	recipient := "recipient@example.com"
	input.Draft.RecipientEmail = &recipient
	input.Draft.Subject = "Hello"
	input.Draft.Body = "Hello there"
	input.MessageID = "<test-123@example.com>"
	input.Date = time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)

	raw, err := BuildMessage(input)
	if err != nil {
		t.Fatal(err)
	}
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	if message.Header.Get("Message-ID") != input.MessageID {
		t.Fatalf("Message-ID = %q, want %q",
			message.Header.Get("Message-ID"), input.MessageID)
	}
}

func TestBuildMessageBodyRoundTrip(t *testing.T) {
	var input MessageInput
	test_string := "Hello\n\nMerhaba Ömer 🌍"
	input.From = "sender@example.com"
	recipient := "recipient@example.com"
	input.Draft.RecipientEmail = &recipient
	input.Draft.Subject = "Hello"
	input.Draft.Body = test_string
	input.MessageID = "<test-123@example.com>"
	input.Date = time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)

	raw, err := BuildMessage(input)
	if err != nil {
		t.Fatal(err)
	}
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := io.ReadAll(
		base64.NewDecoder(base64.StdEncoding, message.Body),
	)

	if err != nil {
		t.Fatal(err)
	}

	want := "Hello\r\n\r\nMerhaba Ömer 🌍"
	if string(decoded) != want {
		t.Fatalf("Body = %q, want %q", string(decoded), want)
	}
}

func TestBuildMessageSubjectRoundTrip(t *testing.T) {
	var input MessageInput
	subject := strings.Repeat("İş başvurusu 🌍 ", 20)
	input.From = "sender@example.com"
	recipient := "recipient@example.com"
	input.Draft.RecipientEmail = &recipient
	input.Draft.Subject = subject
	input.Draft.Body = "Hello there"
	input.MessageID = "<test-123@example.com>"
	input.Date = time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)

	raw, err := BuildMessage(input)
	if err != nil {
		t.Fatal(err)
	}
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}

	decoder := new(mime.WordDecoder)
	decoded, err := decoder.DecodeHeader(message.Header.Get("Subject"))
	if err != nil {
		t.Fatal(err)
	}
	if decoded != subject {
		t.Fatalf("Subject = %q, want %q", decoded, subject)
	}
}

func TestBuildMessageRejectsSubjectLineBreaks(t *testing.T) {
	var input MessageInput
	input.From = "sender@example.com"
	recipient := "recipient@example.com"
	input.Draft.RecipientEmail = &recipient
	input.Draft.Subject = "Hello\r\nBcc: other@example.com"
	input.Draft.Body = "Hello there"
	input.MessageID = "<test-123@example.com>"
	input.Date = time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)

	raw, err := BuildMessage(input)
	if err == nil {
		t.Fatal("expected an error for subject line breaks")
	}
	if raw != nil {
		t.Fatal("expected no message for invalid input")
	}
}
