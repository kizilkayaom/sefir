package webapp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestOnlyOperatorCanManageSharedAISettings(t *testing.T) {
	server, _, bridge, _ := fixture(t)
	server.config.AI = aiConfigFixture(t)
	cookie, csrf := login(t, server)
	body := `{"api_key":"test-key","model":"gpt-6-luna"}`
	for _, path := range []string{"/api/settings/ai/openai", "/api/settings/ai/default"} {
		if got := request(server, "PUT", path, body, nil, "").Code; got != 401 {
			t.Fatal(got)
		}
		if got := request(server, "PUT", path, body, cookie, "wrong-csrf").Code; got != 403 {
			t.Fatal(got)
		}
	}
	operatorID := server.config.OperatorAccountID
	server.config.OperatorAccountID = testID
	for _, operation := range [][3]string{
		{"PUT", "/api/settings/ai/openai", body}, {"DELETE", "/api/settings/ai/openai", ""},
		{"PUT", "/api/settings/ai/default", `{"provider":"openai"}`},
	} {
		if got := request(server, operation[0], operation[1], operation[2], cookie, csrf).Code; got != 403 {
			t.Fatal(got)
		}
	}
	bridge.result = json.RawMessage(`{"default_provider":null,"providers":[],"options":[{"id":"openai","name":"OpenAI (GPT)","model":"gpt-6-luna","configured":false,"api_key":"private-test-key"}],"can_manage":true}`)
	response := request(server, "GET", "/api/settings/ai", "", cookie, "")
	if response.Code != 200 || strings.Contains(response.Body.String(), `"can_manage":true`) || strings.Contains(response.Body.String(), "options") {
		t.Fatal("non-operator received management capabilities")
	}
	server.config.OperatorAccountID = operatorID
	response = request(server, "GET", "/api/settings/ai", "", cookie, "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"can_manage":true`) || strings.Contains(response.Body.String(), "private-test-key") || strings.Contains(response.Body.String(), "api_key") {
		t.Fatal(response.Body.String())
	}
	response = request(server, "PUT", "/api/settings/ai/openai", body, cookie, csrf)
	if response.Code != 200 || strings.Contains(response.Body.String(), "test-key") {
		t.Fatal(response.Body.String())
	}
	if got := request(server, "PUT", "/api/settings/ai/default", `{"provider":"openai"}`, cookie, csrf).Code; got != 200 {
		t.Fatal(got)
	}
	if got := request(server, "DELETE", "/api/settings/ai/openai", "", cookie, csrf).Code; got != 200 {
		t.Fatal(got)
	}
	if got := aiEnvironmentValue(t, server.config.AI, []string{"OPENAI_API_KEY=environment-test-key"}, "OPENAI_API_KEY"); got != "" {
		t.Fatal("removed provider remains enabled")
	}
}

func TestAIManagementErrorsDoNotExposeKeys(t *testing.T) {
	server, _, _, _ := fixture(t)
	server.config.AI = aiConfigFixture(t)
	cookie, csrf := login(t, server)
	for _, body := range []string{`{"api_key":"test-key","model":"https://example.test"}`, `{"api_key":"","model":"gpt-6-luna"}`} {
		response := request(server, "PUT", "/api/settings/ai/openai", body, cookie, csrf)
		if response.Code != 422 || strings.Contains(response.Body.String(), "test-key") {
			t.Fatal(response.Body.String())
		}
	}
	server.config.AI.Path = t.TempDir()
	response := request(server, "PUT", "/api/settings/ai/openai", `{"api_key":"private-test-key","model":"gpt-6-luna"}`, cookie, csrf)
	if response.Code != 503 || strings.Contains(response.Body.String(), "private-test-key") || strings.Contains(response.Body.String(), server.config.AI.Path) {
		t.Fatal(response.Body.String())
	}
}

func TestAISettingsRequireAuthenticationAndHideUnexpectedFields(t *testing.T) {
	server, _, bridge, _ := fixture(t)
	if got := request(server, "GET", "/api/settings/ai", "", nil, "").Code; got != 401 {
		t.Fatal(got)
	}
	if len(bridge.calls) != 0 {
		t.Fatal("unauthenticated request reached the agent")
	}
	cookie, _ := login(t, server)
	bridge.result = json.RawMessage(`{"default_provider":"openai","providers":[{"id":"openai","name":"OpenAI (GPT)","model":"gpt-6-luna","api_key":"private-test-value"}],"api_key":"private-test-value"}`)
	response := request(server, "GET", "/api/settings/ai", "", cookie, "")
	if response.Code != 200 || strings.Contains(response.Body.String(), "private-test-value") || strings.Contains(response.Body.String(), "api_key") {
		t.Fatal(response.Body.String())
	}
	var settings aiSettings
	if err := json.Unmarshal(response.Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	if settings.DefaultProvider == nil || *settings.DefaultProvider != "openai" || len(settings.Providers) != 1 {
		t.Fatal(settings)
	}
	if bridge.calls[0] != "providers" {
		t.Fatal(bridge.calls)
	}
}

func TestAISettingsHandleEmptyAndUnavailableConfiguration(t *testing.T) {
	server, _, bridge, _ := fixture(t)
	cookie, _ := login(t, server)
	bridge.result = json.RawMessage(`{"default_provider":null,"providers":[]}`)
	if got := request(server, "GET", "/api/settings/ai", "", cookie, "").Code; got != 200 {
		t.Fatal(got)
	}
	bridge.result = json.RawMessage(`{"unexpected":true}`)
	if got := request(server, "GET", "/api/settings/ai", "", cookie, "").Code; got != 502 {
		t.Fatal(got)
	}
	bridge.err = errors.New("private upstream details")
	response := request(server, "GET", "/api/settings/ai", "", cookie, "")
	if response.Code != 503 || strings.Contains(response.Body.String(), "private") {
		t.Fatal(response.Body.String())
	}
}

func TestGenerationUsesSelectedProvider(t *testing.T) {
	for _, provider := range []string{"cohere", "anthropic", "openai", "openrouter"} {
		t.Run(provider, func(t *testing.T) {
			server, store, bridge, _ := fixture(t)
			cookie, csrf := login(t, server)
			bridge.result = json.RawMessage(`{"subject":"Hello","body":"Test body"}`)
			body := `{"contact_id":"` + testID + `","cv_text":"Fictional test resume","provider":"` + provider + `"}`
			if got := request(server, "POST", "/api/drafts", body, cookie, csrf).Code; got != 201 {
				t.Fatal(got)
			}
			if len(bridge.calls) != 1 || bridge.calls[0] != "generate" || bridge.payloads[0].(map[string]any)["provider"] != provider {
				t.Fatal("selected provider was not forwarded")
			}
			if store.draftInput.Subject != "Hello" {
				t.Fatal("draft was not saved")
			}
		})
	}
}

func TestGenerationUsesSelectedPreset(t *testing.T) {
	for _, test := range []struct {
		name  string
		extra string
		want  string
	}{
		{name: "professional", extra: `,"preset_id":"prof"`, want: "prof"},
		{name: "warm professional", extra: `,"preset_id":"warm_prof"`, want: "warm_prof"},
		{name: "technical", extra: `,"preset_id":"technical"`, want: "technical"},
		{name: "formal", extra: `,"preset_id":"formal"`, want: "formal"},
		{name: "omitted", want: "prof"},
		{name: "empty", extra: `,"preset_id":""`, want: "prof"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, store, bridge, _ := fixture(t)
			cookie, csrf := login(t, server)
			bridge.result = json.RawMessage(`{"subject":"Hello","body":"Test body"}`)
			body := `{"contact_id":"` + testID + `","cv_text":"Fictional test resume","provider":"anthropic"` + test.extra + `}`
			response := request(server, "POST", "/api/drafts", body, cookie, csrf)
			if response.Code != 201 {
				t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
			}
			if len(bridge.calls) != 1 || bridge.calls[0] != "generate" {
				t.Fatal("draft generation was not called exactly once")
			}
			if got := bridge.payloads[0].(map[string]any)["preset_id"]; got != test.want {
				t.Fatalf("forwarded preset: got %v, want %q", got, test.want)
			}
			if store.draftInput.ContactID != testID || store.draftInput.Subject != "Hello" || store.draftInput.Body != "Test body" {
				t.Fatal("generated draft was not saved")
			}
		})
	}
}

func TestGenerationRejectsUnsupportedPresets(t *testing.T) {
	for _, test := range []struct {
		name string
		busy bool
	}{
		{name: "available"},
		{name: "already generating", busy: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, store, bridge, _ := fixture(t)
			cookie, csrf := login(t, server)
			if test.busy {
				server.generating <- struct{}{}
			}
			body := `{"contact_id":"` + testID + `","cv_text":"Fictional test resume","preset_id":"unknown"}`
			response := request(server, "POST", "/api/drafts", body, cookie, csrf)
			if response.Code != 422 {
				t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
			}
			var result struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Error != "Choose a supported writing style" {
				t.Fatalf("unexpected error: %q", result.Error)
			}
			if len(bridge.calls) != 0 || store.draftInput.ContactID != "" {
				t.Fatal("invalid preset reached generation or saved a draft")
			}
		})
	}
}

func TestGenerationRejectsUnsupportedProvidersAndClientKeys(t *testing.T) {
	for _, extra := range []string{`,"provider":"unknown"`, `,"api_key":"private-test-value"`, `,"model":"custom-model"`} {
		server, store, bridge, _ := fixture(t)
		cookie, csrf := login(t, server)
		body := `{"contact_id":"` + testID + `","cv_text":"Fictional test resume"` + extra + `}`
		response := request(server, "POST", "/api/drafts", body, cookie, csrf)
		if response.Code != 400 && response.Code != 422 {
			t.Fatal(response.Code)
		}
		if len(bridge.calls) != 0 || store.draftInput.Subject != "" {
			t.Fatal("invalid provider request was processed")
		}
	}
}

func TestUnavailableProviderDoesNotSaveDraftOrFallback(t *testing.T) {
	server, store, bridge, _ := fixture(t)
	cookie, csrf := login(t, server)
	body := `{"contact_id":"` + testID + `","cv_text":"Fictional test resume","provider":"openai"}`
	bridge.result = json.RawMessage(`{"error":"provider_unavailable"}`)
	if got := request(server, "POST", "/api/drafts", body, cookie, csrf).Code; got != 422 {
		t.Fatal(got)
	}
	if store.draftInput.Subject != "" || len(bridge.calls) != 1 {
		t.Fatal("unconfigured provider request saved a draft or retried")
	}
	bridge.err = errors.New("private upstream details")
	response := request(server, "POST", "/api/drafts", body, cookie, csrf)
	if response.Code != 502 || strings.Contains(response.Body.String(), "private") || store.draftInput.Subject != "" || len(bridge.calls) != 2 {
		t.Fatal(response.Body.String())
	}
}
