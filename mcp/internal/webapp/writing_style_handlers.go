package webapp

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/kizilkayaom/sefir/mcp/internal/writingstyle"
)

func (s *Server) handleWritingStyles(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := pagination(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	items, err := s.store.WritingStyles(ctx, limit+1, offset)
	if err != nil {
		storeError(w, err)
		return
	}
	page(w, items, limit, offset)
}

func (s *Server) handleUploadWritingStyle(w http.ResponseWriter, r *http.Request) {
	const maxRequestBytes = 64 * 1024
	const maxFileBytes = 32 * 1024

	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "multipart/form-data" {
		fail(w, http.StatusUnsupportedMediaType, "Send multipart/form-data with one profile file")
		return
	}
	if r.ContentLength > maxRequestBytes {
		fail(w, http.StatusRequestEntityTooLarge, "Profile upload request must not exceed 64 KiB")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	readError := func(err error) {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) || errors.Is(err, multipart.ErrMessageTooLarge) {
			fail(w, http.StatusRequestEntityTooLarge, "Profile upload request must not exceed 64 KiB")
			return
		}
		fail(w, http.StatusBadRequest, "Invalid multipart upload; send exactly one file named profile")
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
	filename := part.FileName()
	if part.FormName() != "profile" || filename == "" {
		fail(w, http.StatusBadRequest, "Send exactly one file named profile with a filename")
		return
	}
	data, err := io.ReadAll(io.LimitReader(part, maxFileBytes+1))
	if err != nil {
		readError(err)
		return
	}
	if len(data) > maxFileBytes {
		fail(w, http.StatusRequestEntityTooLarge, "Profile file must not exceed 32 KiB")
		return
	}
	if _, err := reader.NextPart(); err != io.EOF {
		if err != nil {
			readError(err)
		} else {
			fail(w, http.StatusBadRequest, "Send exactly one profile file without extra fields or files")
		}
		return
	}
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		readError(err)
		return
	}
	profile, err := writingstyle.ParseProfile(filename, data)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	saved, err := s.store.CreateWritingStyle(ctx, profile)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, saved)
}
