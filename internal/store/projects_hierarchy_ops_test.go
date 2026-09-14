package store

import (
	"errors"
	"testing"
)

// seedCards creates one card per slug, in order.
func seedCards(t *testing.T, s *Store, slugs ...string) {
	t.Helper()
	for _, slug := range slugs {
		if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{Slug: slug}); err != nil {
			t.Fatalf("UpsertProjectCard(%s): %v", slug, err)
		}
	}
}

func setParent(t *testing.T, s *Store, slug, parent string) {
	t.Helper()
	if err := s.SetProjectParent(slug, &parent); err != nil {
		t.Fatalf("SetProjectParent(%s -> %s): %v", slug, parent, err)
	}
}

func TestSetProjectParentRejectsDirectCycle(t *testing.T) {
	s := newTestStore(t)
	seedCards(t, s, "acme")

	if err := s.SetProjectParent("acme", strPtr("acme")); !errors.Is(err, ErrProjectCycle) {
		t.Fatalf("want ErrProjectCycle, got %v", err)
	}
}

func TestSetProjectParentRejectsIndirectCycle(t *testing.T) {
	s := newTestStore(t)
	seedCards(t, s, "root", "middle", "leaf")
	setParent(t, s, "middle", "root")
	setParent(t, s, "leaf", "middle")

	// root under its own grandchild closes the loop.
	if err := s.SetProjectParent("root", strPtr("leaf")); !errors.Is(err, ErrProjectCycle) {
		t.Fatalf("want ErrProjectCycle, got %v", err)
	}
	card, err := s.GetProjectCard("root")
	if err != nil {
		t.Fatalf("GetProjectCard: %v", err)
	}
	if card.ParentSlug != nil {
		t.Fatalf("a rejected parent must not be written: %v", *card.ParentSlug)
	}
}

func TestSetProjectParentRejectsDepthFour(t *testing.T) {
	s := newTestStore(t)
	seedCards(t, s, "l0", "l1", "l2", "l3", "l4")
	setParent(t, s, "l1", "l0")
	setParent(t, s, "l2", "l1")
	setParent(t, s, "l3", "l2")

	if err := s.SetProjectParent("l4", strPtr("l3")); !errors.Is(err, ErrProjectDepthExceeded) {
		t.Fatalf("want ErrProjectDepthExceeded, got %v", err)
	}

	// The same limit applies to a move that would push an existing subtree
	// past the bottom, not only to a single card.
	seedCards(t, s, "branch", "branch-child")
	setParent(t, s, "branch-child", "branch")
	if err := s.SetProjectParent("branch", strPtr("l3")); !errors.Is(err, ErrProjectDepthExceeded) {
		t.Fatalf("moving a subtree past depth 3 must be refused, got %v", err)
	}
}

func TestSetProjectParentRewritesSubtreeDepth(t *testing.T) {
	s := newTestStore(t)
	seedCards(t, s, "umbrella", "repo", "service", "orphan")
	setParent(t, s, "service", "repo")

	depthOf := func(slug string) int {
		t.Helper()
		card, err := s.GetProjectCard(slug)
		if err != nil {
			t.Fatalf("GetProjectCard(%s): %v", slug, err)
		}
		return card.Depth
	}
	if got := depthOf("service"); got != 1 {
		t.Fatalf("service depth %d, want 1", got)
	}

	// Moving the parent one level down takes the whole subtree with it.
	setParent(t, s, "repo", "umbrella")
	if got := depthOf("repo"); got != 1 {
		t.Fatalf("repo depth %d, want 1", got)
	}
	if got := depthOf("service"); got != 2 {
		t.Fatalf("service depth %d, want 2", got)
	}

	// Detaching returns the subtree to the top.
	if err := s.SetProjectParent("repo", nil); err != nil {
		t.Fatalf("SetProjectParent(repo -> root): %v", err)
	}
	if got := depthOf("repo"); got != 0 {
		t.Fatalf("repo depth %d, want 0", got)
	}
	if got := depthOf("service"); got != 1 {
		t.Fatalf("service depth %d, want 1", got)
	}
	if got := depthOf("orphan"); got != 0 {
		t.Fatalf("orphan depth %d, want 0", got)
	}
}

// TestProjectCardsDepthTriggerBlocksRawSQL pins the safety net under
// SetProjectParent: a parent written straight through SQL, bypassing the Go
// path, is still refused when it contradicts the depth it claims.
func TestProjectCardsDepthTriggerBlocksRawSQL(t *testing.T) {
	s := newTestStore(t)
	seedCards(t, s, "root", "child")

	if _, err := s.DB().Exec(
		`UPDATE project_cards SET parent_slug = 'root' WHERE slug = 'root'`); err == nil {
		t.Fatal("a card must not be its own parent")
	}
	if _, err := s.DB().Exec(
		`UPDATE project_cards SET parent_slug = 'root' WHERE slug = 'child'`); err == nil {
		t.Fatal("a parent without the matching depth must be refused")
	}
	if _, err := s.DB().Exec(
		`UPDATE project_cards SET parent_slug = NULL, depth = 2 WHERE slug = 'child'`); err == nil {
		t.Fatal("a card without a parent must stay at depth 0")
	}
}

func TestProjectTreeIsPreorder(t *testing.T) {
	s := newTestStore(t)
	seedCards(t, s, "alpha", "alpha-one", "alpha-one-deep", "alpha-two", "beta")
	setParent(t, s, "alpha-one", "alpha")
	setParent(t, s, "alpha-one-deep", "alpha-one")
	setParent(t, s, "alpha-two", "alpha")

	nodes, err := s.ProjectTree("", false)
	if err != nil {
		t.Fatalf("ProjectTree: %v", err)
	}
	got := make([]string, 0, len(nodes))
	for _, n := range nodes {
		got = append(got, n.Slug)
	}
	want := []string{"alpha", "alpha-one", "alpha-one-deep", "alpha-two", "beta"}
	if len(got) != len(want) {
		t.Fatalf("tree %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tree %v, want %v", got, want)
		}
	}
	if nodes[0].Children != 2 {
		t.Fatalf("alpha children %d, want 2", nodes[0].Children)
	}
	if nodes[1].Depth != 1 || nodes[2].Depth != 2 {
		t.Fatalf("depths %d/%d, want 1/2", nodes[1].Depth, nodes[2].Depth)
	}

	sub, err := s.ProjectTree("alpha-one", false)
	if err != nil {
		t.Fatalf("ProjectTree(alpha-one): %v", err)
	}
	if len(sub) != 2 || sub[0].Slug != "alpha-one" || sub[1].Slug != "alpha-one-deep" {
		t.Fatalf("subtree %+v", sub)
	}

	slugs, err := s.SubtreeSlugs("alpha")
	if err != nil {
		t.Fatalf("SubtreeSlugs: %v", err)
	}
	if len(slugs) != 4 {
		t.Fatalf("subtree slugs %v, want 4 entries", slugs)
	}
}

// TestSuggestProjectTreeGroupsInstancesUnderPrefix pins the separator folding
// that makes the numbered instances of one product read as one family, however
// their slugs happen to be spelled.
func TestSuggestProjectTreeGroupsInstancesUnderPrefix(t *testing.T) {
	s := newTestStore(t)
	seedCards(t, s, "nextcloud_00", "nextcloud_01", "nextcloud-02", "unrelated")

	suggestions, err := s.SuggestProjectTree()
	if err != nil {
		t.Fatalf("SuggestProjectTree: %v", err)
	}
	var found *ProjectTreeSuggestion
	for i := range suggestions {
		if suggestions[i].Parent == "nextcloud" {
			found = &suggestions[i]
		}
	}
	if found == nil {
		t.Fatalf("no suggestion for nextcloud: %+v", suggestions)
	}
	want := map[string]bool{"nextcloud_00": true, "nextcloud_01": true, "nextcloud-02": true}
	if len(found.Children) != len(want) {
		t.Fatalf("children %v, want %v", found.Children, want)
	}
	for _, child := range found.Children {
		if !want[child] {
			t.Fatalf("unexpected child %q in %v", child, found.Children)
		}
	}
	if found.ParentExists {
		t.Fatal("the suggested parent does not exist yet")
	}

	// Suggesting never writes.
	for slug := range want {
		card, err := s.GetProjectCard(slug)
		if err != nil {
			t.Fatalf("GetProjectCard(%s): %v", slug, err)
		}
		if card.ParentSlug != nil {
			t.Fatalf("%s was reparented by a suggestion", slug)
		}
	}
	if _, err := s.GetProjectCard("nextcloud"); !errors.Is(err, ErrNoProjectCard) {
		t.Fatalf("a suggestion must not create the parent card: %v", err)
	}
}
