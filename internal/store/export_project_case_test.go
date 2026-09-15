package store

import "testing"

// TestExportProjectFindsRowsWrittenUnderAnotherCase pins the project filter of
// the per-project export to the same folded rule enrollment already follows.
// Enrollment stores the slug in lower case while sessions, observations and
// prompts keep whatever case they were written with, so an exact comparison
// returns no rows at all for a project named with capitals: the typed
// collections of the push chunk come out empty, the referential closure that
// attaches the sessions an observation cites has nothing to walk, and the
// server rejects the chunk for citing a session it was never sent.
func TestExportProjectFindsRowsWrittenUnderAnotherCase(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.db.Exec(
		`INSERT INTO sessions (id, project, directory, started_at) VALUES (?, ?, ?, datetime('now'))`,
		"sess-mixed", "Gentleman.Dots", "/tmp/gentleman",
	); err != nil {
		t.Fatalf("insert mixed-case session: %v", err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO observations (sync_id, session_id, type, title, content, project, scope)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"obs-mixed", "sess-mixed", "note", "mixed", "written before normalization", "Gentleman.Dots", "project",
	); err != nil {
		t.Fatalf("insert mixed-case observation: %v", err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO user_prompts (sync_id, session_id, content, project) VALUES (?, ?, ?, ?)`,
		"prompt-mixed", "sess-mixed", "mixed prompt", "Gentleman.Dots",
	); err != nil {
		t.Fatalf("insert mixed-case prompt: %v", err)
	}

	data, err := s.ExportProject("gentleman.dots")
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}
	if len(data.Sessions) != 1 || data.Sessions[0].ID != "sess-mixed" {
		t.Fatalf("expected the mixed-case session in the export, got %+v", data.Sessions)
	}
	if len(data.Observations) != 1 || data.Observations[0].SyncID != "obs-mixed" {
		t.Fatalf("expected the mixed-case observation in the export, got %d row(s)", len(data.Observations))
	}
	if len(data.Prompts) != 1 || data.Prompts[0].SyncID != "prompt-mixed" {
		t.Fatalf("expected the mixed-case prompt in the export, got %d row(s)", len(data.Prompts))
	}
}

// TestExportProjectStillScopesToTheRequestedProjectWhenFolded proves the folded
// comparison did not widen the scope: a project whose name only differs by case
// is the same project, but a different project stays out of the export.
func TestExportProjectStillScopesToTheRequestedProjectWhenFolded(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("sess-other", "proj-other", "/tmp/other"); err != nil {
		t.Fatalf("create other session: %v", err)
	}
	if _, err := s.AddObservation(AddObservationParams{
		SessionID: "sess-other", Type: "note", Title: "other", Content: "other", Project: "proj-other", Scope: "project",
	}); err != nil {
		t.Fatalf("add other observation: %v", err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO sessions (id, project, directory, started_at) VALUES (?, ?, ?, datetime('now'))`,
		"sess-target", "Proj-Target", "/tmp/target",
	); err != nil {
		t.Fatalf("insert target session: %v", err)
	}

	data, err := s.ExportProject("proj-target")
	if err != nil {
		t.Fatalf("ExportProject: %v", err)
	}
	for _, sess := range data.Sessions {
		if sess.ID == "sess-other" {
			t.Fatalf("export leaked a session owned by another project: %+v", data.Sessions)
		}
	}
	if len(data.Observations) != 0 {
		t.Fatalf("export leaked observations owned by another project: %d row(s)", len(data.Observations))
	}
}
