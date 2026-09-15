package store

import (
	"errors"
	"testing"
)

// seedTask creates one task and returns it.
func seedTask(t *testing.T, s *Store, project string, p UpsertTaskParams) Task {
	t.Helper()
	p.Project = project
	if p.Kind == nil {
		p.Kind = strPtr("bugfix")
	}
	r, err := s.UpsertTask(p)
	if err != nil {
		t.Fatalf("UpsertTask(%s): %v", project, err)
	}
	return r.Task
}

// TestResolveTaskRefResolvesASlugWithinItsProject pins the fifth reference
// form. A vault folder is named after the slug, so an import that only has the
// folder name has nothing else to resolve the task with.
func TestResolveTaskRefResolvesASlugWithinItsProject(t *testing.T) {
	s := newTestStore(t)
	task := seedTask(t, s, "koi-garden", UpsertTaskParams{
		JiraKey: strPtr("KOI-1099"), Slug: strPtr("lookup-timeout"), Title: strPtr("Lookup timeout"),
	})

	got, err := s.ResolveTaskRef("koi-garden", "lookup-timeout")
	if err != nil {
		t.Fatalf("ResolveTaskRef(slug): %v", err)
	}
	if got.ID != task.ID {
		t.Fatalf("resolved task %d, want %d", got.ID, task.ID)
	}

	if _, err := s.ResolveTaskRef("koi-garden", "no-such-slug"); !errors.Is(err, ErrUnknownTask) {
		t.Fatalf("unknown slug = %v, want ErrUnknownTask", err)
	}
}

// TestResolveTaskRefWithoutAProjectSearchesEveryProject pins the lookup the
// workspace surfaces need: the reference is given without a project because the
// task is what names the project, not the other way round.
func TestResolveTaskRefWithoutAProjectSearchesEveryProject(t *testing.T) {
	s := newTestStore(t)
	task := seedTask(t, s, "koi-garden", UpsertTaskParams{
		JiraKey: strPtr("KOI-1099"), SDDChange: strPtr("lookup-timeout"),
		Slug: strPtr("lookup-timeout"), Title: strPtr("Lookup timeout"),
	})
	seedTask(t, s, "tsukimi-bridge", UpsertTaskParams{
		JiraKey: strPtr("TSK-7"), Slug: strPtr("bridge-audit"), Title: strPtr("Bridge audit"),
	})

	for _, ref := range []string{"KOI-1099", task.SyncID, "#" + itoa(task.ID), "change:lookup-timeout", "lookup-timeout"} {
		got, err := s.ResolveTaskRef("", ref)
		if err != nil {
			t.Fatalf("ResolveTaskRef(\"\", %q): %v", ref, err)
		}
		if got.ID != task.ID {
			t.Fatalf("ResolveTaskRef(\"\", %q) = task %d, want %d", ref, got.ID, task.ID)
		}
	}

	if _, err := s.ResolveTaskRef("", "nothing-answers-to-this"); !errors.Is(err, ErrUnknownTask) {
		t.Fatalf("unknown global ref = %v, want ErrUnknownTask", err)
	}
	if _, err := s.ResolveTaskRef("", ""); !errors.Is(err, ErrUnknownTask) {
		t.Fatalf("empty ref = %v, want ErrUnknownTask", err)
	}
}

// TestResolveTaskRefWithoutAProjectRefusesAnAmbiguousSlug pins the refusal.
// Two projects may both keep a "hardening" folder, and picking one by row order
// would send a scan's evidence to whichever task happened to be written first.
func TestResolveTaskRefWithoutAProjectRefusesAnAmbiguousSlug(t *testing.T) {
	s := newTestStore(t)
	seedTask(t, s, "koi-garden", UpsertTaskParams{Slug: strPtr("hardening"), Title: strPtr("Hardening")})
	seedTask(t, s, "tsukimi-bridge", UpsertTaskParams{Slug: strPtr("hardening"), Title: strPtr("Hardening")})

	if _, err := s.ResolveTaskRef("", "hardening"); !errors.Is(err, ErrAmbiguousTask) {
		t.Fatalf("ambiguous slug = %v, want ErrAmbiguousTask", err)
	}
	// Scoped, the same slug is unambiguous.
	if _, err := s.ResolveTaskRef("koi-garden", "hardening"); err != nil {
		t.Fatalf("ResolveTaskRef(scoped): %v", err)
	}
}

// TestFindEvidenceBySHAReportsWhatATaskAlreadyHolds pins the read half of
// AddEvidence's idempotency key: a dry run has to say "update" where an apply
// would, and without this it can only guess.
func TestFindEvidenceBySHAReportsWhatATaskAlreadyHolds(t *testing.T) {
	s := newTestStore(t)
	task := seedTask(t, s, "koi-garden", UpsertTaskParams{JiraKey: strPtr("KOI-1099"), Title: strPtr("Lookup")})
	sha := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	if _, found, err := s.FindEvidenceBySHA(task.SyncID, sha); err != nil || found {
		t.Fatalf("FindEvidenceBySHA before the write = (found %v, %v), want not found", found, err)
	}

	saved, _, _, err := s.AddEvidence(AddEvidenceParams{
		Task: task, Path: "koi-garden/KOI-1099/evidences/before.png", SHA256: sha,
		Kind: "png", Proves: "the timeout before the fix",
	})
	if err != nil {
		t.Fatalf("AddEvidence: %v", err)
	}

	got, found, err := s.FindEvidenceBySHA(task.SyncID, sha)
	if err != nil || !found {
		t.Fatalf("FindEvidenceBySHA = (found %v, %v), want the row", found, err)
	}
	if got.ID != saved.ID {
		t.Fatalf("found evidence %d, want %d", got.ID, saved.ID)
	}

	// A hash belongs to one task, not to the store.
	other := seedTask(t, s, "koi-garden", UpsertTaskParams{JiraKey: strPtr("KOI-1100"), Title: strPtr("Other")})
	if _, found, err := s.FindEvidenceBySHA(other.SyncID, sha); err != nil || found {
		t.Fatalf("another task's lookup = (found %v, %v), want not found", found, err)
	}
}

// TestFindBenchmarkReportsWhatATaskAlreadyMeasured pins the read half of
// AddBenchmark's idempotency key, on the same four columns the write uses.
func TestFindBenchmarkReportsWhatATaskAlreadyMeasured(t *testing.T) {
	s := newTestStore(t)
	task := seedTask(t, s, "koi-garden", UpsertTaskParams{JiraKey: strPtr("KOI-1099"), Title: strPtr("Lookup")})

	if _, found, err := s.FindBenchmark(task.SyncID, "lookup", "lookup.p95", "2026-09-01 10:00:00"); err != nil || found {
		t.Fatalf("FindBenchmark before the write = (found %v, %v), want not found", found, err)
	}

	added, err := s.AddBenchmark(AddBenchmarkParams{
		Task: task, Name: "lookup", Metric: "lookup.p95", Unit: "ms", Value: 1512,
		Baseline: true, CapturedAt: "2026-09-01 10:00:00",
	})
	if err != nil {
		t.Fatalf("AddBenchmark: %v", err)
	}

	got, found, err := s.FindBenchmark(task.SyncID, "lookup", "lookup.p95", "2026-09-01 10:00:00")
	if err != nil || !found {
		t.Fatalf("FindBenchmark = (found %v, %v), want the row", found, err)
	}
	if got.SyncID != added.Benchmark.SyncID {
		t.Fatalf("found benchmark %q, want %q", got.SyncID, added.Benchmark.SyncID)
	}

	// A different capture time is a different measurement.
	if _, found, err := s.FindBenchmark(task.SyncID, "lookup", "lookup.p95", "2026-09-02 10:00:00"); err != nil || found {
		t.Fatalf("another captured_at = (found %v, %v), want not found", found, err)
	}
}

// TestProjectTreeDoctorIsQuietOnAHealthyTree pins the answer the doctor gives
// most of the time: a tree this package's own write path built is consistent by
// construction.
func TestProjectTreeDoctorIsQuietOnAHealthyTree(t *testing.T) {
	s := newTestStore(t)
	seedCards(t, s, "koi-garden", "koi-garden-pond-01")
	setParent(t, s, "koi-garden-pond-01", "koi-garden")

	report, err := s.ProjectTreeDoctor()
	if err != nil {
		t.Fatalf("ProjectTreeDoctor: %v", err)
	}
	if len(report.Faults) != 0 {
		t.Fatalf("faults = %+v, want none", report.Faults)
	}
	if report.Cards != 2 {
		t.Fatalf("examined %d cards, want 2", report.Cards)
	}
}

// TestProjectTreeDoctorFindsAnOrphanAndRepairDetachesIt pins the one fault
// ordinary use produces: a parent retired while something still hung from it.
// The foreign key is still satisfied — the row is there — which is exactly why
// this needs a doctor rather than a constraint.
func TestProjectTreeDoctorFindsAnOrphanAndRepairDetachesIt(t *testing.T) {
	s := newTestStore(t)
	seedCards(t, s, "koi-garden", "koi-garden-pond-01")
	setParent(t, s, "koi-garden-pond-01", "koi-garden")
	if _, err := s.db.Exec(
		`UPDATE project_cards SET deleted_at = '2026-09-14 10:00:00' WHERE slug = 'koi-garden'`); err != nil {
		t.Fatalf("retire the umbrella: %v", err)
	}

	report, err := s.ProjectTreeDoctor()
	if err != nil {
		t.Fatalf("ProjectTreeDoctor: %v", err)
	}
	if len(report.Faults) != 1 || report.Faults[0].Fault != TreeFaultOrphan {
		t.Fatalf("faults = %+v, want one orphan", report.Faults)
	}
	if report.Faults[0].Repaired {
		t.Error("a report must not repair anything")
	}

	repaired, err := s.RepairProjectTree()
	if err != nil {
		t.Fatalf("RepairProjectTree: %v", err)
	}
	if len(repaired.Faults) != 1 || !repaired.Faults[0].Repaired {
		t.Fatalf("faults = %+v, want the orphan detached", repaired.Faults)
	}

	card, err := s.GetProjectCard("koi-garden-pond-01")
	if err != nil {
		t.Fatalf("GetProjectCard: %v", err)
	}
	if card.ParentSlug != nil || card.Depth != 0 {
		t.Fatalf("card is at parent %v depth %d, want the top of the tree", card.ParentSlug, card.Depth)
	}

	after, err := s.ProjectTreeDoctor()
	if err != nil {
		t.Fatalf("ProjectTreeDoctor: %v", err)
	}
	if len(after.Faults) != 0 {
		t.Fatalf("faults = %+v after the repair, want none", after.Faults)
	}
}

// TestDiagnoseProjectCardNamesEveryFault covers the verdicts a live database
// cannot be talked into producing.
//
// The schema's own trigger refuses a depth that disagrees with the chain and a
// card that is its own parent, so those rows can only arrive another way — a
// restore, or a sync from a peer whose schema predates the trigger. The doctor
// exists for exactly that, and the only honest way to test it is to hand the
// diagnosis the shape it is meant to catch.
func TestDiagnoseProjectCardNamesEveryFault(t *testing.T) {
	cases := []struct {
		name  string
		cards map[string]treeCardRow
		slug  string
		fault string
		ok    bool
	}{
		{
			name:  "a root at a depth it cannot be at",
			cards: map[string]treeCardRow{"a": {depth: 2}},
			slug:  "a", fault: TreeFaultDepth, ok: true,
		},
		{
			name:  "a depth that disagrees with the chain",
			cards: map[string]treeCardRow{"a": {}, "b": {parent: strPtr("a"), depth: 2}},
			slug:  "b", fault: TreeFaultDepth, ok: true,
		},
		{
			name:  "a card that is its own ancestor",
			cards: map[string]treeCardRow{"a": {parent: strPtr("b"), depth: 1}, "b": {parent: strPtr("a"), depth: 1}},
			slug:  "a", fault: TreeFaultCycle, ok: true,
		},
		{
			name:  "a parent with no live card",
			cards: map[string]treeCardRow{"b": {parent: strPtr("gone"), depth: 1}},
			slug:  "b", fault: TreeFaultOrphan, ok: true,
		},
		{
			name: "a chain deeper than the tree allows",
			cards: map[string]treeCardRow{
				"a": {}, "b": {parent: strPtr("a"), depth: 1},
				"c": {parent: strPtr("b"), depth: 2}, "d": {parent: strPtr("c"), depth: 3},
			},
			slug: "d", fault: TreeFaultDepth, ok: true,
		},
		{
			name:  "a healthy root",
			cards: map[string]treeCardRow{"a": {}},
			slug:  "a",
		},
		{
			name:  "a healthy child",
			cards: map[string]treeCardRow{"a": {}, "b": {parent: strPtr("a"), depth: 1}},
			slug:  "b",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fault, ok := diagnoseProjectCard(tc.cards, tc.slug)
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
