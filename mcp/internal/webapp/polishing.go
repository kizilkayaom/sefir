package webapp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/kizilkayaom/sefir/mcp/internal/draftcontent"
)

func (s *Server) handlePolishDraft(w http.ResponseWriter, r *http.Request) {
	id, version, ok := draftKey(w, r)
	if !ok {
		return
	}
	var input struct {
		ContentHash string `json:"content_hash"`
		Provider    string `json:"provider"`
		PresetID    string `json:"preset_id"`
		ProfileID   string `json:"profile_id"`
	}
	if !decode(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.ContentHash) == "" {
		fail(w, http.StatusUnprocessableEntity, "The saved draft's content hash is required")
		return
	}
	switch input.Provider {
	case "", "cohere", "anthropic", "openai", "openrouter":
	default:
		fail(w, http.StatusUnprocessableEntity, "Choose a supported AI provider")
		return
	}
	if input.ProfileID != "" && input.PresetID != "" {
		fail(w, http.StatusUnprocessableEntity, "Choose either a tone preset or a custom writing profile")
		return
	}
	if input.ProfileID != "" {
		if !uuidPattern.MatchString(input.ProfileID) {
			fail(w, http.StatusUnprocessableEntity, "A writing profile UUID is required")
			return
		}
	} else {
		if input.PresetID == "" {
			input.PresetID = contextAccount(r.Context()).DefaultPresetID
		}
		switch input.PresetID {
		case "prof", "warm_prof", "technical", "formal":
		default:
			fail(w, http.StatusUnprocessableEntity, "Choose a supported writing style")
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	draft, err := s.store.Draft(ctx, id, version)
	if err != nil {
		storeError(w, err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(draft.ContentHash), []byte(input.ContentHash)) != 1 {
		fail(w, http.StatusConflict, "The saved draft does not match. Reload it before polishing.")
		return
	}
	select {
	case s.generating <- struct{}{}:
		defer func() { <-s.generating }()
	default:
		fail(w, http.StatusTooManyRequests, "An AI draft operation is already running. Wait for it to finish.")
		return
	}

	payload := map[string]any{
		"draft": map[string]string{
			"subject": draft.Subject, "body": draft.Body, "body_html": draft.BodyHTML,
		},
		"provider": input.Provider,
	}
	expectedPresetID := input.PresetID
	expectedRevision := 0
	if input.ProfileID == "" {
		payload["preset_id"] = input.PresetID
	} else {
		profile, err := s.store.WritingStyle(ctx, input.ProfileID)
		if err != nil {
			storeError(w, err)
			return
		}
		payload["custom_profile"] = map[string]any{
			"profile_id": profile.ProfileID,
			"revision":   profile.Revision,
			"name":       profile.Name,
			"language":   profile.Language,
			"content":    profile.Content,
		}
		expectedPresetID = "custom:" + profile.ProfileID
		expectedRevision = profile.Revision
	}
	result, err := s.bridge.Call(ctx, "polish", payload)
	if err != nil {
		fail(w, http.StatusBadGateway, "Email polishing failed. Check the selected AI provider's key, model access, and usage limits, then try again.")
		return
	}
	var polished struct {
		Subject        string  `json:"subject"`
		Body           string  `json:"body"`
		BodyHTML       *string `json:"body_html"`
		Error          string  `json:"error"`
		PresetID       string  `json:"preset_id"`
		PresetRevision int     `json:"preset_revision"`
	}
	if json.Unmarshal(result, &polished) != nil {
		fail(w, http.StatusBadGateway, "The model returned an invalid polishing suggestion")
		return
	}
	if polished.Error == "provider_unavailable" {
		fail(w, http.StatusUnprocessableEntity, "The selected AI provider is not configured. Check the server's API key and model settings.")
		return
	}
	if polished.Error != "" || polished.PresetID != expectedPresetID ||
		polished.PresetRevision < 1 || polished.PresetRevision > 2147483647 ||
		(input.ProfileID != "" && polished.PresetRevision != expectedRevision) ||
		strings.TrimSpace(polished.Subject) == "" || strings.TrimSpace(polished.Body) == "" ||
		polished.BodyHTML == nil {
		fail(w, http.StatusBadGateway, "The model returned an invalid polishing suggestion")
		return
	}
	hasHTML := strings.TrimSpace(draft.BodyHTML) != ""
	if hasHTML != (strings.TrimSpace(*polished.BodyHTML) != "") {
		fail(w, http.StatusBadGateway, "The model returned an invalid polishing suggestion")
		return
	}
	subject, body, bodyHTML, err := draftcontent.Prepare(polished.Subject, polished.Body, *polished.BodyHTML)
	if err != nil || hasHTML != (bodyHTML != "") {
		fail(w, http.StatusBadGateway, "The model returned an invalid polishing suggestion")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"draft_id":            draft.DraftID,
		"version":             draft.Version,
		"source_content_hash": draft.ContentHash,
		"subject":             subject,
		"body":                body,
		"body_html":           bodyHTML,
		"preset_id":           polished.PresetID,
		"preset_revision":     polished.PresetRevision,
	})
}
