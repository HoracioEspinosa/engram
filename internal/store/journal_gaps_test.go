package store

import "testing"

// seedUnjournaledProject reproduces the shape the real database was found in: an
// enrolled project whose rows were written outside the sync journal, some of
// them complete enough for the cloud upsert contract and some not.
func seedUnjournaledProject(t *testing.T, s *Store, project string) {
	t.Helper()

	if _, err := s.db.Exec(`INSERT OR IGNORE INTO sync_enrolled_projects (project) VALUES (?)`, project); err != nil {
		t.Fatalf("enroll project: %v", err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO sessions (id, project, directory, started_at) VALUES (?, ?, ?, datetime('now'))`,
		"sess-gap", project, "/tmp/gap",
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	deliverable := []string{"o-live-1", "o-live-2", "o-live-3"}
	for _, syncID := range deliverable {
		if _, err := s.db.Exec(
			`INSERT INTO observations (sync_id, session_id, type, title, content, project, scope)
			 VALUES (?, ?, 'note', 'title', 'content', ?, 'project')`,
			syncID, "sess-gap", project,
		); err != nil {
			t.Fatalf("insert deliverable observation %s: %v", syncID, err)
		}
	}
	// A soft-deleted observation needs a delete mutation of its own and meets no
	// field contract at all.
	if _, err := s.db.Exec(
		`INSERT INTO observations (sync_id, session_id, type, title, content, project, scope, deleted_at)
		 VALUES ('o-deleted', 'sess-gap', 'note', 'title', 'content', ?, 'project', datetime('now'))`,
		project,
	); err != nil {
		t.Fatalf("insert deleted observation: %v", err)
	}
	// Rows the cloud upsert contract rejects: no backfill can deliver them.
	if _, err := s.db.Exec(
		`INSERT INTO observations (sync_id, session_id, type, title, content, project, scope)
		 VALUES ('o-no-scope', 'sess-gap', 'note', 'title', 'content', ?, '')`,
		project,
	); err != nil {
		t.Fatalf("insert scopeless observation: %v", err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO observations (sync_id, session_id, type, title, content, project, scope)
		 VALUES ('o-no-title', 'sess-gap', 'note', '', 'content', ?, 'project')`,
		project,
	); err != nil {
		t.Fatalf("insert titleless observation: %v", err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO user_prompts (sync_id, session_id, content, project) VALUES ('p-live', 'sess-gap', 'hello', ?)`,
		project,
	); err != nil {
		t.Fatalf("insert prompt: %v", err)
	}
}

// TestJournalGapsCountRowsThePushCannotSee pins the counter the upgrade doctor
// reports. A row with no sync_mutations entry never enters a push, and the
// pending-mutation counters say nothing about it: the real database reported
// zero pending mutations while 167 observations of enrolled projects had never
// been offered to the cloud at all.
func TestJournalGapsCountRowsThePushCannotSee(t *testing.T) {
	s := newTestStoreRaw(t)
	seedUnjournaledProject(t, s, "gap-project")

	report, err := s.ProjectJournalGaps("gap-project")
	if err != nil {
		t.Fatalf("ProjectJournalGaps: %v", err)
	}
	if report.Sessions != 1 {
		t.Errorf("expected 1 unjournaled session, got %d", report.Sessions)
	}
	if report.Observations != 4 {
		t.Errorf("expected 4 journalable observations (3 live + 1 deleted), got %d", report.Observations)
	}
	if report.Prompts != 1 {
		t.Errorf("expected 1 journalable prompt, got %d", report.Prompts)
	}
	if report.Blocked != 2 {
		t.Errorf("expected 2 rows the cloud upsert contract rejects, got %d", report.Blocked)
	}
	if report.Total() != 8 {
		t.Errorf("expected 8 rows with no journal entry, got %d", report.Total())
	}
}

// TestEnrollJournalsRowsOfAnAlreadyEnrolledProject pins the recovery path.
// Enrollment used to skip the backfill whenever the enrollment row already
// existed, so rows written outside the journal after the first enrollment had
// no command that could ever recover them.
func TestEnrollJournalsRowsOfAnAlreadyEnrolledProject(t *testing.T) {
	s := newTestStoreRaw(t)
	seedUnjournaledProject(t, s, "gap-project")

	if err := s.EnrollProject("Gap-Project"); err != nil {
		t.Fatalf("re-enroll project: %v", err)
	}

	report, err := s.ProjectJournalGaps("gap-project")
	if err != nil {
		t.Fatalf("ProjectJournalGaps: %v", err)
	}
	if report.Journalable() != 0 {
		t.Fatalf("enrollment must journal every deliverable row, %d left (%s)", report.Journalable(), report.Summary())
	}
	if report.Blocked != 2 {
		t.Fatalf("expected the 2 undeliverable rows to remain counted, got %d", report.Blocked)
	}

	for _, syncID := range []string{"o-live-1", "o-live-2", "o-live-3", "o-deleted"} {
		var n int
		if err := s.db.QueryRow(
			`SELECT COUNT(*) FROM sync_mutations WHERE entity = ? AND entity_key = ?`,
			SyncEntityObservation, syncID,
		).Scan(&n); err != nil {
			t.Fatalf("count mutations for %s: %v", syncID, err)
		}
		if n == 0 {
			t.Errorf("observation %s has no journal entry after enrollment", syncID)
		}
	}

	// And re-running it stays a no-op: nothing is enqueued twice.
	if err := s.EnrollProject("gap-project"); err != nil {
		t.Fatalf("enroll again: %v", err)
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sync_mutations`).Scan(&total); err != nil {
		t.Fatalf("count mutations: %v", err)
	}
	if total != 6 {
		t.Fatalf("expected the journal to hold 6 rows (1 session + 4 observations + 1 prompt), got %d", total)
	}
}

// TestRepairJournalsRowsAndReportsWhatItCannot pins `engram cloud upgrade
// repair --apply` to the same contract: it journals everything deliverable and
// refuses to call the project clean while rows the push cannot see remain.
func TestRepairJournalsRowsAndReportsWhatItCannot(t *testing.T) {
	s := newTestStoreRaw(t)
	seedUnjournaledProject(t, s, "gap-project")

	dryRun, err := s.RepairCloudUpgrade("Gap-Project", false)
	if err != nil {
		t.Fatalf("RepairCloudUpgrade dry run: %v", err)
	}
	if dryRun.ReasonCode != "upgrade_repair_backfill_sync_journal" {
		t.Fatalf("expected the dry run to report a journal backfill, got %+v", dryRun)
	}
	if dryRun.Applied {
		t.Fatal("a dry run must not apply anything")
	}

	applied, err := s.RepairCloudUpgrade("Gap-Project", true)
	if err != nil {
		t.Fatalf("RepairCloudUpgrade apply: %v", err)
	}
	if !applied.Applied {
		t.Fatal("expected the repair to apply the backfill")
	}
	if applied.Class != UpgradeRepairClassBlocked || applied.ReasonCode != UpgradeReasonBlockedUnjournaledRows {
		t.Fatalf("a repair that leaves rows invisible to the push must say so, got %+v", applied)
	}

	report, err := s.ProjectJournalGaps("gap-project")
	if err != nil {
		t.Fatalf("ProjectJournalGaps: %v", err)
	}
	if report.Journalable() != 0 {
		t.Fatalf("repair left %d deliverable row(s) unjournaled (%s)", report.Journalable(), report.Summary())
	}
}

// TestRepairIsReadyOnceEveryRowIsJournaled proves the blocked verdict is about
// the rows, not about the project: a project whose rows all meet the contract
// comes back ready.
func TestRepairIsReadyOnceEveryRowIsJournaled(t *testing.T) {
	s := newTestStoreRaw(t)
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO sync_enrolled_projects (project) VALUES (?)`, "clean-project"); err != nil {
		t.Fatalf("enroll project: %v", err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO sessions (id, project, directory, started_at) VALUES ('sess-clean', 'clean-project', '/tmp/clean', datetime('now'))`,
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO observations (sync_id, session_id, type, title, content, project, scope)
		 VALUES ('o-clean', 'sess-clean', 'note', 'title', 'content', 'clean-project', 'project')`,
	); err != nil {
		t.Fatalf("insert observation: %v", err)
	}

	if _, err := s.RepairCloudUpgrade("clean-project", true); err != nil {
		t.Fatalf("RepairCloudUpgrade apply: %v", err)
	}
	report, err := s.RepairCloudUpgrade("clean-project", true)
	if err != nil {
		t.Fatalf("RepairCloudUpgrade second pass: %v", err)
	}
	if report.Class != UpgradeRepairClassReady {
		t.Fatalf("expected a fully journaled project to report ready, got %+v", report)
	}
}

// TestStoreOpenRepairJournalsDeletedRows pins the fast-path guard to the write
// path. The guard counted only live rows, so a project whose only gap was a
// soft-deleted observation skipped the backfill on every store open, forever.
func TestStoreOpenRepairJournalsDeletedRows(t *testing.T) {
	s := newTestStoreRaw(t)

	if _, err := s.db.Exec(`INSERT OR IGNORE INTO sync_enrolled_projects (project) VALUES (?)`, "delete-only"); err != nil {
		t.Fatalf("enroll project: %v", err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO sessions (id, project, directory, started_at) VALUES ('sess-del', 'delete-only', '/tmp/del', datetime('now'))`,
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if err := s.repairEnrolledProjectSyncMutations(); err != nil {
		t.Fatalf("first repair: %v", err)
	}
	// Now the only gap is a soft-deleted observation with no delete mutation.
	if _, err := s.db.Exec(
		`INSERT INTO observations (sync_id, session_id, type, title, content, project, scope, deleted_at)
		 VALUES ('o-del', 'sess-del', 'note', 'title', 'content', 'delete-only', 'project', datetime('now'))`,
	); err != nil {
		t.Fatalf("insert deleted observation: %v", err)
	}

	if err := s.repairEnrolledProjectSyncMutations(); err != nil {
		t.Fatalf("second repair: %v", err)
	}

	var n int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM sync_mutations WHERE entity = ? AND entity_key = 'o-del' AND op = ?`,
		SyncEntityObservation, SyncOpDelete,
	).Scan(&n); err != nil {
		t.Fatalf("count delete mutations: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected the soft-deleted observation to be journaled once, got %d", n)
	}
}

// TestJournalGapsFindRowsWrittenUnderAnotherCase pins the counter to the folded
// project rule: enrollment stores the slug in lower case while the rows keep the
// case they were written with.
func TestJournalGapsFindRowsWrittenUnderAnotherCase(t *testing.T) {
	s := newTestStoreRaw(t)

	if _, err := s.db.Exec(`INSERT OR IGNORE INTO sync_enrolled_projects (project) VALUES (?)`, "mixed.case"); err != nil {
		t.Fatalf("enroll project: %v", err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO sessions (id, project, directory, started_at) VALUES ('sess-mixed', 'Mixed.Case', '/tmp/mixed', datetime('now'))`,
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO observations (sync_id, session_id, type, title, content, project, scope)
		 VALUES ('o-mixed', 'sess-mixed', 'note', 'title', 'content', 'Mixed.Case', 'project')`,
	); err != nil {
		t.Fatalf("insert observation: %v", err)
	}

	report, err := s.ProjectJournalGaps("mixed.case")
	if err != nil {
		t.Fatalf("ProjectJournalGaps: %v", err)
	}
	if report.Sessions != 1 || report.Observations != 1 {
		t.Fatalf("expected the mixed-case rows to be counted, got %s", report.Summary())
	}
}
