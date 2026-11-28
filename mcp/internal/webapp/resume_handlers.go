package webapp

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
)

const maxResumeRequestBytes = maxPDFBytes + 64*1024

func (s *Server) handleExtractResume(w http.ResponseWriter, r *http.Request) {
	if s.config.ResumeExtractor == nil {
		fail(w, http.StatusServiceUnavailable, "Résumé extraction is unavailable. Please try again later.")
		return
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "multipart/form-data" {
		fail(w, http.StatusUnsupportedMediaType, "Send multipart/form-data with one résumé file")
		return
	}
	if r.ContentLength > maxResumeRequestBytes {
		fail(w, http.StatusRequestEntityTooLarge, "Résumé upload is too large. Choose a PDF of at most 5 MiB.")
		return
	}

	select {
	case s.extracting <- struct{}{}:
		defer func() { <-s.extracting }()
	default:
		fail(w, http.StatusTooManyRequests, "A résumé is already being processed. Please try again shortly.")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxResumeRequestBytes)
	readError := func(err error) {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) || errors.Is(err, multipart.ErrMessageTooLarge) {
			fail(w, http.StatusRequestEntityTooLarge, "Résumé upload is too large. Choose a PDF of at most 5 MiB.")
			return
		}
		fail(w, http.StatusBadRequest, "Invalid multipart upload; send exactly one file named resume")
	}
	reader, err := r.MultipartReader()
	if err != nil {
		readError(err)
		return
	}
	part, err := reader.NextPart()
	if err != nil {
		readError(err)
		return
	}
	defer part.Close()
	if part.FormName() != "resume" || part.FileName() == "" {
		fail(w, http.StatusBadRequest, "Send exactly one file named resume with a filename")
		return
	}
	data, err := io.ReadAll(io.LimitReader(part, maxPDFBytes+1))
	if err != nil {
		readError(err)
		return
	}
	if len(data) > maxPDFBytes {
		fail(w, http.StatusRequestEntityTooLarge, "PDF file must not exceed 5 MiB.")
		return
	}
	if _, err := reader.NextPart(); err != io.EOF {
		if err != nil {
			readError(err)
		} else {
			fail(w, http.StatusBadRequest, "Send exactly one résumé file without extra fields or files")
		}
		return
	}
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		readError(err)
		return
	}

	result, err := s.config.ResumeExtractor.Extract(r.Context(), data)
	var invalid *ResumeValidationError
	if errors.As(err, &invalid) {
		fail(w, http.StatusUnprocessableEntity, invalid.Message)
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		fail(w, http.StatusGatewayTimeout, "Résumé extraction timed out. Please try again.")
		return
	}
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "Could not extract résumé text. Please try again.")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
