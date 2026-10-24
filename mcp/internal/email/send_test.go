package email

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSendGmailMessageEncodesRequest(t *testing.T) {
	rawMessage := []byte("From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: Hello\r\n" +
		"Message-ID: <test-send@example.com>\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n" +
		"\r\n" +
		"Merhaba Ömer 🌍\r\n")
	requests := 0
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			if req.Method != http.MethodPost {
				t.Errorf("request method = %q, want POST", req.Method)
			}
			const wantURL = "https://gmail.googleapis.com/gmail/v1/users/me/messages/send"
			if req.URL.String() != wantURL {
				t.Errorf("request URL = %q, want %q", req.URL.String(), wantURL)
			}
			if got := req.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
			if req.Body == nil {
				return nil, fmt.Errorf("send request has no body")
			}
			defer req.Body.Close()

			var payload struct {
				Raw string `json:"raw"`
			}
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
				return nil, fmt.Errorf("decode send request: %w", err)
			}
			decoded, err := base64.RawURLEncoding.DecodeString(payload.Raw)
			if err != nil {
				return nil, fmt.Errorf("decode raw message: %w", err)
			}
			if !bytes.Equal(decoded, rawMessage) {
				t.Errorf("decoded message = %q, want %q", decoded, rawMessage)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					`{"id":"gmail-message-123","threadId":"gmail-thread-456"}`,
				)),
				Request: req,
			}, nil
		}),
	}

	result, err := SendGmailMessage(context.Background(), client, rawMessage)
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != "gmail-message-123" {
		t.Errorf("message ID = %q, want gmail-message-123", result.ID)
	}
	if result.ThreadID != "gmail-thread-456" {
		t.Errorf("thread ID = %q, want gmail-thread-456", result.ThreadID)
	}
	if requests != 1 {
		t.Errorf("request count = %d, want 1", requests)
	}
}

func TestSendGmailMessageDoesNotRetryTransportErrors(t *testing.T) {
	sendErr := errors.New("connection lost after sending request")
	requests := 0

	rawMessage := []byte("From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: Hello\r\n" +
		"Message-ID: <test-send@example.com>\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n" +
		"\r\n" +
		"Merhaba Ömer 🌍\r\n")
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			if req.Body != nil {
				req.Body.Close()
			}
			return nil, sendErr
		}),
	}
	result, err := SendGmailMessage(context.Background(), client, rawMessage)
	if !errors.Is(err, sendErr) {
		t.Fatalf("send error = %v, want %v", err, sendErr)
	}

	if !(result.ID == "" && result.ThreadID == "") {
		t.Errorf("message ID = %q, thread ID = %q, want both empty", result.ID, result.ThreadID)
	}

	if requests != 1 {
		t.Errorf("request count = %d, want 1", requests)
	}
}

func TestSendGmailMessageRequiresReconnect(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Body != nil {
				req.Body.Close()
			}
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		}),
	}
	rawMessage := []byte("To: recipient@example.com\r\nSubject: Hello\r\n\r\nHello\r\n")

	result, err := SendGmailMessage(context.Background(), client, rawMessage)
	if !errors.Is(err, ErrGmailReconnectRequired) {
		t.Fatalf("send error = %v, want %v", err, ErrGmailReconnectRequired)
	}
	if result.ID != "" || result.ThreadID != "" {
		t.Errorf("message ID = %q, thread ID = %q, want both empty", result.ID, result.ThreadID)
	}
}

func TestSendGmailMessageRejectsMissingMessageID(t *testing.T) {
	requests := 0
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			if req.Body != nil {
				req.Body.Close()
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"threadId":"gmail-thread-456"}`)),
				Request:    req,
			}, nil
		}),
	}
	rawMessage := []byte("To: recipient@example.com\r\nSubject: Hello\r\n\r\nHello\r\n")

	result, err := SendGmailMessage(context.Background(), client, rawMessage)
	if err == nil {
		t.Fatal("send error = nil, want an error for the missing message ID")
	}
	if result.ID != "" || result.ThreadID != "" {
		t.Errorf("message ID = %q, thread ID = %q, want both empty", result.ID, result.ThreadID)
	}
	if requests != 1 {
		t.Errorf("request count = %d, want 1", requests)
	}
}

func TestSendGmailMessagePreservesAPIError(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Body != nil {
				req.Body.Close()
			}
			return &http.Response{
				StatusCode: http.StatusForbidden,
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"permission denied"}}`)),
				Request:    req,
			}, nil
		}),
	}
	rawMessage := []byte("To: recipient@example.com\r\nSubject: Hello\r\n\r\nHello\r\n")

	result, err := SendGmailMessage(context.Background(), client, rawMessage)
	var apiErr *GmailAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("send error = %v, want *GmailAPIError", err)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Errorf("HTTP status = %d, want %d", apiErr.StatusCode, http.StatusForbidden)
	}
	if apiErr.Message != "permission denied" {
		t.Errorf("API message = %q, want permission denied", apiErr.Message)
	}
	if result.ID != "" || result.ThreadID != "" {
		t.Errorf("message ID = %q, thread ID = %q, want both empty", result.ID, result.ThreadID)
	}
}
