package writingstyle

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

type Profile struct {
	FormatVersion int
	Name          string
	Language      string
	Content       string
}

func ParseProfile(filename string, data []byte) (Profile, error) {
	if !strings.EqualFold(filepath.Ext(filename), ".md") {
		return Profile{}, errors.New("profile must be a Markdown (.md) file")
	}
	if len(data) == 0 {
		return Profile{}, errors.New("profile file must not be empty")
	}
	if len(data) > 32*1024 {
		return Profile{}, errors.New("profile file must not exceed 32 KiB")
	}
	if !utf8.Valid(data) {
		return Profile{}, errors.New("profile file must contain valid UTF-8")
	}
	text := strings.TrimPrefix(strings.ReplaceAll(string(data), "\r\n", "\n"), "\ufeff")
	if strings.ContainsRune(text, '\x00') {
		return Profile{}, errors.New("profile file must not contain NUL characters")
	}
	lines := strings.Split(text, "\n")
	if lines[0] != "---" {
		return Profile{}, errors.New("profile must start with a --- frontmatter delimiter")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return Profile{}, errors.New("profile frontmatter must end with a --- delimiter")
	}
	content := strings.Join(lines[end+1:], "\n")
	if strings.TrimSpace(content) == "" {
		return Profile{}, errors.New("profile must include Markdown style guidance after the frontmatter")
	}
	if len(content) > 16*1024 {
		return Profile{}, errors.New("profile Markdown content must not exceed 16 KiB")
	}

	var document yaml.Node
	if err := yaml.Load([]byte(strings.Join(lines[1:end], "\n")), &document); err != nil {
		return Profile{}, fmt.Errorf("invalid profile YAML frontmatter: %w", err)
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return Profile{}, errors.New("profile frontmatter must be a YAML mapping")
	}
	metadata := document.Content[0]
	if metadata.Kind != yaml.MappingNode || metadata.ShortTag() != "!!map" || metadata.Anchor != "" {
		return Profile{}, errors.New("profile frontmatter must be a YAML mapping without tags or anchors")
	}
	fields := make(map[string]*yaml.Node)
	for i := 0; i < len(metadata.Content); i += 2 {
		key, value := metadata.Content[i], metadata.Content[i+1]
		if key.Kind != yaml.ScalarNode || key.ShortTag() != "!!str" || key.Anchor != "" {
			return Profile{}, errors.New("profile metadata keys must be plain strings")
		}
		if key.Value != "format_version" && key.Value != "name" && key.Value != "language" {
			return Profile{}, fmt.Errorf("unknown profile metadata field %q", key.Value)
		}
		if _, exists := fields[key.Value]; exists {
			return Profile{}, fmt.Errorf("duplicate profile metadata field %q", key.Value)
		}
		if value.Kind != yaml.ScalarNode || value.Anchor != "" {
			return Profile{}, fmt.Errorf("profile %s must be a scalar value without anchors or aliases", key.Value)
		}
		if strings.ContainsRune(value.Value, '\x00') {
			return Profile{}, fmt.Errorf("profile %s must not contain NUL characters", key.Value)
		}
		fields[key.Value] = value
	}

	profile := Profile{Language: "en", Content: content}
	version := fields["format_version"]
	if version == nil || version.ShortTag() != "!!int" {
		return Profile{}, errors.New("profile format_version is required and must be the integer 1")
	}
	if err := version.Load(&profile.FormatVersion); err != nil || profile.FormatVersion != 1 {
		return Profile{}, errors.New("profile format_version must be 1")
	}
	name := fields["name"]
	if name == nil || name.ShortTag() != "!!str" {
		return Profile{}, errors.New("profile name is required and must be a string")
	}
	profile.Name = strings.TrimSpace(name.Value)
	if length := utf8.RuneCountInString(profile.Name); length < 1 || length > 80 {
		return Profile{}, errors.New("profile name must contain between 1 and 80 characters")
	}
	if language := fields["language"]; language != nil {
		if language.ShortTag() != "!!str" {
			return Profile{}, errors.New("profile language must be a string")
		}
		profile.Language = strings.TrimSpace(language.Value)
		if length := utf8.RuneCountInString(profile.Language); length < 1 || length > 35 {
			return Profile{}, errors.New("profile language must contain between 1 and 35 characters")
		}
	}
	return profile, nil
}
