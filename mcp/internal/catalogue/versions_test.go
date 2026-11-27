package catalogue

import (
	"context"
	"errors"
	"testing"
)

func TestSaveDraftVersionRejectsInvalidStyleBeforeDatabaseAccess(t *testing.T) {
	preset, empty, blank := "formal", "", " \t\n"
	revision, zero, negative, tooLarge := 1, 0, -1, 2147483648
	for _, test := range []struct {
		name     string
		preset   *string
		revision *int
	}{
		{name: "missing revision", preset: &preset},
		{name: "missing preset", revision: &revision},
		{name: "empty preset", preset: &empty, revision: &revision},
		{name: "blank preset", preset: &blank, revision: &revision},
		{name: "zero revision", preset: &preset, revision: &zero},
		{name: "negative revision", preset: &preset, revision: &negative},
		{name: "large revision", preset: &preset, revision: &tooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := SaveDraftVersion(context.Background(), nil, "11111111-1111-4111-8111-111111111111", 1, EditDraftInput{
				Subject: "Subject", Body: "Body", ContentHash: "original-hash",
				PresetID: test.preset, PresetRevision: test.revision,
			})
			if !errors.Is(err, ErrInvalidDraftStyle) {
				t.Fatalf("expected invalid style before database access, got %v", err)
			}
		})
	}
}
