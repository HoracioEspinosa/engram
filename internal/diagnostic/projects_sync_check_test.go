package diagnostic

import (
	"context"
	"strings"
	"testing"

	"github.com/Gentleman-Programming/engram/internal/store"
)

func runProjectsSyncCheck(t *testing.T, s *store.Store, project string) CheckResult {
	t.Helper()
	report, err := NewRunner().RunOne(context.Background(), Scope{Store: s, Project: project}, CheckProjectsSync)
	if err != nil {
		t.Fatalf("RunOne(%s): %v", CheckProjectsSync, err)
	}
	if len(report.Checks) != 1 {
		t.Fatalf("expected one check result, got %d", len(report.Checks))
	}
	return report.Checks[0]
}

const (
	syncCardPayload = `{"slug":"nextcloud","sync_id":"proj-1","display_name":"Nextcloud",` +
		`"default_branch":"master","jira_project":"PROJ","graph_path":"graphify-out/graph.json",` +
		`"created_at":"2026-01-01 09:00:00","updated_at":"2026-01-01 09:00:00","project":"nextcloud"}`
	syncOrphanEvidencePayload = `{"sync_id":"evd-1","project":"nextcloud","task_sync_id":"task-missing",` +
		`"path":"evidence/a.png","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",` +
		`"kind":"png","proves":"x","captured_at":"2026-01-04 10:00:00","created_at":"2026-01-04 10:00:00",` +
		`"occurred_at":"2026-01-04 10:00:00"}`
)

func syncTaskPayloadJSON(syncID, createdAt string) string {
	return `{"sync_id":"` + syncID + `","project":"nextcloud","jira_key":"PROJ-100","title":"t",` +
		`"kind":"bugfix","state":"open","created_at":"` + createdAt + `","updated_at":"` + createdAt + `"}`
}

// parkOrphanEvidence produces a real deferred row the way a pull does: an
// evidence mutation whose task has not arrived yet.
func parkOrphanEvidence(t *testing.T, s *store.Store) {
	t.Helper()
	err := s.ApplyPulledChunk(store.DefaultSyncTargetKey, "chunk-orphan", []store.SyncMutation{{
		Entity: store.SyncEntityEvidence, EntityKey: "evd-1", Op: store.SyncOpUpsert,
		Payload: syncOrphanEvidencePayload, Project: "nextcloud", Source: store.SyncSourceRemote,
	}})
	if err != nil {
		t.Fatalf("park orphan evidence: %v", err)
	}
}

// parkTaskKeyConflict produces a real dead row: two tasks claiming one ticket.
func parkTaskKeyConflict(t *testing.T, s *store.Store) {
	t.Helper()
	err := s.ApplyPulledChunk(store.DefaultSyncTargetKey, "chunk-conflict", []store.SyncMutation{
		{Entity: store.SyncEntityProjectCard, EntityKey: "proj-1", Op: store.SyncOpUpsert,
			Payload: syncCardPayload, Project: "nextcloud", Source: store.SyncSourceRemote},
		{Entity: store.SyncEntityTask, EntityKey: "task-1", Op: store.SyncOpUpsert,
			Payload: syncTaskPayloadJSON("task-1", "2026-01-01 10:00:00"),
			Project: "nextcloud", Source: store.SyncSourceRemote},
		{Entity: store.SyncEntityTask, EntityKey: "task-2", Op: store.SyncOpUpsert,
			Payload: syncTaskPayloadJSON("task-2", "2026-01-02 10:00:00"),
			Project: "nextcloud", Source: store.SyncSourceRemote},
	})
	if err != nil {
		t.Fatalf("park task key conflict: %v", err)
	}
}

func TestProjectsSyncCheck_CleanDatabaseIsOK(t *testing.T) {
	t.Setenv("ENGRAM_PROJECTS_SYNC", "0")
	s := newDiagnosticTestStore(t)

	result := runProjectsSyncCheck(t, s, "nextcloud")
	if result.Result != StatusOK {
		t.Fatalf("expected ok on an empty database, got %+v", result)
	}
	if result.ReasonCode != CheckProjectsSync+"_ok" {
		t.Fatalf("reason_code = %q", result.ReasonCode)
	}
}

func TestProjectsSyncCheck_ReportsThePendingBacklogAsInformational(t *testing.T) {
	t.Setenv("ENGRAM_PROJECTS_SYNC", "1")
	s := newDiagnosticTestStore(t)
	if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: "nextcloud"}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}

	result := runProjectsSyncCheck(t, s, "nextcloud")
	if result.Result != StatusOK {
		t.Fatalf("a queued backlog is not a fault; got %+v", result)
	}
	if result.ReasonCode != CheckProjectsSync+"_pending" {
		t.Fatalf("reason_code = %q, want the pending variant", result.ReasonCode)
	}
	if !strings.Contains(string(result.Evidence), `"total_pending"`) {
		t.Fatalf("evidence must carry the per-entity counters: %s", result.Evidence)
	}
}

func TestProjectsSyncCheck_SaysTheFlagIsOffWhenItIs(t *testing.T) {
	t.Setenv("ENGRAM_PROJECTS_SYNC", "1")
	s := newDiagnosticTestStore(t)
	if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: "nextcloud"}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}

	// The mutations were recorded while replication was on; the operator has
	// since switched it off. The check must say so instead of implying a fault.
	t.Setenv("ENGRAM_PROJECTS_SYNC", "0")
	result := runProjectsSyncCheck(t, s, "nextcloud")
	if result.ReasonCode != CheckProjectsSync+"_disabled" {
		t.Fatalf("reason_code = %q, want the disabled variant", result.ReasonCode)
	}
	if !strings.Contains(result.SafeNextStep, "ENGRAM_PROJECTS_SYNC=1") {
		t.Fatalf("the next step must name the flag: %q", result.SafeNextStep)
	}
}

func TestProjectsSyncCheck_WarnsOnDeferredRows(t *testing.T) {
	t.Setenv("ENGRAM_PROJECTS_SYNC", "1")
	s := newDiagnosticTestStore(t)
	parkOrphanEvidence(t, s)

	result := runProjectsSyncCheck(t, s, "")
	if result.Result != StatusWarning || result.ReasonCode != CheckProjectsSync+"_deferred_mutations" {
		t.Fatalf("expected a deferred warning, got %+v", result)
	}
	if !strings.Contains(result.SafeNextStep, "engram conflicts replay") {
		t.Fatalf("the next step must point at replay: %q", result.SafeNextStep)
	}
}

func TestProjectsSyncCheck_WarnsOnATaskKeyConflict(t *testing.T) {
	t.Setenv("ENGRAM_PROJECTS_SYNC", "1")
	s := newDiagnosticTestStore(t)
	parkTaskKeyConflict(t, s)

	result := runProjectsSyncCheck(t, s, "")
	if result.Result != StatusWarning || result.ReasonCode != CheckProjectsSync+"_dead_mutations" {
		t.Fatalf("expected a dead-row warning, got %+v", result)
	}
	if !result.RequiresConfirmation {
		t.Fatal("a dead row needs a human decision; the check must say so")
	}
	if !strings.Contains(result.Message, "jira_key collision") {
		t.Fatalf("the message must name the collision: %q", result.Message)
	}
	if !strings.Contains(string(result.Evidence), `"task_key_conflicts":1`) {
		t.Fatalf("evidence must count the collisions: %s", result.Evidence)
	}
}

// TestProjectsSyncCheck_DeadOutranksDeferred: the operator hears first about
// the thing that will never resolve on its own.
func TestProjectsSyncCheck_DeadOutranksDeferred(t *testing.T) {
	t.Setenv("ENGRAM_PROJECTS_SYNC", "1")
	s := newDiagnosticTestStore(t)
	parkOrphanEvidence(t, s)
	parkTaskKeyConflict(t, s)

	result := runProjectsSyncCheck(t, s, "")
	if result.ReasonCode != CheckProjectsSync+"_dead_mutations" {
		t.Fatalf("reason_code = %q, want the dead variant to win", result.ReasonCode)
	}
}

// TestProjectsSyncCheck_IgnoresUpstreamParkedRows is the negative control: a
// deferred relation belongs to a different repair path and must not surface
// here.
func TestProjectsSyncCheck_IgnoresUpstreamParkedRows(t *testing.T) {
	t.Setenv("ENGRAM_PROJECTS_SYNC", "1")
	s := newDiagnosticTestStore(t)
	err := s.ApplyPulledMutation(store.DefaultSyncTargetKey, store.SyncMutation{
		Seq: 1, Entity: store.SyncEntityRelation, EntityKey: "rel-1", Op: store.SyncOpUpsert,
		Payload: `{"sync_id":"rel-1","source_id":"obs-missing-1","target_id":"obs-missing-2",` +
			`"relation":"supersedes","judgment_status":"confirmed","project":"nextcloud"}`,
		Project: "nextcloud", Source: store.SyncSourceRemote,
	})
	if err != nil {
		t.Fatalf("park relation: %v", err)
	}

	result := runProjectsSyncCheck(t, s, "")
	if result.Result != StatusOK {
		t.Fatalf("a parked relation is not an engram-projects finding: %+v", result)
	}
}
