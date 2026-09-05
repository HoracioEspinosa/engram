package store

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"
)

// Tests for engram-projects cloud replication (RFC section 10). Every test
// runs against throwaway stores opened with t.TempDir()
// (newProjectsSchemaTestStore, defined in projects_schema_test.go); none of
// them touches ~/.engram/engram.db, and none of them needs a network.

// ─── Fixtures ────────────────────────────────────────────────────────────────

const (
	fxProject   = "nextcloud"
	fxCardSync  = "proj-c0ffee0000000001"
	fxTaskA     = "task-aaaa000000000001"
	fxTaskB     = "task-bbbb000000000002"
	fxTaskC     = "task-cccc000000000003"
	fxEvidence  = "evd-eeee000000000001"
	fxObsSyncID = "obs-1111000000000001"
	fxJiraKey   = "PROJ-100"
)

func mut(entity, entityKey, payload string) SyncMutation {
	return SyncMutation{
		Entity:    entity,
		EntityKey: entityKey,
		Op:        SyncOpUpsert,
		Payload:   payload,
		Project:   fxProject,
		Source:    SyncSourceRemote,
	}
}

func mustJSONString(t *testing.T, v any) string {
	t.Helper()
	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return string(encoded)
}

func cardPayload(t *testing.T, displayName, updatedAt string, graphCommit, graphBuiltAt *string) string {
	t.Helper()
	return mustJSONString(t, syncProjectCardPayload{
		Slug:          fxProject,
		SyncID:        fxCardSync,
		DisplayName:   displayName,
		DefaultBranch: "master",
		JiraProject:   "PROJ",
		GraphPath:     "graphify-out/graph.json",
		GraphCommit:   graphCommit,
		GraphBuiltAt:  graphBuiltAt,
		CreatedAt:     "2026-01-01 09:00:00",
		UpdatedAt:     updatedAt,
		Project:       fxProject,
	})
}

func taskPayload(t *testing.T, p syncTaskPayload) string {
	t.Helper()
	if p.Project == "" {
		p.Project = fxProject
	}
	if p.Kind == "" {
		p.Kind = "bugfix"
	}
	if p.State == "" {
		p.State = "open"
	}
	return mustJSONString(t, p)
}

func evidencePayload(t *testing.T, taskSyncID string, attachedJira bool, confluence *string, occurredAt string) string {
	t.Helper()
	return mustJSONString(t, syncEvidencePayload{
		SyncID:                fxEvidence,
		Project:               fxProject,
		TaskSyncID:            taskSyncID,
		Path:                  "evidence/shot.png",
		SHA256:                strings.Repeat("a", 64),
		Kind:                  "png",
		Proves:                "the upload finishes",
		CapturedAt:            "2026-01-04 10:00:00",
		AttachedJira:          attachedJira,
		AttachedConfluenceURL: confluence,
		CreatedAt:             "2026-01-04 10:00:00",
		OccurredAt:            occurredAt,
	})
}

func linkPayload(t *testing.T, taskSyncID, role, linkedAt string, deletedAt *string) string {
	t.Helper()
	return mustJSONString(t, syncTaskLinkPayload{
		TaskSyncID:        taskSyncID,
		ObservationSyncID: fxObsSyncID,
		Role:              role,
		LinkedAt:          linkedAt,
		DeletedAt:         deletedAt,
		Project:           fxProject,
	})
}

func refPayload(t *testing.T, refKind, ref string, graphCommit *string) string {
	t.Helper()
	return mustJSONString(t, syncObservationRefPayload{
		ObservationSyncID: fxObsSyncID,
		RefKind:           refKind,
		Ref:               ref,
		GraphCommit:       graphCommit,
		CreatedAt:         "2026-01-05 10:00:00",
		Project:           fxProject,
	})
}

// seedReplica opens a store and gives it the one row the replicated payloads
// reference but never carry: an observation with a fixed sync_id. Both
// replicas get the identical seed, so any difference in the final dump comes
// from the merge rules and nothing else.
func seedReplica(t *testing.T) *Store {
	t.Helper()
	s := newProjectsSchemaTestStore(t)
	if err := s.CreateSession("sess-sync", fxProject, t.TempDir()); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	obsID, err := s.AddObservation(AddObservationParams{
		SessionID: "sess-sync", Type: "manual", Title: "seed", Content: "seed", Project: fxProject,
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	if _, err := s.db.Exec(`UPDATE observations SET sync_id = ? WHERE id = ?`, fxObsSyncID, obsID); err != nil {
		t.Fatalf("pin observation sync_id: %v", err)
	}
	return s
}

// ─── State dump ──────────────────────────────────────────────────────────────

// dumpProjectsState renders every replicated engram-projects row in a stable
// order. It is the yardstick of the convergence test: two replicas converge
// when and only when these strings are equal.
//
// Local integer ids are excluded on purpose — they are AUTOINCREMENT counters
// whose values depend on insertion order and carry no replicated meaning. The
// deferred journal is reduced to (sync_id, entity, apply_status) for the same
// reason: its timestamps come from datetime('now').
func dumpProjectsState(t *testing.T, s *Store) string {
	t.Helper()
	var b strings.Builder
	queries := []struct {
		label string
		query string
	}{
		{"project_cards", `SELECT slug, sync_id, display_name, ifnull(repo_url,''), default_branch,
			jira_project, ifnull(jira_component,''), ifnull(knowledge_hub_path,''), graph_path,
			ifnull(graph_commit,''), ifnull(graph_built_at,''), ifnull(graph_summary,''),
			ifnull(owner,''), created_at, updated_at, ifnull(deleted_at,'')
			FROM project_cards ORDER BY slug`},
		{"tasks", `SELECT sync_id, project, ifnull(jira_key,''), ifnull(sdd_change,''), title, kind, state,
			ifnull(jira_status,''), ifnull(jira_status_category,''), ifnull(state_synced_at,''),
			ifnull(branch,''), ifnull(pr_url,''), ifnull(knowledge_ref,''), ifnull(assignee,''),
			created_at, updated_at, ifnull(closed_at,''), ifnull(deleted_at,'')
			FROM tasks ORDER BY sync_id`},
		{"evidence", `SELECT sync_id, project, task_sync_id, path, sha256, kind, proves,
			ifnull(config_stamp,''), captured_at, attached_jira, ifnull(attached_confluence_url,''),
			ifnull(size_bytes,-1), ifnull(manifest_path,''), created_at, ifnull(deleted_at,'')
			FROM evidence ORDER BY sync_id`},
		{"task_observations", `SELECT task_sync_id, observation_sync_id, role, linked_at
			FROM task_observations ORDER BY task_sync_id, observation_sync_id`},
		{"task_link_tombstones", `SELECT task_sync_id, observation_sync_id, deleted_at
			FROM task_link_tombstones ORDER BY task_sync_id, observation_sync_id`},
		{"observation_refs", `SELECT observation_sync_id, ref_kind, ref, ifnull(graph_commit,''), created_at
			FROM observation_refs ORDER BY observation_sync_id, ref_kind, ref`},
		{"deferred", `SELECT sync_id, entity, apply_status FROM sync_apply_deferred ORDER BY sync_id`},
	}
	for _, q := range queries {
		rows, err := s.db.Query(q.query)
		if err != nil {
			t.Fatalf("dump %s: %v", q.label, err)
		}
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatalf("dump %s columns: %v", q.label, err)
		}
		for rows.Next() {
			cells := make([]any, len(cols))
			values := make([]string, len(cols))
			for i := range cells {
				cells[i] = &values[i]
			}
			if err := rows.Scan(cells...); err != nil {
				rows.Close()
				t.Fatalf("dump %s scan: %v", q.label, err)
			}
			fmt.Fprintf(&b, "%s|%s\n", q.label, strings.Join(values, "|"))
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatalf("dump %s rows: %v", q.label, err)
		}
	}
	return b.String()
}

// applyAsReplica delivers mutations one chunk at a time — the real pull path,
// parking included — and then drains the deferred queue the way a pull cycle
// does. Chunk ids are content-derived so a repeated delivery of the same
// mutation is a genuine duplicate, not a new chunk.
func applyAsReplica(t *testing.T, s *Store, mutations []SyncMutation) {
	t.Helper()
	for i, m := range mutations {
		chunkID := fmt.Sprintf("chunk-%02d-%s", i, m.EntityKey)
		if err := s.ApplyPulledChunk(DefaultSyncTargetKey, chunkID, []SyncMutation{m}); err != nil {
			t.Fatalf("ApplyPulledChunk(%s/%s): %v", m.Entity, m.EntityKey, err)
		}
	}
	// Deferred rows can chain (a link waiting for a task that was itself
	// waiting for its card), so replay until it stops making progress.
	for round := 0; round < 4; round++ {
		res, err := s.ReplayDeferred()
		if err != nil {
			t.Fatalf("ReplayDeferred: %v", err)
		}
		if res.Succeeded == 0 {
			break
		}
	}
}

// deferredStatusFor reads the apply_status of the row parked for an entity.
// Parked rows are keyed by entity plus payload digest, so the lookup is by
// prefix: that composite key is what lets two payloads for one entity wait
// side by side instead of overwriting each other.
func deferredStatusFor(t *testing.T, s *Store, entityKey string) string {
	t.Helper()
	var status string
	err := s.db.QueryRow(
		`SELECT apply_status FROM sync_apply_deferred WHERE substr(sync_id, 1, ?) = ?`,
		len(entityKey)+1, entityKey+"#",
	).Scan(&status)
	if err != nil {
		t.Fatalf("read deferred row for %q: %v", entityKey, err)
	}
	return status
}

func countDeferredFor(t *testing.T, s *Store, entityKey string) int {
	t.Helper()
	var count int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM sync_apply_deferred WHERE substr(sync_id, 1, ?) = ?`,
		len(entityKey)+1, entityKey+"#",
	).Scan(&count); err != nil {
		t.Fatalf("count deferred rows for %q: %v", entityKey, err)
	}
	return count
}

// ─── Convergence ─────────────────────────────────────────────────────────────

// crossReplicaMutations is the scenario RFC section 13 asks the convergence
// test to cover: concurrent upserts of the same task, a Jira refresh landing
// on two replicas with different freshness, evidence and a link that reference
// a task that may not have arrived yet, and two tasks fighting over one
// jira_key.
func crossReplicaMutations(t *testing.T) []SyncMutation {
	t.Helper()
	commit := strings.Repeat("1", 40)
	newerCommit := strings.Repeat("2", 40)
	builtOld := "2026-01-02 08:00:00"
	builtNew := "2026-01-06 08:00:00"

	return []SyncMutation{
		mut(SyncEntityProjectCard, fxCardSync, cardPayload(t, "Nextcloud", "2026-01-01 09:00:00", nil, nil)),
		// A newer graph stamp, and a later descriptive update that still
		// carries the OLD stamp: the monotone group must not walk backwards.
		mut(SyncEntityProjectCard, fxCardSync, cardPayload(t, "Nextcloud server", "2026-01-06 09:00:00", &newerCommit, &builtNew)),
		mut(SyncEntityProjectCard, fxCardSync, cardPayload(t, "Nextcloud NG", "2026-01-07 09:00:00", &commit, &builtOld)),

		// Task A owns PROJ-100 and is the older of the two claimants.
		mut(SyncEntityTask, fxTaskA, taskPayload(t, syncTaskPayload{
			SyncID: fxTaskA, JiraKey: strp(fxJiraKey), Title: "upload fails", Kind: "bugfix",
			State: "open", CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-01 10:00:00",
		})),
		// Concurrent descriptive update of the same task from another replica.
		mut(SyncEntityTask, fxTaskA, taskPayload(t, syncTaskPayload{
			SyncID: fxTaskA, JiraKey: strp(fxJiraKey), Title: "upload fails on big files", Kind: "bugfix",
			State: "open", Branch: strp("fix/PROJ-100"),
			CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-03 10:00:00",
		})),
		// Two Jira refreshes of the same task; the fresher read must win even
		// though it carries the older updated_at.
		mut(SyncEntityTask, fxTaskA, taskPayload(t, syncTaskPayload{
			SyncID: fxTaskA, JiraKey: strp(fxJiraKey), Title: "upload fails", Kind: "bugfix",
			State: "review", JiraStatus: strp("In Review"), JiraStatusCategory: strp("indeterminate"),
			StateSyncedAt: strp("2026-01-05 08:00:00"),
			CreatedAt:     "2026-01-01 10:00:00", UpdatedAt: "2026-01-02 08:00:00",
		})),
		mut(SyncEntityTask, fxTaskA, taskPayload(t, syncTaskPayload{
			SyncID: fxTaskA, JiraKey: strp(fxJiraKey), Title: "upload fails", Kind: "bugfix",
			State: "done", JiraStatus: strp("Done"), JiraStatusCategory: strp("done"),
			StateSyncedAt: strp("2026-01-08 08:00:00"),
			CreatedAt:     "2026-01-01 10:00:00", UpdatedAt: "2026-01-02 09:00:00",
		})),
		// Task B claims the same jira_key but was created later: it loses.
		mut(SyncEntityTask, fxTaskB, taskPayload(t, syncTaskPayload{
			SyncID: fxTaskB, JiraKey: strp(fxJiraKey), Title: "duplicate ticket", Kind: "bugfix",
			State: "open", CreatedAt: "2026-01-02 10:00:00", UpdatedAt: "2026-01-02 10:00:00",
		})),
		// Task C is keyed by an SDD change and owns the evidence and the link.
		mut(SyncEntityTask, fxTaskC, taskPayload(t, syncTaskPayload{
			SyncID: fxTaskC, SDDChange: strp("cdbs-1"), Title: "sdd change", Kind: "feature",
			State: "in_progress", CreatedAt: "2026-01-03 10:00:00", UpdatedAt: "2026-01-03 10:00:00",
		})),

		mut(SyncEntityEvidence, fxEvidence, evidencePayload(t, fxTaskC, false, nil, "2026-01-04 10:00:00")),
		mut(SyncEntityEvidence, fxEvidence, evidencePayload(t, fxTaskC, true,
			strp("https://confluence.example/x"), "2026-01-05 10:00:00")),

		mut(SyncEntityTaskLink, taskLinkKey(fxTaskC, fxObsSyncID),
			linkPayload(t, fxTaskC, "context", "2026-01-04 11:00:00", nil)),
		mut(SyncEntityTaskLink, taskLinkKey(fxTaskC, fxObsSyncID),
			linkPayload(t, fxTaskC, "root_cause", "2026-01-06 11:00:00", nil)),

		mut(SyncEntityObservationRef, observationRefKey(fxObsSyncID, "knowledge", "Runbooks/RB-003.md"),
			refPayload(t, "knowledge", "Runbooks/RB-003.md", nil)),
		mut(SyncEntityObservationRef, observationRefKey(fxObsSyncID, "graph", "Uploader::put"),
			refPayload(t, "graph", "Uploader::put", &commit)),
	}
}

// TestProjectsSync_ConvergesUnderPermutedDelivery is the property this whole
// file exists for: the same set of mutations, delivered in any order, leaves
// every replica in the same state. It is the offline half of the T-04.05
// acceptance criterion — no cloud, no token, no second machine, just the rules.
func TestProjectsSync_ConvergesUnderPermutedDelivery(t *testing.T) {
	base := crossReplicaMutations(t)

	reference := seedReplica(t)
	applyAsReplica(t, reference, base)
	want := dumpProjectsState(t, reference)
	if !strings.Contains(want, "tasks|"+fxTaskA) {
		t.Fatalf("reference replica did not materialize task A:\n%s", want)
	}

	rng := rand.New(rand.NewSource(20260405))
	const permutations = 40
	for i := 0; i < permutations; i++ {
		shuffled := make([]SyncMutation, len(base))
		copy(shuffled, base)
		rng.Shuffle(len(shuffled), func(a, b int) {
			shuffled[a], shuffled[b] = shuffled[b], shuffled[a]
		})

		replica := seedReplica(t)
		applyAsReplica(t, replica, shuffled)
		got := dumpProjectsState(t, replica)
		if got != want {
			t.Fatalf("permutation %d diverged.\norder: %s\n--- want ---\n%s\n--- got ---\n%s",
				i, describeOrder(shuffled), want, got)
		}
	}
}

func describeOrder(mutations []SyncMutation) string {
	parts := make([]string, 0, len(mutations))
	for _, m := range mutations {
		parts = append(parts, m.Entity+":"+m.EntityKey)
	}
	return strings.Join(parts, " ")
}

// TestProjectsSync_ConvergenceDumpIsSensitive guards the convergence test
// itself: a yardstick that cannot tell two different states apart proves
// nothing. Delivering a strictly larger set of mutations must change the dump.
func TestProjectsSync_ConvergenceDumpIsSensitive(t *testing.T) {
	base := crossReplicaMutations(t)

	full := seedReplica(t)
	applyAsReplica(t, full, base)

	partial := seedReplica(t)
	applyAsReplica(t, partial, base[:len(base)-1])

	if dumpProjectsState(t, full) == dumpProjectsState(t, partial) {
		t.Fatal("dumpProjectsState cannot distinguish two different replica states; the convergence test would pass vacuously")
	}
}

// ─── Individual merge rules ──────────────────────────────────────────────────

func TestProjectsSync_GraphGroupNeverWalksBackwards(t *testing.T) {
	s := seedReplica(t)
	newCommit := strings.Repeat("2", 40)
	oldCommit := strings.Repeat("1", 40)
	builtNew := "2026-01-06 08:00:00"
	builtOld := "2026-01-02 08:00:00"

	applyAsReplica(t, s, []SyncMutation{
		mut(SyncEntityProjectCard, fxCardSync, cardPayload(t, "Nextcloud", "2026-01-06 09:00:00", &newCommit, &builtNew)),
		mut(SyncEntityProjectCard, fxCardSync, cardPayload(t, "Nextcloud NG", "2026-01-07 09:00:00", &oldCommit, &builtOld)),
	})

	var commit, builtAt, displayName string
	if err := s.db.QueryRow(
		`SELECT graph_commit, graph_built_at, display_name FROM project_cards WHERE slug = ?`, fxProject,
	).Scan(&commit, &builtAt, &displayName); err != nil {
		t.Fatalf("read card: %v", err)
	}
	if commit != newCommit || builtAt != builtNew {
		t.Fatalf("graph group regressed: commit=%s built_at=%s", commit, builtAt)
	}
	if displayName != "Nextcloud NG" {
		t.Fatalf("descriptive field did not follow the later updated_at: %q", displayName)
	}
}

func TestProjectsSync_JiraMirrorWinsOnFreshnessNotUpdatedAt(t *testing.T) {
	s := seedReplica(t)
	applyAsReplica(t, s, []SyncMutation{
		mut(SyncEntityProjectCard, fxCardSync, cardPayload(t, "Nextcloud", "2026-01-01 09:00:00", nil, nil)),
		// Later updated_at, older Jira read.
		mut(SyncEntityTask, fxTaskA, taskPayload(t, syncTaskPayload{
			SyncID: fxTaskA, JiraKey: strp(fxJiraKey), Title: "t", State: "open",
			JiraStatus: strp("In Develop"), JiraStatusCategory: strp("indeterminate"),
			StateSyncedAt: strp("2026-01-02 08:00:00"),
			CreatedAt:     "2026-01-01 10:00:00", UpdatedAt: "2026-01-09 10:00:00",
		})),
		// Earlier updated_at, fresher Jira read: the mirror must still win.
		mut(SyncEntityTask, fxTaskA, taskPayload(t, syncTaskPayload{
			SyncID: fxTaskA, JiraKey: strp(fxJiraKey), Title: "t", State: "done",
			JiraStatus: strp("Done"), JiraStatusCategory: strp("done"),
			StateSyncedAt: strp("2026-01-08 08:00:00"),
			CreatedAt:     "2026-01-01 10:00:00", UpdatedAt: "2026-01-03 10:00:00",
		})),
	})

	var state, jiraStatus, closedAt string
	if err := s.db.QueryRow(
		`SELECT state, jira_status, ifnull(closed_at,'') FROM tasks WHERE sync_id = ?`, fxTaskA,
	).Scan(&state, &jiraStatus, &closedAt); err != nil {
		t.Fatalf("read task: %v", err)
	}
	if state != "done" || jiraStatus != "Done" {
		t.Fatalf("Jira mirror lost to updated_at: state=%s jira_status=%s", state, jiraStatus)
	}
	if closedAt == "" {
		t.Fatal("closed_at must follow the resulting closed state")
	}
}

func TestProjectsSync_TaskKeyConflictQuarantinesLoserAndParksItDead(t *testing.T) {
	s := seedReplica(t)
	applyAsReplica(t, s, []SyncMutation{
		mut(SyncEntityProjectCard, fxCardSync, cardPayload(t, "Nextcloud", "2026-01-01 09:00:00", nil, nil)),
		mut(SyncEntityTask, fxTaskB, taskPayload(t, syncTaskPayload{
			SyncID: fxTaskB, JiraKey: strp(fxJiraKey), Title: "younger", State: "open",
			CreatedAt: "2026-01-02 10:00:00", UpdatedAt: "2026-01-02 10:00:00",
		})),
		mut(SyncEntityTask, fxTaskA, taskPayload(t, syncTaskPayload{
			SyncID: fxTaskA, JiraKey: strp(fxJiraKey), Title: "older", State: "open",
			CreatedAt: "2026-01-01 10:00:00", UpdatedAt: "2026-01-01 10:00:00",
		})),
	})

	var winnerKey, loserKey string
	if err := s.db.QueryRow(`SELECT jira_key FROM tasks WHERE sync_id = ?`, fxTaskA).Scan(&winnerKey); err != nil {
		t.Fatalf("read winner: %v", err)
	}
	if err := s.db.QueryRow(`SELECT jira_key FROM tasks WHERE sync_id = ?`, fxTaskB).Scan(&loserKey); err != nil {
		t.Fatalf("read loser: %v", err)
	}
	if winnerKey != fxJiraKey {
		t.Fatalf("older task lost the ticket: %q", winnerKey)
	}
	if loserKey != quarantinedJiraKey(fxJiraKey, fxTaskB) {
		t.Fatalf("loser key not quarantined: %q", loserKey)
	}

	var status, lastError string
	if err := s.db.QueryRow(
		`SELECT apply_status, ifnull(last_error,'') FROM sync_apply_deferred WHERE sync_id = ?`, fxTaskB,
	).Scan(&status, &lastError); err != nil {
		t.Fatalf("read deferred row: %v", err)
	}
	if status != "dead" || !strings.HasPrefix(lastError, taskKeyConflictReason) {
		t.Fatalf("expected a dead task_key_conflict row, got status=%s error=%q", status, lastError)
	}

	syncStatus, err := s.ProjectsSyncStatus(fxProject)
	if err != nil {
		t.Fatalf("ProjectsSyncStatus: %v", err)
	}
	if syncStatus.KeyConflicts != 1 {
		t.Fatalf("doctor read model missed the conflict: %+v", syncStatus)
	}
}

func TestProjectsSync_EvidenceIsImmutableWithMonotoneFlags(t *testing.T) {
	s := seedReplica(t)
	confluence := "https://confluence.example/first"
	applyAsReplica(t, s, []SyncMutation{
		mut(SyncEntityProjectCard, fxCardSync, cardPayload(t, "Nextcloud", "2026-01-01 09:00:00", nil, nil)),
		mut(SyncEntityTask, fxTaskC, taskPayload(t, syncTaskPayload{
			SyncID: fxTaskC, SDDChange: strp("cdbs-1"), Title: "t", Kind: "feature", State: "open",
			CreatedAt: "2026-01-03 10:00:00", UpdatedAt: "2026-01-03 10:00:00",
		})),
		mut(SyncEntityEvidence, fxEvidence, evidencePayload(t, fxTaskC, true, &confluence, "2026-01-05 10:00:00")),
		// A stale replica re-sends the row with the flag still down.
		mut(SyncEntityEvidence, fxEvidence, evidencePayload(t, fxTaskC, false, nil, "2026-01-04 10:00:00")),
	})

	var attachedJira int
	var url string
	if err := s.db.QueryRow(
		`SELECT attached_jira, ifnull(attached_confluence_url,'') FROM evidence WHERE sync_id = ?`, fxEvidence,
	).Scan(&attachedJira, &url); err != nil {
		t.Fatalf("read evidence: %v", err)
	}
	if attachedJira != 1 {
		t.Fatal("attached_jira went back to 0; the flag must be monotone")
	}
	if url != confluence {
		t.Fatalf("attached_confluence_url lost its first value: %q", url)
	}
}

func TestProjectsSync_TaskLinkDeleteBeatsAnEarlierLink(t *testing.T) {
	deletedAt := "2026-01-07 11:00:00"
	setup := []SyncMutation{
		mut(SyncEntityProjectCard, fxCardSync, cardPayload(t, "Nextcloud", "2026-01-01 09:00:00", nil, nil)),
		mut(SyncEntityTask, fxTaskC, taskPayload(t, syncTaskPayload{
			SyncID: fxTaskC, SDDChange: strp("cdbs-1"), Title: "t", Kind: "feature", State: "open",
			CreatedAt: "2026-01-03 10:00:00", UpdatedAt: "2026-01-03 10:00:00",
		})),
	}
	link := mut(SyncEntityTaskLink, taskLinkKey(fxTaskC, fxObsSyncID),
		linkPayload(t, fxTaskC, "context", "2026-01-06 11:00:00", nil))
	unlink := mut(SyncEntityTaskLink, taskLinkKey(fxTaskC, fxObsSyncID),
		linkPayload(t, fxTaskC, "context", "2026-01-06 11:00:00", &deletedAt))
	unlink.Op = SyncOpDelete

	// Both delivery orders must end with the link gone: without the tombstone
	// the "unlink first" order silently resurrects it.
	for _, tc := range []struct {
		name  string
		order []SyncMutation
	}{
		{"link then unlink", append(append([]SyncMutation{}, setup...), link, unlink)},
		{"unlink then link", append(append([]SyncMutation{}, setup...), unlink, link)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := seedReplica(t)
			applyAsReplica(t, s, tc.order)
			var links int
			if err := s.db.QueryRow(
				`SELECT COUNT(*) FROM task_observations WHERE task_sync_id = ?`, fxTaskC,
			).Scan(&links); err != nil {
				t.Fatalf("count links: %v", err)
			}
			if links != 0 {
				t.Fatalf("the unlink lost to a link created before it (%d rows left)", links)
			}
		})
	}

	// A link created after the unlink is a legitimate re-link and must survive.
	s := seedReplica(t)
	relink := mut(SyncEntityTaskLink, taskLinkKey(fxTaskC, fxObsSyncID),
		linkPayload(t, fxTaskC, "decision", "2026-01-09 11:00:00", nil))
	applyAsReplica(t, s, append(append([]SyncMutation{}, setup...), unlink, relink))
	var role string
	if err := s.db.QueryRow(
		`SELECT role FROM task_observations WHERE task_sync_id = ?`, fxTaskC,
	).Scan(&role); err != nil {
		t.Fatalf("re-link did not survive its own unlink: %v", err)
	}
	if role != "decision" {
		t.Fatalf("unexpected role after re-link: %q", role)
	}
}

func TestProjectsSync_ObservationRefIsGrowOnly(t *testing.T) {
	s := seedReplica(t)
	ref := mut(SyncEntityObservationRef, observationRefKey(fxObsSyncID, "knowledge", "Runbooks/RB-003.md"),
		refPayload(t, "knowledge", "Runbooks/RB-003.md", nil))
	applyAsReplica(t, s, []SyncMutation{ref, ref, ref})

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM observation_refs`).Scan(&count); err != nil {
		t.Fatalf("count refs: %v", err)
	}
	if count != 1 {
		t.Fatalf("re-applying a grow-only ref duplicated it: %d rows", count)
	}
}

// ─── Deferral, parking and idempotency ───────────────────────────────────────

func TestProjectsSync_EvidenceWaitsForItsTaskInsteadOfFailingTheChunk(t *testing.T) {
	s := seedReplica(t)
	evidence := mut(SyncEntityEvidence, fxEvidence, evidencePayload(t, fxTaskC, false, nil, "2026-01-04 10:00:00"))

	// The chunk must be accepted even though the task is missing: a rejection
	// here would stop the whole pull for every other entity behind it.
	if err := s.ApplyPulledChunk(DefaultSyncTargetKey, "chunk-orphan", []SyncMutation{evidence}); err != nil {
		t.Fatalf("orphan evidence failed its chunk instead of being parked: %v", err)
	}
	if status := deferredStatusFor(t, s, fxEvidence); status != "deferred" {
		t.Fatalf("expected a retryable deferred row, got %q", status)
	}

	// Once the card and the task arrive, the replay applies it.
	applyAsReplica(t, s, []SyncMutation{
		mut(SyncEntityProjectCard, fxCardSync, cardPayload(t, "Nextcloud", "2026-01-01 09:00:00", nil, nil)),
		mut(SyncEntityTask, fxTaskC, taskPayload(t, syncTaskPayload{
			SyncID: fxTaskC, SDDChange: strp("cdbs-1"), Title: "t", Kind: "feature", State: "open",
			CreatedAt: "2026-01-03 10:00:00", UpdatedAt: "2026-01-03 10:00:00",
		})),
	})

	var evidenceRows int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM evidence WHERE sync_id = ?`, fxEvidence).Scan(&evidenceRows); err != nil {
		t.Fatalf("count evidence: %v", err)
	}
	deferredRows := countDeferredFor(t, s, fxEvidence)
	if evidenceRows != 1 || deferredRows != 0 {
		t.Fatalf("deferred evidence did not apply after its task arrived (evidence=%d deferred=%d)", evidenceRows, deferredRows)
	}
}

func TestProjectsSync_UndecodablePayloadIsParkedDeadNotRetriedForever(t *testing.T) {
	s := seedReplica(t)
	broken := mut(SyncEntityTask, fxTaskA, `{"sync_id":"`+fxTaskA+`","project":"`+fxProject+`"}`)

	if err := s.ApplyPulledChunk(DefaultSyncTargetKey, "chunk-broken", []SyncMutation{broken}); err != nil {
		t.Fatalf("a permanently invalid payload must not fail its chunk: %v", err)
	}
	if status := deferredStatusFor(t, s, fxTaskA); status != "dead" {
		t.Fatalf("expected a dead row for an unusable payload, got %q", status)
	}
}

// TestProjectsSync_RelationChunkFailureIsUnchanged pins the boundary of the
// change above: parking is for engram-projects entities only. A relation whose
// observations are missing must still fail its chunk exactly as it did before,
// because that behavior is part of the upstream contract.
func TestProjectsSync_RelationChunkFailureIsUnchanged(t *testing.T) {
	s := seedReplica(t)
	relation := SyncMutation{
		Entity:    SyncEntityRelation,
		EntityKey: "rel-0000000000000001",
		Op:        SyncOpUpsert,
		Payload: `{"sync_id":"rel-0000000000000001","source_id":"obs-missing-1","target_id":"obs-missing-2",` +
			`"relation":"supersedes","judgment_status":"confirmed","project":"` + fxProject + `"}`,
		Project: fxProject,
		Source:  SyncSourceRemote,
	}
	if err := s.ApplyPulledChunk(DefaultSyncTargetKey, "chunk-relation", []SyncMutation{relation}); err == nil {
		t.Fatal("a relation FK miss inside a chunk must still fail the chunk")
	}
}

func TestProjectsSync_ChunkReplayIsIdempotent(t *testing.T) {
	s := seedReplica(t)
	batch := []SyncMutation{
		mut(SyncEntityProjectCard, fxCardSync, cardPayload(t, "Nextcloud", "2026-01-01 09:00:00", nil, nil)),
		mut(SyncEntityTask, fxTaskC, taskPayload(t, syncTaskPayload{
			SyncID: fxTaskC, SDDChange: strp("cdbs-1"), Title: "t", Kind: "feature", State: "open",
			CreatedAt: "2026-01-03 10:00:00", UpdatedAt: "2026-01-03 10:00:00",
		})),
		mut(SyncEntityEvidence, fxEvidence, evidencePayload(t, fxTaskC, false, nil, "2026-01-04 10:00:00")),
	}
	if err := s.ApplyPulledChunk(DefaultSyncTargetKey, "chunk-once", batch); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	first := dumpProjectsState(t, s)
	if err := s.ApplyPulledChunk(DefaultSyncTargetKey, "chunk-once", batch); err != nil {
		t.Fatalf("re-apply of the same chunk id: %v", err)
	}
	if got := dumpProjectsState(t, s); got != first {
		t.Fatalf("re-applying chunk-once changed state:\n--- first ---\n%s\n--- second ---\n%s", first, got)
	}
}

// ─── Enqueue half ────────────────────────────────────────────────────────────

func pendingProjectsMutations(t *testing.T, s *Store) []string {
	t.Helper()
	rows, err := s.db.Query(
		`SELECT entity, entity_key FROM sync_mutations
		 WHERE entity IN (?,?,?,?,?) ORDER BY seq`,
		SyncEntityProjectCard, SyncEntityTask, SyncEntityEvidence,
		SyncEntityTaskLink, SyncEntityObservationRef)
	if err != nil {
		t.Fatalf("list mutations: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var entity, key string
		if err := rows.Scan(&entity, &key); err != nil {
			t.Fatalf("scan mutation: %v", err)
		}
		out = append(out, entity+":"+key)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list mutations: %v", err)
	}
	return out
}

// writeOneOfEachEntity exercises every local writer that RFC section 10.2
// expects to journal a mutation.
func writeOneOfEachEntity(t *testing.T, s *Store) {
	t.Helper()
	if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{Slug: fxProject, DisplayName: strp("Nextcloud")}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}
	if err := s.StampProjectGraph(fxProject, strings.Repeat("1", 40), "2026-01-02 08:00:00", nil); err != nil {
		t.Fatalf("StampProjectGraph: %v", err)
	}
	result, err := s.UpsertTask(UpsertTaskParams{
		Project: fxProject, JiraKey: strp(fxJiraKey), Title: strp("t"), Kind: strp("bugfix"),
	})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	if err := s.CreateSession("sess-enqueue", fxProject, t.TempDir()); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	obsID, err := s.AddObservation(AddObservationParams{
		SessionID: "sess-enqueue", Type: "manual", Title: "t", Content: "c", Project: fxProject,
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	if _, err := s.LinkTaskObservation(LinkTaskObservationParams{
		Task: result.Task, ObservationID: obsID, KnowledgeRef: strp("Runbooks/RB-003.md"),
	}); err != nil {
		t.Fatalf("LinkTaskObservation: %v", err)
	}
	if _, _, _, err := s.AddEvidence(AddEvidenceParams{
		Task: result.Task, Path: "evidence/shot.png", SHA256: strings.Repeat("a", 64),
		Kind: "png", Proves: "it works",
	}); err != nil {
		t.Fatalf("AddEvidence: %v", err)
	}
}

func TestProjectsSync_NothingIsJournalledWhileTheFlagIsOff(t *testing.T) {
	t.Setenv(projectsSyncEnvVar, "0")
	s := newProjectsSchemaTestStore(t)
	writeOneOfEachEntity(t, s)

	if got := pendingProjectsMutations(t, s); len(got) != 0 {
		t.Fatalf("ENGRAM_PROJECTS_SYNC=0 must not journal anything, got %v", got)
	}
}

func TestProjectsSync_EveryWriterJournalsItsEntity(t *testing.T) {
	t.Setenv(projectsSyncEnvVar, "1")
	s := newProjectsSchemaTestStore(t)
	writeOneOfEachEntity(t, s)

	got := pendingProjectsMutations(t, s)
	seen := map[string]bool{}
	for _, entry := range got {
		seen[strings.SplitN(entry, ":", 2)[0]] = true
	}
	for _, entity := range ProjectsSyncEntities() {
		if !seen[entity] {
			t.Fatalf("no mutation journalled for %q; got %v", entity, got)
		}
	}

	// Every journalled payload must survive the doctor's own validation: an
	// entity it does not recognize is reported as a blocking finding.
	rows, err := s.db.Query(
		`SELECT entity, op, payload, entity_key FROM sync_mutations WHERE entity IN (?,?,?,?,?)`,
		SyncEntityProjectCard, SyncEntityTask, SyncEntityEvidence,
		SyncEntityTaskLink, SyncEntityObservationRef)
	if err != nil {
		t.Fatalf("list mutations: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var entity, op, payload, entityKey string
		if err := rows.Scan(&entity, &op, &payload, &entityKey); err != nil {
			t.Fatalf("scan mutation: %v", err)
		}
		validation := ValidateSyncMutationPayload(entity, op, payload, entityKey)
		if validation.ReasonCode != "" {
			t.Fatalf("doctor rejects a journalled %s mutation: %s (%v)",
				entity, validation.Message, validation.MissingFields)
		}
		key, err := ProjectsEntityKey(entity, []byte(payload))
		if err != nil {
			t.Fatalf("derive entity key for %s: %v", entity, err)
		}
		if key != entityKey {
			t.Fatalf("%s entity_key %q does not match the key derived from its payload %q", entity, entityKey, key)
		}
	}
}

// TestProjectsSync_MutationAndRowShareOneTransaction proves the atomicity the
// enqueue half claims: a failure inside the writing transaction leaves neither
// the row nor its mutation behind.
func TestProjectsSync_MutationAndRowShareOneTransaction(t *testing.T) {
	t.Setenv(projectsSyncEnvVar, "1")
	s := newProjectsSchemaTestStore(t)

	// A task whose kind violates the DDL CHECK: the insert fails, and the
	// mutation that would have replicated it must not survive on its own.
	_, err := s.UpsertTask(UpsertTaskParams{
		Project: fxProject, JiraKey: strp(fxJiraKey), Title: strp("t"), Kind: strp("not-a-kind"),
	})
	if err == nil {
		t.Fatal("expected the DDL CHECK to reject an invalid kind")
	}
	for _, entry := range pendingProjectsMutations(t, s) {
		if strings.HasPrefix(entry, SyncEntityTask+":") {
			t.Fatalf("a task mutation outlived its rejected row: %v", entry)
		}
	}
	var tasks int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks`).Scan(&tasks); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if tasks != 0 {
		t.Fatalf("expected no task row, got %d", tasks)
	}
}

// ─── Doctor read model ───────────────────────────────────────────────────────

func TestProjectsSyncStatus_ReportsBacklogPerEntity(t *testing.T) {
	t.Setenv(projectsSyncEnvVar, "1")
	s := newProjectsSchemaTestStore(t)
	writeOneOfEachEntity(t, s)

	status, err := s.ProjectsSyncStatus(fxProject)
	if err != nil {
		t.Fatalf("ProjectsSyncStatus: %v", err)
	}
	if !status.Enabled {
		t.Fatal("status.Enabled must follow ENGRAM_PROJECTS_SYNC")
	}
	if status.TotalPending == 0 {
		t.Fatalf("expected a pending backlog, got %+v", status)
	}
	entities := make([]string, 0, len(status.Entities))
	for _, e := range status.Entities {
		entities = append(entities, e.Entity)
	}
	want := ProjectsSyncEntities()
	sortedEntities := append([]string{}, entities...)
	sortedWant := append([]string{}, want...)
	sort.Strings(sortedEntities)
	sort.Strings(sortedWant)
	if strings.Join(sortedEntities, ",") != strings.Join(sortedWant, ",") {
		t.Fatalf("status must report every entity, got %v", entities)
	}

	// A different project must not see this project's backlog.
	other, err := s.ProjectsSyncStatus("middleware")
	if err != nil {
		t.Fatalf("ProjectsSyncStatus(other): %v", err)
	}
	if other.TotalPending != 0 {
		t.Fatalf("project scoping leaked %d mutations into another project", other.TotalPending)
	}
}
