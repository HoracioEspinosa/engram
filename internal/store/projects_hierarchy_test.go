package store

import (
	"strings"
	"testing"
)

func strPtr(v string) *string { return &v }

// TestUpsertProjectCardAcceptsHierarchyFields pins that the descriptive
// hierarchy columns survive a round trip and that a later partial upsert never
// wipes the ones it does not name.
func TestUpsertProjectCardAcceptsHierarchyFields(t *testing.T) {
	s := newTestStore(t)

	card, created, err := s.UpsertProjectCard(UpsertProjectCardParams{
		Slug:        "nextcloud",
		Kind:        strPtr("umbrella"),
		Description: strPtr("every nextcloud instance"),
		Icon:        strPtr("cod-project"),
		Color:       strPtr("#1e88e5"),
		Tags:        strPtr(`["infra","nextcloud"]`),
	})
	if err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}
	if !created {
		t.Fatal("first upsert must create the card")
	}
	if card.Kind != "umbrella" {
		t.Fatalf("kind %q, want umbrella", card.Kind)
	}
	if card.Depth != 0 {
		t.Fatalf("depth %d, want 0", card.Depth)
	}
	if card.ParentSlug != nil {
		t.Fatalf("parent_slug %v, want nil", *card.ParentSlug)
	}
	if card.Description == nil || *card.Description != "every nextcloud instance" {
		t.Fatalf("description %v", card.Description)
	}
	if card.Icon == nil || *card.Icon != "cod-project" {
		t.Fatalf("icon %v", card.Icon)
	}
	if card.Color == nil || *card.Color != "#1e88e5" {
		t.Fatalf("color %v", card.Color)
	}
	if card.Tags == nil || *card.Tags != `["infra","nextcloud"]` {
		t.Fatalf("tags %v", card.Tags)
	}

	// A partial upsert leaves the fields it does not carry alone.
	again, created, err := s.UpsertProjectCard(UpsertProjectCardParams{
		Slug:        "nextcloud",
		DisplayName: strPtr("Nextcloud"),
	})
	if err != nil {
		t.Fatalf("second UpsertProjectCard: %v", err)
	}
	if created {
		t.Fatal("second upsert must update, not create")
	}
	if again.Kind != "umbrella" || again.Icon == nil || *again.Icon != "cod-project" {
		t.Fatalf("a partial upsert overwrote hierarchy fields: %+v", again)
	}

	// A colour token is accepted alongside the hex form.
	token, _, err := s.UpsertProjectCard(UpsertProjectCardParams{
		Slug:  "nextcloud",
		Color: strPtr("accent"),
	})
	if err != nil {
		t.Fatalf("colour token upsert: %v", err)
	}
	if token.Color == nil || *token.Color != "accent" {
		t.Fatalf("colour token %v", token.Color)
	}
}

// TestProjectCardRejectsInvalidColor pins the CHECK behind project_cards.color:
// a hex triple that is not hexadecimal and a token that is not lowercase are
// both refused, so the TUI never has to guess what a stored colour means.
func TestProjectCardRejectsInvalidColor(t *testing.T) {
	s := newTestStore(t)

	for _, colour := range []string{"#gggggg", "Rojo"} {
		if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{
			Slug:  "acme",
			Color: strPtr(colour),
		}); err == nil {
			t.Fatalf("colour %q must be rejected", colour)
		}
	}

	// The rejection leaves nothing half-written behind.
	if _, err := s.GetProjectCard("acme"); err == nil {
		t.Fatal("a rejected colour must not create the card")
	}
}

// TestStampGraphStalenessPersistsTheLocalColumns pins that the three staleness
// columns are written and read back, and that stamping them does not disturb
// the graph pointer the card already carries.
func TestStampGraphStalenessPersistsTheLocalColumns(t *testing.T) {
	s := newTestStore(t)
	if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{Slug: "acme"}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}
	commit := strings.Repeat("a", 40)
	if err := s.StampProjectGraph("acme", commit, "2026-09-14 10:00:00", nil); err != nil {
		t.Fatalf("StampProjectGraph: %v", err)
	}

	if err := s.StampGraphStaleness("acme", "docs_only", 0, "2026-09-14 11:00:00"); err != nil {
		t.Fatalf("StampGraphStaleness: %v", err)
	}
	card, err := s.GetProjectCard("acme")
	if err != nil {
		t.Fatalf("GetProjectCard: %v", err)
	}
	if card.GraphStaleReason == nil || *card.GraphStaleReason != "docs_only" {
		t.Fatalf("graph_stale_reason %v", card.GraphStaleReason)
	}
	if card.GraphChangedFiles == nil || *card.GraphChangedFiles != 0 {
		t.Fatalf("graph_changed_files %v", card.GraphChangedFiles)
	}
	if card.GraphCheckedAt == nil || *card.GraphCheckedAt != "2026-09-14 11:00:00" {
		t.Fatalf("graph_checked_at %v", card.GraphCheckedAt)
	}
	if card.GraphCommit == nil || *card.GraphCommit != commit {
		t.Fatalf("stamping staleness disturbed graph_commit: %v", card.GraphCommit)
	}
}
