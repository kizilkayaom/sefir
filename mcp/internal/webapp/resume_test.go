package webapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func resumeExtractorScript(t *testing.T, script string) PythonResumeExtractor {
	t.Helper()
	root := t.TempDir()
	python := filepath.Join(root, "fake-python")
	if err := os.WriteFile(python, []byte("#!/bin/sh\nset -eu\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	return PythonResumeExtractor{Python: python, Root: root}
}

func resumeExtractorResponse(t *testing.T, response string) PythonResumeExtractor {
	t.Helper()
	extractor := resumeExtractorScript(t, "/bin/cat > /dev/null\n/bin/cat response.json\n")
	if err := os.WriteFile(filepath.Join(extractor.Root, "response.json"), []byte(response), 0600); err != nil {
		t.Fatal(err)
	}
	return extractor
}

func TestPythonResumeExtractorPassesPDFAndIsolatesEnvironment(t *testing.T) {
	for _, key := range []string{"OPENAI_API_KEY", "DATABASE_URL", "PYTHONPATH", "SEFIR_TEST_PRIVATE_VALUE"} {
		t.Setenv(key, "synthetic-private-value")
	}
	extractor := resumeExtractorScript(t, `
test "$#" -eq 2
test "$1" = "-m"
test "$2" = "sefir_ingestion.resume_bridge"
test "$PYTHON_DOTENV_DISABLED" = "1"
test "$PYTHONUTF8" = "1"
test "$PYTHONDONTWRITEBYTECODE" = "1"
test "${OPENAI_API_KEY+x}" != "x"
test "${DATABASE_URL+x}" != "x"
test "${PYTHONPATH+x}" != "x"
test "${SEFIR_TEST_PRIVATE_VALUE+x}" != "x"
/bin/cat > received.pdf
printf '%s\n' '{"text":"  Résumé\nSecond page.  ","page_count":2}'
`)
	data := []byte{'%', 'P', 'D', 'F', '-', 0, 255, '\n'}

	result, err := extractor.Extract(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if result != (ExtractedResume{Text: "  Résumé\nSecond page.  ", PageCount: 2}) {
		t.Fatal("extracted text or page count changed")
	}
	received, err := os.ReadFile(filepath.Join(extractor.Root, "received.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(received, data) {
		t.Fatal("PDF bytes changed before reaching the subprocess")
	}
}

func TestPythonResumeExtractorValidatesInputBeforeStartingProcess(t *testing.T) {
	extractor := PythonResumeExtractor{Python: filepath.Join(t.TempDir(), "missing-python")}
	for _, test := range []struct {
		name    string
		data    []byte
		message string
	}{
		{name: "empty", message: "PDF file must not be empty."},
		{name: "oversized", data: make([]byte, maxPDFBytes+1), message: "PDF file must not exceed 5 MiB."},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := extractor.Extract(context.Background(), test.data)
			var invalid *ResumeValidationError
			if !errors.As(err, &invalid) || invalid.Message != test.message || err.Error() != test.message {
				t.Fatalf("expected input validation error, got %v", err)
			}
			if result != (ExtractedResume{}) {
				t.Fatal("invalid input returned extracted text")
			}
		})
	}
}

func TestPythonResumeExtractorAcceptsExactLimits(t *testing.T) {
	want := ExtractedResume{Text: strings.Repeat("é", maxResumeTextBytes/2), PageCount: maxPDFPages}
	response, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	extractor := resumeExtractorResponse(t, string(response))

	result, err := extractor.Extract(context.Background(), make([]byte, maxPDFBytes))
	if err != nil {
		t.Fatal(err)
	}
	if result != want {
		t.Fatal("valid output at the size and page limits was changed")
	}
}

func TestPythonResumeExtractorReturnsValidationError(t *testing.T) {
	message := "Encrypted PDFs are not supported. Upload an unencrypted PDF."
	extractor := resumeExtractorResponse(t, `{"error":"invalid_pdf","message":"`+message+`"}`)

	result, err := extractor.Extract(context.Background(), []byte("synthetic PDF"))
	var invalid *ResumeValidationError
	if !errors.As(err, &invalid) || invalid.Message != message || err.Error() != message {
		t.Fatalf("expected the PDF validation error, got %v", err)
	}
	if result != (ExtractedResume{}) {
		t.Fatal("validation error returned extracted text")
	}
}

func TestPythonResumeExtractorRejectsInvalidResponses(t *testing.T) {
	for _, test := range []struct {
		name     string
		response string
	}{
		{name: "empty"},
		{name: "malformed JSON", response: `{"text":`},
		{name: "null", response: `null`},
		{name: "array", response: `[{"text":"Example","page_count":1}]`},
		{name: "trailing JSON", response: `{"text":"Example","page_count":1}{}`},
		{name: "unknown field", response: `{"text":"Example","page_count":1,"extra":"private"}`},
		{name: "wrong field case", response: `{"Text":"Example","page_count":1}`},
		{name: "missing page count", response: `{"text":"Example"}`},
		{name: "mixed result", response: `{"text":"Example","page_count":1,"error":"invalid_pdf","message":"Invalid PDF"}`},
		{name: "mixed null field", response: `{"text":"Example","page_count":1,"error":null}`},
		{name: "blank text", response: `{"text":" \n\t ","page_count":1}`},
		{name: "null text", response: `{"text":null,"page_count":1}`},
		{name: "wrong text type", response: `{"text":42,"page_count":1}`},
		{name: "text bytes exceeded", response: `{"text":"` + strings.Repeat("é", maxResumeTextBytes/2+1) + `","page_count":1}`},
		{name: "zero pages", response: `{"text":"Example","page_count":0}`},
		{name: "too many pages", response: `{"text":"Example","page_count":21}`},
		{name: "fractional pages", response: `{"text":"Example","page_count":1.5}`},
		{name: "wrong page count type", response: `{"text":"Example","page_count":"1"}`},
		{name: "unknown error", response: `{"error":"extraction_failed","message":"private details"}`},
		{name: "blank error message", response: `{"error":"invalid_pdf","message":"  "}`},
		{name: "null error message", response: `{"error":"invalid_pdf","message":null}`},
		{name: "wrong error message type", response: `{"error":"invalid_pdf","message":42}`},
		{name: "invalid UTF-8", response: "{\"text\":\"\xff\",\"page_count\":1}"},
		{name: "oversized output", response: strings.Repeat(" ", maxResumeResponseBytes) + `{"text":"Example","page_count":1}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			extractor := resumeExtractorResponse(t, test.response)

			result, err := extractor.Extract(context.Background(), []byte("synthetic PDF"))
			if err == nil || result != (ExtractedResume{}) {
				t.Fatal("invalid response was accepted")
			}
			var invalid *ResumeValidationError
			if errors.As(err, &invalid) {
				t.Fatal("invalid subprocess response was reported as an invalid upload")
			}
			if err.Error() != "resume extractor returned an invalid response" && err.Error() != "resume extraction failed" {
				t.Fatalf("unexpected response error: %v", err)
			}
		})
	}
}

func TestPythonResumeExtractorSanitizesProcessFailures(t *testing.T) {
	failed := resumeExtractorScript(t, `
/bin/cat > /dev/null
printf '%s\n' '{"text":"Fictional private document contents","page_count":1}'
printf '%s\n' 'Fictional private parser failure' >&2
exit 1
`)
	for _, test := range []struct {
		name      string
		extractor PythonResumeExtractor
	}{
		{name: "startup", extractor: PythonResumeExtractor{Python: filepath.Join(t.TempDir(), "missing-python")}},
		{name: "nonzero exit", extractor: failed},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.extractor.Extract(context.Background(), []byte("synthetic PDF"))
			if err == nil || err.Error() != "resume extraction failed" || result != (ExtractedResume{}) {
				t.Fatalf("expected a generic extraction failure, got %v", err)
			}
		})
	}
}

func TestPythonResumeExtractorHonorsContextCancellation(t *testing.T) {
	extractor := resumeExtractorScript(t, "exec /bin/sleep 30\n")
	for _, test := range []struct {
		name      string
		cancelled bool
		want      error
	}{
		{name: "already cancelled", cancelled: true, want: context.Canceled},
		{name: "deadline during extraction", want: context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			if test.cancelled {
				cancel()
			}

			started := time.Now()
			result, err := extractor.Extract(ctx, []byte("synthetic PDF"))
			if !errors.Is(err, test.want) || result != (ExtractedResume{}) {
				t.Fatalf("expected %v, got %v", test.want, err)
			}
			if time.Since(started) > 5*time.Second {
				t.Fatal("extraction did not stop promptly after cancellation")
			}
		})
	}
}

func TestLimitedBufferAllowsExactLimit(t *testing.T) {
	buffer := limitedBuffer{limit: 6}
	for _, chunk := range []string{"abc", "dé", ""} {
		n, err := buffer.Write([]byte(chunk))
		if err != nil || n != len(chunk) {
			t.Fatalf("write returned (%d, %v), want (%d, nil)", n, err, len(chunk))
		}
	}
	if got := buffer.buffer.String(); got != "abcdé" {
		t.Fatalf("unexpected buffered output: %q", got)
	}
}

func TestLimitedBufferRejectsOverflowWithoutChangingOutput(t *testing.T) {
	for _, test := range []struct {
		name    string
		limit   int
		initial string
		chunk   string
	}{
		{name: "single oversized write", limit: 4, chunk: "abcde"},
		{name: "cumulative overflow", limit: 4, initial: "abc", chunk: "de"},
		{name: "already full", limit: 4, initial: "abcd", chunk: "e"},
		{name: "zero limit", limit: 0, chunk: "a"},
	} {
		t.Run(test.name, func(t *testing.T) {
			buffer := limitedBuffer{limit: test.limit}
			if _, err := buffer.Write([]byte(test.initial)); err != nil {
				t.Fatal(err)
			}

			n, err := buffer.Write([]byte(test.chunk))
			if err == nil || n != 0 {
				t.Fatalf("write returned (%d, %v), want zero bytes and an error", n, err)
			}
			if got := buffer.buffer.String(); got != test.initial {
				t.Fatalf("rejected write changed buffered output: got %q, want %q", got, test.initial)
			}
		})
	}
}
