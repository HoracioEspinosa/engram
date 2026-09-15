package main

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// TestCmdCloudUpgradeDoctorRefusesReadyWhileRowsAreUnjournaled pins the doctor
// to what the push can actually see.
//
// A row with no sync_mutations entry never enters a push, and the pending
// counters say nothing about it. The doctor used to look only at pending
// mutations and legacy payloads, so a project could report `ready` with zero
// pending work while hundreds of its observations had never been offered to the
// cloud at all.
func TestCmdCloudUpgradeDoctorRefusesReadyWhileRowsAreUnjournaled(t *testing.T) {
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
	if err := s.EnrollProject("gap-project"); err != nil {
		_ = s.Close()
		t.Fatalf("enroll project: %v", err)
	}
	// Written straight into the tables, the way rows that predate enrollment
	// (or arrive through an import) are: no journal entry anywhere. The second
	// one carries no scope, so the cloud upsert contract rejects it and no
	// backfill can ever give it a journal entry.
	if _, err := s.DB().Exec(
		`INSERT INTO sessions (id, project, directory, started_at) VALUES ('sess-gap', 'gap-project', '/tmp/gap', datetime('now'))`,
	); err != nil {
		_ = s.Close()
		t.Fatalf("insert session: %v", err)
	}
	if _, err := s.DB().Exec(
		`INSERT INTO observations (sync_id, session_id, type, title, content, project, scope)
		 VALUES ('o-unjournaled', 'sess-gap', 'note', 'title', 'content', 'gap-project', '')`,
	); err != nil {
		_ = s.Close()
		t.Fatalf("insert observation: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	withArgs(t, "engram", "cloud", "upgrade", "doctor", "--project", "gap-project")
	stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdCloud(cfg) })
	if recovered != nil || stderr != "" {
		t.Fatalf("doctor should succeed, panic=%v stderr=%q", recovered, stderr)
	}
	if strings.Contains(stdout, "status: ready") {
		t.Fatalf("doctor must not call a project ready while rows have no journal entry, got %q", stdout)
	}
	if !strings.Contains(stdout, "unjournaled_rows: 1") || !strings.Contains(stdout, "blocked=1") {
		t.Fatalf("expected the unjournaled row count in the report, got %q", stdout)
	}

	// Completing the row is the only thing that clears it, and then the doctor
	// stops reporting a gap.
	repaired, err := store.New(cfg)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if _, err := repaired.DB().Exec(`UPDATE observations SET scope = 'project' WHERE sync_id = 'o-unjournaled'`); err != nil {
		_ = repaired.Close()
		t.Fatalf("complete observation: %v", err)
	}
	if err := repaired.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	withArgs(t, "engram", "cloud", "upgrade", "repair", "--project", "gap-project", "--apply")
	if _, repairErr, repairPanic := captureOutputAndRecover(t, func() { cmdCloud(cfg) }); repairPanic != nil || repairErr != "" {
		t.Fatalf("repair should succeed, panic=%v stderr=%q", repairPanic, repairErr)
	}

	withArgs(t, "engram", "cloud", "upgrade", "doctor", "--project", "gap-project")
	afterOut, afterErr, afterPanic := captureOutputAndRecover(t, func() { cmdCloud(cfg) })
	if afterPanic != nil || afterErr != "" {
		t.Fatalf("doctor should succeed, panic=%v stderr=%q", afterPanic, afterErr)
	}
	if !strings.Contains(afterOut, "unjournaled_rows: 0") {
		t.Fatalf("expected the repair to leave no unjournaled rows, got %q", afterOut)
	}
}
