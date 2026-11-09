package webapp

import (
	"context"
	"errors"
	"net/http"
	"time"
)

func (s *Server) handleDefaultPreset(w http.ResponseWriter, r *http.Request) {
	var input struct {
		PresetID string `json:"preset_id"`
	}
	if !decode(w, r, &input) {
		return
	}

	account := contextAccount(r.Context())
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	updated, err := s.accounts.SetDefaultPreset(ctx, account.ID, input.PresetID)
	if errors.Is(err, ErrInvalidPreset) {
		fail(w, http.StatusUnprocessableEntity, "Choose a supported writing style")
		return
	}
	if err != nil {
		storeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"default_preset_id": updated.DefaultPresetID,
	})
}
