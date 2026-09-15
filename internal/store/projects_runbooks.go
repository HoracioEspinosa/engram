package store

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var runbookIDPattern = regexp.MustCompile(`^RB-[0-9]{3}$`)

var runbookValidStatuses = map[string]bool{"draft": true, "verified": true, "outdated": true}

// RunbookStaleAgeDays is D-11's freshness threshold: a runbook entry older
// than this many days is stale regardless of what its source claims. It is
// the single source of truth for the threshold — internal/runbooks reuses it
// instead of keeping a second copy that could drift out of sync with this
// one.
const RunbookStaleAgeDays = 90

// RunbookAgeDays parses a "YYYY-MM-DD"-prefixed date string and reports how
// many whole days have elapsed since then, relative to now. ok is false when
// raw carries no parseable date.
//
// It is the single place that turns a stored date into an age in days: the
// vault scanner (internal/runbooks) derives entry.AgeDays through it, and
// SyncRunbookIndex falls back to it below, so a runbook's age is never
// computed two different ways.
func RunbookAgeDays(raw string, now time.Time) (int, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) < 10 {
		return 0, false
	}
	t, err := time.Parse("2006-01-02", raw[:10])
	if err != nil {
		return 0, false
	}
	days := int(now.UTC().Sub(t.UTC()).Hours() / 24)
	if days < 0 {
		days = 0
	}
	return days, true
}

// RunbookIndexEntryInput is one entry of mem_runbook_index_sync's `entries`
// array (RFC §5.8).
type RunbookIndexEntryInput struct {
	ID              string
	VaultPath       string
	Title           string
	Service         string
	Category        string
	Pattern         string
	Severity        string
	Status          string
	Symptoms        []string
	Tags            []string
	Owner           string
	AutomationLevel string
	LastUpdated     string
	LastVerified    string
	// NeedsReview is the "knowledge-mcp" source's own freshness claim. A
	// non-nil value — true or false — is honored as-is; nil means the source
	// made no claim at all and SyncRunbookIndex falls back to the age
	// derived from LastUpdated instead of treating the absence as "fresh".
	NeedsReview *bool
	AgeDays     *int
}

// RunbookIndexSyncParams holds mem_runbook_index_sync's input.
type RunbookIndexSyncParams struct {
	Project      string // optional filter; scopes prune_missing when set
	Source       string // "knowledge-mcp" | "vault-fs"
	PruneMissing bool
	Entries      []RunbookIndexEntryInput

	// ResolveService maps a vault `service:` value to the project slug the
	// index is keyed by, rejecting anything outside the canonical list. The
	// map itself lives in internal/runbooks, which imports this package, so
	// it is injected rather than imported; runbooks.SyncIndex is the entry
	// point that wires it for every caller (RFC §9.3).
	//
	// A nil resolver keeps the lenient fallback: the value is normalized as
	// a project name and only a blank one is rejected, with reason
	// `missing_service`.
	ResolveService func(raw string) (slug string, ok bool)
}

// RunbookSkipped is one row of mem_runbook_index_sync's `skipped` array.
type RunbookSkipped struct {
	ID        string `json:"id,omitempty"`
	VaultPath string `json:"vault_path,omitempty"`
	Reason    string `json:"reason"`
}

// RunbookSyncResult is the outcome of SyncRunbookIndex.
type RunbookSyncResult struct {
	Upserted       int              `json:"upserted"`
	Unchanged      int              `json:"unchanged"`
	Pruned         int              `json:"pruned"`
	Skipped        []RunbookSkipped `json:"skipped"`
	StaleCount     int              `json:"stale_count"`
	ExecRecomputed int              `json:"exec_recomputed"`
}

// SyncRunbookIndex rebuilds the runbook index from entries obtained
// externally from the knowledge vault (RFC §5.8). Templates and malformed
// entries are reported in `skipped` rather than rejecting the whole call.
func (s *Store) SyncRunbookIndex(p RunbookIndexSyncParams) (RunbookSyncResult, error) {
	var result RunbookSyncResult
	processedIDs := map[string]bool{}
	touchedProjects := map[string]bool{}

	for _, e := range p.Entries {
		if hasTemplateTag(e.Tags) || strings.HasPrefix(e.VaultPath, "Runbooks/Templates/") {
			result.Skipped = append(result.Skipped, RunbookSkipped{ID: e.ID, VaultPath: e.VaultPath, Reason: "template"})
			continue
		}
		if !runbookIDPattern.MatchString(e.ID) {
			result.Skipped = append(result.Skipped, RunbookSkipped{ID: e.ID, VaultPath: e.VaultPath, Reason: "invalid_id"})
			continue
		}
		if !runbookValidStatuses[e.Status] {
			result.Skipped = append(result.Skipped, RunbookSkipped{ID: e.ID, VaultPath: e.VaultPath, Reason: "invalid_status"})
			continue
		}

		project, reason := resolveRunbookService(e.Service, p.ResolveService)
		if reason != "" {
			result.Skipped = append(result.Skipped, RunbookSkipped{ID: e.ID, VaultPath: e.VaultPath, Reason: reason})
			continue
		}
		if _, err := s.ensureMinimalProjectCard(project); err != nil {
			return result, err
		}
		touchedProjects[project] = true
		processedIDs[e.ID] = true

		stale := 0
		switch p.Source {
		case "vault-fs":
			if e.AgeDays != nil && *e.AgeDays > RunbookStaleAgeDays {
				stale = 1
			}
		default: // "knowledge-mcp"
			if e.NeedsReview != nil {
				// An explicit claim from the source is honored as-is: only
				// an explicit true marks the row stale.
				if *e.NeedsReview {
					stale = 1
				}
			} else if age, ok := RunbookAgeDays(e.LastUpdated, time.Now()); ok && age > RunbookStaleAgeDays {
				// No explicit claim was made. Absence of needs_review is not
				// itself a freshness claim, so fall back to the age computed
				// from last_updated — the same rule vault-fs applies to its
				// own age_days.
				stale = 1
			}
		}
		if stale == 1 {
			result.StaleCount++
		}
		symptoms := strings.Join(e.Symptoms, "\n")

		changed, err := s.upsertRunbookIndexRow(e, project, symptoms, stale)
		if err != nil {
			return result, err
		}
		if changed {
			result.Upserted++
		} else {
			result.Unchanged++
		}

		execCount, lastExecAt, err := s.recomputeRunbookExec(e.ID)
		if err != nil {
			return result, err
		}
		if _, err := s.db.Exec(`UPDATE runbook_index SET exec_count = ?, last_exec_at = ? WHERE id = ?`,
			execCount, lastExecAt, e.ID); err != nil {
			return result, fmt.Errorf("engram-projects: update runbook exec stats: %w", err)
		}
		result.ExecRecomputed++
	}

	if p.PruneMissing {
		scope := touchedProjects
		if strings.TrimSpace(p.Project) != "" {
			normalized, _ := NormalizeProject(p.Project)
			scope = map[string]bool{normalized: true}
		}
		for project := range scope {
			rows, err := s.db.Query(`SELECT id FROM runbook_index WHERE project = ?`, project)
			if err != nil {
				return result, fmt.Errorf("engram-projects: list runbooks for prune: %w", err)
			}
			var toPrune []string
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return result, err
				}
				if !processedIDs[id] {
					toPrune = append(toPrune, id)
				}
			}
			rows.Close()
			for _, id := range toPrune {
				if _, err := s.db.Exec(`DELETE FROM runbook_index WHERE id = ?`, id); err != nil {
					return result, fmt.Errorf("engram-projects: prune runbook %s: %w", id, err)
				}
				result.Pruned++
			}
		}
	}

	return result, nil
}

// resolveRunbookService turns an entry's `service` into the project slug its
// index row is keyed by, returning the skip reason instead when it cannot.
func resolveRunbookService(raw string, resolve func(string) (string, bool)) (project, reason string) {
	if resolve != nil {
		slug, ok := resolve(raw)
		if !ok {
			if strings.TrimSpace(raw) == "" {
				return "", "missing_service"
			}
			return "", "unknown_service"
		}
		return slug, ""
	}
	slug, _ := NormalizeProject(raw)
	if slug == "" {
		return "", "missing_service"
	}
	return slug, ""
}

func hasTemplateTag(tags []string) bool {
	for _, t := range tags {
		if strings.EqualFold(strings.TrimSpace(t), "template") {
			return true
		}
	}
	return false
}

// upsertRunbookIndexRow inserts or updates one runbook_index row, returning
// changed=true when the row was newly created or any comparable field
// differed from what was already stored.
func (s *Store) upsertRunbookIndexRow(e RunbookIndexEntryInput, project, symptoms string, stale int) (bool, error) {
	var existing struct {
		vaultPath, title, category, pattern, severity, status, symptoms, owner, automationLevel string
		lastUpdated, lastVerified                                                               string
		stale                                                                                   int
		ageDays                                                                                 sql.NullInt64
	}
	err := s.db.QueryRow(`
		SELECT vault_path, title, category, COALESCE(pattern, ''), COALESCE(severity, ''), status, symptoms,
		       COALESCE(owner, ''), COALESCE(automation_level, ''), COALESCE(last_updated, ''),
		       COALESCE(last_verified, ''), stale, age_days
		FROM runbook_index WHERE id = ?`, e.ID,
	).Scan(&existing.vaultPath, &existing.title, &existing.category, &existing.pattern, &existing.severity,
		&existing.status, &existing.symptoms, &existing.owner, &existing.automationLevel,
		&existing.lastUpdated, &existing.lastVerified, &existing.stale, &existing.ageDays)

	now := s.nowUTC()
	ageDays := any(nil)
	if e.AgeDays != nil {
		ageDays = *e.AgeDays
	}

	if errors.Is(err, sql.ErrNoRows) {
		_, insertErr := s.db.Exec(`
			INSERT INTO runbook_index (id, project, vault_path, title, category, pattern, severity, status,
				symptoms, owner, automation_level, last_updated, last_verified, stale, age_days, synced_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			e.ID, project, e.VaultPath, e.Title, e.Category, nullableEmptyStr(e.Pattern), nullableEmptyStr(e.Severity),
			e.Status, symptoms, nullableEmptyStr(e.Owner), nullableEmptyStr(e.AutomationLevel),
			nullableEmptyStr(e.LastUpdated), nullableEmptyStr(e.LastVerified), stale, ageDays, now)
		if insertErr != nil {
			return false, fmt.Errorf("engram-projects: insert runbook: %w", insertErr)
		}
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("engram-projects: read runbook: %w", err)
	}

	sameAge := (!existing.ageDays.Valid && e.AgeDays == nil) ||
		(existing.ageDays.Valid && e.AgeDays != nil && existing.ageDays.Int64 == int64(*e.AgeDays))
	unchanged := existing.vaultPath == e.VaultPath && existing.title == e.Title && existing.category == e.Category &&
		existing.pattern == e.Pattern && existing.severity == e.Severity && existing.status == e.Status &&
		existing.symptoms == symptoms && existing.owner == e.Owner && existing.automationLevel == e.AutomationLevel &&
		existing.lastUpdated == e.LastUpdated && existing.lastVerified == e.LastVerified &&
		existing.stale == stale && sameAge

	if unchanged {
		return false, nil
	}

	_, updateErr := s.db.Exec(`
		UPDATE runbook_index SET project = ?, vault_path = ?, title = ?, category = ?, pattern = ?, severity = ?,
			status = ?, symptoms = ?, owner = ?, automation_level = ?, last_updated = ?, last_verified = ?,
			stale = ?, age_days = ?, synced_at = ?
		WHERE id = ?`,
		project, e.VaultPath, e.Title, e.Category, nullableEmptyStr(e.Pattern), nullableEmptyStr(e.Severity),
		e.Status, symptoms, nullableEmptyStr(e.Owner), nullableEmptyStr(e.AutomationLevel),
		nullableEmptyStr(e.LastUpdated), nullableEmptyStr(e.LastVerified), stale, ageDays, now, e.ID)
	if updateErr != nil {
		return false, fmt.Errorf("engram-projects: update runbook: %w", updateErr)
	}
	return true, nil
}

func (s *Store) recomputeRunbookExec(id string) (int, any, error) {
	var count int
	var lastExecAt sql.NullString
	err := s.db.QueryRow(
		`SELECT COUNT(*), MAX(created_at) FROM observations WHERE topic_key LIKE ? AND deleted_at IS NULL`,
		"runbook/"+id+"/exec/%",
	).Scan(&count, &lastExecAt)
	if err != nil {
		return 0, nil, fmt.Errorf("engram-projects: recompute runbook exec: %w", err)
	}
	if lastExecAt.Valid {
		return count, lastExecAt.String, nil
	}
	return count, nil, nil
}

func nullableEmptyStr(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

// RunbookFindParams holds mem_runbook_find's input (RFC §5.9).
type RunbookFindParams struct {
	Query        string
	Project      string
	Category     string
	Pattern      string
	IncludeStale bool
	MatchMode    string // "all" | "any", default "any"
	Limit        int
}

// RunbookFindItem is one row of mem_runbook_find's items array.
type RunbookFindItem struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Project    string  `json:"project"`
	VaultPath  string  `json:"vault_path"`
	Category   string  `json:"category"`
	Pattern    *string `json:"pattern,omitempty"`
	Severity   *string `json:"severity,omitempty"`
	Status     string  `json:"status"`
	Stale      bool    `json:"stale"`
	AgeDays    *int    `json:"age_days,omitempty"`
	ExecCount  int     `json:"exec_count"`
	LastExecAt *string `json:"last_exec_at,omitempty"`
	Rank       float64 `json:"rank"`
}

// runbookFindQuery ranks the matches and carries their total in the same round
// trip. The total is a scalar subquery rather than COUNT(*) OVER (): a window
// function makes SQLite build an ephemeral table and a sorter for what is here
// a handful of rows, measured at 94µs against 67µs for the subquery and the
// same 67µs the two separate statements cost. One statement, one snapshot, and
// the total can no longer disagree with the rows it describes because a write
// landed between two queries.
//
// Both joins are CROSS JOINs deliberately. Given a filter on runbook_index,
// SQLite is free to reorder an ordinary join, picks that table as the outer
// loop and re-runs the MATCH once per row; naming the order costs nothing and
// removes the possibility.
const runbookFindQuery = `
SELECT ri.id, ri.title, ri.project, ri.vault_path, ri.category, ri.pattern, ri.severity,
       ri.status, ri.stale, ri.age_days, ri.exec_count, ri.last_exec_at,
       bm25(runbook_index_fts) AS hit_rank,
       (SELECT COUNT(*)
        FROM runbook_index_fts total_fts
        CROSS JOIN runbook_index total_ri ON total_ri.seq = total_fts.rowid
        WHERE %s)
FROM runbook_index_fts
CROSS JOIN runbook_index ri ON ri.seq = runbook_index_fts.rowid
WHERE %s
ORDER BY hit_rank
LIMIT ?`

// runbookFindPredicate renders the filter under the given aliases. It is called
// twice per search — once for the page, once for the total that rides with it —
// under different aliases, so the uncorrelated subquery cannot end up
// referencing the outer row by accident.
//
// matchColumn is the FTS5 hidden column the MATCH is spelled against. It is
// named after the table, so an aliased instance has to qualify it: the
// subquery's is total_fts.runbook_index_fts, not total_fts.
func runbookFindPredicate(p RunbookFindParams, ftsQuery, matchColumn, ri string) (string, []any) {
	where := []string{matchColumn + " MATCH ?"}
	args := []any{ftsQuery}
	if p.Project != "" {
		where = append(where, ri+".project = ?")
		args = append(args, p.Project)
	}
	if p.Category != "" {
		where = append(where, ri+".category = ?")
		args = append(args, p.Category)
	}
	if p.Pattern != "" {
		where = append(where, ri+".pattern = ?")
		args = append(args, p.Pattern)
	}
	if !p.IncludeStale {
		where = append(where, ri+".stale = 0")
	}
	return strings.Join(where, " AND "), args
}

// FindRunbooks ranks candidate runbooks by BM25 over runbook_index_fts.
func (s *Store) FindRunbooks(p RunbookFindParams) ([]RunbookFindItem, int, error) {
	matchMode := p.MatchMode
	if matchMode == "" {
		matchMode = "any"
	}
	ftsQuery := ftsMatchQuery(p.Query, matchMode)

	totalWhere, totalArgs := runbookFindPredicate(p, ftsQuery, "total_fts.runbook_index_fts", "total_ri")
	pageWhere, pageArgs := runbookFindPredicate(p, ftsQuery, "runbook_index_fts", "ri")

	limit := p.Limit
	if limit <= 0 {
		limit = 5
	}

	// The subquery is spelled first in the projection, so its bindings come
	// first too.
	args := append(append([]any{}, totalArgs...), pageArgs...)
	args = append(args, limit)

	rows, err := s.queryHook(s.readDB(), fmt.Sprintf(runbookFindQuery, totalWhere, pageWhere), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("engram-projects: find runbooks: %w", err)
	}
	defer rows.Close()

	var items []RunbookFindItem
	total := 0
	for rows.Next() {
		var item RunbookFindItem
		var stale int
		if err := rows.Scan(&item.ID, &item.Title, &item.Project, &item.VaultPath, &item.Category,
			&item.Pattern, &item.Severity, &item.Status, &stale, &item.AgeDays, &item.ExecCount,
			&item.LastExecAt, &item.Rank, &total); err != nil {
			return nil, 0, err
		}
		item.Stale = stale == 1
		items = append(items, item)
	}
	// An empty page means nothing matched: this search takes no offset, so
	// there is no page past the end for the total to have to explain.
	return items, total, rows.Err()
}

// ftsMatchQuery quotes every token (guarding interior quotes, same rule as
// sanitizeFTS) and joins them with AND or OR depending on matchMode.
func ftsMatchQuery(query, matchMode string) string {
	fields := strings.Fields(query)
	quoted := make([]string, len(fields))
	for i, w := range fields {
		w = strings.Trim(w, `"`)
		w = strings.ReplaceAll(w, `"`, `""`)
		quoted[i] = `"` + w + `"`
	}
	sep := " OR "
	if matchMode == "all" {
		sep = " AND "
	}
	return strings.Join(quoted, sep)
}

// SearchRunbookIndex searches runbook_index_fts by title and symptoms,
// returning full runbook_index rows ranked by BM25 — the TUI Runbooks tab's
// search-by-symptoms query. project scopes the match to one project;
// passing "" searches every project, backing the Runbooks tab's "a" toggle
// for searching across every project at once.
//
// Unlike FindRunbooks — built for mem_runbook_find's thinner MCP envelope —
// this returns the same RunbookIndexRow shape ListRunbookIndex does,
// including Symptoms, because the TUI renders search results in the exact
// same table as the unfiltered index.
func (s *Store) SearchRunbookIndex(query, project string, limit int) ([]RunbookIndexRow, error) {
	ftsQuery := ftsMatchQuery(query, "any")

	where := []string{"runbook_index_fts MATCH ?"}
	args := []any{ftsQuery}
	if project != "" {
		where = append(where, "ri.project = ?")
		args = append(args, project)
	}
	whereSQL := strings.Join(where, " AND ")

	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit)

	rows, err := s.db.Query(`
		SELECT ri.id, ri.project, ri.vault_path, ri.title, ri.category, ri.pattern, ri.severity, ri.status,
		       ri.symptoms, ri.owner, ri.automation_level, ri.last_updated, ri.last_verified, ri.stale,
		       ri.age_days, ri.exec_count, ri.last_exec_at, ri.synced_at
		FROM runbook_index_fts
		JOIN runbook_index ri ON ri.seq = runbook_index_fts.rowid
		WHERE `+whereSQL+`
		ORDER BY bm25(runbook_index_fts) LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("engram-projects: search runbook index: %w", err)
	}
	defer rows.Close()

	items := make([]RunbookIndexRow, 0)
	for rows.Next() {
		var r RunbookIndexRow
		var symptoms string
		var stale int
		if err := rows.Scan(&r.ID, &r.Project, &r.VaultPath, &r.Title, &r.Category, &r.Pattern,
			&r.Severity, &r.Status, &symptoms, &r.Owner, &r.AutomationLevel, &r.LastUpdated,
			&r.LastVerified, &stale, &r.AgeDays, &r.ExecCount, &r.LastExecAt, &r.SyncedAt); err != nil {
			return nil, fmt.Errorf("engram-projects: scan searched runbook row: %w", err)
		}
		r.Stale = stale == 1
		if strings.TrimSpace(symptoms) != "" {
			r.Symptoms = strings.Split(symptoms, "\n")
		}
		items = append(items, r)
	}
	return items, rows.Err()
}

// ─── Index listing (backs GET /projects/{slug}/runbooks) ─────────────────────

// RunbookListFilter holds the filters of the runbook index listing. Unlike
// FindRunbooks it takes no query: this is the browsable index, not the BM25
// ranking.
// defaultRunbookListLimit is the page size a runbook listing takes when the
// caller names none. It is spelled once so ListRunbooksPage reports the same
// number the query actually applied.
const defaultRunbookListLimit = 20

type RunbookListFilter struct {
	Stale    *bool
	Category string
	Pattern  string
	Status   string
	Limit    int
	Offset   int
}

// RunbookIndexRow is one full row of runbook_index.
type RunbookIndexRow struct {
	ID              string   `json:"id"`
	Project         string   `json:"project"`
	VaultPath       string   `json:"vault_path"`
	Title           string   `json:"title"`
	Category        string   `json:"category"`
	Pattern         *string  `json:"pattern,omitempty"`
	Severity        *string  `json:"severity,omitempty"`
	Status          string   `json:"status"`
	Symptoms        []string `json:"symptoms"`
	Owner           *string  `json:"owner,omitempty"`
	AutomationLevel *string  `json:"automation_level,omitempty"`
	LastUpdated     *string  `json:"last_updated,omitempty"`
	LastVerified    *string  `json:"last_verified,omitempty"`
	Stale           bool     `json:"stale"`
	AgeDays         *int     `json:"age_days,omitempty"`
	ExecCount       int      `json:"exec_count"`
	LastExecAt      *string  `json:"last_exec_at,omitempty"`
	SyncedAt        string   `json:"synced_at"`
}

// ListRunbookIndex pages through the runbook index, scoped to one project or,
// when project is "", every project — the cross-project mode the Runbooks
// tab's "a" (all projects) toggle needs, alongside
// GET /projects/{slug}/runbooks (internal/server/projects_routes.go), which
// always resolves a real slug through knownProject before calling this and so
// never hits the empty-string branch.
func (s *Store) ListRunbookIndex(project string, f RunbookListFilter) ([]RunbookIndexRow, int, error) {
	var where []string
	var args []any
	if project != "" {
		where = append(where, "project = ?")
		args = append(args, project)
	}
	if f.Stale != nil {
		v := 0
		if *f.Stale {
			v = 1
		}
		where = append(where, "stale = ?")
		args = append(args, v)
	}
	if f.Category != "" {
		where = append(where, "category = ?")
		args = append(args, f.Category)
	}
	if f.Pattern != "" {
		where = append(where, "pattern = ?")
		args = append(args, f.Pattern)
	}
	if f.Status != "" {
		where = append(where, "status = ?")
		args = append(args, f.Status)
	}
	whereSQL := "1=1"
	if len(where) > 0 {
		whereSQL = strings.Join(where, " AND ")
	}

	rdb := s.readDB()

	var total int
	if err := s.queryRowHook(rdb, `SELECT COUNT(*) FROM runbook_index WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("engram-projects: count runbook index: %w", err)
	}

	limit := f.Limit
	if limit <= 0 {
		limit = defaultRunbookListLimit
	}
	listArgs := append(append([]any{}, args...), limit, f.Offset)
	rows, err := s.queryHook(rdb, `
		SELECT id, project, vault_path, title, category, pattern, severity, status, symptoms,
		       owner, automation_level, last_updated, last_verified, stale, age_days,
		       exec_count, last_exec_at, synced_at
		FROM runbook_index WHERE `+whereSQL+`
		ORDER BY stale DESC, id ASC LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("engram-projects: list runbook index: %w", err)
	}
	defer rows.Close()

	items := make([]RunbookIndexRow, 0)
	for rows.Next() {
		var r RunbookIndexRow
		var symptoms string
		var stale int
		if err := rows.Scan(&r.ID, &r.Project, &r.VaultPath, &r.Title, &r.Category, &r.Pattern,
			&r.Severity, &r.Status, &symptoms, &r.Owner, &r.AutomationLevel, &r.LastUpdated,
			&r.LastVerified, &stale, &r.AgeDays, &r.ExecCount, &r.LastExecAt, &r.SyncedAt); err != nil {
			return nil, 0, fmt.Errorf("engram-projects: scan runbook index row: %w", err)
		}
		r.Stale = stale == 1
		if strings.TrimSpace(symptoms) != "" {
			r.Symptoms = strings.Split(symptoms, "\n")
		}
		items = append(items, r)
	}
	return items, total, rows.Err()
}

// ListRunbooksPage is ListRunbookIndex with the page's own shape reported
// alongside it, so a caller that paginates does not have to remember which
// default limit the store applied.
//
// The project stays its own argument rather than moving into the filter: it is
// the scope of the listing, not one more thing being filtered out of it, and
// every other listing in this package spells it the same way.
func (s *Store) ListRunbooksPage(project string, f RunbookListFilter) (Page[RunbookIndexRow], error) {
	items, total, err := s.ListRunbookIndex(project, f)
	if err != nil {
		return Page[RunbookIndexRow]{}, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = defaultRunbookListLimit
	}
	return Page[RunbookIndexRow]{Items: items, Total: total, Limit: limit, Offset: f.Offset}, nil
}
