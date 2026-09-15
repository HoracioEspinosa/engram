package sync

import (
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// TestCloudChunkLeavesSoftDeletedObservationsOutOfTypedCollections pins the
// typed collections of a push chunk to rows the cloud can still upsert.
//
// A soft delete keeps the row so the deletion can replicate, and the deletion
// itself travels as its own `delete` mutation. Carrying the tombstone in the
// `observations` array on top of that asks the cloud to upsert a row the
// deletion no longer guarantees anything about: a row deleted precisely because
// it had no title answers `400 invalid push payload: observations[N].title is
// required` and takes the whole project's push down with it.
func TestCloudChunkLeavesSoftDeletedObservationsOutOfTypedCollections(t *testing.T) {
	resetSyncTestHooks(t)
	s := newTestStore(t)
	sy := NewCloudWithTransport(s, newFakeCloudTransport(), "proj-a")

	if err := s.CreateSession("sess-a", "proj-a", "/tmp/proj-a"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	// A row that predates the title requirement, written straight into the
	// table the way an import or an older client leaves one. Enrollment skips
	// it in the backfill — the cloud upsert contract rejects it — so the only
	// mutation it ever gets is the delete below.
	if _, err := s.DB().Exec(
		`INSERT INTO observations (sync_id, session_id, type, title, content, project, scope)
		 VALUES ('obs-empty', 'sess-a', 'note', '', '', 'proj-a', 'project')`,
	); err != nil {
		t.Fatalf("insert titleless observation: %v", err)
	}
	if err := s.EnrollProject("proj-a"); err != nil {
		t.Fatalf("enroll project: %v", err)
	}
	liveID, err := s.AddObservation(store.AddObservationParams{
		SessionID: "sess-a",
		Type:      "note",
		Title:     "still here",
		Content:   "a live row has to keep travelling",
		Project:   "proj-a",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add live observation: %v", err)
	}
	liveSyncID := observationSyncID(t, s, liveID)

	var deletedID int64
	if err := s.DB().QueryRow(`SELECT id FROM observations WHERE sync_id = 'obs-empty'`).Scan(&deletedID); err != nil {
		t.Fatalf("read titleless observation id: %v", err)
	}
	if err := s.DeleteObservation(deletedID, false); err != nil {
		t.Fatalf("soft-delete observation: %v", err)
	}

	data, err := s.ExportProject("proj-a")
	if err != nil {
		t.Fatalf("export project: %v", err)
	}
	chunk, _, err := sy.filterByPendingMutations(data, "proj-a")
	if err != nil {
		t.Fatalf("build chunk: %v", err)
	}

	liveTravels := false
	for _, observation := range chunk.Observations {
		if observation.SyncID == "obs-empty" {
			t.Fatalf("the typed collections carry the soft-deleted observation; the cloud rejects the whole chunk over it")
		}
		if observation.SyncID == liveSyncID {
			liveTravels = true
		}
	}
	if !liveTravels {
		t.Fatalf("the live observation must still travel in the typed collections; got %d observation(s)", len(chunk.Observations))
	}

	deleteTravels := false
	for _, mutation := range chunk.Mutations {
		if mutation.Entity == store.SyncEntityObservation &&
			mutation.EntityKey == "obs-empty" &&
			mutation.Op == store.SyncOpDelete {
			deleteTravels = true
		}
	}
	if !deleteTravels {
		t.Fatal("the deletion must still reach the cloud as a delete mutation")
	}
}

// TestLocalChunkStillCarriesSoftDeletedObservations pins the other half of the
// rule: the exclusion belongs to the cloud chunk, not to every chunk.
//
// A local filesystem chunk carries no delete mutations — only relations get
// one — so the tombstone in the typed collection IS the deletion signal, and
// the import path turns it back into a delete. Dropping it here would stop
// deletions from replicating between local checkouts.
func TestLocalChunkStillCarriesSoftDeletedObservations(t *testing.T) {
	resetSyncTestHooks(t)
	s := newTestStore(t)
	sy := New(s, t.TempDir())

	if err := s.CreateSession("sess-a", "proj-a", "/tmp/proj-a"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	createdID, err := s.AddObservation(store.AddObservationParams{
		SessionID: "sess-a",
		Type:      "note",
		Title:     "removed later",
		Content:   "a deletion has to reach the other checkout",
		Project:   "proj-a",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add observation: %v", err)
	}
	createdSyncID := observationSyncID(t, s, createdID)
	if err := s.DeleteObservation(createdID, false); err != nil {
		t.Fatalf("soft-delete observation: %v", err)
	}

	data, err := s.ExportProject("proj-a")
	if err != nil {
		t.Fatalf("export project: %v", err)
	}
	chunk := sy.filterNewData(data, "")

	for _, observation := range chunk.Observations {
		if observation.SyncID == createdSyncID {
			if observation.DeletedAt == nil {
				t.Fatal("the local chunk must carry the tombstone with its deleted_at")
			}
			return
		}
	}
	t.Fatalf("the local chunk dropped the tombstone, so the deletion never replicates; got %d observation(s)", len(chunk.Observations))
}

func observationSyncID(t *testing.T, s *store.Store, id int64) string {
	t.Helper()
	var syncID string
	if err := s.DB().QueryRow(`SELECT ifnull(sync_id, '') FROM observations WHERE id = ?`, id).Scan(&syncID); err != nil {
		t.Fatalf("read sync_id of observation %d: %v", id, err)
	}
	return syncID
}
