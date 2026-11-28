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
	"testing"
)

type resumeExtractorFunc func(context.Context, []byte) (ExtractedResume, error)

func (f resumeExtractorFunc) Extract(ctx context.Context, data []byte) (ExtractedResume, error) {
	return f(ctx, data)
}

type resumeUploadPart struct {
	field    string
	filename string
	content  []byte
}

type resumeBodyTracker struct {
	io.ReadCloser
	reads int
}

func (b *resumeBodyTracker) Read(p []byte) (int, error) {
	b.reads++
	return b.ReadCloser.Read(p)
}

func resumeUploadBody(t *testing.T, parts ...resumeUploadPart) ([]byte, string) {
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
		if _, err := output.Write(part.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func resumeUploadRequest(body []byte, contentType string, cookie *http.Cookie, csrf string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://localhost:8080/api/resume/extract", bytes.NewReader(body))
	r.Header.Set("Origin", "http://localhost:8080")
	r.Header.Set("Content-Type", contentType)
	r.Header.Set("X-CSRF-Token", csrf)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	return r
}

func TestExtractResumeReturnsTextWithoutSaving(t *testing.T) {
	server, _, _, _ := fixture(t)
	cookie, csrf := login(t, server)
	accountID := server.sessions[cookie.Value].AccountID
	server.store, server.bridge = nil, nil
	want := ExtractedResume{Text: "  Alex Example\nRésumé with Go experience.  ", PageCount: 2}
	for _, test := range []struct {
		name           string
		data           []byte
		streamed       bool
		requestAtLimit bool
	}{
		{name: "binary PDF", data: []byte("%PDF-1.7\n\x00\xffsynthetic")},
		{name: "exact file limit", data: bytes.Repeat([]byte("x"), maxPDFBytes)},
		{name: "streamed PDF", data: []byte("synthetic PDF"), streamed: true},
		{name: "exact request limit", data: []byte("synthetic PDF"), streamed: true, requestAtLimit: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server.config.ResumeExtractor = resumeExtractorFunc(func(ctx context.Context, data []byte) (ExtractedResume, error) {
				calls++
				if !bytes.Equal(data, test.data) || contextAccount(ctx).ID != accountID {
					t.Fatal("extractor received different bytes or account context")
				}
				if len(server.extracting) != 1 {
					t.Fatal("extraction ran without holding the extraction slot")
				}
				return want, nil
			})
			body, contentType := resumeUploadBody(t, resumeUploadPart{"resume", "résumé.pdf", test.data})
			if test.requestAtLimit {
				body = append(body, bytes.Repeat([]byte(" "), maxResumeRequestBytes-len(body))...)
			}
			r := resumeUploadRequest(body, contentType, cookie, csrf)
			if test.streamed {
				r.ContentLength = -1
			}
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, r)

			if response.Code != http.StatusOK {
				t.Fatalf("extraction: %d %s", response.Code, response.Body.String())
			}
			var result ExtractedResume
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result != want {
				t.Fatalf("unexpected extracted result: %+v %v", result, err)
			}
			if calls != 1 || len(server.extracting) != 0 || r.MultipartForm != nil {
				t.Fatal("upload repeated extraction, retained its slot, or used disk-backed form parsing")
			}
			if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Content-Type") != "application/json" {
				t.Fatal("extracted text is missing JSON or no-store headers")
			}
		})
	}
}

func TestExtractResumeRequiresSessionAndCSRFBeforeReading(t *testing.T) {
	server, _, _, _ := fixture(t)
	cookie, csrf := login(t, server)
	calls := 0
	server.config.ResumeExtractor = resumeExtractorFunc(func(context.Context, []byte) (ExtractedResume, error) {
		calls++
		return ExtractedResume{Text: "Example", PageCount: 1}, nil
	})
	body, contentType := resumeUploadBody(t, resumeUploadPart{"resume", "resume.pdf", []byte("synthetic PDF")})
	for _, test := range []struct {
		name   string
		cookie *http.Cookie
		csrf   string
		origin string
		status int
	}{
		{name: "missing session", csrf: csrf, origin: "http://localhost:8080", status: 401},
		{name: "invalid session", cookie: &http.Cookie{Name: cookie.Name, Value: "invalid"}, csrf: csrf, origin: "http://localhost:8080", status: 401},
		{name: "missing CSRF", cookie: cookie, origin: "http://localhost:8080", status: 403},
		{name: "wrong CSRF", cookie: cookie, csrf: "wrong", origin: "http://localhost:8080", status: 403},
		{name: "missing origin", cookie: cookie, csrf: csrf, status: 403},
		{name: "wrong origin", cookie: cookie, csrf: csrf, origin: "https://other.example", status: 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := resumeUploadRequest(body, contentType, test.cookie, test.csrf)
			r.Header.Set("Origin", test.origin)
			tracker := &resumeBodyTracker{ReadCloser: r.Body}
			r.Body = tracker
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, r)

			if response.Code != test.status || calls != 0 || tracker.reads != 0 || len(server.extracting) != 0 {
				t.Fatalf("unauthorized upload: status=%d calls=%d reads=%d", response.Code, calls, tracker.reads)
			}
		})
	}
}

func TestExtractResumeRejectsInvalidUploads(t *testing.T) {
	server, _, _, _ := fixture(t)
	cookie, csrf := login(t, server)
	calls := 0
	server.config.ResumeExtractor = resumeExtractorFunc(func(context.Context, []byte) (ExtractedResume, error) {
		calls++
		return ExtractedResume{Text: "Example", PageCount: 1}, nil
	})
	file := resumeUploadPart{"resume", "resume.pdf", []byte("synthetic PDF")}
	for _, test := range []struct {
		name        string
		parts       []resumeUploadPart
		contentType string
		truncate    bool
		preamble    bool
		padding     bool
		streamed    bool
		status      int
	}{
		{name: "JSON body", parts: []resumeUploadPart{file}, contentType: "application/json", status: 415},
		{name: "mixed multipart", parts: []resumeUploadPart{file}, contentType: "multipart/mixed; boundary=test", status: 415},
		{name: "missing boundary", parts: []resumeUploadPart{file}, contentType: "multipart/form-data", status: 400},
		{name: "wrong boundary", parts: []resumeUploadPart{file}, contentType: "multipart/form-data; boundary=wrong", status: 400},
		{name: "no file", status: 400},
		{name: "missing filename", parts: []resumeUploadPart{{"resume", "", file.content}}, status: 400},
		{name: "wrong field", parts: []resumeUploadPart{{"other", "resume.pdf", file.content}}, status: 400},
		{name: "two files", parts: []resumeUploadPart{file, file}, status: 400},
		{name: "extra field", parts: []resumeUploadPart{file, {"account_id", "", []byte(testID)}}, status: 400},
		{name: "field before file", parts: []resumeUploadPart{{"account_id", "", []byte(testID)}, file}, status: 400},
		{name: "truncated file", parts: []resumeUploadPart{file}, truncate: true, status: 400},
		{name: "truncated second file", parts: []resumeUploadPart{file, file}, truncate: true, status: 400},
		{name: "oversized file", parts: []resumeUploadPart{{"resume", "resume.pdf", bytes.Repeat([]byte("x"), maxPDFBytes+1)}}, streamed: true, status: 413},
		{name: "oversized request", parts: []resumeUploadPart{file}, padding: true, status: 413},
		{name: "oversized streamed request", parts: []resumeUploadPart{file}, padding: true, streamed: true, status: 413},
		{name: "oversized preamble", parts: []resumeUploadPart{file}, preamble: true, streamed: true, status: 413},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, contentType := resumeUploadBody(t, test.parts...)
			if test.contentType != "" {
				contentType = test.contentType
			}
			if test.truncate {
				body = body[:bytes.LastIndex(body, []byte("\r\n--"))]
			}
			if test.preamble {
				body = append(bytes.Repeat([]byte("padding\r\n"), maxResumeRequestBytes/9+1), body...)
			}
			if test.padding {
				body = append(body, bytes.Repeat([]byte(" "), maxResumeRequestBytes-len(body)+1)...)
			}
			r := resumeUploadRequest(body, contentType, cookie, csrf)
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
			if calls != 0 || len(server.extracting) != 0 || r.MultipartForm != nil {
				t.Fatal("invalid upload called the extractor, retained its slot, or parsed a disk-backed form")
			}
		})
	}
}

func TestExtractResumeHandlesExtractorErrors(t *testing.T) {
	server, _, _, _ := fixture(t)
	cookie, csrf := login(t, server)
	body, contentType := resumeUploadBody(t, resumeUploadPart{"resume", "resume.pdf", []byte("synthetic PDF")})
	for _, test := range []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{name: "invalid PDF", err: &ResumeValidationError{Message: "PDF file must not be empty."}, status: 422, message: "PDF file must not be empty."},
		{name: "wrapped validation error", err: fmt.Errorf("private details: %w", &ResumeValidationError{Message: "Encrypted PDFs are not supported."}), status: 422, message: "Encrypted PDFs are not supported."},
		{name: "deadline", err: fmt.Errorf("private details: %w", context.DeadlineExceeded), status: 504, message: "Résumé extraction timed out. Please try again."},
		{name: "process failure", err: errors.New("private process details"), status: 503, message: "Could not extract résumé text. Please try again."},
		{name: "cancelled extraction", err: context.Canceled, status: 503, message: "Could not extract résumé text. Please try again."},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server.config.ResumeExtractor = resumeExtractorFunc(func(context.Context, []byte) (ExtractedResume, error) {
				calls++
				return ExtractedResume{Text: "private partial extraction", PageCount: 1}, test.err
			})
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, resumeUploadRequest(body, contentType, cookie, csrf))

			var result map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || len(result) != 1 || result["error"] != test.message {
				t.Fatalf("unexpected extraction error response: %s %v", response.Body.String(), err)
			}
			if response.Code != test.status || calls != 1 || len(server.extracting) != 0 {
				t.Fatalf("extraction error left incorrect status or slot: %d calls=%d", response.Code, calls)
			}
		})
	}
}

func TestExtractResumeUnavailableDoesNotReadUpload(t *testing.T) {
	server, _, _, _ := fixture(t)
	cookie, csrf := login(t, server)
	body, contentType := resumeUploadBody(t, resumeUploadPart{"resume", "resume.pdf", []byte("synthetic PDF")})
	r := resumeUploadRequest(body, contentType, cookie, csrf)
	tracker := &resumeBodyTracker{ReadCloser: r.Body}
	r.Body = tracker
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, r)

	if response.Code != http.StatusServiceUnavailable || tracker.reads != 0 || len(server.extracting) != 0 {
		t.Fatalf("unavailable extractor: status=%d reads=%d", response.Code, tracker.reads)
	}
}

func TestExtractResumeBusyDoesNotReadUploadAndAllowsRetry(t *testing.T) {
	server, _, _, _ := fixture(t)
	cookie, csrf := login(t, server)
	calls := 0
	server.config.ResumeExtractor = resumeExtractorFunc(func(context.Context, []byte) (ExtractedResume, error) {
		calls++
		return ExtractedResume{Text: "Example", PageCount: 1}, nil
	})
	if cap(server.extracting) != 1 {
		t.Fatal("server must allow one extraction at a time")
	}
	server.extracting <- struct{}{}
	body, contentType := resumeUploadBody(t, resumeUploadPart{"resume", "resume.pdf", []byte("synthetic PDF")})
	r := resumeUploadRequest(body, contentType, cookie, csrf)
	tracker := &resumeBodyTracker{ReadCloser: r.Body}
	r.Body = tracker
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, r)

	if response.Code != http.StatusTooManyRequests || calls != 0 || tracker.reads != 0 || len(server.extracting) != 1 {
		t.Fatalf("busy extraction: status=%d calls=%d reads=%d", response.Code, calls, tracker.reads)
	}
	<-server.extracting
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, resumeUploadRequest(body, contentType, cookie, csrf))
	if response.Code != http.StatusOK || calls != 1 || len(server.extracting) != 0 {
		t.Fatalf("extraction retry failed: %d %s", response.Code, response.Body.String())
	}
}
