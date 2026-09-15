package sync

import (
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// TestChunkAttachesSessionsCitedOnlyByMutations pins the referential closure of
// a push chunk to the citations the cloud actually validates.
//
// The closure used to walk only the typed collections, so a session that the
// per-project export left out never entered the chunk. The shared manual-save
// fallback row is exactly that: it belongs to one project while observations of
// many others cite it, and the cloud's session index is per project, so the
// citation resolves nowhere and the server rejects the whole chunk.
func TestChunkAttachesSessionsCitedOnlyByMutations(t *testing.T) {
	resetSyncTestHooks(t)
	s := newTestStore(t)
	sy := NewCloudWithTransport(s, newFakeCloudTransport(), "proj-a")

	if err := s.EnrollProject("proj-a"); err != nil {
		t.Fatalf("enroll project: %v", err)
	}
	if err := s.CreateSession("manual-save", "proj-other", "/tmp/other"); err != nil {
		t.Fatalf("create shared session: %v", err)
	}
	if _, err := s.AddObservation(store.AddObservationParams{
		SessionID: "manual-save",
		Type:      "note",
		Title:     "cross-project",
		Content:   "observation of proj-a on a session owned by proj-other",
		Project:   "proj-a",
		Scope:     "project",
	}); err != nil {
		t.Fatalf("add cross-project observation: %v", err)
	}

	data, err := s.ExportProject("proj-a")
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}
	// The export is what the closure used to depend on. Drop the session from
	// it to stand in for every reason the two selectors can disagree — an
	// unexported row, a journal entry naming a project the row no longer
	// carries — and prove the chunk completes itself anyway.
	data.Sessions = nil

	chunk, seqs, err := sy.filterByPendingMutations(data, "proj-a")
	if err != nil {
		t.Fatalf("filterByPendingMutations: %v", err)
	}
	if len(seqs) == 0 {
		t.Fatal("expected pending mutations for the enrolled project")
	}

	found := false
	for _, session := range chunk.Sessions {
		if session.ID == "manual-save" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("chunk must carry every session its mutations cite; got %d session(s)", len(chunk.Sessions))
	}
}

// TestChunkDoesNotDuplicateSessionsAlreadyExported proves the completion step
// only adds what is missing: a session the export already provided must not be
// appended a second time, or the chunk would carry duplicate upserts.
func TestChunkDoesNotDuplicateSessionsAlreadyExported(t *testing.T) {
	resetSyncTestHooks(t)
	s := newTestStore(t)
	sy := NewCloudWithTransport(s, newFakeCloudTransport(), "proj-a")

	if err := s.EnrollProject("proj-a"); err != nil {
		t.Fatalf("enroll project: %v", err)
	}
	if err := s.CreateSession("sess-a", "proj-a", "/tmp/proj-a"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := s.AddObservation(store.AddObservationParams{
		SessionID: "sess-a", Type: "note", Title: "own", Content: "own", Project: "proj-a", Scope: "project",
	}); err != nil {
		t.Fatalf("add observation: %v", err)
	}

	data, err := s.ExportProject("proj-a")
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}
	chunk, _, err := sy.filterByPendingMutations(data, "proj-a")
	if err != nil {
		t.Fatalf("filterByPendingMutations: %v", err)
	}

	seen := map[string]int{}
	for _, session := range chunk.Sessions {
		seen[session.ID]++
	}
	if seen["sess-a"] != 1 {
		t.Fatalf("expected sess-a exactly once in the chunk, got %d", seen["sess-a"])
	}
}
