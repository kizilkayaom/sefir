package catalogue

import (
	"context"
	"testing"
)

func TestCreateDraftRejectsInvalidPresetMetadata(t *testing.T) {
	preset, empty, whitespace := "formal", "", " \t\n"
	revision, zero, negative := 1, 0, -1
	tests := []struct {
		name     string
		preset   *string
		revision *int
		message  string
	}{
		{"missing revision", &preset, nil, "preset_id and preset_revision must be provided together"},
		{"missing preset", nil, &revision, "preset_id and preset_revision must be provided together"},
		{"empty preset", &empty, &revision, "preset_id must not be blank"},
		{"blank preset", &whitespace, &revision, "preset_id must not be blank"},
		{"zero revision", &preset, &zero, "preset_revision must be at least 1"},
		{"negative revision", &preset, &negative, "preset_revision must be at least 1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := CreateDraft(context.Background(), nil, CreateDraftInput{
				ContactID: "11111111-1111-4111-8111-111111111111", Subject: "Subject", Body: "Body",
				PresetID: test.preset, PresetRevision: test.revision,
			})
			if err == nil || err.Error() != test.message {
				t.Fatalf("expected %q before database access, got %v", test.message, err)
			}
		})
	}
}
