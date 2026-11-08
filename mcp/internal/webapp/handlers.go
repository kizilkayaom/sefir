package webapp

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/kizilkayaom/sefir/mcp/internal/catalogue"
)

func (s *Server) handleDrafts(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := pagination(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	items, err := s.store.Drafts(ctx, limit+1, offset)
	if err != nil {
		storeError(w, err)
		return
	}
	page(w, items, limit, offset)
}

func (s *Server) handleDraft(w http.ResponseWriter, r *http.Request) {
	id, version, ok := draftKey(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	draft, err := s.store.Draft(ctx, id, version)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, draft)
}

func (s *Server) handleContacts(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := pagination(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	items, err := s.store.Contacts(ctx, limit+1, offset)
	if err != nil {
		storeError(w, err)
		return
	}
	page(w, items, limit, offset)
}

func validEmail(value string) bool {
	if len(value) > 254 || strings.ContainsAny(value, "\r\n") {
		return false
	}
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && address.Name == ""
}

func (s *Server) handleSaveContact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if r.Method == http.MethodPut && !uuidPattern.MatchString(id) {
		fail(w, http.StatusBadRequest, "A contact UUID is required")
		return
	}
	var input ContactInput
	if !decode(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Email = strings.TrimSpace(input.Email)
	input.Organization = strings.TrimSpace(input.Organization)
	input.Role = strings.TrimSpace(input.Role)
	input.Context = strings.TrimSpace(input.Context)
	if input.Name == "" || len(input.Name) > 200 || len(input.Organization) > 300 ||
		len(input.Role) > 300 || len(input.Context) > 10000 ||
		(input.Status != "proposed" && input.Status != "confirmed") {
		fail(w, http.StatusUnprocessableEntity, "Provide a name and valid contact fields and status")
		return
	}
	if input.Email != "" && !validEmail(input.Email) {
		fail(w, http.StatusUnprocessableEntity, "Enter a valid email address without a display name")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	contact, err := s.store.SaveContact(ctx, id, input)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, contact)
}

func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ContactID string `json:"contact_id"`
		CVText    string `json:"cv_text"`
		Provider  string `json:"provider"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !uuidPattern.MatchString(input.ContactID) || strings.TrimSpace(input.CVText) == "" || len(input.CVText) > 100000 {
		fail(w, http.StatusUnprocessableEntity, "Select a confirmed contact and provide resume text of at most 100,000 bytes")
		return
	}
	switch input.Provider {
	case "", "cohere", "anthropic", "openai", "openrouter":
	default:
		fail(w, http.StatusUnprocessableEntity, "Choose a supported AI provider")
		return
	}
	select {
	case s.generating <- struct{}{}:
		defer func() { <-s.generating }()
	default:
		fail(w, http.StatusTooManyRequests, "A draft is already being generated. Wait for it to finish.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	contact, err := s.store.ConfirmedContact(ctx, input.ContactID)
	if err != nil {
		storeError(w, err)
		return
	}
	if contact.Name == nil || strings.TrimSpace(*contact.Name) == "" {
		fail(w, http.StatusUnprocessableEntity, "Add the contact's name before generating a draft")
		return
	}
	result, err := s.bridge.Call(ctx, "generate", map[string]any{
		"contact": contact, "cv_text": input.CVText, "provider": input.Provider,
	})
	if err != nil {
		fail(w, http.StatusBadGateway, "Draft generation failed. Check the selected AI provider's key, model access, and usage limits, then try again.")
		return
	}
	var generated struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(result, &generated) != nil {
		fail(w, http.StatusBadGateway, "The model returned an invalid draft")
		return
	}
	if generated.Error == "provider_unavailable" {
		fail(w, http.StatusUnprocessableEntity, "The selected AI provider is not configured. Check the server's API key and model settings.")
		return
	}
	if generated.Error != "" || strings.TrimSpace(generated.Subject) == "" ||
		strings.TrimSpace(generated.Body) == "" || len(generated.Subject) > 998 ||
		strings.ContainsAny(generated.Subject, "\r\n") || len(generated.Body) > 100000 {
		fail(w, http.StatusBadGateway, "The model returned an invalid draft")
		return
	}
	draft, err := s.store.CreateDraft(ctx, catalogue.CreateDraftInput{
		ContactID: input.ContactID, Subject: generated.Subject, Body: generated.Body,
	})
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, draft)
}

func approvalID(id string, version int, hash string) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "sefir-web-approval:%s:%d:%s", id, version, hash))
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	id, version, ok := draftKey(w, r)
	if !ok {
		return
	}
	var input struct {
		ContentHash string `json:"content_hash"`
	}
	if !decode(w, r, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	draft, err := s.store.Draft(ctx, id, version)
	if err != nil {
		storeError(w, err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(draft.ContentHash), []byte(input.ContentHash)) != 1 {
		fail(w, http.StatusConflict, "The reviewed draft does not match. Reload it before approving.")
		return
	}
	if draft.RecipientEmail == nil || !validEmail(*draft.RecipientEmail) {
		fail(w, http.StatusUnprocessableEntity, "This saved draft has no valid recipient. Update the contact and generate a new draft.")
		return
	}
	entry, err := s.store.DraftActivity(ctx, id, version)
	if err != nil {
		storeError(w, err)
		return
	}
	if entry != nil {
		writeJSON(w, http.StatusOK, map[string]any{"delivery": entry})
		return
	}
	gmail, err := s.accountGmail(ctx)
	if err != nil || gmail == nil || !gmail.Status(ctx).Connected {
		fail(w, http.StatusUnprocessableEntity, "Connect Gmail in Settings before approving this email.")
		return
	}
	var result json.RawMessage
	err = s.accounts.WithActiveAccount(ctx, contextAccount(ctx).ID, func() error {
		var callErr error
		result, callErr = s.bridge.Call(ctx, "approve", map[string]any{
			"draft_id": id, "version": version, "content_hash": draft.ContentHash,
			"approval_id": approvalID(id, version, draft.ContentHash), "approved_by": contextAccount(ctx).Username,
		})
		return callErr
	})
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, http.StatusUnauthorized, "This account is no longer available")
		return
	}
	if err != nil {
		fail(w, http.StatusBadGateway, "Approval could not be confirmed. Refresh status before retrying; retries use the same approval.")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"workflow": result})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	id, version, ok := draftKey(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if _, err := s.store.Draft(ctx, id, version); err != nil {
		storeError(w, err)
		return
	}
	entry, err := s.store.DraftActivity(ctx, id, version)
	if err != nil {
		storeError(w, err)
		return
	}
	result, err := s.bridge.Call(ctx, "status", map[string]any{"draft_id": id, "version": version})
	if err != nil {
		result = json.RawMessage(`{"status":"UNAVAILABLE","failure":"Temporal is unavailable. Start Temporal and the worker, then refresh."}`)
	}
	writeJSON(w, http.StatusOK, map[string]any{"workflow": result, "delivery": entry})
}

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := pagination(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	items, err := s.store.Activities(ctx, limit+1, offset)
	if err != nil {
		storeError(w, err)
		return
	}
	page(w, items, limit, offset)
}
