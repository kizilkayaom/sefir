package webapp

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kizilkayaom/sefir/mcp/internal/catalogue"
	"github.com/kizilkayaom/sefir/mcp/internal/email"
	"github.com/kizilkayaom/sefir/mcp/internal/workspace"
)

func TestPostgresWebFlow(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if config.ConnConfig.Database != "sefir_test" {
		t.Fatal("test must use sefir_test")
	}
	config.MaxConns = 1
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ctx = withAccount(ctx, Account{ID: workspace.LegacyAccountID})
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, table := range []string{"contacts", "drafts", "approvals", "outbox"} {
		if _, err := pool.Exec(ctx, "CREATE TEMP TABLE "+table+" (LIKE public."+table+" INCLUDING ALL)"); err != nil {
			t.Fatal(err)
		}
	}
	store := PostgresStore{Pool: pool}
	input := ContactInput{Name: "Test contact", Email: "before@example.com", Status: "confirmed"}
	contact, err := store.SaveContact(ctx, "", input)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := store.CreateDraft(ctx, catalogue.CreateDraftInput{ContactID: contact.ID, Subject: "Subject", Body: "Body"})
	if err != nil {
		t.Fatal(err)
	}
	input.Email = "after@example.com"
	if _, err := store.SaveContact(ctx, contact.ID, input); err != nil {
		t.Fatal(err)
	}
	drafts, err := store.Drafts(ctx, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || drafts[0].DraftID != draft.DraftID || *drafts[0].RecipientEmail != "before@example.com" {
		t.Fatal("saved draft changed")
	}
	contacts, err := store.Contacts(ctx, 2, 0)
	if err != nil || len(contacts) != 1 || *contacts[0].Email != "after@example.com" {
		t.Fatalf("contacts: %v %v", contacts, err)
	}
	if entry, err := store.DraftActivity(ctx, draft.DraftID, 1); err != nil || entry != nil {
		t.Fatalf("unexpected activity: %v %v", entry, err)
	}
	var approval string
	err = pool.QueryRow(ctx, `INSERT INTO approvals (draft_id, version, content_hash, approved_by) VALUES ($1, 1, $2, 'Owner') RETURNING approval_id::text`, draft.DraftID, draft.ContentHash).Scan(&approval)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO outbox (approval_id, state) VALUES ($1, 'needs_attention')`, approval); err != nil {
		t.Fatal(err)
	}
	activity, err := store.Activities(ctx, 2, 0)
	if err != nil || len(activity) != 1 || activity[0].State != "needs_attention" {
		t.Fatalf("activity: %v %v", activity, err)
	}
	entry, err := store.DraftActivity(ctx, draft.DraftID, 1)
	if err != nil || entry == nil || entry.DraftID != draft.DraftID {
		t.Fatalf("draft activity: %v %v", entry, err)
	}
	revised, err := store.SaveDraftVersion(ctx, draft.DraftID, 1, catalogue.EditDraftInput{
		Subject: "Revised", BodyHTML: "<p>Hello <strong>there</strong></p>", ContentHash: draft.ContentHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if revised.DraftID != draft.DraftID || revised.Version != 2 || revised.Body != "Hello there" ||
		revised.BodyHTML != "<p>Hello <strong>there</strong></p>" || revised.ContentHash == draft.ContentHash ||
		revised.RecipientEmail == nil || *revised.RecipientEmail != "before@example.com" {
		t.Fatalf("revision lost its saved content or recipient: %+v", revised)
	}
	original, err := store.Draft(ctx, draft.DraftID, 1)
	if err != nil || original.Body != "Body" || original.ContentHash != draft.ContentHash || original.BodyHTML != "" {
		t.Fatal("revision changed the approved version")
	}
	approved, err := email.GetApprovedDraft(ctx, pool, entry.OutboxID)
	if err != nil || approved.Body != "Body" || approved.Version != 1 {
		t.Fatal("revision changed the delivery's approved content")
	}
	if activity, err := store.DraftActivity(ctx, revised.DraftID, 2); err != nil || activity != nil {
		t.Fatal("new version reused an earlier approval")
	}
	for _, edit := range []struct {
		version int
		hash    string
	}{{1, draft.ContentHash}, {2, draft.ContentHash}} {
		_, err := store.SaveDraftVersion(ctx, draft.DraftID, edit.version, catalogue.EditDraftInput{
			Subject: "Overwrite", Body: "Should not be saved", ContentHash: edit.hash,
		})
		if !errors.Is(err, catalogue.ErrDraftConflict) {
			t.Fatalf("stale edit was not rejected: %v", err)
		}
	}
	recipient := "revised@example.com"
	retargeted, err := store.SaveDraftVersion(ctx, revised.DraftID, revised.Version, catalogue.EditDraftInput{
		RecipientEmail: &recipient, Subject: revised.Subject, Body: revised.Body,
		BodyHTML: revised.BodyHTML, ContentHash: revised.ContentHash,
	})
	if err != nil || retargeted.Version != 3 || retargeted.RecipientEmail == nil ||
		*retargeted.RecipientEmail != recipient || retargeted.ContentHash == revised.ContentHash {
		t.Fatalf("recipient revision: %+v %v", retargeted, err)
	}
	previous, err := store.Draft(ctx, revised.DraftID, 2)
	if err != nil || previous.RecipientEmail == nil || *previous.RecipientEmail != "before@example.com" {
		t.Fatal("recipient edit changed a previous version")
	}
	contactAfterEdit, err := store.ConfirmedContact(ctx, contact.ID)
	if err != nil || contactAfterEdit.Email == nil || *contactAfterEdit.Email != "after@example.com" {
		t.Fatal("recipient edit changed the contact")
	}
	approved, err = email.GetApprovedDraft(ctx, pool, entry.OutboxID)
	if err != nil || approved.RecipientEmail == nil || *approved.RecipientEmail != "before@example.com" {
		t.Fatal("recipient edit changed an approved delivery")
	}
	richDraft, err := store.CreateDraft(ctx, catalogue.CreateDraftInput{
		ContactID: contact.ID, Subject: "Manual", BodyHTML: "<p><em>Written by hand</em></p>",
	})
	if err != nil || richDraft.Body != "Written by hand" || richDraft.BodyHTML == "" {
		t.Fatalf("manual formatted draft: %+v %v", richDraft, err)
	}
	if richDraft.PresetID != nil || richDraft.PresetRevision != nil {
		t.Fatal("manual draft response included writing style metadata")
	}
	var storedPreset *string
	var storedRevision *int
	if err := pool.QueryRow(ctx, "SELECT preset_id, preset_revision FROM drafts WHERE draft_id=$1 AND version=$2",
		richDraft.DraftID, richDraft.Version).Scan(&storedPreset, &storedRevision); err != nil {
		t.Fatal(err)
	}
	if storedPreset != nil || storedRevision != nil {
		t.Fatal("manual draft received writing style metadata")
	}
	richApproval, err := email.ApproveDraft(ctx, pool, email.ApproveDraftInput{
		ApprovalID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		DraftID:    richDraft.DraftID, Version: 1, ContentHash: richDraft.ContentHash, ApprovedBy: "Owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	richApproved, err := email.GetApprovedDraft(ctx, pool, richApproval.OutboxID)
	if err != nil || richApproved.BodyHTML != richDraft.BodyHTML {
		t.Fatal("approved formatting was lost")
	}
	if _, err := pool.Exec(ctx, "UPDATE drafts SET body_html = '<p>Changed</p>' WHERE draft_id = $1", richDraft.DraftID); err != nil {
		t.Fatal(err)
	}
	if _, err := email.GetApprovedDraft(ctx, pool, richApproval.OutboxID); err == nil {
		t.Fatal("delivery accepted HTML changed after approval")
	}

	preset, revision := "formal", 2
	styledDraft, err := store.CreateDraft(ctx, catalogue.CreateDraftInput{
		ContactID: contact.ID, Subject: "Styled draft", Body: "Fictional test email",
		PresetID: &preset, PresetRevision: &revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if styledDraft.PresetID == nil || *styledDraft.PresetID != preset ||
		styledDraft.PresetRevision == nil || *styledDraft.PresetRevision != revision {
		t.Fatal("created draft response lost its writing style metadata")
	}
	if err := pool.QueryRow(ctx, "SELECT preset_id, preset_revision FROM drafts WHERE draft_id=$1 AND version=$2",
		styledDraft.DraftID, styledDraft.Version).Scan(&storedPreset, &storedRevision); err != nil {
		t.Fatal(err)
	}
	if storedPreset == nil || *storedPreset != preset || storedRevision == nil || *storedRevision != revision {
		t.Fatal("draft writing style metadata was not saved")
	}
	styledRevision, err := store.SaveDraftVersion(ctx, styledDraft.DraftID, styledDraft.Version, catalogue.EditDraftInput{
		Subject: "Edited styled draft", Body: "Revised fictional test email", ContentHash: styledDraft.ContentHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if styledRevision.DraftID != styledDraft.DraftID || styledRevision.Version != styledDraft.Version+1 {
		t.Fatal("editing did not create the expected draft version")
	}
	if styledRevision.PresetID == nil || *styledRevision.PresetID != preset ||
		styledRevision.PresetRevision == nil || *styledRevision.PresetRevision != revision {
		t.Fatal("edited draft response lost its writing style metadata")
	}
	for _, version := range []int{styledDraft.Version, styledRevision.Version} {
		if err := pool.QueryRow(ctx, "SELECT preset_id, preset_revision FROM drafts WHERE draft_id=$1 AND version=$2",
			styledDraft.DraftID, version).Scan(&storedPreset, &storedRevision); err != nil {
			t.Fatal(err)
		}
		if storedPreset == nil || *storedPreset != preset || storedRevision == nil || *storedRevision != revision {
			t.Fatalf("draft version %d lost its writing style metadata", version)
		}
		fetched, err := store.Draft(ctx, styledDraft.DraftID, version)
		if err != nil {
			t.Fatal(err)
		}
		if fetched.DraftID != styledDraft.DraftID || fetched.Version != version ||
			fetched.PresetID == nil || *fetched.PresetID != preset ||
			fetched.PresetRevision == nil || *fetched.PresetRevision != revision {
			t.Fatalf("fetched draft version %d lost its identity or writing style metadata", version)
		}
	}

	webDrafts, err := store.Drafts(ctx, 25, 0)
	if err != nil {
		t.Fatal(err)
	}
	catalogueDrafts, err := catalogue.ListDrafts(ctx, pool, 25, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range []struct {
		name   string
		drafts []catalogue.Draft
	}{
		{name: "web", drafts: webDrafts},
		{name: "catalogue", drafts: catalogueDrafts},
	} {
		found := map[int]bool{}
		for _, listed := range result.drafts {
			if listed.DraftID != styledDraft.DraftID ||
				(listed.Version != styledDraft.Version && listed.Version != styledRevision.Version) {
				continue
			}
			if listed.PresetID == nil || *listed.PresetID != preset ||
				listed.PresetRevision == nil || *listed.PresetRevision != revision {
				t.Fatalf("%s listing lost writing style metadata for version %d", result.name, listed.Version)
			}
			found[listed.Version] = true
		}
		for _, version := range []int{styledDraft.Version, styledRevision.Version} {
			if !found[version] {
				t.Fatalf("%s listing is missing styled draft version %d", result.name, version)
			}
		}
	}
}
