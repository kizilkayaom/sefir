package email

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/kizilkayaom/sefir/mcp/internal/draftcontent"
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

func TestBuildFormattedMessageRoundTrip(t *testing.T) {
	recipient := "recipient@example.com"
	subject, body, bodyHTML, err := draftcontent.Prepare("Hello", "", "<p>Hello <strong>Ömer 🌍</strong></p><ul><li>First</li><li>Second</li></ul>")
	if err != nil {
		t.Fatal(err)
	}
	input := MessageInput{
		From: "sender@example.com", MessageID: "<formatted@example.com>",
		Date:  time.Date(2026, time.October, 28, 12, 0, 0, 0, time.UTC),
		Draft: ApprovedDraft{RecipientEmail: &recipient, Subject: subject, Body: body, BodyHTML: bodyHTML},
	}
	raw, err := BuildMessage(input)
	if err != nil {
		t.Fatal(err)
	}
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	kind, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil || kind != "multipart/alternative" || params["boundary"] == "" {
		t.Fatalf("unexpected MIME content: %s", message.Header.Get("Content-Type"))
	}
	parts := multipart.NewReader(message.Body, params["boundary"])
	for _, expected := range []struct{ kind, body string }{{"text/plain", body}, {"text/html", bodyHTML}} {
		part, err := parts.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		if part.Header.Get("Content-Type") != expected.kind+"; charset=utf-8" || part.Header.Get("Content-Transfer-Encoding") != "base64" {
			t.Fatal(part.Header)
		}
		decoded, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, part))
		if err != nil || string(decoded) != strings.ReplaceAll(expected.body, "\n", "\r\n") {
			t.Fatalf("formatted message content changed: %q, %v", decoded, err)
		}
	}
	if _, err := parts.NextPart(); err != io.EOF {
		t.Fatal("expected exactly two alternatives")
	}
	input.Draft.BodyHTML = "<p onclick=\"alert(1)\">Changed</p>"
	if _, err := BuildMessage(input); err == nil {
		t.Fatal("accepted unsafe content that differs from the text alternative")
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
