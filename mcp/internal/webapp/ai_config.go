package webapp

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

func (c *AIConfiguration) modelCredentialFingerprint(id string) ([32]byte, error) {
	if aiKeyVariable(id) == "" {
		return [32]byte{}, aiInputError("Choose a supported AI provider")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	settings, err := c.load()
	if err != nil {
		return [32]byte{}, err
	}
	provider := settings.Providers[id]
	key := provider.APIKey
	if key == "" {
		key = strings.TrimSpace(os.Getenv(aiKeyVariable(id)))
	}
	if provider.Disabled || key == "" || !validAIKey(key) {
		return [32]byte{}, aiInputError("Save an API key before loading models")
	}
	return sha256.Sum256([]byte(key)), nil
}

type AIConfiguration struct {
	Path string
	mu   sync.Mutex
}

type savedAIProvider struct {
	APIKey   string `json:"api_key,omitempty"`
	Model    string `json:"model,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

type savedAIConfiguration struct {
	Version         int                        `json:"version"`
	DefaultProvider *string                    `json:"default_provider,omitempty"`
	Providers       map[string]savedAIProvider `json:"providers"`
}

type aiInputError string

func (e aiInputError) Error() string { return string(e) }

var aiModelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var openRouterModelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*(:[A-Za-z0-9][A-Za-z0-9._-]*)?$`)

func validAIModel(provider, model string) bool {
	if provider == "openrouter" {
		return len(model) <= 256 && openRouterModelPattern.MatchString(model)
	}
	return aiModelPattern.MatchString(model)
}

func aiKeyVariable(provider string) string {
	switch provider {
	case "cohere":
		return "COHERE_API_KEY"
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	case "openrouter":
		return "OPENROUTER_API_KEY"
	default:
		return ""
	}
}

func validAIKey(key string) bool {
	if len(key) > 4096 {
		return false
	}
	for _, character := range key {
		if character < 33 || character > 126 {
			return false
		}
	}
	return true
}

func (c *AIConfiguration) load() (savedAIConfiguration, error) {
	settings := savedAIConfiguration{Version: 1, Providers: map[string]savedAIProvider{}}
	info, err := os.Lstat(c.Path)
	if errors.Is(err, os.ErrNotExist) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 32768 {
		return settings, errors.New("AI settings file must be a private regular file")
	}
	file, err := os.Open(c.Path)
	if err != nil {
		return settings, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 32769))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil {
		return settings, errors.New("AI settings file is invalid")
	}
	if decoder.Decode(new(any)) != io.EOF || settings.Version != 1 || settings.Providers == nil {
		return settings, errors.New("AI settings file is invalid")
	}
	if settings.DefaultProvider != nil && *settings.DefaultProvider != "" && aiKeyVariable(*settings.DefaultProvider) == "" {
		return settings, errors.New("AI default provider is invalid")
	}
	for id, provider := range settings.Providers {
		if aiKeyVariable(id) == "" || !validAIKey(provider.APIKey) || (provider.Model != "" && !validAIModel(id, provider.Model)) {
			return settings, errors.New("AI provider settings are invalid")
		}
	}
	return settings, nil
}

func (c *AIConfiguration) save(settings savedAIConfiguration) error {
	directory := filepath.Dir(c.Path)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("AI settings directory is invalid")
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".ai-settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := json.NewEncoder(file).Encode(settings); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), c.Path)
}

func (c *AIConfiguration) UpdateProvider(id, key, model string) error {
	key, model = strings.TrimSpace(key), strings.TrimSpace(model)
	if aiKeyVariable(id) == "" {
		return aiInputError("Choose a supported AI provider")
	}
	if !validAIKey(key) {
		return aiInputError("Enter an API key without spaces or control characters, up to 4,096 characters")
	}
	if !validAIModel(id, model) {
		if id == "openrouter" {
			return aiInputError("Enter an OpenRouter model ID such as openai/gpt-6-luna, without a URL or extra provider prefix")
		}
		return aiInputError("Enter a model ID without a provider prefix or URL")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	settings, err := c.load()
	if err != nil {
		return err
	}
	provider := settings.Providers[id]
	if key != "" {
		provider.APIKey = key
	}
	if provider.APIKey == "" && strings.TrimSpace(os.Getenv(aiKeyVariable(id))) == "" {
		return aiInputError("Enter an API key before enabling this provider")
	}
	provider.Model, provider.Disabled = model, false
	settings.Providers[id] = provider
	return c.save(settings)
}

func (c *AIConfiguration) DisableProvider(id string) error {
	if aiKeyVariable(id) == "" {
		return aiInputError("Choose a supported AI provider")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	settings, err := c.load()
	if err != nil {
		return err
	}
	settings.Providers[id] = savedAIProvider{Disabled: true}
	defaultProvider := strings.TrimSpace(os.Getenv("SEFIR_DEFAULT_PROVIDER"))
	if settings.DefaultProvider != nil {
		defaultProvider = *settings.DefaultProvider
	}
	if defaultProvider == id {
		automatic := ""
		settings.DefaultProvider = &automatic
	}
	return c.save(settings)
}

func (c *AIConfiguration) SetDefault(id string) error {
	if id != "" && aiKeyVariable(id) == "" {
		return aiInputError("Choose a supported AI provider")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	settings, err := c.load()
	if err != nil {
		return err
	}
	if id != "" {
		provider := settings.Providers[id]
		if provider.Disabled || (provider.APIKey == "" && strings.TrimSpace(os.Getenv(aiKeyVariable(id))) == "") {
			return aiInputError("Configure this provider before making it the default")
		}
	}
	settings.DefaultProvider = &id
	return c.save(settings)
}

func (c *AIConfiguration) Environment(base []string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	settings, err := c.load()
	if err != nil {
		return nil, err
	}
	overrides := map[string]string{"PYTHON_DOTENV_DISABLED": "1"}
	for id, provider := range settings.Providers {
		variable := aiKeyVariable(id)
		if provider.Disabled {
			overrides[variable] = ""
		} else if provider.APIKey != "" {
			overrides[variable] = provider.APIKey
		}
		if provider.Model != "" {
			overrides["SEFIR_"+strings.ToUpper(id)+"_MODEL"] = provider.Model
		}
	}
	if settings.DefaultProvider != nil {
		overrides["SEFIR_DEFAULT_PROVIDER"] = *settings.DefaultProvider
	}
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if _, replaced := overrides[key]; !replaced {
			result = append(result, entry)
		}
	}
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	return result, nil
}
