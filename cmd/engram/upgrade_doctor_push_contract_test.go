package main

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// TestCmdCloudUpgradeDoctorRefusesReadyWhenTheNextPushWouldBeRejected pins the
// doctor to the contract the server actually applies.
//
// The unjournaled-row counters only see rows with no sync_mutations entry at
// all. A row that was journaled and later lost a field the cloud requires has
// one, so it is counted nowhere — and it still travels in the next chunk, where
// it takes the whole project's push down with a 400. The doctor answered
// `ready` for exactly that state.
func TestCmdCloudUpgradeDoctorRefusesReadyWhenTheNextPushWouldBeRejected(t *testing.T) {
	stubExitWithPanic(t)
	stubRuntimeHooks(t)

	cfg := testConfig(t)
	if err := saveCloudConfig(cfg, &cloudConfig{ServerURL: "https://cloud.example.test"}); err != nil {
		t.Fatalf("save cloud config: %v", err)
	}

	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := s.DB().Exec(
		`INSERT INTO sessions (id, project, directory, started_at) VALUES ('sess-contract', 'contract-project', '/tmp/contract', datetime('now'))`,
	); err != nil {
		_ = s.Close()
		t.Fatalf("insert session: %v", err)
	}
	if _, err := s.DB().Exec(
		`INSERT INTO observations (sync_id, session_id, type, title, content, project, scope)
		 VALUES ('o-journaled', 'sess-contract', 'note', 'complete', 'content', 'contract-project', 'project')`,
	); err != nil {
		_ = s.Close()
		t.Fatalf("insert observation: %v", err)
	}
	// Enrollment journals the row as an upsert, so no gap counter watches it
	// any more. Clearing the title afterwards is what an edit through a path
	// that does not enqueue leaves behind: the journal entry stays, the row no
	// longer meets the contract, and the next chunk carries it as it is now.
	if err := s.EnrollProject("contract-project"); err != nil {
		_ = s.Close()
		t.Fatalf("enroll project: %v", err)
	}
	if _, err := s.DB().Exec(`UPDATE observations SET title = '' WHERE sync_id = 'o-journaled'`); err != nil {
		_ = s.Close()
		t.Fatalf("clear observation title: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	withArgs(t, "engram", "cloud", "upgrade", "doctor", "--project", "contract-project")
	stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdCloud(cfg) })
	if recovered != nil || stderr != "" {
		t.Fatalf("doctor should succeed, panic=%v stderr=%q", recovered, stderr)
	}
	if strings.Contains(stdout, "status: ready") {
		t.Fatalf("doctor must not call a project ready while the next push would be rejected, got %q", stdout)
	}
	if !strings.Contains(stdout, "unjournaled_rows: 0") {
		t.Fatalf("the row is journaled, so the gap counters must stay at zero; got %q", stdout)
	}
	// The report has to name the row and the reason, or the operator has
	// nothing to act on.
	if !strings.Contains(stdout, "o-journaled") || !strings.Contains(stdout, "title is required") {
		t.Fatalf("expected the rejected row and its reason in the report, got %q", stdout)
	}

	// Completing the row is what clears it, and then the doctor agrees with the
	// push again.
	repaired, err := store.New(cfg)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if _, err := repaired.DB().Exec(`UPDATE observations SET title = 'complete' WHERE sync_id = 'o-journaled'`); err != nil {
		_ = repaired.Close()
		t.Fatalf("complete observation: %v", err)
	}
	if err := repaired.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	withArgs(t, "engram", "cloud", "upgrade", "doctor", "--project", "contract-project")
	afterOut, afterErr, afterPanic := captureOutputAndRecover(t, func() { cmdCloud(cfg) })
	if afterPanic != nil || afterErr != "" {
		t.Fatalf("doctor should succeed, panic=%v stderr=%q", afterPanic, afterErr)
	}
	if !strings.Contains(afterOut, "status: ready") {
		t.Fatalf("expected the completed row to make the project ready, got %q", afterOut)
	}
}

// TestCmdCloudUpgradeDoctorStaysReadyForASoftDeletedRowTheChunkDrops is the
// other side of the same rule: the doctor applies the contract to the rows the
// next push would carry, not to every row in the table.
//
// A title-less observation that was deleted travels as a `delete` mutation and
// is left out of the typed collections, so the push is accepted — and a doctor
// that flagged it would be reporting a problem the operator cannot fix (no
// command reaches an already-deleted row) and that does not exist.
func TestCmdCloudUpgradeDoctorStaysReadyForASoftDeletedRowTheChunkDrops(t *testing.T) {
	stubExitWithPanic(t)
	stubRuntimeHooks(t)

	cfg := testConfig(t)
	if err := saveCloudConfig(cfg, &cloudConfig{ServerURL: "https://cloud.example.test"}); err != nil {
		t.Fatalf("save cloud config: %v", err)
	}

	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := s.DB().Exec(
		`INSERT INTO sessions (id, project, directory, started_at) VALUES ('sess-deleted', 'deleted-project', '/tmp/deleted', datetime('now'))`,
	); err != nil {
		_ = s.Close()
		t.Fatalf("insert session: %v", err)
	}
	if _, err := s.DB().Exec(
		`INSERT INTO observations (sync_id, session_id, type, title, content, project, scope)
		 VALUES ('o-titleless', 'sess-deleted', 'note', '', '', 'deleted-project', 'project')`,
	); err != nil {
		_ = s.Close()
		t.Fatalf("insert titleless observation: %v", err)
	}
	if err := s.EnrollProject("deleted-project"); err != nil {
		_ = s.Close()
		t.Fatalf("enroll project: %v", err)
	}
	var deletedID int64
	if err := s.DB().QueryRow(`SELECT id FROM observations WHERE sync_id = 'o-titleless'`).Scan(&deletedID); err != nil {
		_ = s.Close()
		t.Fatalf("read titleless observation id: %v", err)
	}
	if err := s.DeleteObservation(deletedID, false); err != nil {
		_ = s.Close()
		t.Fatalf("soft-delete observation: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	withArgs(t, "engram", "cloud", "upgrade", "doctor", "--project", "deleted-project")
	stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdCloud(cfg) })
	if recovered != nil || stderr != "" {
		t.Fatalf("doctor should succeed, panic=%v stderr=%q", recovered, stderr)
	}
	if !strings.Contains(stdout, "status: ready") {
		t.Fatalf("a deleted row the chunk drops must not block the project, got %q", stdout)
	}
}
