package cloudserver

import (
	"net/http/httptest"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/cloud/remote"
	"github.com/HoracioEspinosa/engram/internal/store"
	engramsync "github.com/HoracioEspinosa/engram/internal/sync"
)

// TestPushAcceptsChunkExportedFromMixedCaseProject drives the whole push path —
// local store, per-project export, chunk build, HTTP push, server validation —
// for a project whose rows were written with capitals and whose enrollment slug
// is lower case.
//
// The export used to compare the project column for exact equality, so the
// typed collections of the chunk came out empty while the mutation selector
// (which folds the name) still picked the rows. The chunk then carried
// observation mutations citing a session the chunk did not include, and the
// server answered 400 "references missing session_id".
func TestPushAcceptsChunkExportedFromMixedCaseProject(t *testing.T) {
	st := newPushTestStore(t)

	// The shared manual-save session belongs to another project, so it travels
	// only through the referential closure the typed collections feed.
	if _, err := st.DB().Exec(
		`INSERT INTO sessions (id, project, directory, started_at) VALUES (?, ?, ?, datetime('now'))`,
		"manual-save", "other-project", "/tmp/other",
	); err != nil {
		t.Fatalf("insert shared session: %v", err)
	}
	if _, err := st.DB().Exec(
		`INSERT INTO observations (sync_id, session_id, type, title, content, project, scope)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"obs-mixed", "manual-save", "note", "mixed", "row written before normalization", "Gentleman.Dots", "project",
	); err != nil {
		t.Fatalf("insert mixed-case observation: %v", err)
	}
	// Enrollment normalizes the slug to lower case and journals the rows that
	// predate it, so the push has pending mutations to carry.
	if err := st.EnrollProject("Gentleman.Dots"); err != nil {
		t.Fatalf("enroll project: %v", err)
	}

	// A server that knows nothing about this project yet: every session the
	// chunk cites has to travel inside the chunk itself.
	srv := httptest.NewServer(New(&fakeStore{}, fakeAuth{}, 0).Handler())
	t.Cleanup(srv.Close)

	transport, err := remote.NewRemoteTransport(srv.URL, "test-token", "gentleman.dots")
	if err != nil {
		t.Fatalf("new remote transport: %v", err)
	}
	syncer := engramsync.NewCloudWithTransport(st, transport, "gentleman.dots")

	result, err := syncer.Export("tester", "gentleman.dots")
	if err != nil {
		t.Fatalf("push of a mixed-case project must be accepted: %v", err)
	}
	if result.IsEmpty {
		t.Fatal("expected the mixed-case project to export rows, got an empty push")
	}
}

func newPushTestStore(t *testing.T) *store.Store {
	t.Helper()
	cfg, err := store.DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig: %v", err)
	}
	cfg.DataDir = t.TempDir()
	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
