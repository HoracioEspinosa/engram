package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Evidence mirrors the evidence row (RFC §5.6).
type Evidence struct {
	ID      int64  `json:"id"`
	SyncID  string `json:"sync_id"`
	Project string `json:"project"`
	// TaskID is the tasks table's numeric primary key, exposed alongside
	// TaskSyncID (the cross-machine identity mem_evidence_list and the sync
	// pipeline filter by) so the TUI's Evidence tab can filter and deep-link
	// by the same local row id tabs.NavigateMsg already carries for
	// observations (rfc-tui.md §9.2's S6 query: `e.task_id = ?2`).
	TaskID     int64  `json:"task_id"`
	TaskSyncID string `json:"task_sync_id"`
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	// Category is the vault folder the capture belongs to. Where a file sits
	// is what tells a reader whether it is a plan, a patch or a benchmark run;
	// the bytes alone never say.
	Category              string  `json:"category"`
	Kind                  string  `json:"kind"`
	Proves                string  `json:"proves"`
	ConfigStamp           *string `json:"config_stamp,omitempty"`
	CapturedAt            string  `json:"captured_at"`
	AttachedJira          bool    `json:"attached_jira"`
	AttachedConfluenceURL *string `json:"attached_confluence_url,omitempty"`
	SizeBytes             *int64  `json:"size_bytes,omitempty"`
	ManifestPath          *string `json:"manifest_path,omitempty"`
}

const evidenceSelectColumns = `id, sync_id, project, task_id, task_sync_id, path, sha256, category, kind,
	proves, config_stamp, captured_at, attached_jira, attached_confluence_url, size_bytes, manifest_path`

func scanEvidence(row interface{ Scan(dest ...any) error }) (Evidence, error) {
	var e Evidence
	var attachedJira int
	err := row.Scan(&e.ID, &e.SyncID, &e.Project, &e.TaskID, &e.TaskSyncID, &e.Path, &e.SHA256, &e.Category,
		&e.Kind, &e.Proves, &e.ConfigStamp, &e.CapturedAt, &attachedJira, &e.AttachedConfluenceURL,
		&e.SizeBytes, &e.ManifestPath)
	e.AttachedJira = attachedJira == 1
	return e, err
}

// DefaultEvidenceCategory is where a capture lands when the caller does not say
// which vault folder it came from.
const DefaultEvidenceCategory = "evidences"

// evidenceKindByteLimits are the per-file caps from D-06 rule 3.
var evidenceKindByteLimits = map[string]int64{
	"png": 2097152,
	"gif": 5242880,
	"mp4": 8388608,
}

// evidenceTaskTotalByteLimit is D-06's total-per-task cap.
const evidenceTaskTotalByteLimit int64 = 41943040

// EvidenceLimits reports whether AddEvidence's D-06 size rules were
// respected. A violation never blocks the write — the caller (the
// capture-evidence skill) uses it to decide whether to fall back to
// Confluence.
type EvidenceLimits struct {
	OK         bool     `json:"ok"`
	Violations []string `json:"violations"`
}

// AddEvidenceParams holds mem_evidence_add's input, with Task already
// resolved by the MCP handler.
type AddEvidenceParams struct {
	Task   Task
	Path   string
	SHA256 string
	// Category is the vault folder the file came from. An empty value means
	// the caller did not say, and the row takes DefaultEvidenceCategory.
	Category              string
	Kind                  string
	Proves                string
	ConfigStamp           *string
	CapturedAt            *string
	SizeBytes             *int64
	ManifestPath          *string
	AttachedJira          bool
	AttachedConfluenceURL *string
}

// FindEvidenceBySHA returns the evidence a task already holds for these exact
// bytes, and whether there was one.
//
// It is the read half of AddEvidence's idempotency key, exposed because a dry
// run has to say "update" where an apply would, and the write path alone cannot
// answer that without writing.
func (s *Store) FindEvidenceBySHA(taskSyncID, sha256 string) (Evidence, bool, error) {
	e, err := scanEvidence(s.readDB().QueryRow(
		`SELECT `+evidenceSelectColumns+` FROM evidence
		 WHERE task_sync_id = ? AND sha256 = ? AND deleted_at IS NULL`,
		taskSyncID, sha256))
	if errors.Is(err, sql.ErrNoRows) {
		return Evidence{}, false, nil
	}
	if err != nil {
		return Evidence{}, false, fmt.Errorf("engram-projects: check duplicate evidence: %w", err)
	}
	return e, true, nil
}

// AddEvidence registers a captured evidence file, idempotent by
// (task_sync_id, sha256) (RFC §5.6).
//
// The same bytes found somewhere else are the same evidence: a file that was
// moved or refiled updates its path and category in place instead of being
// registered twice. Everything else about an evidence row stays immutable —
// the hash, when it was captured, what it proves.
func (s *Store) AddEvidence(p AddEvidenceParams) (Evidence, bool, EvidenceLimits, error) {
	category := strings.TrimSpace(p.Category)
	if category == "" {
		category = DefaultEvidenceCategory
	}

	existing, found, err := s.FindEvidenceBySHA(p.Task.SyncID, p.SHA256)
	if err != nil {
		return Evidence{}, false, EvidenceLimits{}, err
	}
	if found {
		if existing.Path != p.Path || existing.Category != category {
			if err := s.relocateEvidence(existing.ID, p.Path, category); err != nil {
				return Evidence{}, false, EvidenceLimits{}, err
			}
			existing.Path = p.Path
			existing.Category = category
		}
		limits, limErr := s.evidenceLimitsForTask(p.Task.SyncID, "", 0)
		return existing, true, limits, limErr
	}

	capturedAt := p.CapturedAt
	now := s.nowUTC()
	captured := now
	if capturedAt != nil && strings.TrimSpace(*capturedAt) != "" {
		captured = *capturedAt
	}

	syncID := newSyncID("evd")
	var attachedJira int
	if p.AttachedJira {
		attachedJira = 1
	}
	var id int64
	if err := s.withTx(func(tx *sql.Tx) error {
		res, err := s.execHook(tx, `
			INSERT INTO evidence (sync_id, project, task_id, task_sync_id, path, sha256, category, kind, proves,
				config_stamp, captured_at, attached_jira, attached_confluence_url, size_bytes, manifest_path,
				location_set_at, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			syncID, p.Task.Project, p.Task.ID, p.Task.SyncID, p.Path, p.SHA256, category, p.Kind, p.Proves,
			nullableStr(p.ConfigStamp), captured, attachedJira, nullableStr(p.AttachedConfluenceURL),
			nullableInt64(p.SizeBytes), nullableStr(p.ManifestPath), now, now)
		if err != nil {
			return fmt.Errorf("engram-projects: insert evidence: %w", err)
		}
		id, err = res.LastInsertId()
		if err != nil {
			return err
		}
		return s.enqueueEvidenceTx(tx, id)
	}); err != nil {
		return Evidence{}, false, EvidenceLimits{}, err
	}

	limits, err := s.evidenceLimitsForTask(p.Task.SyncID, p.Kind, valInt64(p.SizeBytes))
	if err != nil {
		return Evidence{}, false, EvidenceLimits{}, err
	}

	saved, err := scanEvidence(s.db.QueryRow(`SELECT `+evidenceSelectColumns+` FROM evidence WHERE id = ?`, id))
	if err != nil {
		return Evidence{}, false, EvidenceLimits{}, fmt.Errorf("engram-projects: reload evidence: %w", err)
	}
	return saved, false, limits, nil
}

// relocateEvidence moves an evidence row to a new path and category. The pair
// travels together because they answer one question — where the file is — and
// letting them disagree is what makes a re-scan look like a second capture.
func (s *Store) relocateEvidence(id int64, path, category string) error {
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := s.execHook(tx,
			`UPDATE evidence SET path = ?, category = ?, location_set_at = ? WHERE id = ?`,
			path, category, s.nowUTC(), id,
		); err != nil {
			return fmt.Errorf("engram-projects: relocate evidence: %w", err)
		}
		return s.enqueueEvidenceTx(tx, id)
	})
}

// evidenceLimitsForTask computes the D-06 violations for a task, optionally
// checking one additional (kind, size) pair that was just inserted (its
// size_bytes is already counted in the SUM, so extraKind/extraSize are only
// used to phrase the per-kind violation message; pass "" / 0 to skip).
func (s *Store) evidenceLimitsForTask(taskSyncID, extraKind string, extraSize int64) (EvidenceLimits, error) {
	limits := EvidenceLimits{OK: true}

	if extraKind != "" && extraSize > 0 {
		if max, ok := evidenceKindByteLimits[extraKind]; ok && extraSize > max {
			limits.OK = false
			limits.Violations = append(limits.Violations, fmt.Sprintf(
				"%s evidence exceeds %d bytes limit (attached size %d)", extraKind, max, extraSize))
		}
	}

	var totalBytes sql.NullInt64
	if err := s.db.QueryRow(
		`SELECT SUM(size_bytes) FROM evidence WHERE task_sync_id = ? AND deleted_at IS NULL`, taskSyncID,
	).Scan(&totalBytes); err != nil {
		return limits, fmt.Errorf("engram-projects: sum evidence size: %w", err)
	}
	if totalBytes.Valid && totalBytes.Int64 > evidenceTaskTotalByteLimit {
		limits.OK = false
		limits.Violations = append(limits.Violations, fmt.Sprintf(
			"task evidence total exceeds %d bytes limit (current total %d)", evidenceTaskTotalByteLimit, totalBytes.Int64))
	}
	return limits, nil
}

// defaultEvidenceListLimit is the page size an evidence listing takes when the
// caller names none. It is spelled once so ListEvidencePage reports the same
// number the query actually applied.
const defaultEvidenceListLimit = 50

// EvidenceListFilter holds mem_evidence_list's filter parameters.
type EvidenceListFilter struct {
	TaskSyncID string // "" lists the whole project
	// TaskID scopes the list to one task by its numeric row id instead of
	// its sync_id. mem_evidence_list never sets it (TaskSyncID is the
	// cross-machine identity the MCP tool resolves task refs to); it exists
	// for the TUI's Evidence tab, whose deep link from a task's detail
	// screen carries only the row id tabs.NavigateMsg.TaskID gives it. 0
	// means "no task filter", matching TaskSyncID's "" convention.
	TaskID       int64
	AttachedJira *bool
	Kind         string
	// Category narrows the list to one vault folder.
	Category string
	// Query is a full-text search over the path, what the capture proves, its
	// category, its kind and its project.
	Query  string
	Limit  int
	Offset int
}

// EvidenceListItem is one row of mem_evidence_list's items array.
type EvidenceListItem struct {
	Evidence
	JiraKey   *string `json:"jira_key,omitempty"`
	TaskTitle *string `json:"task_title,omitempty"`
}

// ListEvidence lists evidence for a project, optionally scoped to one task
// (RFC §5.7).
func (s *Store) ListEvidence(project string, f EvidenceListFilter) ([]EvidenceListItem, int, int64, error) {
	where := []string{"e.project = ?", "e.deleted_at IS NULL"}
	args := []any{project}
	if f.TaskSyncID != "" {
		where = append(where, "e.task_sync_id = ?")
		args = append(args, f.TaskSyncID)
	}
	if f.TaskID != 0 {
		where = append(where, "e.task_id = ?")
		args = append(args, f.TaskID)
	}
	if f.AttachedJira != nil {
		v := 0
		if *f.AttachedJira {
			v = 1
		}
		where = append(where, "e.attached_jira = ?")
		args = append(args, v)
	}
	if f.Kind != "" {
		where = append(where, "e.kind = ?")
		args = append(args, f.Kind)
	}
	if f.Category != "" {
		where = append(where, "e.category = ?")
		args = append(args, f.Category)
	}
	if strings.TrimSpace(f.Query) != "" {
		where = append(where, "e.id IN (SELECT rowid FROM evidence_fts WHERE evidence_fts MATCH ?)")
		args = append(args, sanitizeFTS(f.Query))
	}
	whereSQL := strings.Join(where, " AND ")

	rdb := s.readDB()

	var total int
	var totalBytes sql.NullInt64
	if err := s.queryRowHook(rdb, `SELECT COUNT(*), SUM(e.size_bytes) FROM evidence e WHERE `+whereSQL, args...).
		Scan(&total, &totalBytes); err != nil {
		return nil, 0, 0, fmt.Errorf("engram-projects: count evidence: %w", err)
	}

	limit := f.Limit
	if limit <= 0 {
		limit = defaultEvidenceListLimit
	}
	listArgs := append(append([]any{}, args...), limit, f.Offset)
	rows, err := s.queryHook(rdb, `
		SELECT e.id, e.sync_id, e.project, e.task_id, e.task_sync_id, e.path, e.sha256, e.category, e.kind,
		       e.proves, e.config_stamp, e.captured_at, e.attached_jira, e.attached_confluence_url,
		       e.size_bytes, e.manifest_path, t.jira_key, t.title
		FROM evidence e
		JOIN tasks t ON t.id = e.task_id
		WHERE `+whereSQL+`
		ORDER BY e.captured_at DESC LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("engram-projects: list evidence: %w", err)
	}
	defer rows.Close()

	var items []EvidenceListItem
	for rows.Next() {
		var item EvidenceListItem
		var attachedJira int
		if err := rows.Scan(&item.ID, &item.SyncID, &item.Project, &item.TaskID, &item.TaskSyncID, &item.Path,
			&item.SHA256, &item.Category, &item.Kind, &item.Proves, &item.ConfigStamp, &item.CapturedAt,
			&attachedJira, &item.AttachedConfluenceURL, &item.SizeBytes, &item.ManifestPath,
			&item.JiraKey, &item.TaskTitle,
		); err != nil {
			return nil, 0, 0, err
		}
		item.AttachedJira = attachedJira == 1
		items = append(items, item)
	}
	return items, total, totalBytes.Int64, rows.Err()
}

// ListEvidencePage is ListEvidence with the page's own shape reported alongside
// it. The byte total stays a separate return rather than a field on the page:
// it is a property of the filtered set, not of the page, and folding it into
// Page would make it mean something different there than everywhere else.
func (s *Store) ListEvidencePage(project string, f EvidenceListFilter) (Page[EvidenceListItem], int64, error) {
	items, total, totalBytes, err := s.ListEvidence(project, f)
	if err != nil {
		return Page[EvidenceListItem]{}, 0, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = defaultEvidenceListLimit
	}
	return Page[EvidenceListItem]{Items: items, Total: total, Limit: limit, Offset: f.Offset}, totalBytes, nil
}

func nullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func valInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}
