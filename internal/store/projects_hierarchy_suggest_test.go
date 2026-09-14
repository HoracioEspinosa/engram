package store

import (
	"errors"
	"testing"
)

// seedObservationProject writes one observation under project, which is how a
// project exists without anybody ever having made it a card — the shape most of
// a real database is in.
func seedObservationProject(t *testing.T, s *Store, project string) {
	t.Helper()
	sessionID := "sess-" + project
	if err := s.CreateSession(sessionID, project, ""); err != nil {
		t.Fatalf("CreateSession(%s): %v", sessionID, err)
	}
	if _, err := s.AddObservation(AddObservationParams{
		SessionID: sessionID, Type: "manual", Title: project, Content: project, Project: project,
	}); err != nil {
		t.Fatalf("AddObservation(%s): %v", project, err)
	}
}

func findSuggestion(suggestions []ProjectTreeSuggestion, parent string) *ProjectTreeSuggestion {
	for i := range suggestions {
		if suggestions[i].Parent == parent {
			return &suggestions[i]
		}
	}
	return nil
}

// TestSuggestProjectTreePrefersExistingPrefixProject pins the parent a family
// is offered. Inventing "koi-garden-pond" next to a koi-garden that is already
// there adds a project nobody asked for and buries the real one a level deeper;
// the longest name the store already answers to is the honest parent.
func TestSuggestProjectTreePrefersExistingPrefixProject(t *testing.T) {
	s := newTestStore(t)
	seedCards(t, s, "koi-garden", "koi-garden-pond-01", "koi-garden-pond-02")

	suggestions, err := s.SuggestProjectTree()
	if err != nil {
		t.Fatalf("SuggestProjectTree: %v", err)
	}

	found := findSuggestion(suggestions, "koi-garden")
	if found == nil {
		t.Fatalf("no suggestion for koi-garden: %+v", suggestions)
	}
	if !found.ParentExists {
		t.Error("koi-garden is a card, so the parent exists")
	}
	if len(found.Children) != 2 || found.Children[0] != "koi-garden-pond-01" || found.Children[1] != "koi-garden-pond-02" {
		t.Fatalf("children %v, want the two ponds", found.Children)
	}
	if found.Reason != SuggestReasonExistingPrefix {
		t.Fatalf("reason %q, want %q", found.Reason, SuggestReasonExistingPrefix)
	}
	if invented := findSuggestion(suggestions, "koi-garden-pond"); invented != nil {
		t.Fatalf("a parent was invented next to an existing one: %+v", invented)
	}

	// A family whose prefix nothing answers to still gets the synthesized stem.
	seedCards(t, s, "nextcloud_00", "nextcloud_01")
	suggestions, err = s.SuggestProjectTree()
	if err != nil {
		t.Fatalf("SuggestProjectTree: %v", err)
	}
	stem := findSuggestion(suggestions, "nextcloud")
	if stem == nil || stem.ParentExists {
		t.Fatalf("nextcloud suggestion = %+v, want a parent that does not exist yet", stem)
	}

	// Suggesting never writes.
	if _, err := s.GetProjectCard("koi-garden-pond"); !errors.Is(err, ErrNoProjectCard) {
		t.Fatalf("a suggestion must not create the parent card: %v", err)
	}
	card, err := s.GetProjectCard("koi-garden-pond-01")
	if err != nil {
		t.Fatalf("GetProjectCard: %v", err)
	}
	if card.ParentSlug != nil {
		t.Fatalf("a suggestion reparented %s", card.Slug)
	}
}

// TestSuggestProjectTreeSeesObservationOnlyProjects pins that the suggestion
// works on the database people actually have. Cards are rare; a project is
// usually nothing more than the name its observations were saved under, and a
// grouping that only reads project_cards sees almost none of them.
func TestSuggestProjectTreeSeesObservationOnlyProjects(t *testing.T) {
	s := newTestStore(t)
	// Only the umbrella has a card; the four members are names observations
	// were filed under.
	seedObservationProject(t, s, "clarodrive")
	for _, slug := range []string{"clarodrive-patches", "clarodrive-portal", "clarodrive-middleware"} {
		seedObservationProject(t, s, slug)
	}

	suggestions, err := s.SuggestProjectTree()
	if err != nil {
		t.Fatalf("SuggestProjectTree: %v", err)
	}
	found := findSuggestion(suggestions, "clarodrive")
	if found == nil {
		t.Fatalf("no suggestion for clarodrive: %+v", suggestions)
	}
	if len(found.Children) != 3 {
		t.Fatalf("children %v, want the three members", found.Children)
	}
	if found.ParentExists {
		t.Error("clarodrive has observations but no card, so the apply still has to create one")
	}
}

// TestSuggestProjectTreeReportsSeparatorPairs pins the other half of the
// reorganisation: two names that differ only in the character somebody typed
// between the words are one project written twice, and the tree cannot fix that
// — a merge can.
func TestSuggestProjectTreeReportsSeparatorPairs(t *testing.T) {
	s := newTestStore(t)
	seedCards(t, s, "ai-engram", "koi-garden")
	seedObservationProject(t, s, "ai_engram")
	seedObservationProject(t, s, "ai.engram")

	pairs, err := s.SuggestProjectSeparatorPairs()
	if err != nil {
		t.Fatalf("SuggestProjectSeparatorPairs: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("pairs = %+v, want the one family spelled three ways", pairs)
	}
	if pairs[0].Folded != "ai-engram" {
		t.Fatalf("folded = %q, want ai-engram", pairs[0].Folded)
	}
	want := []string{"ai-engram", "ai.engram", "ai_engram"}
	if len(pairs[0].Names) != len(want) {
		t.Fatalf("names = %v, want %v", pairs[0].Names, want)
	}
	for i, name := range want {
		if pairs[0].Names[i] != name {
			t.Fatalf("names = %v, want %v", pairs[0].Names, want)
		}
	}

	// A store where every project is spelled one way reports nothing.
	clean := newTestStore(t)
	seedCards(t, clean, "koi-garden", "tsukimi-bridge")
	none, err := clean.SuggestProjectSeparatorPairs()
	if err != nil {
		t.Fatalf("SuggestProjectSeparatorPairs: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("pairs = %+v, want none", none)
	}
}

// TestSuggestProjectTreeGroupsAFamilyWithNoParentYet pins the third and
// weakest grouping: several names that share their first segment and nothing
// else. It is the shape the real store is in — four clarodrive-* projects and
// no clarodrive — and it is also the shape a coincidence takes, so an invented
// segment has to hold three members before it is offered.
func TestSuggestProjectTreeGroupsAFamilyWithNoParentYet(t *testing.T) {
	s := newTestStore(t)
	for _, slug := range []string{
		"clarodrive-dockerized", "clarodrive-patches", "clarodrive-cleanup-accounts",
		"web-angular-skeleton", "web-metronic-angular",
	} {
		seedObservationProject(t, s, slug)
	}

	suggestions, err := s.SuggestProjectTree()
	if err != nil {
		t.Fatalf("SuggestProjectTree: %v", err)
	}

	found := findSuggestion(suggestions, "clarodrive")
	if found == nil {
		t.Fatalf("no suggestion for clarodrive: %+v", suggestions)
	}
	if len(found.Children) != 3 {
		t.Fatalf("children %v, want the three clarodrive projects", found.Children)
	}
	if found.ParentExists {
		t.Error("no project answers to clarodrive yet")
	}
	if found.Reason != SuggestReasonSharedFirstSegment {
		t.Fatalf("reason %q, want %q", found.Reason, SuggestReasonSharedFirstSegment)
	}

	// Two names that merely start with the same word are not a family.
	if weak := findSuggestion(suggestions, "web"); weak != nil {
		t.Fatalf("a two-member segment was proposed: %+v", weak)
	}
}

// TestSuggestProjectTreePrefersTheLongestExistingPrefix pins which parent a
// child is offered when two known projects are both prefixes of it: the nearer
// one, so a family already broken out does not get flattened back.
func TestSuggestProjectTreePrefersTheLongestExistingPrefix(t *testing.T) {
	s := newTestStore(t)
	seedCards(t, s, "koi", "koi-garden", "koi-garden-pond-01", "koi-garden-pond-02")

	suggestions, err := s.SuggestProjectTree()
	if err != nil {
		t.Fatalf("SuggestProjectTree: %v", err)
	}
	under := findSuggestion(suggestions, "koi-garden")
	if under == nil || len(under.Children) != 2 {
		t.Fatalf("koi-garden suggestion = %+v, want the two ponds", under)
	}
	// koi-garden is the only child koi has, so koi is not proposed at all.
	if shallow := findSuggestion(suggestions, "koi"); shallow != nil {
		t.Fatalf("koi was proposed with a single child: %+v", shallow)
	}
}
