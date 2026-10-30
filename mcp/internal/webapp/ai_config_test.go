package webapp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func aiConfigFixture(t *testing.T) *AIConfiguration {
	t.Helper()
	for _, variable := range []string{"COHERE_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_API_KEY", "OPENROUTER_API_KEY", "SEFIR_DEFAULT_PROVIDER"} {
		t.Setenv(variable, "")
	}
	return &AIConfiguration{Path: filepath.Join(t.TempDir(), ".sefir", "ai-settings.json")}
}

func aiEnvironmentValue(t *testing.T, configuration *AIConfiguration, base []string, key string) string {
	t.Helper()
	environment, err := configuration.Environment(base)
	if err != nil {
		t.Fatal(err)
	}
	value, matches := "", 0
	for _, entry := range environment {
		name, candidate, _ := strings.Cut(entry, "=")
		if name == key {
			value, matches = candidate, matches+1
		}
	}
	if matches != 1 {
		t.Fatalf("expected one %s setting, got %d", key, matches)
	}
	return value
}

func TestAIConfigurationPersistsPrivateKeysAndModelChanges(t *testing.T) {
	configuration := aiConfigFixture(t)
	if err := configuration.UpdateProvider("openai", "test-openai-key", "gpt-6-luna"); err != nil {
		t.Fatal(err)
	}
	if err := configuration.UpdateProvider("anthropic", "test-anthropic-key", "claude-sonnet-5-5"); err != nil {
		t.Fatal(err)
	}
	if err := configuration.UpdateProvider("openai", "", "gpt-6.1-sol"); err != nil {
		t.Fatal(err)
	}
	if err := configuration.SetDefault("openai"); err != nil {
		t.Fatal(err)
	}
	reloaded := &AIConfiguration{Path: configuration.Path}
	base := []string{"OPENAI_API_KEY=old-environment-key", "UNRELATED=kept"}
	for key, expected := range map[string]string{
		"OPENAI_API_KEY": "test-openai-key", "ANTHROPIC_API_KEY": "test-anthropic-key",
		"SEFIR_OPENAI_MODEL": "gpt-6.1-sol", "SEFIR_DEFAULT_PROVIDER": "openai", "UNRELATED": "kept",
		"PYTHON_DOTENV_DISABLED": "1",
	} {
		if got := aiEnvironmentValue(t, reloaded, base, key); got != expected {
			t.Fatalf("incorrect %s setting", key)
		}
	}
	for path, permissions := range map[string]os.FileMode{configuration.Path: 0600, filepath.Dir(configuration.Path): 0700} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != permissions {
			t.Fatalf("incorrect permissions for %s", filepath.Base(path))
		}
	}
	files, err := os.ReadDir(filepath.Dir(configuration.Path))
	if err != nil || len(files) != 1 {
		t.Fatal("temporary secret files were left behind")
	}
}

func TestRemovingAIKeyDisablesEnvironmentFallbackAcrossRestart(t *testing.T) {
	configuration := aiConfigFixture(t)
	t.Setenv("SEFIR_DEFAULT_PROVIDER", " openai ")
	if err := configuration.UpdateProvider("openai", "test-saved-key", "gpt-6-luna"); err != nil {
		t.Fatal(err)
	}
	if err := configuration.DisableProvider("openai"); err != nil {
		t.Fatal(err)
	}
	reloaded := &AIConfiguration{Path: configuration.Path}
	base := []string{"OPENAI_API_KEY=test-environment-key", "SEFIR_DEFAULT_PROVIDER=openai", "PYTHON_DOTENV_DISABLED=0"}
	if got := aiEnvironmentValue(t, reloaded, base, "OPENAI_API_KEY"); got != "" {
		t.Fatal("environment key re-enabled a disabled provider")
	}
	if got := aiEnvironmentValue(t, reloaded, base, "SEFIR_DEFAULT_PROVIDER"); got != "" {
		t.Fatal("disabled default provider was retained")
	}
	contents, err := os.ReadFile(configuration.Path)
	if err != nil || strings.Contains(string(contents), "test-saved-key") {
		t.Fatal("removed key was retained on disk")
	}
	if err := reloaded.UpdateProvider("openai", "replacement-test-key", "gpt-6-luna"); err != nil {
		t.Fatal(err)
	}
	if got := aiEnvironmentValue(t, reloaded, base, "OPENAI_API_KEY"); got != "replacement-test-key" {
		t.Fatal("replacement key was not enabled")
	}
}

func TestAIConfigurationRejectsInvalidInputWithoutWriting(t *testing.T) {
	configuration := aiConfigFixture(t)
	for _, input := range [][3]string{
		{"unknown", "test-key", "model"}, {"openai", "", "gpt-6-luna"},
		{"openai", "line\nbreak", "gpt-6-luna"}, {"openai", strings.Repeat("x", 4097), "gpt-6-luna"},
		{"openai", "test-key", "anthropic/model"}, {"openai", "test-key", "https://example.test"},
	} {
		var invalid aiInputError
		if err := configuration.UpdateProvider(input[0], input[1], input[2]); !errors.As(err, &invalid) {
			t.Fatal("invalid provider input was accepted")
		}
	}
	if err := configuration.SetDefault("openai"); err == nil {
		t.Fatal("unconfigured default was accepted")
	}
	if _, err := os.Stat(configuration.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid settings were written")
	}
	t.Setenv("OPENAI_API_KEY", "environment-test-key")
	if err := configuration.UpdateProvider("openai", "", "gpt-6-luna"); err != nil {
		t.Fatal("existing environment key could not be used")
	}
}

func TestAIConfigurationRejectsInsecureOrInvalidFiles(t *testing.T) {
	for _, contents := range []string{`{"version":9,"providers":{}}`, `{"version":1,"providers":null}`, `{"version":1,"providers":{},"unexpected":true}`, `{"version":1,"providers":{"unknown":{}}}`, `not JSON`} {
		configuration := aiConfigFixture(t)
		if err := os.MkdirAll(filepath.Dir(configuration.Path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(configuration.Path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := configuration.Environment(nil); err == nil {
			t.Fatal("invalid configuration was accepted")
		}
	}
	configuration := aiConfigFixture(t)
	if err := configuration.UpdateProvider("openai", "test-key", "gpt-6-luna"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(configuration.Path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := configuration.Environment(nil); err == nil {
		t.Fatal("publicly readable settings were accepted")
	}
	linked := &AIConfiguration{Path: filepath.Join(t.TempDir(), "linked.json")}
	if err := os.Symlink(configuration.Path, linked.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := linked.Environment(nil); err == nil {
		t.Fatal("symlinked settings were accepted")
	}
}

func TestConcurrentAIUpdatesPreserveEachProvider(t *testing.T) {
	configuration := aiConfigFixture(t)
	var workers sync.WaitGroup
	for _, provider := range []string{"cohere", "anthropic", "openai", "openrouter"} {
		workers.Go(func() {
			model := "test-model"
			if provider == "openrouter" {
				model = "vendor/test-model"
			}
			for range 5 {
				if err := configuration.UpdateProvider(provider, "test-key-"+provider, model); err != nil {
					t.Error(err)
				}
				if _, err := configuration.Environment(nil); err != nil {
					t.Error(err)
				}
			}
		})
	}
	workers.Wait()
	for _, provider := range []string{"cohere", "anthropic", "openai", "openrouter"} {
		if got := aiEnvironmentValue(t, configuration, nil, aiKeyVariable(provider)); got != "test-key-"+provider {
			t.Fatal("concurrent update lost a provider")
		}
	}
}
