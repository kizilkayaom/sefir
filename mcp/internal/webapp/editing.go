package webapp

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/kizilkayaom/sefir/mcp/internal/catalogue"
	"github.com/kizilkayaom/sefir/mcp/internal/draftcontent"
)

func (s *Server) handleCompose(w http.ResponseWriter, r *http.Request) {
	var input catalogue.CreateDraftInput
	if !decode(w, r, &input) {
		return
	}
	if !uuidPattern.MatchString(input.ContactID) {
		fail(w, http.StatusUnprocessableEntity, "Select a confirmed contact")
		return
	}
	var err error
	input.Subject, input.Body, input.BodyHTML, err = draftcontent.Prepare(input.Subject, input.Body, input.BodyHTML)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	draft, err := s.store.CreateDraft(ctx, input)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, draft)
}

func (s *Server) handleEditDraft(w http.ResponseWriter, r *http.Request) {
	id, version, ok := draftKey(w, r)
	if !ok {
		return
	}
	var input catalogue.EditDraftInput
	if !decode(w, r, &input) {
		return
	}
	if input.ContentHash == "" {
		fail(w, http.StatusUnprocessableEntity, "The saved draft's content hash is required")
		return
	}
	if input.RecipientEmail != nil {
		recipient, err := draftcontent.NormalizeRecipient(*input.RecipientEmail)
		if err != nil {
			fail(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		input.RecipientEmail = &recipient
	}
	var err error
	input.Subject, input.Body, input.BodyHTML, err = draftcontent.Prepare(input.Subject, input.Body, input.BodyHTML)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	draft, err := s.store.SaveDraftVersion(ctx, id, version, input)
	if errors.Is(err, catalogue.ErrDraftConflict) {
		fail(w, http.StatusConflict, "A newer version exists or this draft changed. Your edits are still here; copy them before refreshing to open the latest version.")
		return
	}
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, draft)
}
