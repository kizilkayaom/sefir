package email

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFindSentGmailMessageFindsUniqueMatch(t *testing.T) {
	requests := 0
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			if req.Method != http.MethodGet {
				t.Errorf("request method = %q, want GET", req.Method)
			}
			if req.URL.Scheme != "https" || req.URL.Host != "gmail.googleapis.com" {
				t.Errorf("request origin = %s://%s, want https://gmail.googleapis.com", req.URL.Scheme, req.URL.Host)
			}
			const wantPath = "/gmail/v1/users/me/messages"
			if req.URL.Path != wantPath {
				t.Errorf("request path = %q, want %q", req.URL.Path, wantPath)
			}

			query := req.URL.Query()
			const wantQuery = "rfc822msgid:<sefir.test+one@example.com>"
			if got := query.Get("q"); got != wantQuery {
				t.Errorf("q = %q, want %q", got, wantQuery)
			}
			if got := query.Get("labelIds"); got != "SENT" {
				t.Errorf("labelIds = %q, want SENT", got)
			}
			if got := query.Get("maxResults"); got != "2" {
				t.Errorf("maxResults = %q, want 2", got)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					`{"messages":[{"id":"gmail-123","threadId":"thread-456"}]}`,
				)),
				Request: req,
			}, nil
		}),
	}

	messageID, found, err := FindSentGmailMessage(context.Background(), client, "<sefir.test+one@example.com>")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Error("found = false, want true")
	}
	if messageID != "gmail-123" {
		t.Errorf("message ID = %q, want gmail-123", messageID)
	}
	if requests != 1 {
		t.Errorf("request count = %d, want 1", requests)
	}
}

func TestFindSentGmailMessageReturnsNotFound(t *testing.T) {
	requests := 0

	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("{}")),
				Request:    req,
			}, nil
		}),
	}
	messageID, found, err := FindSentGmailMessage(context.Background(), client, "<sefir.test+one@example.com>")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("found = true, want false")
	}
	if messageID != "" {
		t.Errorf("message ID = %q, want empty", messageID)
	}
	if requests != 1 {
		t.Errorf("request count = %d, want 1", requests)
	}
}

func TestFindSentGmailMessageRejectsMultipleMatches(t *testing.T) {
	requests := 0
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					`{"messages":[{"id":"gmail-123"},{"id":"gmail-456"}]}`,
				)),
				Request: req,
			}, nil
		}),
	}

	messageID, found, err := FindSentGmailMessage(context.Background(), client, "<sefir.test+one@example.com>")
	if err == nil {
		t.Fatal("lookup error = nil, want an error for multiple matches")
	}
	if found {
		t.Error("found = true, want false")
	}
	if messageID != "" {
		t.Errorf("message ID = %q, want empty", messageID)
	}
	if requests != 1 {
		t.Errorf("request count = %d, want 1", requests)
	}
}

func TestFindSentGmailMessageRejectsAdditionalPages(t *testing.T) {
	requests := 0
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					`{"messages":[{"id":"gmail-123"}],"nextPageToken":"page-2"}`,
				)),
				Request: req,
			}, nil
		}),
	}

	messageID, found, err := FindSentGmailMessage(context.Background(), client, "<sefir.test+one@example.com>")
	if err == nil {
		t.Fatal("lookup error = nil, want an error for additional pages")
	}
	if found {
		t.Error("found = true, want false")
	}
	if messageID != "" {
		t.Errorf("message ID = %q, want empty", messageID)
	}
	if requests != 1 {
		t.Errorf("request count = %d, want 1", requests)
	}
}

func TestFindSentGmailMessageRequiresReconnect(t *testing.T) {
	requests := 0
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Body:       io.NopCloser(strings.NewReader("{}")),
				Request:    req,
			}, nil
		}),
	}

	messageID, found, err := FindSentGmailMessage(context.Background(), client, "<sefir.test+one@example.com>")
	if !errors.Is(err, ErrGmailReconnectRequired) {
		t.Fatalf("lookup error = %v, want ErrGmailReconnectRequired", err)
	}
	if found {
		t.Error("found = true, want false")
	}
	if messageID != "" {
		t.Errorf("message ID = %q, want empty", messageID)
	}
	if requests != 1 {
		t.Errorf("request count = %d, want 1", requests)
	}
}

func TestFindSentGmailMessageDoesNotRetryTransportErrors(t *testing.T) {
	requests := 0
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			return nil, context.DeadlineExceeded
		}),
	}

	messageID, found, err := FindSentGmailMessage(context.Background(), client, "<sefir.test+one@example.com>")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lookup error = %v, want context.DeadlineExceeded", err)
	}
	if found {
		t.Error("found = true, want false")
	}
	if messageID != "" {
		t.Errorf("message ID = %q, want empty", messageID)
	}
	if requests != 1 {
		t.Errorf("request count = %d, want 1", requests)
	}
}

func TestFindSentGmailMessagePreservesHTTPFailures(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantError  string
	}{
		{
			name:       "forbidden with error details",
			statusCode: http.StatusForbidden,
			body:       `{"error":{"message":"permission denied"}}`,
			wantError:  "HTTP 403: permission denied",
		},
		{
			name:       "service unavailable without JSON",
			statusCode: http.StatusServiceUnavailable,
			body:       "Service unavailable",
			wantError:  "HTTP 503",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			client := &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					requests++
					return &http.Response{
						StatusCode: tt.statusCode,
						Body:       io.NopCloser(strings.NewReader(tt.body)),
						Request:    req,
					}, nil
				}),
			}

			messageID, found, err := FindSentGmailMessage(context.Background(), client, "<sefir.test+one@example.com>")
			if err == nil {
				t.Fatal("lookup error = nil, want an HTTP error")
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Errorf("lookup error = %v, want it to contain %q", err, tt.wantError)
			}
			if errors.Is(err, ErrGmailReconnectRequired) {
				t.Error("lookup error = ErrGmailReconnectRequired, want the HTTP failure")
			}
			if found {
				t.Error("found = true, want false")
			}
			if messageID != "" {
				t.Errorf("message ID = %q, want empty", messageID)
			}
			if requests != 1 {
				t.Errorf("request count = %d, want 1", requests)
			}
		})
	}
}

func TestFindSentGmailMessageRejectsInvalidResponses(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantError string
	}{
		{
			name:      "malformed JSON",
			body:      `{"messages":`,
			wantError: "decode Gmail lookup response",
		},
		{
			name:      "null response",
			body:      "null",
			wantError: "must be an object",
		},
		{
			name:      "array response",
			body:      "[]",
			wantError: "decode Gmail lookup response",
		},
		{
			name:      "missing message ID",
			body:      `{"messages":[{"threadId":"thread-456"}]}`,
			wantError: "missing a message ID",
		},
		{
			name:      "empty message ID",
			body:      `{"messages":[{"id":""}]}`,
			wantError: "missing a message ID",
		},
		{
			name:      "blank message ID",
			body:      `{"messages":[{"id":" \t "}]}`,
			wantError: "missing a message ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			client := &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					requests++
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(tt.body)),
						Request:    req,
					}, nil
				}),
			}

			messageID, found, err := FindSentGmailMessage(context.Background(), client, "<sefir.test+one@example.com>")
			if err == nil {
				t.Fatal("lookup error = nil, want an error for the invalid response")
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Errorf("lookup error = %v, want it to contain %q", err, tt.wantError)
			}
			if found {
				t.Error("found = true, want false")
			}
			if messageID != "" {
				t.Errorf("message ID = %q, want empty", messageID)
			}
			if requests != 1 {
				t.Errorf("request count = %d, want 1", requests)
			}
		})
	}
}

func TestFindSentGmailMessageRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name      string
		messageID string
		nilClient bool
	}{
		{name: "nil client", messageID: "<sefir.test+one@example.com>", nilClient: true},
		{name: "empty message ID", messageID: ""},
		{name: "blank message ID", messageID: " \t "},
		{name: "missing opening bracket", messageID: "sefir.test@example.com>"},
		{name: "missing closing bracket", messageID: "<sefir.test@example.com"},
		{name: "empty brackets", messageID: "<>"},
		{name: "missing separator", messageID: "<sefir.test>"},
		{name: "missing local part", messageID: "<@example.com>"},
		{name: "missing domain", messageID: "<sefir.test@>"},
		{name: "multiple separators", messageID: "<sefir.test@@example.com>"},
		{name: "space in local part", messageID: "<sefir test@example.com>"},
		{name: "line break in message ID", messageID: "<sefir.\ntest@example.com>"},
		{name: "surrounding line breaks", messageID: "\r\n<sefir.test@example.com>\r\n"},
		{name: "query syntax in local part", messageID: "<sefir:test@example.com>"},
		{name: "invalid domain", messageID: "<sefir.test@-example.com>"},
		{name: "nested brackets", messageID: "<<sefir.test@example.com>>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			var client *http.Client
			if !tt.nilClient {
				client = &http.Client{
					Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						requests++
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader("{}")),
							Request:    req,
						}, nil
					}),
				}
			}

			messageID, found, err := FindSentGmailMessage(context.Background(), client, tt.messageID)
			if err == nil {
				t.Error("lookup error = nil, want an error for the invalid input")
			}
			if found {
				t.Error("found = true, want false")
			}
			if messageID != "" {
				t.Errorf("message ID = %q, want empty", messageID)
			}
			if requests != 0 {
				t.Errorf("request count = %d, want 0", requests)
			}
		})
	}
}
