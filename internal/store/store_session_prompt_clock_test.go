package store

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// distantPast is a fixture timestamp used to rewind a row's clock so a
// subsequent write's effect on it is observable regardless of the wall
// clock's resolution (SQLite's datetime('now') has 1-second resolution, and
// two writes inside the same test can otherwise land on the same second).
const distantPast = "2020-01-01 00:00:00"

func readSessionUpdatedAt(t *testing.T, s *Store, id string) string {
	t.Helper()
	var updatedAt string
	if err := s.db.QueryRow(`SELECT ifnull(updated_at, '') FROM sessions WHERE id = ?`, id).Scan(&updatedAt); err != nil {
		t.Fatalf("read sessions.updated_at for %q: %v", id, err)
	}
	return updatedAt
}

func rewindSessionUpdatedAt(t *testing.T, s *Store, id, past string) {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE sessions SET updated_at = ? WHERE id = ?`, past, id); err != nil {
		t.Fatalf("rewind sessions.updated_at for %q: %v", id, err)
	}
}

// ─── Invariant 2: every real write path advances the clock ────────────────

// TestCreateSession_AdvancesUpdatedAt is the "creación" write path from the
// per-table clock-advance inventory: sessions had no updated_at column at
// all, so the fact that CreateSession stamps one — and keeps stamping it
// when a later call actually backfills project/directory — is what this
// asserts.
func TestCreateSession_AdvancesUpdatedAt(t *testing.T) {
	s := newTestStore(t)

	if err := s.CreateSession("sess-clock-create", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	first := readSessionUpdatedAt(t, s, "sess-clock-create")
	if strings.TrimSpace(first) == "" {
		t.Fatalf("CreateSession did not stamp updated_at")
	}

	// Simulate a session row that lost its project/directory (as a prior
	// partial write might race), with its clock rewound to the past.
	// CreateSession backfilling them is a real change to the row and must
	// advance the clock, not just re-affirm it.
	if _, err := s.db.Exec(`UPDATE sessions SET project = '', directory = '', updated_at = ? WHERE id = ?`, distantPast, "sess-clock-create"); err != nil {
		t.Fatalf("rewind fixture: %v", err)
	}

	if err := s.CreateSession("sess-clock-create", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("CreateSession (backfill): %v", err)
	}
	second := readSessionUpdatedAt(t, s, "sess-clock-create")
	if second == distantPast {
		t.Fatalf("CreateSession backfilled project/directory but did not advance updated_at: still %q", second)
	}
}

// TestCreateSession_NoOpReaffirmationDoesNotAdvanceClock guards the other
// direction: a CreateSession call that finds project/directory already set
// must NOT bump updated_at, or the clock would lie about the row having
// changed and could out-rank a genuinely newer pull on a later comparison
// purely because this session was touched again.
func TestCreateSession_NoOpReaffirmationDoesNotAdvanceClock(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateSession("sess-clock-noop", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	rewindSessionUpdatedAt(t, s, "sess-clock-noop", distantPast)

	if err := s.CreateSession("sess-clock-noop", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("CreateSession (re-affirm): %v", err)
	}
	got := readSessionUpdatedAt(t, s, "sess-clock-noop")
	if got != distantPast {
		t.Fatalf("CreateSession bumped updated_at on a no-op re-affirmation: %q, want unchanged %q", got, distantPast)
	}
}

// TestEndSession_AdvancesUpdatedAtOnClose is the "cierre de sesión" write
// path.
func TestEndSession_AdvancesUpdatedAtOnClose(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateSession("sess-clock-close", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	rewindSessionUpdatedAt(t, s, "sess-clock-close", distantPast)

	if err := s.EndSession("sess-clock-close", ""); err != nil {
		t.Fatalf("EndSession: %v", err)
	}
	got := readSessionUpdatedAt(t, s, "sess-clock-close")
	if got == distantPast {
		t.Fatalf("EndSession (close) did not advance updated_at")
	}
}

// TestEndSession_AdvancesUpdatedAtOnSummaryUpdate is the "resumen" write
// path: a session already closed gets a richer summary attached in a second
// EndSession call, which must advance the clock again.
func TestEndSession_AdvancesUpdatedAtOnSummaryUpdate(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateSession("sess-clock-summary", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := s.EndSession("sess-clock-summary", "first summary"); err != nil {
		t.Fatalf("EndSession (first close): %v", err)
	}
	rewindSessionUpdatedAt(t, s, "sess-clock-summary", distantPast)

	if err := s.EndSession("sess-clock-summary", "richer summary attached later"); err != nil {
		t.Fatalf("EndSession (summary update): %v", err)
	}
	got := readSessionUpdatedAt(t, s, "sess-clock-summary")
	if got == distantPast {
		t.Fatalf("EndSession (summary update) did not advance updated_at")
	}

	var summary sql.NullString
	if err := s.db.QueryRow(`SELECT summary FROM sessions WHERE id = ?`, "sess-clock-summary").Scan(&summary); err != nil {
		t.Fatalf("read summary: %v", err)
	}
	if !summary.Valid || summary.String != "richer summary attached later" {
		t.Fatalf("summary = %v, want the updated summary to have been written", summary)
	}
}

// TestAddPrompt_AdvancesUpdatedAt is the "captura" write path for prompts.
func TestAddPrompt_AdvancesUpdatedAt(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateSession("sess-prompt-clock", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	id, err := s.AddPrompt(AddPromptParams{SessionID: "sess-prompt-clock", Content: "hello", Project: "engram"})
	if err != nil {
		t.Fatalf("AddPrompt: %v", err)
	}
	var updatedAt string
	if err := s.db.QueryRow(`SELECT ifnull(updated_at, '') FROM user_prompts WHERE id = ?`, id).Scan(&updatedAt); err != nil {
		t.Fatalf("read user_prompts.updated_at: %v", err)
	}
	if strings.TrimSpace(updatedAt) == "" {
		t.Fatalf("AddPrompt did not stamp updated_at")
	}
}

// TestDeletePrompt_AdvancesTombstoneClock is the "borrado lógico" write path
// for prompts: DeletePrompt hard-deletes the user_prompts row, so its
// modification clock going forward is prompt_tombstones.deleted_at (see
// isStalePromptUpsert). A stale, previously-seeded tombstone must be
// overwritten with a fresher deleted_at on a real delete, not left standing.
func TestDeletePrompt_AdvancesTombstoneClock(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateSession("sess-prompt-delete", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	id, err := s.AddPrompt(AddPromptParams{SessionID: "sess-prompt-delete", Content: "prompt to delete", Project: "engram"})
	if err != nil {
		t.Fatalf("AddPrompt: %v", err)
	}
	var syncID string
	if err := s.db.QueryRow(`SELECT sync_id FROM user_prompts WHERE id = ?`, id).Scan(&syncID); err != nil {
		t.Fatalf("read sync_id: %v", err)
	}

	// Seed a stale tombstone for this sync_id, as if this prompt had been
	// deleted once before, at some point in the past.
	if _, err := s.db.Exec(
		`INSERT INTO prompt_tombstones (sync_id, session_id, project, deleted_at) VALUES (?, ?, ?, ?)`,
		syncID, "sess-prompt-delete", "engram", distantPast,
	); err != nil {
		t.Fatalf("seed stale tombstone: %v", err)
	}

	if err := s.DeletePrompt(id); err != nil {
		t.Fatalf("DeletePrompt: %v", err)
	}

	var deletedAt string
	if err := s.db.QueryRow(`SELECT deleted_at FROM prompt_tombstones WHERE sync_id = ?`, syncID).Scan(&deletedAt); err != nil {
		t.Fatalf("read tombstone: %v", err)
	}
	if deletedAt == distantPast {
		t.Fatalf("DeletePrompt did not advance the tombstone clock past the stale seed value %q", distantPast)
	}
}

// TestApplySessionProjectReclassification_AdvancesClocks covers a write path
// the repo-wide inventory turned up beyond the three the task names
// explicitly: the doctor-repair project rename. Without bumping updated_at
// here, a pull carrying the pre-repair project would tie on timestamp and
// silently revert the repair on the next pull.
func TestApplySessionProjectReclassification_AdvancesClocks(t *testing.T) {
	s := newTestStore(t)
	seedRepairRows(t, s, "repair-clock-1", "sias-app")
	rewindSessionUpdatedAt(t, s, "repair-clock-1", distantPast)
	if _, err := s.db.Exec(`UPDATE user_prompts SET updated_at = ? WHERE session_id = ?`, distantPast, "repair-clock-1"); err != nil {
		t.Fatalf("rewind prompt clock: %v", err)
	}

	if _, err := s.ApplySessionProjectReclassification([]SessionProjectReclassification{
		{SessionID: "repair-clock-1", FromProject: "sias-app", ToProject: "engram"},
	}); err != nil {
		t.Fatalf("ApplySessionProjectReclassification: %v", err)
	}

	if got := readSessionUpdatedAt(t, s, "repair-clock-1"); got == distantPast {
		t.Fatalf("session project reclassification did not advance sessions.updated_at")
	}
	var promptUpdatedAt string
	if err := s.db.QueryRow(`SELECT updated_at FROM user_prompts WHERE session_id = ?`, "repair-clock-1").Scan(&promptUpdatedAt); err != nil {
		t.Fatalf("read prompt updated_at: %v", err)
	}
	if promptUpdatedAt == distantPast {
		t.Fatalf("session project reclassification did not advance user_prompts.updated_at")
	}
}

// ─── Invariant 3: the two upsert appliers gate a pull on the clock ─────────

// TestApplySessionUpsertRejectsAStaleProjectRevert mirrors
// TestApplyObservationUpsertRejectsAStaleProjectRevert for sessions: before
// this change, applySessionPayloadTx overwrote project and directory
// unconditionally on every pull because sessions had nothing to compare a
// pull against. A pull describing the pre-merge project — same original
// updated_at — must not win merely by arriving after MergeProjects renamed
// it.
func TestApplySessionUpsertRejectsAStaleProjectRevert(t *testing.T) {
	s := newTestStore(t)

	const originalUpdatedAt = "2026-01-01T00:00:00Z"
	if _, err := s.db.Exec(
		`INSERT INTO sessions (id, project, directory, started_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"s1", "old-project", "/work", originalUpdatedAt, originalUpdatedAt,
	); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	if _, err := s.MergeProjects([]string{"old-project"}, "new-project"); err != nil {
		t.Fatalf("MergeProjects: %v", err)
	}

	stalePull := SyncMutation{
		Seq:       1,
		TargetKey: DefaultSyncTargetKey,
		Entity:    SyncEntitySession,
		EntityKey: "s1",
		Op:        SyncOpUpsert,
		Payload: fmt.Sprintf(
			`{"id":"s1","project":"old-project","directory":"/work","started_at":%q,"updated_at":%q}`,
			originalUpdatedAt, originalUpdatedAt,
		),
	}
	if err := s.ApplyPulledMutation(DefaultSyncTargetKey, stalePull); err != nil {
		t.Fatalf("apply stale pull: %v", err)
	}

	var project string
	if err := s.db.QueryRow(`SELECT project FROM sessions WHERE id = ?`, "s1").Scan(&project); err != nil {
		t.Fatalf("read session: %v", err)
	}
	if project != "new-project" {
		t.Fatalf("project = %q after a stale pull, want %q preserved (last-write-wins must reject it)", project, "new-project")
	}
}

// TestApplySessionUpsertAppliesAGenuinelyNewerUpdate guards against the
// guard being overly conservative: a pull whose updated_at is actually later
// than what is stored must still apply.
func TestApplySessionUpsertAppliesAGenuinelyNewerUpdate(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.db.Exec(
		`INSERT INTO sessions (id, project, directory, started_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"s1", "engram", "/work", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	newerPull := SyncMutation{
		Seq:       1,
		TargetKey: DefaultSyncTargetKey,
		Entity:    SyncEntitySession,
		EntityKey: "s1",
		Op:        SyncOpUpsert,
		Payload:   `{"id":"s1","project":"engram","directory":"/work","started_at":"2026-01-01T00:00:00Z","summary":"finished","updated_at":"2026-01-02T00:00:00Z"}`,
	}
	if err := s.ApplyPulledMutation(DefaultSyncTargetKey, newerPull); err != nil {
		t.Fatalf("apply newer pull: %v", err)
	}

	var summary sql.NullString
	if err := s.db.QueryRow(`SELECT summary FROM sessions WHERE id = ?`, "s1").Scan(&summary); err != nil {
		t.Fatalf("read session: %v", err)
	}
	if !summary.Valid || summary.String != "finished" {
		t.Fatalf("summary = %v, want the genuinely newer pull to have applied", summary)
	}
}

// TestApplyPromptUpsertRejectsAStalePush is the prompts counterpart: before
// this change, applyPromptUpsertTx's UPDATE branch overwrote content,
// project and session_id unconditionally on every pull. A pull describing an
// earlier edit of the same prompt — older updated_at — must not win over a
// later local edit.
func TestApplyPromptUpsertRejectsAStalePush(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.db.Exec(`INSERT INTO sessions (id, project, directory) VALUES (?, ?, ?)`, "s1", "engram", "/work"); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	const originalUpdatedAt = "2026-01-01T00:00:00Z"
	const newerUpdatedAt = "2026-01-02T00:00:00Z"
	if _, err := s.db.Exec(
		`INSERT INTO user_prompts (sync_id, session_id, content, project, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"prompt-1", "s1", "edited content", "engram", originalUpdatedAt, newerUpdatedAt,
	); err != nil {
		t.Fatalf("seed prompt: %v", err)
	}

	stalePull := SyncMutation{
		Seq:       1,
		TargetKey: DefaultSyncTargetKey,
		Entity:    SyncEntityPrompt,
		EntityKey: "prompt-1",
		Op:        SyncOpUpsert,
		Payload: fmt.Sprintf(
			`{"sync_id":"prompt-1","session_id":"s1","content":"original content","project":"engram","created_at":%q,"updated_at":%q}`,
			originalUpdatedAt, originalUpdatedAt,
		),
	}
	if err := s.ApplyPulledMutation(DefaultSyncTargetKey, stalePull); err != nil {
		t.Fatalf("apply stale pull: %v", err)
	}

	var content string
	if err := s.db.QueryRow(`SELECT content FROM user_prompts WHERE sync_id = ?`, "prompt-1").Scan(&content); err != nil {
		t.Fatalf("read prompt: %v", err)
	}
	if content != "edited content" {
		t.Fatalf("content = %q after a stale pull, want %q preserved (last-write-wins must reject it)", content, "edited content")
	}
}

// TestApplyPromptUpsertAppliesAGenuinelyNewerUpdate guards against the guard
// being overly conservative for prompts.
func TestApplyPromptUpsertAppliesAGenuinelyNewerUpdate(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.db.Exec(`INSERT INTO sessions (id, project, directory) VALUES (?, ?, ?)`, "s1", "engram", "/work"); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO user_prompts (sync_id, session_id, content, project, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"prompt-1", "s1", "original content", "engram", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("seed prompt: %v", err)
	}

	newerPull := SyncMutation{
		Seq:       1,
		TargetKey: DefaultSyncTargetKey,
		Entity:    SyncEntityPrompt,
		EntityKey: "prompt-1",
		Op:        SyncOpUpsert,
		Payload:   `{"sync_id":"prompt-1","session_id":"s1","content":"edited content","project":"engram","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-02T00:00:00Z"}`,
	}
	if err := s.ApplyPulledMutation(DefaultSyncTargetKey, newerPull); err != nil {
		t.Fatalf("apply newer pull: %v", err)
	}

	var content string
	if err := s.db.QueryRow(`SELECT content FROM user_prompts WHERE sync_id = ?`, "prompt-1").Scan(&content); err != nil {
		t.Fatalf("read prompt: %v", err)
	}
	if content != "edited content" {
		t.Fatalf("content = %q, want the genuinely newer pull to have applied", content)
	}
}
