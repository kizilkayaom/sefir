package writingstyle

import (
	"strings"
	"testing"
)

func TestParseProfile(t *testing.T) {
	body := "\n## Style\nUse everyday vocabulary.  \n\n---\n\n    Keep example indentation.\n"
	input := "---\nformat_version: 1\nname: Warm and direct\nlanguage: en\n---\n" + body
	want := Profile{FormatVersion: 1, Name: "Warm and direct", Language: "en", Content: body}
	for _, test := range []struct {
		name     string
		filename string
		input    string
		want     Profile
	}{
		{name: "Markdown body preserved", filename: "profile.md", input: input, want: want},
		{name: "uppercase extension", filename: "profile.MD", input: input, want: want},
		{name: "Windows line endings", filename: "profile.md", input: strings.ReplaceAll(input, "\n", "\r\n"), want: want},
		{name: "UTF-8 BOM", filename: "profile.md", input: "\ufeff" + input, want: want},
		{name: "language defaults to English", filename: "profile.md", input: strings.Replace(input, "language: en\n", "", 1), want: want},
		{
			name: "metadata whitespace trimmed", filename: "profile.md",
			input: "---\nformat_version: 1\nname: '  Sıcak ve doğrudan  '\nlanguage: ' tr '\n---\n" + body,
			want:  Profile{FormatVersion: 1, Name: "Sıcak ve doğrudan", Language: "tr", Content: body},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseProfile(test.filename, []byte(test.input))
			if err != nil {
				t.Fatalf("parse profile: %v", err)
			}
			if got != test.want {
				t.Fatalf("profile = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestParseProfileAcceptsSizeBoundaries(t *testing.T) {
	prefix := "---\nformat_version: 1\nname: Warm\n# "
	suffix := "\n---\nUse short sentences."
	for _, test := range []struct {
		name  string
		input string
		want  Profile
	}{
		{
			name:  "32 KiB file",
			input: prefix + strings.Repeat("x", 32*1024-len(prefix)-len(suffix)) + suffix,
			want:  Profile{FormatVersion: 1, Name: "Warm", Language: "en", Content: "Use short sentences."},
		},
		{
			name:  "16 KiB body",
			input: "---\nformat_version: 1\nname: Warm\n---\n" + strings.Repeat("é", 8*1024),
			want:  Profile{FormatVersion: 1, Name: "Warm", Language: "en", Content: strings.Repeat("é", 8*1024)},
		},
		{
			name:  "80 character Unicode name",
			input: "---\nformat_version: 1\nname: " + strings.Repeat("é", 80) + "\n---\nUse short sentences.",
			want:  Profile{FormatVersion: 1, Name: strings.Repeat("é", 80), Language: "en", Content: "Use short sentences."},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseProfile("profile.md", []byte(test.input))
			if err != nil {
				t.Fatalf("parse profile at size limit: %v", err)
			}
			if got != test.want {
				t.Fatal("parsed profile differs from expected content")
			}
		})
	}
}

func TestParseProfileRejectsInvalidUploads(t *testing.T) {
	withMetadata := func(metadata string) string {
		return "---\n" + metadata + "\n---\nUse short sentences."
	}
	valid := withMetadata("format_version: 1\nname: Warm")
	for _, test := range []struct {
		name     string
		filename string
		input    string
		want     string
	}{
		{name: "wrong extension", filename: "profile.txt", input: valid, want: ".md"},
		{name: "empty file", filename: "profile.md", want: "must not be empty"},
		{name: "oversize file", filename: "profile.md", input: strings.Repeat("x", 32*1024+1), want: "32 KiB"},
		{name: "invalid UTF-8", filename: "profile.md", input: valid + "\xff", want: "UTF-8"},
		{name: "NUL in body", filename: "profile.md", input: valid + "\x00", want: "NUL"},
		{name: "missing opening delimiter", filename: "profile.md", input: "Use short sentences.", want: "start with"},
		{name: "missing closing delimiter", filename: "profile.md", input: "---\nformat_version: 1\nname: Warm", want: "end with"},
		{name: "empty body", filename: "profile.md", input: "---\nformat_version: 1\nname: Warm\n---", want: "style guidance"},
		{name: "blank body", filename: "profile.md", input: "---\nformat_version: 1\nname: Warm\n---\n \t\n", want: "style guidance"},
		{name: "oversize body", filename: "profile.md", input: valid + strings.Repeat("x", 16*1024), want: "16 KiB"},
		{name: "multibyte oversize body", filename: "profile.md", input: valid + strings.Repeat("é", 8*1024), want: "16 KiB"},
		{name: "malformed YAML", filename: "profile.md", input: withMetadata("name: ["), want: "invalid profile YAML"},
		{name: "empty metadata", filename: "profile.md", input: withMetadata(""), want: "frontmatter"},
		{name: "sequence metadata", filename: "profile.md", input: withMetadata("- Warm"), want: "mapping"},
		{name: "scalar metadata", filename: "profile.md", input: withMetadata("Warm"), want: "mapping"},
		{name: "unknown field", filename: "profile.md", input: withMetadata("format_version: 1\nname: Warm\naccount_id: another-account"), want: "unknown profile metadata"},
		{name: "body supplied as metadata", filename: "profile.md", input: withMetadata("format_version: 1\nname: Warm\ncontent: replacement"), want: "unknown profile metadata"},
		{name: "duplicate name", filename: "profile.md", input: withMetadata("format_version: 1\nname: Warm\nname: Other"), want: "duplicate"},
		{name: "missing version", filename: "profile.md", input: withMetadata("name: Warm"), want: "format_version"},
		{name: "unsupported version", filename: "profile.md", input: withMetadata("format_version: 2\nname: Warm"), want: "format_version"},
		{name: "quoted version", filename: "profile.md", input: withMetadata("format_version: '1'\nname: Warm"), want: "integer 1"},
		{name: "float version", filename: "profile.md", input: withMetadata("format_version: 1.0\nname: Warm"), want: "integer 1"},
		{name: "null version", filename: "profile.md", input: withMetadata("format_version: null\nname: Warm"), want: "integer 1"},
		{name: "missing name", filename: "profile.md", input: withMetadata("format_version: 1"), want: "name is required"},
		{name: "numeric name", filename: "profile.md", input: withMetadata("format_version: 1\nname: 42"), want: "must be a string"},
		{name: "blank name", filename: "profile.md", input: withMetadata("format_version: 1\nname: '  '"), want: "1 and 80"},
		{name: "long name", filename: "profile.md", input: withMetadata("format_version: 1\nname: " + strings.Repeat("é", 81)), want: "1 and 80"},
		{name: "escaped NUL in name", filename: "profile.md", input: withMetadata("format_version: 1\nname: \"Warm\\0\""), want: "NUL"},
		{name: "blank language", filename: "profile.md", input: withMetadata("format_version: 1\nname: Warm\nlanguage: ''"), want: "1 and 35"},
		{name: "long language", filename: "profile.md", input: withMetadata("format_version: 1\nname: Warm\nlanguage: " + strings.Repeat("a", 36)), want: "1 and 35"},
		{name: "null language", filename: "profile.md", input: withMetadata("format_version: 1\nname: Warm\nlanguage: null"), want: "language must be a string"},
		{name: "nested value", filename: "profile.md", input: withMetadata("format_version: 1\nname: [Warm]"), want: "scalar value"},
		{name: "custom tag", filename: "profile.md", input: withMetadata("format_version: 1\nname: !custom Warm"), want: "must be a string"},
		{name: "anchor", filename: "profile.md", input: withMetadata("format_version: 1\nname: &style Warm\nlanguage: *style"), want: "anchors or aliases"},
		{name: "non-string key", filename: "profile.md", input: withMetadata("format_version: 1\nname: Warm\n42: value"), want: "keys must be plain strings"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseProfile(test.filename, []byte(test.input))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want an error containing %q", err, test.want)
			}
			if got != (Profile{}) {
				t.Fatalf("invalid upload returned a partial profile: %+v", got)
			}
		})
	}
}
