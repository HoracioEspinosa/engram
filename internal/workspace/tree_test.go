package workspace

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	projectpkg "github.com/HoracioEspinosa/engram/internal/project"
	"github.com/HoracioEspinosa/engram/internal/store"
)

// TestApplySuggestedTreeGroupsInstances pins the reorganisation the suggestion
// exists for: three numbered instances of one product, whatever separator each
// one spells, end up under one umbrella the apply had to create.
func TestApplySuggestedTreeGroupsInstances(t *testing.T) {
	s := newStore(t)
	for _, slug := range []string{"nextcloud-00", "nextcloud_01", "nextcloud-02"} {
		if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: slug}); err != nil {
			t.Fatalf("UpsertProjectCard(%s): %v", slug, err)
		}
	}

	suggestions, err := SuggestTree(s)
	if err != nil {
		t.Fatalf("SuggestTree: %v", err)
	}
	if len(suggestions) != 1 {
		t.Fatalf("suggested %+v, want one family", suggestions)
	}
	if suggestions[0].Parent != "nextcloud" {
		t.Fatalf("parent %q, want nextcloud", suggestions[0].Parent)
	}
	if len(suggestions[0].Children) != 3 {
		t.Fatalf("children %v, want the three instances", suggestions[0].Children)
	}

	// Suggesting is not applying: nothing moved yet.
	for _, slug := range []string{"nextcloud-00", "nextcloud_01", "nextcloud-02"} {
		card, err := s.GetProjectCard(slug)
		if err != nil {
			t.Fatalf("GetProjectCard(%s): %v", slug, err)
		}
		if card.ParentSlug != nil {
			t.Fatalf("%s already has a parent before the apply", slug)
		}
	}

	report, err := ApplySuggestedTree(s, suggestions)
	if err != nil {
		t.Fatalf("ApplySuggestedTree: %v", err)
	}
	if len(report.Conflicts) != 0 {
		t.Fatalf("conflicts %+v, want none", report.Conflicts)
	}
	if len(report.Applied) != 3 {
		t.Fatalf("applied %+v, want three moves", report.Applied)
	}
	if len(report.ParentsCreated) != 1 || report.ParentsCreated[0] != "nextcloud" {
		t.Fatalf("parents created %v, want [nextcloud]", report.ParentsCreated)
	}

	nodes, err := s.ProjectTree("nextcloud", false)
	if err != nil {
		t.Fatalf("ProjectTree: %v", err)
	}
	if len(nodes) != 4 {
		t.Fatalf("tree holds %d nodes, want the umbrella and its three instances", len(nodes))
	}
	for _, node := range nodes[1:] {
		if node.ParentSlug == nil || *node.ParentSlug != "nextcloud" {
			t.Errorf("%s is not under nextcloud", node.Slug)
		}
		if node.Depth != 1 {
			t.Errorf("%s sits at depth %d, want 1", node.Slug, node.Depth)
		}
	}
}

// TestApplySuggestedTreeCollectsConflicts pins that one refused move does not
// abandon a reorganisation somebody already approved.
func TestApplySuggestedTreeCollectsConflicts(t *testing.T) {
	s := newStore(t)
	for _, slug := range []string{"koi-garden", "koi-garden-pond-01"} {
		if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: slug}); err != nil {
			t.Fatalf("UpsertProjectCard(%s): %v", slug, err)
		}
	}

	report, err := ApplySuggestedTree(s, []store.ProjectTreeSuggestion{{
		Parent:       "koi-garden",
		Children:     []string{"koi-garden", "koi-garden-pond-01"},
		ParentExists: true,
	}})
	if err != nil {
		t.Fatalf("ApplySuggestedTree: %v", err)
	}
	if len(report.Applied) != 1 || report.Applied[0].Child != "koi-garden-pond-01" {
		t.Fatalf("applied %+v, want only the instance", report.Applied)
	}
	if len(report.Conflicts) != 1 {
		t.Fatalf("conflicts %+v, want the self-parent refusal", report.Conflicts)
	}
	if report.Conflicts[0].Code != "project_cycle" {
		t.Fatalf("conflict code %q, want project_cycle", report.Conflicts[0].Code)
	}
}

// TestTreeDoctorIsQuietOnAHealthyTree pins the answer the doctor gives most of
// the time: a tree the store's own write path built is consistent by
// construction.
func TestTreeDoctorIsQuietOnAHealthyTree(t *testing.T) {
	s := newStore(t)
	for _, slug := range []string{"koi-garden", "koi-garden-pond-01"} {
		if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: slug}); err != nil {
			t.Fatalf("UpsertProjectCard(%s): %v", slug, err)
		}
	}
	parent := "koi-garden"
	if err := s.SetProjectParent("koi-garden-pond-01", &parent); err != nil {
		t.Fatalf("SetProjectParent: %v", err)
	}

	faults, err := TreeDoctor(s, false)
	if err != nil {
		t.Fatalf("TreeDoctor: %v", err)
	}
	if len(faults) != 0 {
		t.Fatalf("faults = %+v, want none", faults)
	}
}

// TestTreeDoctorFindsAnOrphanAndDetachesIt pins the repair for the one fault
// ordinary use produces: a parent that was retired while something still hung
// from it. The card is reported, and --fix moves it where a person can see it
// rather than guessing where it belonged.
func TestTreeDoctorFindsAnOrphanAndDetachesIt(t *testing.T) {
	s := newStore(t)
	for _, slug := range []string{"koi-garden", "koi-garden-pond-01"} {
		if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: slug}); err != nil {
			t.Fatalf("UpsertProjectCard(%s): %v", slug, err)
		}
	}
	parent := "koi-garden"
	if err := s.SetProjectParent("koi-garden-pond-01", &parent); err != nil {
		t.Fatalf("SetProjectParent: %v", err)
	}
	// Retiring the umbrella leaves the instance pointing at a card no live
	// query returns. The foreign key is still satisfied — the row is there —
	// which is exactly why this needs a doctor rather than a constraint.
	if _, err := s.DB().Exec(
		`UPDATE project_cards SET deleted_at = '2026-09-14 10:00:00' WHERE slug = 'koi-garden'`); err != nil {
		t.Fatalf("retire the umbrella: %v", err)
	}

	faults, err := TreeDoctor(s, false)
	if err != nil {
		t.Fatalf("TreeDoctor: %v", err)
	}
	if len(faults) != 1 || faults[0].Fault != FaultOrphan {
		t.Fatalf("faults = %+v, want one orphan", faults)
	}
	if faults[0].Fixed {
		t.Error("a report without --fix must not repair anything")
	}

	fixed, err := TreeDoctor(s, true)
	if err != nil {
		t.Fatalf("TreeDoctor(fix): %v", err)
	}
	if len(fixed) != 1 || !fixed[0].Fixed {
		t.Fatalf("faults = %+v, want the orphan detached", fixed)
	}

	card, err := s.GetProjectCard("koi-garden-pond-01")
	if err != nil {
		t.Fatalf("GetProjectCard: %v", err)
	}
	if card.ParentSlug != nil || card.Depth != 0 {
		t.Fatalf("card is at parent %v depth %d, want the top of the tree", card.ParentSlug, card.Depth)
	}

	after, err := TreeDoctor(s, false)
	if err != nil {
		t.Fatalf("TreeDoctor: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("faults = %+v after the repair, want none", after)
	}
}

// TestDiagnoseCardNamesEveryFault covers the verdicts a live database cannot
// be talked into producing.
//
// The schema's own trigger refuses a depth that disagrees with the chain and a
// card that is its own parent, so those rows can only arrive another way — a
// restore, or a sync from a peer whose schema predates the trigger. The doctor
// exists for exactly that, and the only honest way to test it is to hand the
// diagnosis the shape it is meant to catch.
func TestDiagnoseCardNamesEveryFault(t *testing.T) {
	ptr := func(s string) *string { return &s }

	cases := []struct {
		name  string
		cards map[string]treeCard
		slug  string
		fault string
		ok    bool
	}{
		{
			name:  "a root at a depth it cannot be at",
			cards: map[string]treeCard{"a": {depth: 2}},
			slug:  "a", fault: FaultDepth, ok: true,
		},
		{
			name:  "a depth that disagrees with the chain",
			cards: map[string]treeCard{"a": {}, "b": {parent: ptr("a"), depth: 2}},
			slug:  "b", fault: FaultDepth, ok: true,
		},
		{
			name:  "a card that is its own ancestor",
			cards: map[string]treeCard{"a": {parent: ptr("b"), depth: 1}, "b": {parent: ptr("a"), depth: 1}},
			slug:  "a", fault: FaultCycle, ok: true,
		},
		{
			name:  "a parent with no live card",
			cards: map[string]treeCard{"b": {parent: ptr("gone"), depth: 1}},
			slug:  "b", fault: FaultOrphan, ok: true,
		},
		{
			name: "a chain deeper than the tree allows",
			cards: map[string]treeCard{
				"a": {}, "b": {parent: ptr("a"), depth: 1},
				"c": {parent: ptr("b"), depth: 2}, "d": {parent: ptr("c"), depth: 3},
			},
			slug: "d", fault: FaultDepth, ok: true,
		},
		{
			name:  "a healthy root",
			cards: map[string]treeCard{"a": {}},
			slug:  "a",
		},
		{
			name:  "a healthy child",
			cards: map[string]treeCard{"a": {}, "b": {parent: ptr("a"), depth: 1}},
			slug:  "b",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fault, ok := diagnoseCard(tc.cards, tc.slug)
			if ok != tc.ok {
				t.Fatalf("diagnosed = %v, want %v (fault %+v)", ok, tc.ok, fault)
			}
			if ok && fault.Fault != tc.fault {
				t.Fatalf("fault = %q, want %q", fault.Fault, tc.fault)
			}
			if ok && fault.Detail == "" {
				t.Error("a fault with no detail tells nobody what to fix")
			}
		})
	}
}

// TestGraphCheckStampsDocsOnly pins the distinction the indicator lives or
// dies by: a commit that only moves prose leaves the graph correct, and
// reporting that as stale trains everyone to ignore the indicator.
func TestGraphCheckStampsDocsOnly(t *testing.T) {
	s := newStore(t)
	repo := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		full := append([]string{"-C", repo,
			"-c", "user.name=engram", "-c", "user.email=engram@example.test",
			"-c", "commit.gpgsign=false"}, args...)
		out, err := exec.Command("git", full...).CombinedOutput()
		if err != nil {
			t.Skipf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	git("init", "--initial-branch=main")
	writeFile(t, filepath.Join(repo, "main.go"), "package main\n\nfunc main() {}\n")
	git("add", "-A")
	git("commit", "-m", "feat: add the entry point")
	head := exec.Command("git", "-C", repo, "rev-parse", "HEAD")
	commitBytes, err := head.Output()
	if err != nil {
		t.Skipf("git rev-parse: %v", err)
	}
	graphCommit := string(commitBytes[:len(commitBytes)-1])

	writeFile(t, filepath.Join(repo, "README.md"), "# Proyecto\n\nProsa nueva.\n")
	git("add", "-A")
	git("commit", "-m", "docs: describe the project")

	if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: "koi-garden"}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}
	if err := s.StampProjectGraph("koi-garden", graphCommit, "2026-09-01 10:00:00", nil); err != nil {
		t.Fatalf("StampProjectGraph: %v", err)
	}

	staleness, err := GraphCheckIn(s, "koi-garden", repo)
	if err != nil {
		t.Fatalf("GraphCheckIn: %v", err)
	}
	if staleness.Reason != projectpkg.StaleReasonDocsOnly {
		t.Fatalf("reason %q, want %q", staleness.Reason, projectpkg.StaleReasonDocsOnly)
	}
	if staleness.Stale {
		t.Error("a documentation-only change does not make the graph stale")
	}

	card, err := s.GetProjectCard("koi-garden")
	if err != nil {
		t.Fatalf("GetProjectCard: %v", err)
	}
	if card.GraphStaleReason == nil || *card.GraphStaleReason != projectpkg.StaleReasonDocsOnly {
		t.Fatalf("card records %v, want the verdict stamped", card.GraphStaleReason)
	}
	if card.GraphCheckedAt == nil || *card.GraphCheckedAt == "" {
		t.Error("the card does not record when it was checked")
	}
}

// TestGraphCheckUnknownProjectCarriesItsCode pins the failure for a slug no
// card answers to.
func TestGraphCheckUnknownProjectCarriesItsCode(t *testing.T) {
	s := newStore(t)

	_, err := GraphCheckIn(s, "no-such-project", t.TempDir())
	if !errors.Is(err, ErrUnknownProject) {
		t.Fatalf("got %v, want ErrUnknownProject", err)
	}
	if code := CodeOf(err); code != "unknown_project" {
		t.Fatalf("CodeOf = %q, want unknown_project", code)
	}
}

// TestGraphCheckWithoutAGraphIsStale pins that a card with no graph commit is
// reported as stale: nothing can be shown to be current.
func TestGraphCheckWithoutAGraphIsStale(t *testing.T) {
	s := newStore(t)
	if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: "koi-garden"}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}

	staleness, err := GraphCheckIn(s, "koi-garden", t.TempDir())
	if err != nil {
		t.Fatalf("GraphCheckIn: %v", err)
	}
	if !staleness.Stale || staleness.Reason != projectpkg.StaleReasonNoGraph {
		t.Fatalf("got %+v, want a no_graph verdict", staleness)
	}
}

// TestCodeOfIgnoresErrorsThisPackageDoesNotName pins that an error from
// somewhere else does not borrow a code it was never given.
func TestCodeOfIgnoresErrorsThisPackageDoesNotName(t *testing.T) {
	if code := CodeOf(os.ErrNotExist); code != "" {
		t.Fatalf("CodeOf = %q, want an empty code", code)
	}
	if code := CodeOf(nil); code != "" {
		t.Fatalf("CodeOf(nil) = %q, want an empty code", code)
	}
}
