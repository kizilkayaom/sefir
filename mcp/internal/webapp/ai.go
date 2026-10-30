package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

type aiProvider struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Model string `json:"model"`
}

type aiSettings struct {
	DefaultProvider *string            `json:"default_provider"`
	Providers       []aiProvider       `json:"providers"`
	Options         []aiProviderOption `json:"options,omitempty"`
	CanManage       bool               `json:"can_manage"`
}

type aiProviderOption struct {
	aiProvider
	Configured bool `json:"configured"`
}

func (s *Server) canManageAI(r *http.Request) bool {
	return s.config.AI != nil && contextAccount(r.Context()).ID == s.config.OperatorAccountID
}

func (s *Server) requireAIOperator(w http.ResponseWriter, r *http.Request) bool {
	if !s.canManageAI(r) {
		fail(w, http.StatusForbidden, "Only the installation operator can change shared AI settings")
		return false
	}
	return true
}

func (s *Server) handleAIProviders(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	result, err := s.bridge.Call(ctx, "providers", map[string]any{})
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "Could not load AI providers. Check the server's AI configuration.")
		return
	}
	var settings aiSettings
	if json.Unmarshal(result, &settings) != nil || settings.Providers == nil {
		fail(w, http.StatusBadGateway, "The agent returned invalid AI settings")
		return
	}
	settings.CanManage = s.canManageAI(r)
	if !settings.CanManage {
		settings.Options = nil
	}
	writeJSON(w, http.StatusOK, settings)
}

func aiSaveResult(w http.ResponseWriter, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
		return
	}
	var invalid aiInputError
	if errors.As(err, &invalid) {
		fail(w, http.StatusUnprocessableEntity, invalid.Error())
		return
	}
	fail(w, http.StatusServiceUnavailable, "Could not save AI settings. Check the server's private data directory.")
}

func (s *Server) handleAISave(w http.ResponseWriter, r *http.Request) {
	if !s.requireAIOperator(w, r) {
		return
	}
	var input struct {
		APIKey string `json:"api_key"`
		Model  string `json:"model"`
	}
	if !decode(w, r, &input) {
		return
	}
	aiSaveResult(w, s.config.AI.UpdateProvider(r.PathValue("provider"), input.APIKey, input.Model))
}

func (s *Server) handleAIRemove(w http.ResponseWriter, r *http.Request) {
	if !s.requireAIOperator(w, r) {
		return
	}
	aiSaveResult(w, s.config.AI.DisableProvider(r.PathValue("provider")))
}

func (s *Server) handleAIDefault(w http.ResponseWriter, r *http.Request) {
	if !s.requireAIOperator(w, r) {
		return
	}
	var input struct {
		Provider string `json:"provider"`
	}
	if !decode(w, r, &input) {
		return
	}
	aiSaveResult(w, s.config.AI.SetDefault(input.Provider))
}
