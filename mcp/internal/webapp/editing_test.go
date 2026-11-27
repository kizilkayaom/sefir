package webapp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kizilkayaom/sefir/mcp/internal/catalogue"
)

func TestComposeAndEditDraft(t *testing.T) {
	server, store, bridge, _ := fixture(t)
	input := catalogue.CreateDraftInput{
		ContactID: testID, Subject: " Revised ", Body: "ignored alternate",
		BodyHTML: `<p>Hello <strong>there</strong><img src="https://example.com/pixel"></p>`,
	}
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if got := request(server, "POST", "/api/drafts/manual", string(body), nil, "").Code; got != 401 {
		t.Fatal(got)
	}
	cookie, csrf := login(t, server)
	if got := request(server, "POST", "/api/drafts/manual", string(body), cookie, "").Code; got != 403 {
		t.Fatal(got)
	}
	response := request(server, "POST", "/api/drafts/manual", string(body), cookie, csrf)
	if response.Code != 201 || store.draftInput.Body != "Hello there" || strings.Contains(store.draftInput.BodyHTML, "<img") {
		t.Fatalf("compose: %d %s %+v", response.Code, response.Body.String(), store.draftInput)
	}
	if len(bridge.calls) != 0 {
		t.Fatal("manual writing invoked generation or delivery")
	}
	path := "/api/drafts/" + testID + "/versions/1/edit"
	recipient := " revised@example.com "
	edit := catalogue.EditDraftInput{RecipientEmail: &recipient, Subject: "New subject", BodyHTML: "<p><em>New body</em></p>", ContentHash: store.draft.ContentHash}
	body, err = json.Marshal(edit)
	if err != nil {
		t.Fatal(err)
	}
	response = request(server, "POST", path, string(body), cookie, csrf)
	if response.Code != 201 || store.editID != testID || store.editVersion != 1 ||
		store.editInput.ContentHash != edit.ContentHash || store.editInput.Body != "New body" ||
		store.editInput.RecipientEmail == nil || *store.editInput.RecipientEmail != "revised@example.com" {
		t.Fatalf("edit: %d %s %+v", response.Code, response.Body.String(), store.editInput)
	}
	store.err = catalogue.ErrDraftConflict
	if got := request(server, "POST", path, string(body), cookie, csrf).Code; got != 409 {
		t.Fatal(got)
	}
	for _, invalid := range []string{
		`{"subject":"Subject","body_html":"<p><br></p>","content_hash":"abc"}`,
		`{"subject":"Subject\r\nBcc: other@example.com","body":"Body","content_hash":"abc"}`,
		`{"subject":"Subject","body":"Body"}`,
		`{"subject":"Subject","body":"Body","content_hash":"abc","recipient_email":""}`,
		`{"subject":"Subject","body":"Body","content_hash":"abc","recipient_email":"Name <other@example.com>"}`,
		`{"subject":"Subject","body":"Body","content_hash":"abc","recipient_email":"one@example.com, two@example.com"}`,
		`{"subject":"Subject","body":"Body","content_hash":"abc","recipient_email":"one@example.com\r\nBcc: two@example.com"}`,
	} {
		response = request(server, "POST", path, invalid, cookie, csrf)
		if response.Code != 400 && response.Code != 422 {
			t.Fatalf("invalid edit: %d %s", response.Code, response.Body.String())
		}
	}
	if len(bridge.calls) != 0 {
		t.Fatal("editing invoked delivery")
	}
}

func TestEditDraftPassesOptionalWritingStyle(t *testing.T) {
	for _, test := range []struct {
		name   string
		extra  string
		preset string
	}{
		{name: "omitted"},
		{name: "built-in", extra: `,"preset_id":"technical","preset_revision":3`, preset: "technical"},
		{name: "custom", extra: `,"preset_id":"custom:` + testID + `","preset_revision":3`, preset: "custom:" + testID},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, store, bridge, _ := fixture(t)
			cookie, csrf := login(t, server)
			body := `{"subject":"Polished","body":"Polished body","content_hash":"` + store.draft.ContentHash + `"` + test.extra + `}`
			response := request(server, "POST", "/api/drafts/"+testID+"/versions/1/edit", body, cookie, csrf)
			if response.Code != 201 {
				t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
			}
			if test.preset == "" {
				if store.editInput.PresetID != nil || store.editInput.PresetRevision != nil {
					t.Fatal("ordinary edit supplied writing style metadata")
				}
			} else if store.editInput.PresetID == nil || *store.editInput.PresetID != test.preset ||
				store.editInput.PresetRevision == nil || *store.editInput.PresetRevision != 3 {
				t.Fatal("editing did not forward the selected writing style")
			}
			if store.editInput.RecipientEmail != nil || store.editInput.ContentHash != store.draft.ContentHash || len(bridge.calls) != 0 {
				t.Fatal("saving a suggestion changed the recipient, lost the source hash, or invoked the agent")
			}
		})
	}
}

func TestEditDraftReturnsInvalidWritingStyleError(t *testing.T) {
	server, store, bridge, _ := fixture(t)
	cookie, csrf := login(t, server)
	store.err = errors.Join(catalogue.ErrInvalidDraftStyle, errors.New("private storage details"))
	body := `{"subject":"Polished","body":"Polished body","content_hash":"` + store.draft.ContentHash + `","preset_id":"formal"}`
	response := request(server, "POST", "/api/drafts/"+testID+"/versions/1/edit", body, cookie, csrf)
	var result struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != 422 || result.Error != catalogue.ErrInvalidDraftStyle.Error() {
		t.Fatalf("unexpected style error: %d %s", response.Code, response.Body.String())
	}
	if len(bridge.calls) != 0 {
		t.Fatal("invalid style invoked the agent")
	}
}
