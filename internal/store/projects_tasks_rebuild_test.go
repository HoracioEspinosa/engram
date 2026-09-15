package store

import (
	"errors"
	"testing"
)

// seedVersionTwoProjectsSchema brings a database to the version-2 shape of the
// engram-projects schema with the two additive steps already recorded, so the
// next migrateProjects has exactly the rebuilds left to do.
func seedVersionTwoProjectsSchema(t *testing.T, s *Store) {
	t.Helper()
	if err := s.ensureMigrationLedger(); err != nil {
		t.Fatalf("ensureMigrationLedger: %v", err)
	}
	if _, err := s.db.Exec(projectsSchemaDDL); err != nil {
		t.Fatalf("apply version-2 schema: %v", err)
	}
	for _, step := range []struct {
		id  string
		ddl string
	}{
		{projCardsHierarchyID, projectsHierarchyDDL},
		{projAliasesID, projectAliasesDDL},
	} {
		if _, err := s.db.Exec(step.ddl); err != nil {
			t.Fatalf("apply %s: %v", step.id, err)
		}
		if err := s.recordMigration(step.id); err != nil {
			t.Fatalf("record %s: %v", step.id, err)
		}
	}
}

// TestTasksRebuildPreservesRows pins the promise of a rebuild: every row that
// existed before it exists afterwards, with the same values, and the columns
// the new shape adds arrive empty rather than invented.
func TestTasksRebuildPreservesRows(t *testing.T) {
	dir := t.TempDir()
	s := openMigrationTestStore(t, dir, defaultStoreHooks())
	seedVersionTwoProjectsSchema(t, s)

	if _, err := s.db.Exec(`
		INSERT INTO project_cards (slug, sync_id, display_name, created_at, updated_at)
		VALUES ('acme', 'proj-0000000000000001', 'Acme', '2026-01-01 00:00:00', '2026-01-01 00:00:00');
		INSERT INTO tasks (sync_id, project, jira_key, title, kind, state, branch, created_at, updated_at)
		VALUES ('task-0000000000000001', 'acme', 'CDBS-1', 'First', 'feature', 'in_progress', 'feat/one',
		        '2026-01-02 00:00:00', '2026-01-02 00:00:00');
		INSERT INTO tasks (sync_id, project, sdd_change, title, kind, state, closed_at, created_at, updated_at)
		VALUES ('task-0000000000000002', 'acme', 'some-change', 'Second', 'spike', 'done',
		        '2026-01-03 00:00:00', '2026-01-03 00:00:00', '2026-01-03 00:00:00');
	`); err != nil {
		t.Fatalf("seed version-2 rows: %v", err)
	}

	// Columns are read as one joined string so a comparison is about values,
	// not about which pointers a scan happened to allocate.
	read := func() []string {
		t.Helper()
		rows, err := s.db.Query(`
			SELECT id || '|' || sync_id || '|' || project || '|' || title || '|' || kind || '|' || state
			       || '|' || COALESCE(jira_key, '-') || '|' || COALESCE(sdd_change, '-')
			       || '|' || COALESCE(branch, '-') || '|' || COALESCE(closed_at, '-')
			       || '|' || created_at || '|' || updated_at
			FROM tasks ORDER BY id`)
		if err != nil {
			t.Fatalf("read tasks: %v", err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var r string
			if err := rows.Scan(&r); err != nil {
				t.Fatalf("scan task: %v", err)
			}
			out = append(out, r)
		}
		return out
	}
	before := read()
	if len(before) != 2 {
		t.Fatalf("seeded %d rows, want 2", len(before))
	}

	if err := s.migrateProjects(); err != nil {
		t.Fatalf("migrateProjects: %v", err)
	}

	after := read()
	if len(after) != len(before) {
		t.Fatalf("rows after the rebuild: %d, want %d", len(after), len(before))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("row %d changed:\n before %s\n after  %s", i, before[i], after[i])
		}
	}

	// The new columns arrive empty.
	task, err := s.getTaskByID(1)
	if err != nil {
		t.Fatalf("getTaskByID: %v", err)
	}
	if task.Slug != nil || task.Summary != nil || task.PendingNote != nil ||
		task.VaultPath != nil || task.ParentTaskID != nil || task.ParentTaskSyncID != nil {
		t.Fatalf("a migrated task must not gain invented values: %+v", task)
	}

	// The full-text index still finds what it indexed before the rebuild.
	items, total, err := s.ListTasks("acme", TaskListFilter{Query: "First"})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].SyncID != "task-0000000000000001" {
		t.Fatalf("the rebuilt index lost the task: total=%d items=%+v", total, items)
	}

	// The backup the rebuild takes before touching anything is on disk.
	backup := dir + "/engram.db.pre-" + projTasksRebuildID + ".bak"
	if _, err := openDB("sqlite", backup); err != nil {
		t.Fatalf("the rebuild must leave a backup at %s: %v", backup, err)
	}
}

func TestTaskAcceptsPendingArchivedUnverified(t *testing.T) {
	s := newTestStore(t)
	for _, state := range []string{"pending", "archived", "unverified"} {
		res, err := s.UpsertTask(UpsertTaskParams{
			Project: "acme",
			Slug:    strPtr("task-" + state),
			Title:   strPtr("Task " + state),
			Kind:    strPtr("feature"),
			State:   strPtr(state),
		})
		if err != nil {
			t.Fatalf("UpsertTask(%s): %v", state, err)
		}
		if res.Task.State != state {
			t.Fatalf("state %q, want %q", res.Task.State, state)
		}
		// Only a closed state may carry a closing time.
		if state == "archived" && res.Task.ClosedAt == nil {
			t.Fatal("an archived task is closed and must carry closed_at")
		}
		if state != "archived" && res.Task.ClosedAt != nil {
			t.Fatalf("%s must not carry closed_at: %v", state, *res.Task.ClosedAt)
		}
	}
}

func TestTaskSlugUniquePerProject(t *testing.T) {
	s := newTestStore(t)
	mk := func(project, slug, title string) error {
		_, err := s.UpsertTask(UpsertTaskParams{
			Project: project,
			Slug:    strPtr(slug),
			Title:   strPtr(title),
			Kind:    strPtr("feature"),
		})
		return err
	}
	if err := mk("acme", "hardening", "Hardening"); err != nil {
		t.Fatalf("first task: %v", err)
	}
	// The same slug in another project is a different task.
	if err := mk("other", "hardening", "Hardening elsewhere"); err != nil {
		t.Fatalf("same slug in another project: %v", err)
	}

	// Two rows with the same (project, slug) cannot coexist, even when the
	// upsert is bypassed.
	if _, err := s.DB().Exec(`
		INSERT INTO tasks (sync_id, project, slug, title, kind, state, created_at, updated_at)
		VALUES ('task-000000000000dead', 'acme', 'hardening', 'Duplicate', 'feature', 'open',
		        datetime('now'), datetime('now'))`); err == nil {
		t.Fatal("a second task with the same slug in one project must be refused")
	}
}

func TestActiveTasksExcludeArchived(t *testing.T) {
	s := newTestStore(t)
	states := []string{"open", "pending", "unverified", "done", "cancelled", "archived"}
	for _, state := range states {
		if _, err := s.UpsertTask(UpsertTaskParams{
			Project: "acme",
			Slug:    strPtr("task-" + state),
			Title:   strPtr("Task " + state),
			Kind:    strPtr("feature"),
			State:   strPtr(state),
		}); err != nil {
			t.Fatalf("UpsertTask(%s): %v", state, err)
		}
	}

	items, total, err := s.ListTasks("acme", TaskListFilter{})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if total != 3 {
		t.Fatalf("active tasks %d, want 3 (open, pending, unverified)", total)
	}
	for _, item := range items {
		if item.State == "archived" || item.State == "done" || item.State == "cancelled" {
			t.Fatalf("closed state %q appeared in the active list", item.State)
		}
	}

	withArchived, total, err := s.ListTasks("acme", TaskListFilter{IncludeArchived: true})
	if err != nil {
		t.Fatalf("ListTasks(IncludeArchived): %v", err)
	}
	if total != 4 {
		t.Fatalf("active plus archived %d, want 4; got %+v", total, withArchived)
	}

	_, total, err = s.ListTasks("acme", TaskListFilter{States: []string{"done", "archived"}})
	if err != nil {
		t.Fatalf("ListTasks(States): %v", err)
	}
	if total != 2 {
		t.Fatalf("explicit states %d, want 2", total)
	}

	counts, err := s.ProjectCardCounts("acme")
	if err != nil {
		t.Fatalf("ProjectCardCounts: %v", err)
	}
	if counts.TasksActive != 3 || counts.TasksTotal != len(states) {
		t.Fatalf("counts %+v, want 3 active of %d", counts, len(states))
	}
}

func TestTaskUpsertResolvesBySlug(t *testing.T) {
	s := newTestStore(t)
	first, err := s.UpsertTask(UpsertTaskParams{
		Project: "acme",
		Slug:    strPtr("Hardening-Autologin "),
		Title:   strPtr("Hardening"),
		Kind:    strPtr("feature"),
	})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if first.Task.Slug == nil || *first.Task.Slug != "hardening-autologin" {
		t.Fatalf("slug %v, want hardening-autologin", first.Task.Slug)
	}

	second, err := s.UpsertTask(UpsertTaskParams{
		Project:     "acme",
		Slug:        strPtr("hardening-autologin"),
		Summary:     strPtr("What the task is about"),
		PendingNote: strPtr("the rollback script is untested"),
		VaultPath:   strPtr("acme/hardening-autologin"),
		State:       strPtr("pending"),
	})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if second.Created {
		t.Fatal("a known slug must update the task, not create a second one")
	}
	if second.Task.ID != first.Task.ID {
		t.Fatalf("task id %d, want %d", second.Task.ID, first.Task.ID)
	}
	if second.Task.Summary == nil || *second.Task.Summary != "What the task is about" {
		t.Fatalf("summary %v", second.Task.Summary)
	}
	if second.Task.PendingNote == nil || *second.Task.PendingNote != "the rollback script is untested" {
		t.Fatalf("pending_note %v", second.Task.PendingNote)
	}
	if second.Task.VaultPath == nil || *second.Task.VaultPath != "acme/hardening-autologin" {
		t.Fatalf("vault_path %v", second.Task.VaultPath)
	}

	// The slug is the weakest identity: a sync_id in the same call still
	// decides which row is meant, and the slug follows it.
	renamed, err := s.UpsertTask(UpsertTaskParams{
		Project: "acme",
		SyncID:  strPtr(first.Task.SyncID),
		Slug:    strPtr("renamed-task"),
	})
	if err != nil {
		t.Fatalf("sync_id upsert: %v", err)
	}
	if renamed.Created || renamed.Task.ID != first.Task.ID {
		t.Fatalf("a sync_id must resolve the same task: %+v", renamed)
	}
	if renamed.Task.Slug == nil || *renamed.Task.Slug != "renamed-task" {
		t.Fatalf("slug %v, want renamed-task", renamed.Task.Slug)
	}
}

func TestTaskRejectsSelfParent(t *testing.T) {
	s := newTestStore(t)
	parent, err := s.UpsertTask(UpsertTaskParams{
		Project: "acme",
		Slug:    strPtr("umbrella"),
		Title:   strPtr("Umbrella"),
		Kind:    strPtr("feature"),
	})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}

	if _, err := s.UpsertTask(UpsertTaskParams{
		Project:    "acme",
		Slug:       strPtr("umbrella"),
		ParentTask: strPtr(parent.Task.SyncID),
	}); !errors.Is(err, ErrTaskSelfParent) {
		t.Fatalf("want ErrTaskSelfParent, got %v", err)
	}

	// The rejection leaves the task alone.
	reloaded, err := s.getTaskByID(parent.Task.ID)
	if err != nil {
		t.Fatalf("getTaskByID: %v", err)
	}
	if reloaded.ParentTaskID != nil {
		t.Fatalf("parent_task_id %v, want nil", *reloaded.ParentTaskID)
	}

	// A real parent is accepted and carries both identities.
	child, err := s.UpsertTask(UpsertTaskParams{
		Project:    "acme",
		Slug:       strPtr("child"),
		Title:      strPtr("Child"),
		Kind:       strPtr("feature"),
		ParentTask: strPtr(parent.Task.SyncID),
	})
	if err != nil {
		t.Fatalf("UpsertTask(child): %v", err)
	}
	if child.Task.ParentTaskID == nil || *child.Task.ParentTaskID != parent.Task.ID {
		t.Fatalf("parent_task_id %v", child.Task.ParentTaskID)
	}
	if child.Task.ParentTaskSyncID == nil || *child.Task.ParentTaskSyncID != parent.Task.SyncID {
		t.Fatalf("parent_task_sync_id %v", child.Task.ParentTaskSyncID)
	}

	// The CHECK stands even when the upsert is bypassed.
	if _, err := s.DB().Exec(
		`UPDATE tasks SET parent_task_id = id WHERE id = ?`, parent.Task.ID); err == nil {
		t.Fatal("a task must not be able to point at itself through raw SQL")
	}
}
