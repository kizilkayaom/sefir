package webapp

import (
	"context"
	"net/http"
	"time"
)

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Confirmation string `json:"confirmation"`
	}

	if !decode(w, r, &input) {
		return
	}

	if input.Confirmation != "DELETE" {
		fail(w, http.StatusUnprocessableEntity, "Type DELETE to confirm account deletion")
		return
	}

	account := contextAccount(r.Context())
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := s.accounts.RequestDeletion(ctx, account.ID); err != nil {
		storeError(w, err)
		return
	}
	s.invalidateAccountSessions(account.ID)
	s.setCookie(w, "", -1)
	s.googleCookie(w, "", -1)
	writeJSON(w, http.StatusAccepted, map[string]string{
		"status": "deletion_requested",
	})
}
