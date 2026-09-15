package cloudserver

import (
	"net/http/httptest"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/cloud/remote"
	engramsync "github.com/HoracioEspinosa/engram/internal/sync"
)

// TestPushAcceptsChunkWithASoftDeletedObservation drives the whole push path —
// local store, soft delete, per-project export, chunk build, HTTP push, server
// validation — for a project that deleted a row the cloud never accepted.
//
// The export selects observations without filtering `deleted_at`, so the
// tombstone entered the `observations` array of the chunk. The upsert contract
// requires a title and a deleted row that never had one cannot grow one, so the
// server answered `400 invalid push payload: observations[N].title is required`
// and every other row of the project stayed behind it. The deletion already
// travels as a `delete` mutation, so the tombstone has no business in the typed
// collections at all.
func TestPushAcceptsChunkWithASoftDeletedObservation(t *testing.T) {
	st := newPushTestStore(t)

	if err := st.CreateSession("sess-deleted", "proj-deleted", "/tmp/proj-deleted"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	// Written straight into the table, the way rows that predate the title
	// requirement exist: enrollment cannot journal it (the cloud rejects the
	// upsert), so its only mutation is the delete below.
	if _, err := st.DB().Exec(
		`INSERT INTO observations (sync_id, session_id, type, title, content, project, scope)
		 VALUES ('obs-titleless', 'sess-deleted', 'note', '', '', 'proj-deleted', 'project')`,
	); err != nil {
		t.Fatalf("insert titleless observation: %v", err)
	}
	if err := st.EnrollProject("proj-deleted"); err != nil {
		t.Fatalf("enroll project: %v", err)
	}

	var deletedID int64
	if err := st.DB().QueryRow(`SELECT id FROM observations WHERE sync_id = 'obs-titleless'`).Scan(&deletedID); err != nil {
		t.Fatalf("read titleless observation id: %v", err)
	}
	if err := st.DeleteObservation(deletedID, false); err != nil {
		t.Fatalf("soft-delete observation: %v", err)
	}

	srv := httptest.NewServer(New(&fakeStore{}, fakeAuth{}, 0).Handler())
	t.Cleanup(srv.Close)

	transport, err := remote.NewRemoteTransport(srv.URL, "test-token", "proj-deleted")
	if err != nil {
		t.Fatalf("new remote transport: %v", err)
	}
	syncer := engramsync.NewCloudWithTransport(st, transport, "proj-deleted")

	result, err := syncer.Export("tester", "proj-deleted")
	if err != nil {
		t.Fatalf("push of a project with a soft-deleted row must be accepted: %v", err)
	}
	if result.IsEmpty {
		t.Fatal("expected the deletion to export as a mutation, got an empty push")
	}
	if result.MutationsExported == 0 {
		t.Fatal("expected the delete mutation to travel in the chunk")
	}
}
