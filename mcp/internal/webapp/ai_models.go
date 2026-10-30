package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"
	"unicode"
)

type aiModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type aiModelCache struct {
	mu          sync.Mutex
	fingerprint [32]byte
	expires     time.Time
	models      []aiModel
}

func (s *Server) handleAIModels(w http.ResponseWriter, r *http.Request) {
	if !s.requireAIOperator(w, r) {
		return
	}
	id := r.PathValue("provider")
	cache, ok := s.modelCatalogs[id]
	if !ok {
		fail(w, http.StatusUnprocessableEntity, "Choose a supported AI provider")
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	fingerprint, err := s.config.AI.modelCredentialFingerprint(id)
	if err != nil {
		var invalid aiInputError
		if errors.As(err, &invalid) {
			fail(w, http.StatusUnprocessableEntity, invalid.Error())
		} else {
			fail(w, http.StatusServiceUnavailable, "Could not load the server's AI configuration")
		}
		return
	}
	if r.Method == http.MethodGet && cache.fingerprint == fingerprint && time.Now().Before(cache.expires) {
		writeJSON(w, http.StatusOK, map[string]any{"models": cache.models})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	result, err := s.bridge.Call(ctx, "models", map[string]string{"provider": id})
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "Could not fetch models. Try again or enter a model ID manually.")
		return
	}
	var response struct {
		Models []aiModel `json:"models"`
		Error  string    `json:"error"`
	}
	if json.Unmarshal(result, &response) != nil {
		fail(w, http.StatusBadGateway, "The provider returned an invalid model list")
		return
	}
	switch response.Error {
	case "":
	case "models_auth_failed":
		fail(w, http.StatusUnprocessableEntity, "The provider rejected the saved API key or its model-list permissions. Check the key or enter a model ID manually.")
		return
	case "models_rate_limited":
		fail(w, http.StatusServiceUnavailable, "The provider is limiting model-list requests. Wait before refreshing or enter a model ID manually.")
		return
	default:
		fail(w, http.StatusServiceUnavailable, "Could not fetch models. Try again or enter a model ID manually.")
		return
	}
	if response.Models == nil || len(response.Models) > 10000 {
		fail(w, http.StatusBadGateway, "The provider returned an invalid model list")
		return
	}
	for _, model := range response.Models {
		if !validAIModel(id, model.ID) || model.Name == "" || len([]rune(model.Name)) > 160 {
			fail(w, http.StatusBadGateway, "The provider returned an invalid model list")
			return
		}
		for _, character := range model.Name {
			if !unicode.IsPrint(character) {
				fail(w, http.StatusBadGateway, "The provider returned an invalid model list")
				return
			}
		}
	}
	current, err := s.config.AI.modelCredentialFingerprint(id)
	if err != nil || current != fingerprint {
		fail(w, http.StatusConflict, "AI settings changed while loading models. Refresh the model list.")
		return
	}
	cache.models, cache.fingerprint, cache.expires = response.Models, fingerprint, time.Now().Add(10*time.Minute)
	writeJSON(w, http.StatusOK, map[string]any{"models": cache.models})
}
