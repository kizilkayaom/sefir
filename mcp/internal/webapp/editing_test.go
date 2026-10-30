package webapp

import (
	"encoding/json"
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
