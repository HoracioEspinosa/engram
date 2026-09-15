package main

import (
	"testing"

	projectpkg "github.com/HoracioEspinosa/engram/internal/project"
	"github.com/HoracioEspinosa/engram/internal/store"
)

// TestCmdSaveWithoutProjectUsesAProjectScopedSession pins `engram save` to one
// manual-save session per project.
//
// Without --project the command used to write the bare "manual-save" id: a
// single row shared by every project on the machine, owned by whichever project
// stamped it first. Observations of every other project then cited a session
// the cloud indexes under someone else, and the per-project session index has
// no way to express that.
func TestCmdSaveWithoutProjectUsesAProjectScopedSession(t *testing.T) {
	cfg := testConfig(t)

	origDetect := detectProjectFull
	detectProjectFull = func(string) projectpkg.DetectionResult {
		return projectpkg.DetectionResult{Project: "detected-project", Source: projectpkg.SourceGitRoot}
	}
	t.Cleanup(func() { detectProjectFull = origDetect })

	withArgs(t, "engram", "save", "no-project-title", "no-project-content")
	if _, stderr := captureOutput(t, func() { cmdSave(cfg) }); stderr != "" {
		t.Fatalf("expected no stderr, got %q", stderr)
	}

	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	var shared int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = 'manual-save'`).Scan(&shared); err != nil {
		t.Fatalf("count shared session: %v", err)
	}
	if shared != 0 {
		t.Fatal("a manual save must not create the project-less manual-save session shared by every project")
	}

	var scoped int
	if err := s.DB().QueryRow(
		`SELECT COUNT(*) FROM sessions WHERE id = ?`, "manual-save-detected-project",
	).Scan(&scoped); err != nil {
		t.Fatalf("count scoped session: %v", err)
	}
	if scoped != 1 {
		t.Fatalf("expected the manual save to land on manual-save-detected-project, got %d row(s)", scoped)
	}
}

// TestCmdSaveKeepsExplicitProjectSessionID proves the detection fallback only
// fills the gap: an explicit --project still decides the session id, so the
// rows an earlier save already created keep receiving new observations.
func TestCmdSaveKeepsExplicitProjectSessionID(t *testing.T) {
	cfg := testConfig(t)

	origDetect := detectProjectFull
	detectProjectFull = func(string) projectpkg.DetectionResult {
		return projectpkg.DetectionResult{Project: "detected-project", Source: projectpkg.SourceGitRoot}
	}
	t.Cleanup(func() { detectProjectFull = origDetect })

	withArgs(t, "engram", "save", "explicit-title", "explicit-content", "--project", "alpha")
	if _, stderr := captureOutput(t, func() { cmdSave(cfg) }); stderr != "" {
		t.Fatalf("expected no stderr, got %q", stderr)
	}

	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	var scoped int
	if err := s.DB().QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = ?`, "manual-save-alpha").Scan(&scoped); err != nil {
		t.Fatalf("count explicit session: %v", err)
	}
	if scoped != 1 {
		t.Fatalf("expected manual-save-alpha, got %d row(s)", scoped)
	}
}
