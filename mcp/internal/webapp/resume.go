package webapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxPDFBytes            = 5 * 1024 * 1024
	maxPDFPages            = 20
	maxResumeTextBytes     = 100_000
	maxResumeResponseBytes = 1 << 20
)

type ExtractedResume struct {
	Text      string `json:"text"`
	PageCount int    `json:"page_count"`
}

type ResumeExtractor interface {
	Extract(context.Context, []byte) (ExtractedResume, error)
}

type ResumeValidationError struct {
	Message string
}

func (e *ResumeValidationError) Error() string {
	return e.Message
}

type PythonResumeExtractor struct {
	Python string
	Root   string
}

func (e PythonResumeExtractor) Extract(ctx context.Context, data []byte) (ExtractedResume, error) {
	if len(data) == 0 {
		return ExtractedResume{}, &ResumeValidationError{Message: "PDF file must not be empty."}
	}
	if len(data) > maxPDFBytes {
		return ExtractedResume{}, &ResumeValidationError{Message: "PDF file must not exceed 5 MiB."}
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	output := limitedBuffer{limit: maxResumeResponseBytes}
	command := exec.CommandContext(ctx, e.Python, "-m", "sefir_ingestion.resume_bridge")
	command.Dir = e.Root
	command.Stdin = bytes.NewReader(data)
	command.Stdout = &output
	command.Stderr = io.Discard
	command.Env = []string{
		"PYTHON_DOTENV_DISABLED=1",
		"PYTHONUTF8=1",
		"PYTHONDONTWRITEBYTECODE=1",
	}
	command.WaitDelay = time.Second
	err := command.Run()
	if ctx.Err() != nil {
		return ExtractedResume{}, ctx.Err()
	}
	if err != nil {
		return ExtractedResume{}, errors.New("resume extraction failed")
	}
	return decodeResumeResponse(output.buffer.Bytes())
}

func decodeResumeResponse(output []byte) (ExtractedResume, error) {
	invalid := errors.New("resume extractor returned an invalid response")
	var fields map[string]json.RawMessage
	if !utf8.Valid(output) || json.Unmarshal(output, &fields) != nil || len(fields) != 2 {
		return ExtractedResume{}, invalid
	}

	if codeJSON, exists := fields["error"]; exists {
		messageJSON, exists := fields["message"]
		if !exists {
			return ExtractedResume{}, invalid
		}
		var code, message string
		if json.Unmarshal(codeJSON, &code) != nil || code != "invalid_pdf" ||
			json.Unmarshal(messageJSON, &message) != nil || strings.TrimSpace(message) == "" {
			return ExtractedResume{}, invalid
		}
		return ExtractedResume{}, &ResumeValidationError{Message: message}
	}

	textJSON, hasText := fields["text"]
	pageCountJSON, hasPageCount := fields["page_count"]
	if !hasText || !hasPageCount {
		return ExtractedResume{}, invalid
	}
	var result ExtractedResume
	if json.Unmarshal(textJSON, &result.Text) != nil || json.Unmarshal(pageCountJSON, &result.PageCount) != nil ||
		strings.TrimSpace(result.Text) == "" || len(result.Text) > maxResumeTextBytes ||
		result.PageCount < 1 || result.PageCount > maxPDFPages {
		return ExtractedResume{}, invalid
	}
	return result, nil
}

type limitedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		return 0, errors.New("resume extraction response exceeds the size limit")
	}
	return b.buffer.Write(p)
}
