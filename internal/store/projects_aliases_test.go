package store

import (
	"errors"
	"testing"
)

func seedObservation(t *testing.T, s *Store, project, title string) {
	t.Helper()
	sessionID := "sess-" + project
	if err := s.CreateSession(sessionID, project, t.TempDir()); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := s.AddObservation(AddObservationParams{
		SessionID: sessionID,
		Type:      "discovery",
		Title:     title,
		Content:   "body",
		Project:   project,
	}); err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
}

// TestResolveProjectSlugPrefersRealProjectOverAlias pins the precedence: a name
// that is a project in its own right is never redirected, however many aliases
// point away from it.
func TestResolveProjectSlugPrefersRealProjectOverAlias(t *testing.T) {
	s := newTestStore(t)
	seedCards(t, s, "engram", "ai-engram")

	// An alias that shadows a real project must not win the lookup.
	if err := s.UpsertProjectAlias("ai-engram", "engram", "manual"); err != nil {
		t.Fatalf("UpsertProjectAlias: %v", err)
	}

	res, err := s.ResolveProjectSlug("ai-engram")
	if err != nil {
		t.Fatalf("ResolveProjectSlug: %v", err)
	}
	if res.Slug != "ai-engram" {
		t.Fatalf("slug %q, want ai-engram", res.Slug)
	}
	if res.Via != ProjectResolvedViaCard {
		t.Fatalf("via %q, want %q", res.Via, ProjectResolvedViaCard)
	}

	// With the card gone, the same alias is what answers.
	if _, err := s.DB().Exec(`UPDATE project_cards SET deleted_at = datetime('now') WHERE slug = 'ai-engram'`); err != nil {
		t.Fatalf("soft delete card: %v", err)
	}
	res, err = s.ResolveProjectSlug("ai-engram")
	if err != nil {
		t.Fatalf("ResolveProjectSlug: %v", err)
	}
	if res.Slug != "engram" || res.Via != ProjectResolvedViaAlias {
		t.Fatalf("resolution %+v, want engram via alias", res)
	}
	if res.AliasSource != "manual" {
		t.Fatalf("alias_source %q, want manual", res.AliasSource)
	}
}

// TestResolveProjectSlugFoldsUnderscore pins the last step of the ladder: a
// name spelled with the separator somebody's shell produced still finds the
// project, without anything being stored under that spelling.
func TestResolveProjectSlugFoldsUnderscore(t *testing.T) {
	s := newTestStore(t)
	seedCards(t, s, "nextcloud-00")

	res, err := s.ResolveProjectSlug("nextcloud_00")
	if err != nil {
		t.Fatalf("ResolveProjectSlug: %v", err)
	}
	if res.Slug != "nextcloud-00" {
		t.Fatalf("slug %q, want nextcloud-00", res.Slug)
	}
	if res.Via != ProjectResolvedViaFolded {
		t.Fatalf("via %q, want %q", res.Via, ProjectResolvedViaFolded)
	}
	if res.Input != "nextcloud_00" {
		t.Fatalf("input %q must be reported verbatim", res.Input)
	}

	// Nothing is created for the folded spelling.
	if _, err := s.GetProjectCard("nextcloud_00"); !errors.Is(err, ErrNoProjectCard) {
		t.Fatalf("folding must not write: %v", err)
	}

	unknown, err := s.ResolveProjectSlug("something-else")
	if err != nil {
		t.Fatalf("ResolveProjectSlug: %v", err)
	}
	if unknown.Via != ProjectResolvedViaUnresolved || unknown.Slug != "" {
		t.Fatalf("resolution %+v, want unresolved", unknown)
	}
}

func TestUpsertAliasRejectsSelfReference(t *testing.T) {
	s := newTestStore(t)
	seedCards(t, s, "engram")

	if err := s.UpsertProjectAlias("engram", "engram", "manual"); err == nil {
		t.Fatal("an alias that names its own target must be refused")
	}
	aliases, err := s.ListProjectAliases("engram")
	if err != nil {
		t.Fatalf("ListProjectAliases: %v", err)
	}
	if len(aliases) != 0 {
		t.Fatalf("aliases %+v, want none", aliases)
	}
}

// TestUpsertAliasRejectsAliasThatOwnsRows pins the rule that keeps an alias
// from hiding data: a name with memories of its own is a project to merge, not
// a label to redirect.
func TestUpsertAliasRejectsAliasThatOwnsRows(t *testing.T) {
	s := newTestStore(t)
	seedCards(t, s, "engram")
	seedObservation(t, s, "ai-engram", "something worth keeping")

	err := s.UpsertProjectAlias("ai-engram", "engram", "manual")
	if !errors.Is(err, ErrAliasOwnsRows) {
		t.Fatalf("want ErrAliasOwnsRows, got %v", err)
	}
	var owns *AliasOwnsRowsError
	if !errors.As(err, &owns) {
		t.Fatalf("want *AliasOwnsRowsError, got %T", err)
	}
	if owns.Rows != 1 {
		t.Fatalf("rows %d, want 1", owns.Rows)
	}
	if owns.Alias != "ai-engram" {
		t.Fatalf("alias %q", owns.Alias)
	}
}

func TestProjectAliasListAndDelete(t *testing.T) {
	s := newTestStore(t)
	seedCards(t, s, "engram")

	if err := s.UpsertProjectAlias("AI-Engram ", "engram", "normalizer"); err != nil {
		t.Fatalf("UpsertProjectAlias: %v", err)
	}
	aliases, err := s.ListProjectAliases("engram")
	if err != nil {
		t.Fatalf("ListProjectAliases: %v", err)
	}
	if len(aliases) != 1 || aliases[0].Alias != "ai-engram" || aliases[0].Source != "normalizer" {
		t.Fatalf("aliases %+v", aliases)
	}

	// Re-pointing an alias updates it instead of failing.
	seedCards(t, s, "engram-two")
	if err := s.UpsertProjectAlias("ai-engram", "engram-two", "manual"); err != nil {
		t.Fatalf("re-point alias: %v", err)
	}
	if aliases, err = s.ListProjectAliases("engram"); err != nil || len(aliases) != 0 {
		t.Fatalf("old target still lists the alias: %+v %v", aliases, err)
	}

	if err := s.DeleteProjectAlias("ai-engram"); err != nil {
		t.Fatalf("DeleteProjectAlias: %v", err)
	}
	res, err := s.ResolveProjectSlug("ai-engram")
	if err != nil {
		t.Fatalf("ResolveProjectSlug: %v", err)
	}
	if res.Via == ProjectResolvedViaAlias {
		t.Fatalf("a deleted alias must not resolve: %+v", res)
	}
}
