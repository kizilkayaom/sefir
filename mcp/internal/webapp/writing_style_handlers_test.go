package webapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/kizilkayaom/sefir/mcp/internal/writingstyle"
)

const validWritingStyleUpload = "---\nformat_version: 1\nname: Warm and direct\nlanguage: en\n---\n## Style\nUse short sentences.\n\n---\n\n    Keep this indentation.\n"

type writingStyleUploadPart struct {
	field    string
	filename string
	content  string
}

func writingStyleUploadBody(t *testing.T, parts ...writingStyleUploadPart) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, part := range parts {
		var output io.Writer
		var err error
		if part.filename == "" {
			output, err = writer.CreateFormField(part.field)
		} else {
			output, err = writer.CreateFormFile(part.field, part.filename)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(output, part.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func writingStyleUploadRequest(body []byte, contentType string, cookie *http.Cookie, csrf string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://localhost:8080/api/settings/writing-styles", bytes.NewReader(body))
	r.Header.Set("Origin", "http://localhost:8080")
	r.Header.Set("Content-Type", contentType)
	r.Header.Set("X-CSRF-Token", csrf)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	return r
}

func TestHandleWritingStyles(t *testing.T) {
	server, store, _, _ := fixture(t)
	cookie, _ := login(t, server)
	accountID := server.sessions[cookie.Value].AccountID
	created := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	profiles := make([]WritingStyleProfile, 101)
	for i := range profiles {
		profiles[i] = WritingStyleProfile{
			ProfileID: fmt.Sprintf("%08x-0000-4000-8000-000000000001", i+1),
			Name:      fmt.Sprintf("Style %d", i+1), Language: "en", Content: "## Style\nUse short sentences.",
			FormatVersion: 1, Revision: i + 1, CreatedAt: created, UpdatedAt: created.Add(time.Hour),
		}
	}
	for _, test := range []struct {
		name    string
		query   string
		items   []WritingStyleProfile
		limit   int
		offset  int
		count   int
		hasMore bool
	}{
		{name: "defaults", items: profiles[:2], limit: 25, count: 2},
		{name: "next page exists", query: "?limit=2&offset=10", items: profiles[:3], limit: 2, offset: 10, count: 2, hasMore: true},
		{name: "last page", query: "?limit=2&offset=2", items: profiles[2:3], limit: 2, offset: 2, count: 1},
		{name: "empty page", query: "?limit=2&offset=3", limit: 2, offset: 3},
		{name: "maximum limit", query: "?limit=100", items: profiles, limit: 100, count: 100, hasMore: true},
		{name: "maximum offset", query: "?offset=1000000", limit: 25, offset: 1000000},
		{name: "account parameter cannot change owner", query: "?account_id=" + testID, items: profiles[:1], limit: 25, count: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			store.writingStyles = test.items
			calls := store.writingStylesCalls
			before := time.Now()
			response := request(server, "GET", "/api/settings/writing-styles"+test.query, "", cookie, "")
			if response.Code != http.StatusOK {
				t.Fatalf("list writing styles: %d %s", response.Code, response.Body.String())
			}
			var result struct {
				Items      []WritingStyleProfile `json:"items"`
				HasMore    bool                  `json:"has_more"`
				Offset     int                   `json:"offset"`
				NextOffset int                   `json:"next_offset"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Items == nil || len(result.Items) != test.count || result.HasMore != test.hasMore ||
				result.Offset != test.offset || result.NextOffset != test.offset+test.count {
				t.Fatalf("unexpected page: %s", response.Body.String())
			}
			for i, profile := range result.Items {
				if profile != test.items[i] {
					t.Fatalf("profile metadata or order changed: %+v", profile)
				}
			}
			if store.writingStylesCalls != calls+1 || store.limit != test.limit+1 || store.offset != test.offset || store.writingStylesAccountID != accountID {
				t.Fatalf("wrong storage request: account=%s limit=%d offset=%d calls=%d", store.writingStylesAccountID, store.limit, store.offset, store.writingStylesCalls)
			}
			if store.writingStylesDeadline.Before(before.Add(5*time.Second)) || store.writingStylesDeadline.After(time.Now().Add(5*time.Second)) {
				t.Fatal("storage did not receive a five-second deadline")
			}
		})
	}
}

func TestHandleWritingStylesRejectsInvalidPagination(t *testing.T) {
	server, store, _, _ := fixture(t)
	cookie, _ := login(t, server)
	for _, query := range []string{
		"limit=0", "limit=-1", "limit=101", "limit=invalid", "limit=1.5",
		"limit=999999999999999999999", "offset=-1", "offset=1000001", "offset=invalid",
	} {
		t.Run(query, func(t *testing.T) {
			response := request(server, "GET", "/api/settings/writing-styles?"+query, "", cookie, "")
			var result map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusBadRequest || result["error"] != "Invalid pagination" || store.writingStylesCalls != 0 {
				t.Fatalf("invalid pagination reached storage or returned the wrong error: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestHandleWritingStylesRequiresActiveSession(t *testing.T) {
	server, store, _, _ := fixture(t)
	for _, cookie := range []*http.Cookie{nil, {Name: "sefir_session", Value: "unknown"}} {
		response := request(server, "GET", "/api/settings/writing-styles", "", cookie, "")
		if response.Code != http.StatusUnauthorized || store.writingStylesCalls != 0 {
			t.Fatalf("unauthenticated listing: %d %s", response.Code, response.Body.String())
		}
	}
	cookie, _ := login(t, server)
	accountID := server.sessions[cookie.Value].AccountID
	if err := server.accounts.RequestDeletion(context.Background(), accountID); err != nil {
		t.Fatal(err)
	}
	response := request(server, "GET", "/api/settings/writing-styles", "", cookie, "")
	if response.Code != http.StatusNotFound || store.writingStylesCalls != 0 {
		t.Fatalf("deleting account reached profile storage: %d %s", response.Code, response.Body.String())
	}
}

func TestHandleWritingStylesHandlesStoreErrors(t *testing.T) {
	server, store, _, _ := fixture(t)
	cookie, _ := login(t, server)
	for _, test := range []struct {
		err     error
		status  int
		message string
	}{
		{pgx.ErrNoRows, 404, "Record not found"},
		{errors.New("private database details"), 503, "Database operation failed. Please try again."},
		{context.DeadlineExceeded, 503, "Database operation failed. Please try again."},
	} {
		store.err = test.err
		calls := store.writingStylesCalls
		response := request(server, "GET", "/api/settings/writing-styles", "", cookie, "")
		var result map[string]string
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if response.Code != test.status || len(result) != 1 || result["error"] != test.message || store.writingStylesCalls != calls+1 {
			t.Fatalf("unexpected error response: %d %s", response.Code, response.Body.String())
		}
	}
}

func TestUploadWritingStyle(t *testing.T) {
	server, store, bridge, _ := fixture(t)
	cookie, csrf := login(t, server)
	accountID := server.sessions[cookie.Value].AccountID
	content := "## Style\nUse short sentences.\n\n---\n\n    Keep this indentation.\n"
	created := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	store.writingStyle = WritingStyleProfile{
		ProfileID: testID, Name: "Warm and direct", Language: "en", Content: content,
		FormatVersion: 1, Revision: 1, CreatedAt: created, UpdatedAt: created,
	}
	for _, test := range []struct {
		name       string
		file       string
		maxRequest bool
		streamed   bool
	}{
		{name: "valid upload", file: validWritingStyleUpload},
		{
			name: "32 KiB file",
			file: strings.Replace(validWritingStyleUpload, "\n---\n", "\n# "+strings.Repeat("x", 32*1024-len(validWritingStyleUpload)-3)+"\n---\n", 1),
		},
		{name: "64 KiB request", file: validWritingStyleUpload, maxRequest: true},
		{name: "64 KiB request without length", file: validWritingStyleUpload, maxRequest: true, streamed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, contentType := writingStyleUploadBody(t, writingStyleUploadPart{"profile", "style.MD", test.file})
			if test.maxRequest {
				body = append(body, bytes.Repeat([]byte(" "), 64*1024-len(body))...)
			}
			r := writingStyleUploadRequest(body, contentType, cookie, csrf)
			if test.streamed {
				r.ContentLength = -1
			}
			before := time.Now()
			calls := store.writingStyleCalls
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, r)
			if response.Code != http.StatusCreated {
				t.Fatalf("upload: %d %s", response.Code, response.Body.String())
			}
			var saved WritingStyleProfile
			if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
				t.Fatal(err)
			}
			if saved != store.writingStyle {
				t.Fatalf("response lost saved profile metadata: %+v", saved)
			}
			want := writingstyle.Profile{FormatVersion: 1, Name: "Warm and direct", Language: "en", Content: content}
			if store.writingStyleCalls != calls+1 || store.writingStyleInput != want || store.writingStyleAccountID != accountID {
				t.Fatalf("wrong saved profile or account: %+v %s", store.writingStyleInput, store.writingStyleAccountID)
			}
			if store.writingStyleDeadline.Before(before) || store.writingStyleDeadline.After(time.Now().Add(5*time.Second)) {
				t.Fatal("storage did not receive a five-second deadline")
			}
			if r.MultipartForm != nil || len(bridge.calls) != 0 {
				t.Fatal("upload used disk-backed form parsing or called the AI bridge")
			}
		})
	}
}

func TestUploadWritingStyleRejectsInvalidRequests(t *testing.T) {
	server, store, _, _ := fixture(t)
	cookie, csrf := login(t, server)
	file := writingStyleUploadPart{"profile", "style.md", validWritingStyleUpload}
	for _, test := range []struct {
		name        string
		parts       []writingStyleUploadPart
		contentType string
		truncate    bool
		preamble    bool
		padding     int
		streamed    bool
		status      int
	}{
		{name: "JSON body", parts: []writingStyleUploadPart{file}, contentType: "application/json", status: 415},
		{name: "mixed multipart", parts: []writingStyleUploadPart{file}, contentType: "multipart/mixed; boundary=test", status: 415},
		{name: "missing boundary", parts: []writingStyleUploadPart{file}, contentType: "multipart/form-data", status: 400},
		{name: "wrong boundary", parts: []writingStyleUploadPart{file}, contentType: "multipart/form-data; boundary=wrong", status: 400},
		{name: "no file", status: 400},
		{name: "missing filename", parts: []writingStyleUploadPart{{"profile", "", validWritingStyleUpload}}, status: 400},
		{name: "wrong field name", parts: []writingStyleUploadPart{{"other", "style.md", validWritingStyleUpload}}, status: 400},
		{name: "two files", parts: []writingStyleUploadPart{file, file}, status: 400},
		{name: "extra account field", parts: []writingStyleUploadPart{file, {"account_id", "", testID}}, status: 400},
		{name: "field before file", parts: []writingStyleUploadPart{{"account_id", "", testID}, file}, status: 400},
		{name: "truncated multipart", parts: []writingStyleUploadPart{file}, truncate: true, status: 400},
		{name: "truncated second file", parts: []writingStyleUploadPart{file, file}, truncate: true, status: 400},
		{name: "wrong extension", parts: []writingStyleUploadPart{{"profile", "style.txt", validWritingStyleUpload}}, status: 422},
		{name: "empty file", parts: []writingStyleUploadPart{{"profile", "style.md", ""}}, status: 422},
		{name: "invalid UTF-8", parts: []writingStyleUploadPart{{"profile", "style.md", validWritingStyleUpload + "\xff"}}, status: 422},
		{name: "invalid frontmatter", parts: []writingStyleUploadPart{{"profile", "style.md", "---\nname: [\n---\nUse short sentences."}}, status: 422},
		{name: "unknown metadata", parts: []writingStyleUploadPart{{"profile", "style.md", strings.Replace(validWritingStyleUpload, "name:", "account_id: other\nname:", 1)}}, status: 422},
		{name: "oversized Markdown", parts: []writingStyleUploadPart{{"profile", "style.md", validWritingStyleUpload + strings.Repeat("x", 16*1024)}}, status: 422},
		{name: "oversized file", parts: []writingStyleUploadPart{{"profile", "style.md", strings.Repeat("x", 32*1024+1)}}, status: 413},
		{name: "oversized request", parts: []writingStyleUploadPart{file}, padding: 64 * 1024, status: 413},
		{name: "oversized request without length", parts: []writingStyleUploadPart{file}, padding: 64 * 1024, streamed: true, status: 413},
		{name: "oversized multipart preamble", parts: []writingStyleUploadPart{file}, preamble: true, streamed: true, status: 413},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, contentType := writingStyleUploadBody(t, test.parts...)
			if test.contentType != "" {
				contentType = test.contentType
			}
			if test.truncate {
				body = body[:bytes.LastIndex(body, []byte("\r\n--"))]
			}
			if test.preamble {
				body = append([]byte(strings.Repeat("padding\r\n", 9000)), body...)
			}
			body = append(body, bytes.Repeat([]byte(" "), test.padding)...)
			r := writingStyleUploadRequest(body, contentType, cookie, csrf)
			if test.streamed {
				r.ContentLength = -1
			}
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, r)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.status, response.Body.String())
			}
			var result map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result["error"] == "" {
				t.Fatalf("missing JSON error: %s %v", response.Body.String(), err)
			}
			if store.writingStyleCalls != 0 {
				t.Fatal("invalid upload reached storage")
			}
		})
	}
}

func TestUploadWritingStyleRequiresSessionAndCSRF(t *testing.T) {
	server, store, _, _ := fixture(t)
	cookie, csrf := login(t, server)
	body, contentType := writingStyleUploadBody(t, writingStyleUploadPart{"profile", "style.md", validWritingStyleUpload})
	for _, test := range []struct {
		name   string
		cookie *http.Cookie
		csrf   string
		origin string
		status int
	}{
		{name: "missing session", csrf: csrf, origin: "http://localhost:8080", status: 401},
		{name: "missing CSRF", cookie: cookie, origin: "http://localhost:8080", status: 403},
		{name: "wrong CSRF", cookie: cookie, csrf: "incorrect", origin: "http://localhost:8080", status: 403},
		{name: "wrong origin", cookie: cookie, csrf: csrf, origin: "https://other.example", status: 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := writingStyleUploadRequest(body, contentType, test.cookie, test.csrf)
			r.Header.Set("Origin", test.origin)
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, r)
			if response.Code != test.status || store.writingStyleCalls != 0 {
				t.Fatalf("unauthorized upload: %d %s; store calls: %d", response.Code, response.Body.String(), store.writingStyleCalls)
			}
		})
	}
}

func TestUploadWritingStyleHandlesStoreErrors(t *testing.T) {
	server, store, _, _ := fixture(t)
	cookie, csrf := login(t, server)
	body, contentType := writingStyleUploadBody(t, writingStyleUploadPart{"profile", "style.md", validWritingStyleUpload})
	for _, test := range []struct {
		err     error
		status  int
		message string
	}{
		{pgx.ErrNoRows, 404, "Record not found"},
		{errors.New("private database details"), 503, "Database operation failed. Please try again."},
		{context.DeadlineExceeded, 503, "Database operation failed. Please try again."},
	} {
		store.err = test.err
		calls := store.writingStyleCalls
		r := writingStyleUploadRequest(body, contentType, cookie, csrf)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, r)
		var result map[string]string
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if response.Code != test.status || len(result) != 1 || result["error"] != test.message || store.writingStyleCalls != calls+1 {
			t.Fatalf("unexpected error response: %d %s", response.Code, response.Body.String())
		}
	}
}

func TestHandleDeleteWritingStyle(t *testing.T) {
	server, store, _, _ := fixture(t)
	cookie, csrf := login(t, server)
	accountID := server.sessions[cookie.Value].AccountID
	before := time.Now()
	response := request(server, "DELETE", "/api/settings/writing-styles/"+testID, "", cookie, csrf)
	var result map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || len(result) != 1 || result["status"] != "deleted" {
		t.Fatalf("delete writing style: %d %s", response.Code, response.Body.String())
	}
	if store.writingStyleDeleteCalls != 1 || store.writingStyleDeleteID != testID || store.writingStyleDeleteAccountID != accountID {
		t.Fatalf("wrong storage request: account=%s profile=%s calls=%d", store.writingStyleDeleteAccountID, store.writingStyleDeleteID, store.writingStyleDeleteCalls)
	}
	if store.writingStyleDeleteDeadline.Before(before.Add(5*time.Second)) || store.writingStyleDeleteDeadline.After(time.Now().Add(5*time.Second)) {
		t.Fatal("storage did not receive a five-second deadline")
	}
}

func TestHandleDeleteWritingStyleRejectsInvalidID(t *testing.T) {
	server, store, _, _ := fixture(t)
	cookie, csrf := login(t, server)
	for _, id := range []string{"not-a-uuid", testID + "-extra", "gggggggg-1111-4111-8111-111111111111"} {
		t.Run(id, func(t *testing.T) {
			response := request(server, "DELETE", "/api/settings/writing-styles/"+id, "", cookie, csrf)
			var result map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusBadRequest || len(result) != 1 || result["error"] != "A writing profile UUID is required" || store.writingStyleDeleteCalls != 0 {
				t.Fatalf("invalid ID reached storage or returned the wrong error: %d %s; store calls: %d", response.Code, response.Body.String(), store.writingStyleDeleteCalls)
			}
		})
	}
}

func TestHandleDeleteWritingStyleRequiresSessionAndCSRF(t *testing.T) {
	server, store, _, _ := fixture(t)
	cookie, csrf := login(t, server)
	for _, test := range []struct {
		name   string
		cookie *http.Cookie
		csrf   string
		origin string
		status int
	}{
		{name: "missing session", csrf: csrf, origin: "http://localhost:8080", status: 401},
		{name: "unknown session", cookie: &http.Cookie{Name: cookie.Name, Value: "unknown"}, csrf: csrf, origin: "http://localhost:8080", status: 401},
		{name: "missing CSRF", cookie: cookie, origin: "http://localhost:8080", status: 403},
		{name: "wrong CSRF", cookie: cookie, csrf: "incorrect", origin: "http://localhost:8080", status: 403},
		{name: "missing origin", cookie: cookie, csrf: csrf, status: 403},
		{name: "wrong origin", cookie: cookie, csrf: csrf, origin: "https://other.example", status: 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodDelete, "http://localhost:8080/api/settings/writing-styles/"+testID, nil)
			r.Header.Set("Origin", test.origin)
			r.Header.Set("X-CSRF-Token", test.csrf)
			if test.cookie != nil {
				r.AddCookie(test.cookie)
			}
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, r)
			if response.Code != test.status || store.writingStyleDeleteCalls != 0 {
				t.Fatalf("unauthorized deletion: %d %s; store calls: %d", response.Code, response.Body.String(), store.writingStyleDeleteCalls)
			}
		})
	}
	t.Run("deleting account", func(t *testing.T) {
		accountID := server.sessions[cookie.Value].AccountID
		if err := server.accounts.RequestDeletion(context.Background(), accountID); err != nil {
			t.Fatal(err)
		}
		response := request(server, "DELETE", "/api/settings/writing-styles/"+testID, "", cookie, csrf)
		if response.Code != http.StatusNotFound || store.writingStyleDeleteCalls != 0 {
			t.Fatalf("deleting account reached profile storage: %d %s; store calls: %d", response.Code, response.Body.String(), store.writingStyleDeleteCalls)
		}
	})
}

func TestHandleDeleteWritingStyleHandlesStoreErrors(t *testing.T) {
	server, store, _, _ := fixture(t)
	cookie, csrf := login(t, server)
	for _, test := range []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"not found", pgx.ErrNoRows, 404, "Record not found"},
		{"database error", errors.New("private database details"), 503, "Database operation failed. Please try again."},
		{"deadline exceeded", context.DeadlineExceeded, 503, "Database operation failed. Please try again."},
	} {
		t.Run(test.name, func(t *testing.T) {
			store.err = test.err
			calls := store.writingStyleDeleteCalls
			response := request(server, "DELETE", "/api/settings/writing-styles/"+testID, "", cookie, csrf)
			var result map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if response.Code != test.status || len(result) != 1 || result["error"] != test.message || store.writingStyleDeleteCalls != calls+1 {
				t.Fatalf("unexpected error response: %d %s; store calls: %d", response.Code, response.Body.String(), store.writingStyleDeleteCalls)
			}
		})
	}
}
