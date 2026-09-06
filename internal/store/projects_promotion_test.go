package store

import (
	"errors"
	"testing"
)

// promoTestObs saves one observation and returns its id. Every promotion test
// needs the same four-line preamble, and spelling it out per test is how a
// battery quietly ends up asserting against different fixtures.
func promoTestObs(t *testing.T, s *Store, sessionID, obsType, title, project string) int64 {
	t.Helper()
	id, err := s.AddObservation(AddObservationParams{
		SessionID: sessionID, Type: obsType, Title: title, Content: "body of " + title, Project: project,
	})
	if err != nil {
		t.Fatalf("AddObservation(%s/%s): %v", obsType, title, err)
	}
	return id
}

func promoTestStore(t *testing.T) *Store {
	t.Helper()
	s := newProjectsSchemaTestStore(t)
	if err := s.CreateSession("s1", "nextcloud", ""); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return s
}

// The allowlist is the whole point of the bridge, so both halves are asserted:
// what it lets through and what it must keep out.
func TestIsPromotableType(t *testing.T) {
	promotable := []string{"decision", "discovery", "DECISION", "  Discovery  "}
	for _, in := range promotable {
		if !IsPromotableType(in) {
			t.Fatalf("IsPromotableType(%q) = false, want true", in)
		}
	}
	rejected := []string{"bugfix", "manual", "session", "prompt", "evidence", "runbook", "", "   ", "decisions", "dis covery"}
	for _, in := range rejected {
		if IsPromotableType(in) {
			t.Fatalf("IsPromotableType(%q) = true, want false", in)
		}
	}
	if got := PromotionAllowedTypes(); len(got) != 2 || got[0] != "decision" || got[1] != "discovery" {
		t.Fatalf("PromotionAllowedTypes() = %v, want [decision discovery]", got)
	}
	// The returned slice is a copy: mutating it must not widen the allowlist.
	mutated := PromotionAllowedTypes()
	mutated[0] = "bugfix"
	if IsPromotableType("bugfix") {
		t.Fatal("mutating the returned allowlist widened the real one")
	}
}

func TestNormalizePromotionTypes(t *testing.T) {
	all, err := NormalizePromotionTypes(nil)
	if err != nil || len(all) != 2 {
		t.Fatalf("NormalizePromotionTypes(nil) = (%v, %v), want the full allowlist", all, err)
	}
	if got, err := NormalizePromotionTypes([]string{"", "  "}); err != nil || len(got) != 2 {
		t.Fatalf("an all-empty filter = (%v, %v), want the full allowlist", got, err)
	}
	got, err := NormalizePromotionTypes([]string{"Discovery", "discovery", " decision "})
	if err != nil {
		t.Fatalf("NormalizePromotionTypes: %v", err)
	}
	if len(got) != 2 || got[0] != "decision" || got[1] != "discovery" {
		t.Fatalf("got %v, want [decision discovery] deduplicated and sorted", got)
	}
	if got, err := NormalizePromotionTypes([]string{"decision"}); err != nil || len(got) != 1 || got[0] != "decision" {
		t.Fatalf("a narrowing filter = (%v, %v), want [decision]", got, err)
	}
	// The filter can only narrow. Naming a type outside the allowlist is an
	// error, never a widening.
	if _, err := NormalizePromotionTypes([]string{"decision", "bugfix"}); !errors.Is(err, ErrTypeNotPromotable) {
		t.Fatalf("a widening filter = %v, want ErrTypeNotPromotable", err)
	}
}

// The counters exist so an empty candidate list can name its cause. This test
// builds one corpus where every exclusion reason fires exactly once and reads
// them all back.
func TestPromotionCandidates_CountsEveryExclusion(t *testing.T) {
	s := promoTestStore(t)

	promoted := promoTestObs(t, s, "s1", "decision", "already written up", "nextcloud")
	candidate := promoTestObs(t, s, "s1", "discovery", "not yet written up", "nextcloud")
	wrongType := promoTestObs(t, s, "s1", "bugfix", "a work trace", "nextcloud")
	unpinned := promoTestObs(t, s, "s1", "decision", "nobody pinned this", "nextcloud")

	for _, id := range []int64{promoted, candidate, wrongType} {
		if err := s.PinObservation(id); err != nil {
			t.Fatalf("PinObservation(%d): %v", id, err)
		}
	}

	if _, err := s.StampObservationKnowledgeRef(StampKnowledgeRefParams{
		ObservationID: promoted, KnowledgeRef: "Services/Nextcloud/Architecture.md",
	}); err != nil {
		t.Fatalf("stamp the already-promoted fixture: %v", err)
	}

	scan, err := s.PromotionCandidates("nextcloud", nil, 0)
	if err != nil {
		t.Fatalf("PromotionCandidates: %v", err)
	}
	if scan.PinnedInspected != 3 {
		t.Fatalf("PinnedInspected = %d, want 3 (the unpinned one is not inspected)", scan.PinnedInspected)
	}
	if scan.TypeExcluded != 1 {
		t.Fatalf("TypeExcluded = %d, want 1", scan.TypeExcluded)
	}
	if scan.AlreadyPromoted != 1 {
		t.Fatalf("AlreadyPromoted = %d, want 1", scan.AlreadyPromoted)
	}
	if len(scan.Candidates) != 1 {
		t.Fatalf("Candidates = %d, want 1: %+v", len(scan.Candidates), scan.Candidates)
	}
	got := scan.Candidates[0]
	if got.ObservationID != candidate {
		t.Fatalf("candidate id = %d, want %d", got.ObservationID, candidate)
	}
	if got.Type != "discovery" || got.Project != "nextcloud" {
		t.Fatalf("candidate = %+v, want the discovery of project nextcloud", got)
	}
	if got.Content == "" || got.SyncID == "" {
		t.Fatalf("candidate carries no body or no sync_id: %+v", got)
	}
	if len(scan.Types) != 2 {
		t.Fatalf("Types = %v, want the full allowlist echoed back", scan.Types)
	}
	_ = unpinned
}

// An observation with no project cannot replicate its stamp, so it is counted
// out of the candidate list rather than handed to the bridge and rejected on
// the way back.
func TestPromotionCandidates_ExcludesObservationsWithoutProject(t *testing.T) {
	s := promoTestStore(t)
	orphan := promoTestObs(t, s, "s1", "decision", "no project", "")
	if err := s.PinObservation(orphan); err != nil {
		t.Fatalf("PinObservation: %v", err)
	}

	scan, err := s.PromotionCandidates("", nil, 0)
	if err != nil {
		t.Fatalf("PromotionCandidates: %v", err)
	}
	if scan.PinnedInspected != 1 {
		t.Fatalf("PinnedInspected = %d, want 1", scan.PinnedInspected)
	}
	if scan.NoProject != 1 || len(scan.Candidates) != 0 {
		t.Fatalf("NoProject = %d, candidates = %d, want 1 and 0", scan.NoProject, len(scan.Candidates))
	}
}

func TestPromotionCandidates_TypeFilterNarrowsAndRejects(t *testing.T) {
	s := promoTestStore(t)
	dec := promoTestObs(t, s, "s1", "decision", "a choice", "nextcloud")
	dis := promoTestObs(t, s, "s1", "discovery", "a behaviour", "nextcloud")
	for _, id := range []int64{dec, dis} {
		if err := s.PinObservation(id); err != nil {
			t.Fatalf("PinObservation: %v", err)
		}
	}

	scan, err := s.PromotionCandidates("nextcloud", []string{"decision"}, 0)
	if err != nil {
		t.Fatalf("PromotionCandidates: %v", err)
	}
	if len(scan.Candidates) != 1 || scan.Candidates[0].Type != "decision" {
		t.Fatalf("filtered scan = %+v, want only the decision", scan.Candidates)
	}
	if scan.TypeExcluded != 1 {
		t.Fatalf("TypeExcluded = %d, want 1 (the discovery)", scan.TypeExcluded)
	}

	if _, err := s.PromotionCandidates("nextcloud", []string{"bugfix"}, 0); !errors.Is(err, ErrTypeNotPromotable) {
		t.Fatalf("a filter outside the allowlist = %v, want ErrTypeNotPromotable", err)
	}
}

// --limit caps the list without falsifying the counters: a capped run still
// reports how much it inspected.
func TestPromotionCandidates_LimitCapsListNotCounters(t *testing.T) {
	s := promoTestStore(t)
	for _, title := range []string{"one", "two", "three"} {
		id := promoTestObs(t, s, "s1", "decision", title, "nextcloud")
		if err := s.PinObservation(id); err != nil {
			t.Fatalf("PinObservation: %v", err)
		}
	}
	scan, err := s.PromotionCandidates("nextcloud", nil, 2)
	if err != nil {
		t.Fatalf("PromotionCandidates: %v", err)
	}
	if len(scan.Candidates) != 2 {
		t.Fatalf("Candidates = %d, want 2", len(scan.Candidates))
	}
	if scan.PinnedInspected != 3 {
		t.Fatalf("PinnedInspected = %d, want 3 even under --limit 2", scan.PinnedInspected)
	}
}

// The project scope is honoured: promoting one project must not drag another
// project's pinned decisions into the batch.
func TestPromotionCandidates_ScopedByProject(t *testing.T) {
	s := promoTestStore(t)
	if err := s.CreateSession("s2", "portal", ""); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	a := promoTestObs(t, s, "s1", "decision", "nextcloud choice", "nextcloud")
	b := promoTestObs(t, s, "s2", "decision", "portal choice", "portal")
	for _, id := range []int64{a, b} {
		if err := s.PinObservation(id); err != nil {
			t.Fatalf("PinObservation: %v", err)
		}
	}
	scan, err := s.PromotionCandidates("nextcloud", nil, 0)
	if err != nil {
		t.Fatalf("PromotionCandidates: %v", err)
	}
	if len(scan.Candidates) != 1 || scan.Candidates[0].ObservationID != a {
		t.Fatalf("scoped scan = %+v, want only the nextcloud decision", scan.Candidates)
	}
	if scan.PinnedInspected != 1 {
		t.Fatalf("PinnedInspected = %d, want 1: the scope applies to the counters too", scan.PinnedInspected)
	}
}

func TestStampObservationKnowledgeRef_NormalizesAndIsIdempotent(t *testing.T) {
	s := promoTestStore(t)
	id := promoTestObs(t, s, "s1", "decision", "a choice", "nextcloud")
	if err := s.PinObservation(id); err != nil {
		t.Fatalf("PinObservation: %v", err)
	}

	first, err := s.StampObservationKnowledgeRef(StampKnowledgeRefParams{
		ObservationID: id, KnowledgeRef: "[[Work/Claro drive/Services/Nextcloud/Architecture.md#Object Store]]",
	})
	if err != nil {
		t.Fatalf("StampObservationKnowledgeRef: %v", err)
	}
	if !first.Stamped {
		t.Fatal("the first stamp reported Stamped = false")
	}
	if first.KnowledgeRef != "Services/Nextcloud/Architecture.md#Object Store" {
		t.Fatalf("knowledge_ref = %q, want the normalized tool form", first.KnowledgeRef)
	}
	if first.Project != "nextcloud" || first.Type != "decision" {
		t.Fatalf("result = %+v, want project nextcloud and type decision", first)
	}

	// Re-running the bridge over an already merged batch is expected: the
	// same ref again is a success that reports it changed nothing.
	second, err := s.StampObservationKnowledgeRef(StampKnowledgeRefParams{
		ObservationID: id, KnowledgeRef: "Services/Nextcloud/Architecture.md#Object Store",
	})
	if err != nil {
		t.Fatalf("second stamp: %v", err)
	}
	if second.Stamped {
		t.Fatal("the second stamp reported a write it did not make")
	}

	var rows int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM observation_refs WHERE observation_sync_id = ? AND ref_kind = 'knowledge'`,
		first.ObservationSyncID).Scan(&rows); err != nil {
		t.Fatalf("count refs: %v", err)
	}
	if rows != 1 {
		t.Fatalf("knowledge refs = %d, want exactly 1 after two identical stamps", rows)
	}

	// The stamped observation drops out of the candidate list, which is what
	// makes a second bridge run skip it instead of re-proposing it.
	scan, err := s.PromotionCandidates("nextcloud", nil, 0)
	if err != nil {
		t.Fatalf("PromotionCandidates: %v", err)
	}
	if len(scan.Candidates) != 0 || scan.AlreadyPromoted != 1 {
		t.Fatalf("after stamping: candidates = %d, already promoted = %d; want 0 and 1",
			len(scan.Candidates), scan.AlreadyPromoted)
	}
}

// A second, different reference is refused. The export path keeps the earliest
// one, so accepting the insert would write a row nothing reads and report it
// as a success.
func TestStampObservationKnowledgeRef_RejectsAConflictingSecondRef(t *testing.T) {
	s := promoTestStore(t)
	id := promoTestObs(t, s, "s1", "decision", "a choice", "nextcloud")
	if err := s.PinObservation(id); err != nil {
		t.Fatalf("PinObservation: %v", err)
	}
	res, err := s.StampObservationKnowledgeRef(StampKnowledgeRefParams{
		ObservationID: id, KnowledgeRef: "Services/Nextcloud/Architecture.md",
	})
	if err != nil {
		t.Fatalf("first stamp: %v", err)
	}

	_, err = s.StampObservationKnowledgeRef(StampKnowledgeRefParams{
		ObservationID: id, KnowledgeRef: "Services/Portal/Architecture.md",
	})
	if !errors.Is(err, ErrKnowledgeRefConflict) {
		t.Fatalf("a conflicting second ref = %v, want ErrKnowledgeRefConflict", err)
	}
	var rows int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM observation_refs WHERE observation_sync_id = ? AND ref_kind = 'knowledge'`,
		res.ObservationSyncID).Scan(&rows); err != nil {
		t.Fatalf("count refs: %v", err)
	}
	if rows != 1 {
		t.Fatalf("a rejected stamp still wrote: %d ref row(s), want 1", rows)
	}
}

func TestStampObservationKnowledgeRef_Rejections(t *testing.T) {
	s := promoTestStore(t)

	unpinned := promoTestObs(t, s, "s1", "decision", "unpinned", "nextcloud")
	if _, err := s.StampObservationKnowledgeRef(StampKnowledgeRefParams{
		ObservationID: unpinned, KnowledgeRef: "Services/Nextcloud/Architecture.md",
	}); !errors.Is(err, ErrObservationNotPinned) {
		t.Fatalf("stamping an unpinned observation = %v, want ErrObservationNotPinned", err)
	}
	// The repair escape hatch works and is explicit.
	if _, err := s.StampObservationKnowledgeRef(StampKnowledgeRefParams{
		ObservationID: unpinned, KnowledgeRef: "Services/Nextcloud/Architecture.md", AllowUnpinned: true,
	}); err != nil {
		t.Fatalf("--allow-unpinned stamp: %v", err)
	}

	wrongType := promoTestObs(t, s, "s1", "bugfix", "a work trace", "nextcloud")
	if err := s.PinObservation(wrongType); err != nil {
		t.Fatalf("PinObservation: %v", err)
	}
	if _, err := s.StampObservationKnowledgeRef(StampKnowledgeRefParams{
		ObservationID: wrongType, KnowledgeRef: "Services/Nextcloud/Architecture.md",
	}); !errors.Is(err, ErrTypeNotPromotable) {
		t.Fatalf("stamping a bugfix = %v, want ErrTypeNotPromotable", err)
	}
	if _, err := s.StampObservationKnowledgeRef(StampKnowledgeRefParams{
		ObservationID: wrongType, KnowledgeRef: "Services/Nextcloud/Architecture.md", AllowAnyType: true,
	}); err != nil {
		t.Fatalf("--allow-any-type stamp: %v", err)
	}

	noProject := promoTestObs(t, s, "s1", "decision", "no project", "")
	if err := s.PinObservation(noProject); err != nil {
		t.Fatalf("PinObservation: %v", err)
	}
	if _, err := s.StampObservationKnowledgeRef(StampKnowledgeRefParams{
		ObservationID: noProject, KnowledgeRef: "Services/Nextcloud/Architecture.md",
	}); !errors.Is(err, ErrObservationNoProject) {
		t.Fatalf("stamping a project-less observation = %v, want ErrObservationNoProject", err)
	}

	if _, err := s.StampObservationKnowledgeRef(StampKnowledgeRefParams{
		ObservationID: 987654, KnowledgeRef: "Services/Nextcloud/Architecture.md",
	}); !errors.Is(err, ErrUnknownObservation) {
		t.Fatalf("stamping a missing observation = %v, want ErrUnknownObservation", err)
	}
}

// The knowledge_ref shape rule applies here exactly as it does on a task, and
// in particular the bridge's own export folder stays out: a memory citing the
// memory dump as curated knowledge is the loop this rule exists to break.
func TestStampObservationKnowledgeRef_AppliesTheShapeRule(t *testing.T) {
	s := promoTestStore(t)
	id := promoTestObs(t, s, "s1", "discovery", "a behaviour", "nextcloud")
	if err := s.PinObservation(id); err != nil {
		t.Fatalf("PinObservation: %v", err)
	}

	cases := []struct {
		name string
		ref  string
		want error
	}{
		{"memory folder", "90 - Engram/engram/nextcloud/decision/7.md", ErrKnowledgeRefNotCurated},
		{"absolute path", "/absolute/vault/Doc.md", ErrKnowledgeRefAbsolute},
		{"not markdown", "Services/Nextcloud/diagram.png", ErrKnowledgeRefInvalid},
		{"traversal", "Services/../../etc/passwd.md", ErrKnowledgeRefInvalid},
		{"empty", "  ", ErrKnowledgeRefInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.StampObservationKnowledgeRef(StampKnowledgeRefParams{
				ObservationID: id, KnowledgeRef: tc.ref,
			}); !errors.Is(err, tc.want) {
				t.Fatalf("stamp %q = %v, want %v", tc.ref, err, tc.want)
			}
		})
	}

	var rows int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM observation_refs WHERE ref_kind = 'knowledge'`).Scan(&rows); err != nil {
		t.Fatalf("count refs: %v", err)
	}
	if rows != 0 {
		t.Fatalf("rejected refs still wrote %d row(s)", rows)
	}
}
