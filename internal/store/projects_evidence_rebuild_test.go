package store

import (
	"strings"
	"testing"
)

func newTaskForEvidence(t *testing.T, s *Store, project, slug string) Task {
	t.Helper()
	res, err := s.UpsertTask(UpsertTaskParams{
		Project: project,
		Slug:    strPtr(slug),
		Title:   strPtr(slug),
		Kind:    strPtr("feature"),
	})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	return res.Task
}

func hash(seed string) string {
	out := make([]byte, 0, 64)
	for len(out) < 64 {
		for _, c := range seed {
			if len(out) == 64 {
				break
			}
			out = append(out, "0123456789abcdef"[int(c)%16])
		}
	}
	return string(out)
}

// TestEvidenceRebuildBackfillsCategory pins that captures written before the
// column existed keep their rows and land in the category the capture flow has
// always used, rather than being guessed at from their paths.
func TestEvidenceRebuildBackfillsCategory(t *testing.T) {
	dir := t.TempDir()
	s := openMigrationTestStore(t, dir, defaultStoreHooks())
	seedVersionTwoProjectsSchema(t, s)

	if _, err := s.db.Exec(`
		INSERT INTO project_cards (slug, sync_id, display_name, created_at, updated_at)
		VALUES ('acme', 'proj-0000000000000001', 'Acme', '2026-01-01 00:00:00', '2026-01-01 00:00:00');
		INSERT INTO tasks (sync_id, project, jira_key, title, kind, state, created_at, updated_at)
		VALUES ('task-0000000000000001', 'acme', 'CDBS-1', 'First', 'feature', 'open',
		        '2026-01-02 00:00:00', '2026-01-02 00:00:00');
		INSERT INTO evidence (sync_id, project, task_id, task_sync_id, path, sha256, kind, proves,
		                      captured_at, created_at)
		VALUES ('evd-0000000000000001', 'acme', 1, 'task-0000000000000001',
		        'acme/first/evidences/login.png', '` + hash("login") + `', 'png', 'login works',
		        '2026-01-04 00:00:00', '2026-01-04 00:00:00');
	`); err != nil {
		t.Fatalf("seed version-2 evidence: %v", err)
	}

	if err := s.migrateProjects(); err != nil {
		t.Fatalf("migrateProjects: %v", err)
	}

	items, total, _, err := s.ListEvidence("acme", EvidenceListFilter{})
	if err != nil {
		t.Fatalf("ListEvidence: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("evidence after the rebuild: %d rows, want 1", total)
	}
	if items[0].Category != DefaultEvidenceCategory {
		t.Fatalf("category %q, want %q", items[0].Category, DefaultEvidenceCategory)
	}
	if items[0].Path != "acme/first/evidences/login.png" || items[0].Proves != "login works" {
		t.Fatalf("the rebuild altered a row: %+v", items[0])
	}

	// The index the rebuild created can find what was already there.
	_, found, _, err := s.ListEvidence("acme", EvidenceListFilter{Query: "login"})
	if err != nil {
		t.Fatalf("ListEvidence(Query): %v", err)
	}
	if found != 1 {
		t.Fatalf("full-text hits %d, want 1", found)
	}

	backup := dir + "/engram.db.pre-" + projEvidenceRebuildID + ".bak"
	if _, err := openDB("sqlite", backup); err != nil {
		t.Fatalf("the rebuild must leave a backup at %s: %v", backup, err)
	}
}

func TestEvidenceKindOtherAccepted(t *testing.T) {
	s := newTestStore(t)
	task := newTaskForEvidence(t, s, "acme", "capture")

	for _, kind := range []string{"webp", "har", "patch", "md", "other"} {
		if _, _, _, err := s.AddEvidence(AddEvidenceParams{
			Task:   task,
			Path:   "acme/capture/evidences/" + kind + ".file",
			SHA256: hash(kind),
			Kind:   kind,
			Proves: "a capture of kind " + kind,
		}); err != nil {
			t.Fatalf("AddEvidence(%s): %v", kind, err)
		}
	}

	if _, _, _, err := s.AddEvidence(AddEvidenceParams{
		Task:   task,
		Path:   "acme/capture/evidences/nope.file",
		SHA256: hash("nope"),
		Kind:   "docx",
		Proves: "an unknown kind",
	}); err == nil {
		t.Fatal("a kind outside the closed list must be refused")
	}
}

// TestAddEvidenceRelocatesMovedFile pins that the same bytes found in a new
// place are the same evidence: the row follows the file instead of the store
// keeping a path that no longer exists and adding a second row beside it.
func TestAddEvidenceRelocatesMovedFile(t *testing.T) {
	s := newTestStore(t)
	task := newTaskForEvidence(t, s, "acme", "capture")
	sum := hash("moved")

	first, duplicate, _, err := s.AddEvidence(AddEvidenceParams{
		Task:   task,
		Path:   "acme/capture/evidences/before.png",
		SHA256: sum,
		Kind:   "png",
		Proves: "the screen after login",
	})
	if err != nil {
		t.Fatalf("AddEvidence: %v", err)
	}
	if duplicate {
		t.Fatal("the first capture is not a duplicate")
	}
	if first.Category != DefaultEvidenceCategory {
		t.Fatalf("category %q, want the default", first.Category)
	}

	moved, duplicate, _, err := s.AddEvidence(AddEvidenceParams{
		Task:     task,
		Path:     "acme/capture/evidences-qa/after.png",
		SHA256:   sum,
		Category: "evidences-qa",
		Kind:     "png",
		Proves:   "the screen after login",
	})
	if err != nil {
		t.Fatalf("AddEvidence(moved): %v", err)
	}
	if !duplicate {
		t.Fatal("the same bytes must be recognised as the same evidence")
	}
	if moved.ID != first.ID {
		t.Fatalf("a moved file must keep its row: %d vs %d", moved.ID, first.ID)
	}
	if moved.Path != "acme/capture/evidences-qa/after.png" || moved.Category != "evidences-qa" {
		t.Fatalf("the row did not follow the file: %+v", moved)
	}

	_, total, _, err := s.ListEvidence("acme", EvidenceListFilter{})
	if err != nil {
		t.Fatalf("ListEvidence: %v", err)
	}
	if total != 1 {
		t.Fatalf("evidence rows %d, want 1", total)
	}
}

func TestListEvidenceFiltersByCategoryAndQuery(t *testing.T) {
	s := newTestStore(t)
	task := newTaskForEvidence(t, s, "acme", "capture")

	add := func(path, category, kind, proves string) {
		t.Helper()
		if _, _, _, err := s.AddEvidence(AddEvidenceParams{
			Task:     task,
			Path:     path,
			SHA256:   hash(path),
			Category: category,
			Kind:     kind,
			Proves:   proves,
		}); err != nil {
			t.Fatalf("AddEvidence(%s): %v", path, err)
		}
	}
	add("acme/capture/benchmarks/run1.json", "benchmarks", "json", "throughput before the change")
	add("acme/capture/benchmarks/run2.json", "benchmarks", "json", "throughput after the change")
	add("acme/capture/patches/fix.diff", "patches", "diff", "the change itself")

	_, total, _, err := s.ListEvidence("acme", EvidenceListFilter{Category: "benchmarks"})
	if err != nil {
		t.Fatalf("ListEvidence(Category): %v", err)
	}
	if total != 2 {
		t.Fatalf("benchmarks %d, want 2", total)
	}

	items, total, _, err := s.ListEvidence("acme", EvidenceListFilter{Query: "throughput"})
	if err != nil {
		t.Fatalf("ListEvidence(Query): %v", err)
	}
	if total != 2 {
		t.Fatalf("query hits %d, want 2", total)
	}
	for _, item := range items {
		if !strings.Contains(item.Proves, "throughput") {
			t.Fatalf("unexpected hit %+v", item)
		}
	}

	_, total, _, err = s.ListEvidence("acme", EvidenceListFilter{Category: "patches", Query: "change"})
	if err != nil {
		t.Fatalf("ListEvidence(Category+Query): %v", err)
	}
	if total != 1 {
		t.Fatalf("category and query together %d, want 1", total)
	}
}
