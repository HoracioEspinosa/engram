package store

import (
	"errors"
	"testing"
)

// seedLinkedObservation writes one observation carrying a graph reference and
// returns its numeric id.
func seedLinkedObservation(t *testing.T, s *Store, project, title, graphRef, commit string) int64 {
	t.Helper()
	sessionID := "sess-" + project
	if err := s.CreateSession(sessionID, project, ""); err != nil {
		t.Fatalf("CreateSession(%s): %v", sessionID, err)
	}
	res, err := s.AddObservationLinked(AddObservationParams{
		SessionID: sessionID, Type: "decision", Title: title, Content: title, Project: project,
	}, &ObservationLink{GraphRef: graphRef, GraphCommit: commit})
	if err != nil {
		t.Fatalf("AddObservationLinked(%s): %v", title, err)
	}
	return res.ObservationID
}

// TestListObservationRefsPagesAProjectsGraphLinks pins the listing the Graph
// tab pages through: observation_refs rows are keyed by observation_sync_id, so
// only a join against observations can answer "which of this project's
// observations point at the graph?".
func TestListObservationRefsPagesAProjectsGraphLinks(t *testing.T) {
	s := newTestStore(t)
	commit := "1111111111111111111111111111111111111111"
	first := seedLinkedObservation(t, s, "koi-garden", "the pond lookup", "pkg/pond.Lookup", commit)
	second := seedLinkedObservation(t, s, "koi-garden", "the pond writer", "pkg/pond.Write", commit)
	seedLinkedObservation(t, s, "tsukimi-bridge", "the bridge", "pkg/bridge.Span", commit)

	page, err := s.ListObservationRefs("koi-garden", "graph", 10, 0)
	if err != nil {
		t.Fatalf("ListObservationRefs: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("total = %d, want the two refs of koi-garden", page.Total)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %+v, want two", page.Items)
	}
	// Newest first: a graph tab shows what was linked last, not first.
	if page.Items[0].ObservationID != second || page.Items[1].ObservationID != first {
		t.Fatalf("items = %+v, want %d before %d", page.Items, second, first)
	}
	if page.Items[0].RefKind != "graph" || page.Items[0].Ref != "pkg/pond.Write" {
		t.Fatalf("first item = %+v, want the graph ref", page.Items[0])
	}
	if page.Items[0].GraphCommit != commit {
		t.Fatalf("graph commit = %q, want %q", page.Items[0].GraphCommit, commit)
	}

	// The project name is compared folded to lower case, like every other
	// project filter in this package.
	upper, err := s.ListObservationRefs("KOI-GARDEN", "graph", 10, 0)
	if err != nil || upper.Total != 2 {
		t.Fatalf("ListObservationRefs(upper) = (%+v, %v), want the same two", upper, err)
	}

	older, err := s.ListObservationRefs("koi-garden", "graph", 1, 1)
	if err != nil {
		t.Fatalf("ListObservationRefs(offset): %v", err)
	}
	if len(older.Items) != 1 || older.Items[0].ObservationID != first {
		t.Fatalf("offset page = %+v, want only the older ref", older.Items)
	}
	if older.Total != 2 || older.Limit != 1 || older.Offset != 1 {
		t.Fatalf("page frame = %+v, want total 2 limit 1 offset 1", older)
	}
}

// TestListObservationRefsFiltersByKind pins that a kind narrows the listing and
// an empty kind asks for all of them.
func TestListObservationRefsFiltersByKind(t *testing.T) {
	s := newTestStore(t)
	commit := "2222222222222222222222222222222222222222"
	id := seedLinkedObservation(t, s, "koi-garden", "the pond lookup", "pkg/pond.Lookup", commit)

	task := seedTask(t, s, "koi-garden", UpsertTaskParams{JiraKey: strPtr("KOI-1099"), Title: strPtr("Lookup")})
	if _, err := s.LinkTaskObservation(LinkTaskObservationParams{
		Task: task, ObservationID: id, JiraRef: strPtr("KOI-1099"),
	}); err != nil {
		t.Fatalf("LinkTaskObservation: %v", err)
	}

	all, err := s.ListObservationRefs("koi-garden", "", 10, 0)
	if err != nil {
		t.Fatalf("ListObservationRefs(all kinds): %v", err)
	}
	if all.Total != 2 {
		t.Fatalf("total = %d, want the graph ref and the jira ref", all.Total)
	}

	onlyJira, err := s.ListObservationRefs("koi-garden", "jira", 10, 0)
	if err != nil {
		t.Fatalf("ListObservationRefs(jira): %v", err)
	}
	if onlyJira.Total != 1 || onlyJira.Items[0].Ref != "KOI-1099" {
		t.Fatalf("jira page = %+v, want only the ticket", onlyJira)
	}
	if onlyJira.Items[0].GraphCommit != "" {
		t.Fatalf("a jira ref carries no graph commit: %+v", onlyJira.Items[0])
	}
}

// TestListObservationRefsOfAProjectWithNoneIsAnEmptyPage pins that "nothing
// linked" is an answer, not a failure.
func TestListObservationRefsOfAProjectWithNoneIsAnEmptyPage(t *testing.T) {
	s := newTestStore(t)

	page, err := s.ListObservationRefs("koi-garden", "graph", 10, 0)
	if err != nil {
		t.Fatalf("ListObservationRefs: %v", err)
	}
	if page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("page = %+v, want an empty one", page)
	}
}

// TestLinkTaskObservationRejectsAShortGraphCommit pins the refusal that used to
// be a silent drop: observation_refs.graph_commit is CHECKed at exactly 40
// characters, and INSERT OR IGNORE skipped the offending row without a word, so
// the caller was told the link succeeded while the reference it asked for was
// never written.
func TestLinkTaskObservationRejectsAShortGraphCommit(t *testing.T) {
	s := newTestStore(t)
	id := seedLinkedObservation(t, s, "koi-garden", "the pond lookup", "", "")
	task := seedTask(t, s, "koi-garden", UpsertTaskParams{JiraKey: strPtr("KOI-1099"), Title: strPtr("Lookup")})

	short := "1111111"
	_, err := s.LinkTaskObservation(LinkTaskObservationParams{
		Task: task, ObservationID: id, GraphRef: strPtr("pkg/pond.Lookup"), GraphCommit: &short,
	})
	if !errors.Is(err, ErrGraphCommitNotFullSHA) {
		t.Fatalf("LinkTaskObservation(short sha) = %v, want ErrGraphCommitNotFullSHA", err)
	}

	// Nothing was written: not the ref, and not the link either.
	refs, err := s.ListObservationRefs("koi-garden", "", 10, 0)
	if err != nil {
		t.Fatalf("ListObservationRefs: %v", err)
	}
	if refs.Total != 0 {
		t.Fatalf("refs = %+v, want none after a rejected link", refs.Items)
	}
	counts, err := s.TaskCounts(task.ID)
	if err != nil {
		t.Fatalf("TaskCounts: %v", err)
	}
	if counts.Observations != 0 {
		t.Fatalf("task holds %d observations, want none after a rejected link", counts.Observations)
	}

	// The full form is accepted, and writes both rows.
	full := "1111111111111111111111111111111111111111"
	res, err := s.LinkTaskObservation(LinkTaskObservationParams{
		Task: task, ObservationID: id, GraphRef: strPtr("pkg/pond.Lookup"), GraphCommit: &full,
	})
	if err != nil {
		t.Fatalf("LinkTaskObservation(full sha): %v", err)
	}
	if !res.Linked || res.RefsAdded != 1 {
		t.Fatalf("result = %+v, want one link and one ref", res)
	}
}
