package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/kizilkayaom/sefir/mcp/internal/catalogue"
)

type polishingBridgeFunc func(context.Context, string, any) (json.RawMessage, error)

func (f polishingBridgeFunc) Call(ctx context.Context, action string, payload any) (json.RawMessage, error) {
	return f(ctx, action, payload)
}

func TestPolishDraftReturnsSuggestionWithoutSaving(t *testing.T) {
	for _, test := range []struct {
		name     string
		provider string
		preset   string
		want     string
	}{
		{name: "account defaults", want: "warm_prof"},
		{name: "cohere", provider: "cohere", preset: "prof", want: "prof"},
		{name: "anthropic", provider: "anthropic", preset: "technical", want: "technical"},
		{name: "openai", provider: "openai", preset: "formal", want: "formal"},
		{name: "openrouter", provider: "openrouter", preset: "warm_prof", want: "warm_prof"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, store, bridge, _ := fixture(t)
			cookie, csrf := login(t, server)
			accountID := server.sessions[cookie.Value].AccountID
			if _, err := server.accounts.SetDefaultPreset(context.Background(), accountID, "warm_prof"); err != nil {
				t.Fatal(err)
			}
			original := store.draft
			bridge.result = json.RawMessage(`{"subject":" Revised subject ","body":" Revised body ","body_html":"","preset_id":"` + test.want + `","preset_revision":2,"recipient_email":"other@example.com","private":"hidden"}`)
			body := `{"content_hash":"` + original.ContentHash + `","provider":"` + test.provider + `","preset_id":"` + test.preset + `"}`
			started := time.Now()
			response := request(server, "POST", "/api/drafts/"+testID+"/versions/1/polish", body, cookie, csrf)
			if response.Code != 200 {
				t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
			}
			var result map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{
				"draft_id": testID, "version": float64(1), "source_content_hash": original.ContentHash,
				"subject": "Revised subject", "body": "Revised body", "body_html": "",
				"preset_id": test.want, "preset_revision": float64(2),
			}
			if !reflect.DeepEqual(result, want) {
				t.Fatalf("unexpected suggestion: %#v", result)
			}
			if len(bridge.calls) != 1 || bridge.calls[0] != "polish" {
				t.Fatal("polishing was not called exactly once")
			}
			wantPayload := map[string]any{
				"draft":    map[string]string{"subject": original.Subject, "body": original.Body, "body_html": original.BodyHTML},
				"provider": test.provider, "preset_id": test.want,
			}
			if !reflect.DeepEqual(bridge.payloads[0], wantPayload) {
				t.Fatalf("unexpected bridge payload: %#v", bridge.payloads[0])
			}
			if store.draftLookupCalls != 1 || store.draftLookupID != testID || store.draftLookupVersion != 1 || store.draftLookupAccountID != accountID {
				t.Fatal("draft lookup did not use the requested version and current account")
			}
			if store.draftLookupDeadline.Before(started.Add(119*time.Second)) || store.draftLookupDeadline.After(time.Now().Add(120*time.Second)) {
				t.Fatal("draft lookup did not receive the operation deadline")
			}
			if !reflect.DeepEqual(store.draft, original) || store.draftInput != (catalogue.CreateDraftInput{}) || store.editID != "" {
				t.Fatal("polishing changed or saved the draft")
			}
			if store.writingStyleLookupCalls != 0 || len(server.generating) != 0 {
				t.Fatal("built-in polishing loaded a profile or retained the generation slot")
			}
		})
	}
}

func TestPolishDraftUsesCustomProfileSnapshot(t *testing.T) {
	server, store, _, _ := fixture(t)
	cookie, csrf := login(t, server)
	accountID := server.sessions[cookie.Value].AccountID
	const profileID = "22222222-2222-4222-8222-222222222222"
	store.writingStyle = WritingStyleProfile{
		ProfileID: profileID, Revision: 3, FormatVersion: 1,
		Name: "Direct", Language: "en", Content: "Use short sentences.",
	}
	original := store.draft
	var calls int
	var bridgeContext context.Context
	server.bridge = polishingBridgeFunc(func(ctx context.Context, action string, payload any) (json.RawMessage, error) {
		calls++
		bridgeContext = ctx
		if action != "polish" || contextAccount(ctx).ID != accountID || len(server.generating) != 1 {
			t.Fatal("polishing lost its action, account, or generation slot")
		}
		if deadline, ok := ctx.Deadline(); !ok || !deadline.Equal(store.draftLookupDeadline) {
			t.Fatal("bridge did not receive the draft lookup deadline")
		}
		want := map[string]any{
			"draft":    map[string]string{"subject": original.Subject, "body": original.Body, "body_html": original.BodyHTML},
			"provider": "anthropic",
			"custom_profile": map[string]any{
				"profile_id": profileID, "revision": 3, "name": "Direct", "language": "en", "content": "Use short sentences.",
			},
		}
		if !reflect.DeepEqual(payload, want) {
			t.Fatalf("unexpected custom profile payload: %#v", payload)
		}
		store.writingStyle.Revision = 4
		store.writingStyle.Content = "Updated guidance."
		return json.RawMessage(`{"subject":"Revised","body":"Revised body","body_html":"","preset_id":"custom:` + profileID + `","preset_revision":3}`), nil
	})
	body := `{"content_hash":"` + original.ContentHash + `","provider":"anthropic","profile_id":"` + profileID + `"}`
	response := request(server, "POST", "/api/drafts/"+testID+"/versions/1/polish", body, cookie, csrf)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"preset_revision":3`) {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
	if store.writingStyleLookupCalls != 1 || store.writingStyleLookupID != profileID || store.writingStyleLookupAccountID != accountID ||
		!store.writingStyleLookupDeadline.Equal(store.draftLookupDeadline) {
		t.Fatal("custom profile lookup lost its account or deadline")
	}
	if calls != 1 || bridgeContext.Err() != context.Canceled || len(server.generating) != 0 {
		t.Fatal("polishing did not finish and clean up the operation")
	}
	if !reflect.DeepEqual(store.draft, original) || store.draftInput != (catalogue.CreateDraftInput{}) || store.editID != "" {
		t.Fatal("custom profile polishing saved or changed the draft")
	}
}

func TestPolishDraftSanitizesFormattedSuggestion(t *testing.T) {
	server, store, bridge, _ := fixture(t)
	cookie, csrf := login(t, server)
	store.draft.BodyHTML = "<p>Hello <strong>Alex</strong>.</p>"
	original := store.draft
	bridge.result = json.RawMessage(`{"subject":" Revised ","body":"Conflicting alternate text","body_html":"<p>Hi <strong>Alex</strong>.<img src=\"https://example.com/pixel\"></p>","preset_id":"prof","preset_revision":1}`)
	body := `{"content_hash":"` + original.ContentHash + `"}`
	response := request(server, "POST", "/api/drafts/"+testID+"/versions/1/polish", body, cookie, csrf)
	if response.Code != 200 {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
	var result struct {
		Subject  string `json:"subject"`
		Body     string `json:"body"`
		BodyHTML string `json:"body_html"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Subject != "Revised" || result.Body != "Hi Alex." || result.BodyHTML != "<p>Hi <strong>Alex</strong>.</p>" {
		t.Fatalf("suggestion was not sanitized consistently: %+v", result)
	}
	if !reflect.DeepEqual(store.draft, original) || store.draftInput != (catalogue.CreateDraftInput{}) || store.editID != "" {
		t.Fatal("sanitizing a suggestion saved or changed the original draft")
	}
}

func TestPolishDraftRequiresSessionAndCSRF(t *testing.T) {
	server, store, bridge, _ := fixture(t)
	path := "/api/drafts/" + testID + "/versions/1/polish"
	body := `{"content_hash":"` + store.draft.ContentHash + `"}`
	if got := request(server, "POST", path, body, nil, "").Code; got != 401 {
		t.Fatal(got)
	}
	cookie, _ := login(t, server)
	if got := request(server, "POST", path, body, cookie, "wrong").Code; got != 403 {
		t.Fatal(got)
	}
	if store.draftLookupCalls != 0 || len(bridge.calls) != 0 || len(server.generating) != 0 {
		t.Fatal("unauthorized polishing accessed a draft or the model")
	}
}

func TestPolishDraftRejectsInvalidRequests(t *testing.T) {
	path := "/api/drafts/" + testID + "/versions/1/polish"
	hash := strings.Repeat("a", 64)
	for _, test := range []struct {
		name   string
		path   string
		body   string
		status int
	}{
		{name: "invalid draft", path: "/api/drafts/invalid/versions/1/polish", body: `{}`, status: 400},
		{name: "invalid version", path: "/api/drafts/" + testID + "/versions/0/polish", body: `{}`, status: 400},
		{name: "invalid JSON", path: path, body: `{`, status: 400},
		{name: "missing hash", path: path, body: `{}`, status: 422},
		{name: "blank hash", path: path, body: `{"content_hash":" "}`, status: 422},
		{name: "unknown provider", path: path, body: `{"content_hash":"` + hash + `","provider":"unknown"}`, status: 422},
		{name: "unknown preset", path: path, body: `{"content_hash":"` + hash + `","preset_id":"unknown"}`, status: 422},
		{name: "conflicting styles", path: path, body: `{"content_hash":"` + hash + `","preset_id":"formal","profile_id":"` + testID + `"}`, status: 422},
		{name: "invalid profile", path: path, body: `{"content_hash":"` + hash + `","profile_id":"invalid"}`, status: 422},
		{name: "client draft", path: path, body: `{"content_hash":"` + hash + `","draft":{"subject":"Replacement"}}`, status: 400},
		{name: "client profile", path: path, body: `{"content_hash":"` + hash + `","custom_profile":{}}`, status: 400},
		{name: "client credentials", path: path, body: `{"content_hash":"` + hash + `","api_key":"private-test-key"}`, status: 400},
		{name: "recipient", path: path, body: `{"content_hash":"` + hash + `","recipient_email":"other@example.com"}`, status: 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, store, bridge, _ := fixture(t)
			cookie, csrf := login(t, server)
			response := request(server, "POST", test.path, test.body, cookie, csrf)
			if response.Code != test.status {
				t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
			}
			if store.draftLookupCalls != 0 || store.writingStyleLookupCalls != 0 || len(bridge.calls) != 0 || len(server.generating) != 0 {
				t.Fatal("invalid request reached draft lookup or polishing")
			}
		})
	}
}

func TestPolishDraftHandlesFailuresWithoutSaving(t *testing.T) {
	for _, test := range []struct {
		name         string
		draftErr     error
		profileErr   error
		bridgeErr    error
		bridgeResult string
		conflict     bool
		busy         bool
		status       int
		bridgeCalls  int
	}{
		{name: "missing or foreign draft", draftErr: pgx.ErrNoRows, status: 404},
		{name: "draft database failure", draftErr: errors.New("private database details"), status: 503},
		{name: "changed draft", conflict: true, status: 409},
		{name: "busy", busy: true, status: 429},
		{name: "missing or foreign profile", profileErr: pgx.ErrNoRows, status: 404},
		{name: "profile database failure", profileErr: errors.New("private database details"), status: 503},
		{name: "bridge failure", bridgeErr: errors.New("private provider details"), status: 502, bridgeCalls: 1},
		{name: "bridge timeout", bridgeErr: context.DeadlineExceeded, status: 502, bridgeCalls: 1},
		{name: "provider unavailable", bridgeResult: `{"error":"provider_unavailable"}`, status: 422, bridgeCalls: 1},
		{name: "unknown provider error", bridgeResult: `{"error":"private provider details"}`, status: 502, bridgeCalls: 1},
		{name: "invalid JSON", bridgeResult: `{`, status: 502, bridgeCalls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, store, bridge, _ := fixture(t)
			cookie, csrf := login(t, server)
			original := store.draft
			store.err, store.writingStyleLookupErr, bridge.err = test.draftErr, test.profileErr, test.bridgeErr
			bridge.result = json.RawMessage(test.bridgeResult)
			hash := original.ContentHash
			if test.conflict {
				hash = strings.Repeat("b", 64)
			}
			body := `{"content_hash":"` + hash + `"}`
			if test.profileErr != nil {
				body = `{"content_hash":"` + hash + `","profile_id":"` + testID + `"}`
			}
			wantSlots := 0
			if test.busy {
				server.generating <- struct{}{}
				wantSlots = 1
			}
			response := request(server, "POST", "/api/drafts/"+testID+"/versions/1/polish", body, cookie, csrf)
			if response.Code != test.status || strings.Contains(response.Body.String(), "private") {
				t.Fatalf("unexpected failure response: %d %s", response.Code, response.Body.String())
			}
			if len(bridge.calls) != test.bridgeCalls || len(server.generating) != wantSlots {
				t.Fatal("failed polishing made unexpected model calls or retained its slot")
			}
			if test.profileErr != nil && (store.writingStyleLookupCalls != 1 || store.writingStyleLookupAccountID != server.sessions[cookie.Value].AccountID) {
				t.Fatal("profile lookup did not use the current account")
			}
			if !reflect.DeepEqual(store.draft, original) || store.draftInput != (catalogue.CreateDraftInput{}) || store.editID != "" {
				t.Fatal("failed polishing saved or changed the draft")
			}
		})
	}
}

func TestPolishDraftRejectsInvalidSuggestions(t *testing.T) {
	const profileID = "22222222-2222-4222-8222-222222222222"
	for _, test := range []struct {
		name     string
		changes  map[string]any
		remove   string
		original string
		custom   bool
	}{
		{name: "blank subject", changes: map[string]any{"subject": " "}},
		{name: "subject line break", changes: map[string]any{"subject": "Hello\r\nBcc: other@example.com"}},
		{name: "long subject", changes: map[string]any{"subject": strings.Repeat("a", 999)}},
		{name: "blank body", changes: map[string]any{"body": " "}},
		{name: "long body", changes: map[string]any{"body": strings.Repeat("a", 100001)}},
		{name: "missing HTML field", remove: "body_html"},
		{name: "null HTML", changes: map[string]any{"body_html": nil}},
		{name: "wrong HTML type", changes: map[string]any{"body_html": 1}},
		{name: "plain to HTML", changes: map[string]any{"body_html": "<p>Revised</p>"}},
		{name: "HTML to plain", original: "<p>Original</p>"},
		{name: "empty formatted body", original: "<p>Original</p>", changes: map[string]any{"body_html": "<p><br></p>"}},
		{name: "format removed by sanitizer", original: "<p>Original</p>", changes: map[string]any{"body_html": "<img src=\"https://example.com/pixel\">"}},
		{name: "long HTML", original: "<p>Original</p>", changes: map[string]any{"body_html": "<p>" + strings.Repeat("a", 200000) + "</p>"}},
		{name: "missing preset", remove: "preset_id"},
		{name: "wrong preset", changes: map[string]any{"preset_id": "formal"}},
		{name: "missing revision", remove: "preset_revision"},
		{name: "zero revision", changes: map[string]any{"preset_revision": 0}},
		{name: "large revision", changes: map[string]any{"preset_revision": int64(2147483648)}},
		{name: "fractional revision", changes: map[string]any{"preset_revision": 1.5}},
		{name: "wrong custom profile", custom: true, changes: map[string]any{"preset_id": "custom:" + testID}},
		{name: "older custom revision", custom: true, changes: map[string]any{"preset_revision": 2}},
		{name: "newer custom revision", custom: true, changes: map[string]any{"preset_revision": 4}},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, store, bridge, _ := fixture(t)
			cookie, csrf := login(t, server)
			store.draft.BodyHTML = test.original
			original := store.draft
			body := `{"content_hash":"` + original.ContentHash + `"}`
			result := map[string]any{
				"subject": "Revised", "body": "Revised body", "body_html": "", "preset_id": "prof", "preset_revision": 1,
			}
			if test.custom {
				store.writingStyle = WritingStyleProfile{
					ProfileID: profileID, Revision: 3, FormatVersion: 1,
					Name: "Direct", Language: "en", Content: "Use short sentences.",
				}
				body = `{"content_hash":"` + original.ContentHash + `","profile_id":"` + profileID + `"}`
				result["preset_id"], result["preset_revision"] = "custom:"+profileID, 3
			}
			for key, value := range test.changes {
				result[key] = value
			}
			delete(result, test.remove)
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			bridge.result = encoded
			response := request(server, "POST", "/api/drafts/"+testID+"/versions/1/polish", body, cookie, csrf)
			if response.Code != 502 || !strings.Contains(response.Body.String(), "The model returned an invalid polishing suggestion") {
				t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
			}
			if len(bridge.calls) != 1 || bridge.calls[0] != "polish" || len(server.generating) != 0 {
				t.Fatal("invalid suggestion was retried or retained the generation slot")
			}
			if !reflect.DeepEqual(store.draft, original) || store.draftInput != (catalogue.CreateDraftInput{}) || store.editID != "" {
				t.Fatal("invalid suggestion saved or changed the draft")
			}
		})
	}
}
