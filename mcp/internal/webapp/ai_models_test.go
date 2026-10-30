package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type modelDiscoveryBridgeFunc func(context.Context, string, any) (json.RawMessage, error)

func (f modelDiscoveryBridgeFunc) Call(ctx context.Context, action string, payload any) (json.RawMessage, error) {
	return f(ctx, action, payload)
}

func TestAIModelDiscoveryDiscardsResultsWhenKeyChangesDuringFetch(t *testing.T) {
	server, _, _, _ := fixture(t)
	server.config.AI = aiConfigFixture(t)
	t.Setenv("OPENAI_API_KEY", "original-test-key")
	cookie, _ := login(t, server)
	server.bridge = modelDiscoveryBridgeFunc(func(_ context.Context, _ string, _ any) (json.RawMessage, error) {
		if err := server.config.AI.UpdateProvider("openai", "replacement-test-key", "gpt-6-luna"); err != nil {
			t.Fatal(err)
		}
		return json.RawMessage(`{"models":[{"id":"private-model","name":"Private model"}]}`), nil
	})
	response := request(server, "GET", "/api/settings/ai/openai/models", "", cookie, "")
	if response.Code != 409 || strings.Contains(response.Body.String(), "private-model") || !server.modelCatalogs["openai"].expires.IsZero() {
		t.Fatal("previous credentials' results were cached or returned")
	}
}

func TestAIModelDiscoveryProcessUsesSavedKeyAndDisablesDotenv(t *testing.T) {
	configuration := aiConfigFixture(t)
	t.Setenv("OPENAI_API_KEY", "environment-test-key")
	if err := configuration.UpdateProvider("openai", "saved-test-key", "gpt-6-luna"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	script := filepath.Join(root, "fake-python")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '{\"key\":\"%s\",\"dotenv\":\"%s\"}' \"$OPENAI_API_KEY\" \"$PYTHON_DOTENV_DISABLED\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	bridge := PythonBridge{Python: script, Root: root, AI: configuration}
	result, err := bridge.Call(context.Background(), "models", map[string]string{"provider": "openai"})
	if err != nil || string(result) != `{"key":"saved-test-key","dotenv":"1"}` {
		t.Fatal("model-discovery process did not receive the effective saved configuration")
	}
}

func TestAIModelDiscoveryRequiresOperatorAndRefreshCSRF(t *testing.T) {
	server, _, bridge, _ := fixture(t)
	server.config.AI = aiConfigFixture(t)
	path := "/api/settings/ai/openai/models"
	for _, method := range []string{"GET", "POST"} {
		if got := request(server, method, path, "", nil, "").Code; got != 401 {
			t.Fatal(got)
		}
	}
	cookie, csrf := login(t, server)
	if got := request(server, "POST", path, "", cookie, "wrong").Code; got != 403 {
		t.Fatal(got)
	}
	server.config.OperatorAccountID = testID
	for _, method := range []string{"GET", "POST"} {
		if got := request(server, method, path, "", cookie, csrf).Code; got != 403 {
			t.Fatal(got)
		}
	}
	if len(bridge.calls) != 0 {
		t.Fatal("unauthorized discovery reached the provider")
	}
}

func TestOpenRouterSettingsDiscoveryAndRemoval(t *testing.T) {
	server, _, bridge, _ := fixture(t)
	server.config.AI = aiConfigFixture(t)
	cookie, csrf := login(t, server)
	path := "/api/settings/ai/openrouter"
	if got := request(server, "PUT", path, `{"api_key":"openrouter-test-key","model":"vendor/model:free"}`, cookie, csrf).Code; got != 200 {
		t.Fatal(got)
	}
	if got := request(server, "PUT", "/api/settings/ai/default", `{"provider":"openrouter"}`, cookie, csrf).Code; got != 200 {
		t.Fatal(got)
	}
	bridge.result = json.RawMessage(`{"models":[{"id":"vendor/model:free","name":"Free model"}]}`)
	if response := request(server, "GET", path+"/models", "", cookie, ""); response.Code != 200 || !strings.Contains(response.Body.String(), "vendor/model:free") {
		t.Fatal(response.Code, response.Body.String())
	}
	for _, model := range []string{"gpt-6-luna", "openrouter/openai/gpt-6-luna", "https://example.test/model", "vendor/model?key=secret", "vendor/../model", "vendor/" + strings.Repeat("x", 250)} {
		body, err := json.Marshal(map[string]string{"api_key": "", "model": model})
		if err != nil {
			t.Fatal(err)
		}
		if got := request(server, "PUT", path, string(body), cookie, csrf).Code; got != 422 {
			t.Fatal("invalid OpenRouter model accepted", got)
		}
	}
	reloaded := &AIConfiguration{Path: server.config.AI.Path}
	for key, expected := range map[string]string{
		"OPENROUTER_API_KEY": "openrouter-test-key", "SEFIR_OPENROUTER_MODEL": "vendor/model:free", "SEFIR_DEFAULT_PROVIDER": "openrouter",
	} {
		if got := aiEnvironmentValue(t, reloaded, nil, key); got != expected {
			t.Fatalf("incorrect reloaded %s", key)
		}
	}
	if got := request(server, "DELETE", path, "", cookie, csrf).Code; got != 200 {
		t.Fatal(got)
	}
	t.Setenv("OPENROUTER_API_KEY", "environment-test-key")
	if got := request(server, "GET", path+"/models", "", cookie, "").Code; got != 422 {
		t.Fatal("removed OpenRouter provider used the environment key")
	}
	if got := aiEnvironmentValue(t, reloaded, []string{"OPENROUTER_API_KEY=environment-test-key"}, "OPENROUTER_API_KEY"); got != "" {
		t.Fatal("removed OpenRouter provider remained enabled")
	}
}

func TestAIModelCacheRefreshExpiryAndKeyReplacement(t *testing.T) {
	server, _, bridge, _ := fixture(t)
	server.config.AI = aiConfigFixture(t)
	cookie, csrf := login(t, server)
	path := "/api/settings/ai/openai/models"
	if got := request(server, "GET", path, "", cookie, "").Code; got != 422 {
		t.Fatal(got)
	}
	if err := server.config.AI.UpdateProvider("openai", "test-key", "gpt-6-luna"); err != nil {
		t.Fatal(err)
	}
	bridge.result = json.RawMessage(`{"models":[{"id":"gpt-6-luna","name":"GPT Luna","api_key":"private-key"}],"api_key":"private-key"}`)
	for range 2 {
		response := request(server, "GET", path, "", cookie, "")
		if response.Code != 200 || strings.Contains(response.Body.String(), "private-key") || strings.Contains(response.Body.String(), "api_key") {
			t.Fatal(response.Body.String())
		}
	}
	if len(bridge.calls) != 1 || bridge.calls[0] != "models" || bridge.payloads[0].(map[string]string)["provider"] != "openai" {
		t.Fatal("catalogue was not cached")
	}
	if got := request(server, "POST", path, "", cookie, csrf).Code; got != 200 || len(bridge.calls) != 2 {
		t.Fatal("explicit refresh reused the cache")
	}
	server.modelCatalogs["openai"].expires = time.Now().Add(-time.Second)
	if got := request(server, "GET", path, "", cookie, "").Code; got != 200 || len(bridge.calls) != 3 {
		t.Fatal("expired catalogue was reused")
	}
	if err := server.config.AI.UpdateProvider("openai", "replacement-test-key", "gpt-6-luna"); err != nil {
		t.Fatal(err)
	}
	if got := request(server, "GET", path, "", cookie, "").Code; got != 200 || len(bridge.calls) != 4 {
		t.Fatal("previous key's catalogue was reused")
	}
	if err := server.config.AI.DisableProvider("openai"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENAI_API_KEY", "environment-test-key")
	if got := request(server, "GET", path, "", cookie, "").Code; got != 422 || len(bridge.calls) != 4 {
		t.Fatal("disabled provider still allowed discovery")
	}
}

func TestAIModelErrorsAreSafeAndDoNotReplaceTheCache(t *testing.T) {
	server, _, bridge, _ := fixture(t)
	server.config.AI = aiConfigFixture(t)
	t.Setenv("COHERE_API_KEY", "private-test-key")
	cookie, csrf := login(t, server)
	path := "/api/settings/ai/cohere/models"
	bridge.result = json.RawMessage(`{"models":[]}`)
	if got := request(server, "GET", path, "", cookie, "").Code; got != 200 {
		t.Fatal(got)
	}
	for _, test := range []struct {
		result string
		status int
	}{
		{`{"error":"models_auth_failed"}`, 422},
		{`{"error":"models_rate_limited"}`, 503},
		{`{"error":"private-test-key"}`, 503},
		{`{}`, 502}, {`{"models":null}`, 502},
		{`{"models":[{"id":"https://example.test","name":"Model"}]}`, 502},
		{`{"models":[{"id":"model","name":"unsafe\nname"}]}`, 502},
	} {
		bridge.result = json.RawMessage(test.result)
		response := request(server, "POST", path, "", cookie, csrf)
		if response.Code != test.status || strings.Contains(response.Body.String(), "private-test-key") {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	bridge.err = errors.New("private-test-key")
	if response := request(server, "POST", path, "", cookie, csrf); response.Code != 503 || strings.Contains(response.Body.String(), "private-test-key") {
		t.Fatal(response.Body.String())
	}
	calls := len(bridge.calls)
	if got := request(server, "GET", path, "", cookie, "").Code; got != 200 || len(bridge.calls) != calls {
		t.Fatal("failed refresh replaced a valid cached result")
	}
	if got := request(server, "GET", "/api/settings/ai/unknown/models", "", cookie, "").Code; got != 422 || len(bridge.calls) != calls {
		t.Fatal("unknown provider reached the agent")
	}
}
