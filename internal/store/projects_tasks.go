package store

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/HoracioEspinosa/engram/internal/tasks"
)

// MissingFieldError is returned when a required field is absent on create
// (RFC §5.3: title/kind are required when a task does not exist yet).
type MissingFieldError struct {
	Field string
}

func (e *MissingFieldError) Error() string {
	return fmt.Sprintf("missing required field: %s", e.Field)
}

// Task mirrors the tasks row (RFC §5.3).
type Task struct {
	ID                 int64   `json:"id"`
	SyncID             string  `json:"sync_id"`
	Project            string  `json:"project"`
	JiraKey            *string `json:"jira_key,omitempty"`
	SDDChange          *string `json:"sdd_change,omitempty"`
	Title              string  `json:"title"`
	Kind               string  `json:"kind"`
	State              string  `json:"state"`
	JiraStatus         *string `json:"jira_status,omitempty"`
	JiraStatusCategory *string `json:"jira_status_category,omitempty"`
	StateSyncedAt      *string `json:"state_synced_at,omitempty"`
	Branch             *string `json:"branch,omitempty"`
	PRUrl              *string `json:"pr_url,omitempty"`
	KnowledgeRef       *string `json:"knowledge_ref,omitempty"`
	Assignee           *string `json:"assignee,omitempty"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
	ClosedAt           *string `json:"closed_at,omitempty"`

	// Slug is the task's own name inside its project — the folder the vault
	// keeps it in, for work that has no Jira ticket and no SDD change.
	Slug *string `json:"slug,omitempty"`
	// Summary is the opening paragraph of the task README, so a list can say
	// what a task is about without opening it.
	Summary *string `json:"summary,omitempty"`
	// PendingNote says what is still open on a task the vault marks as
	// "Con pendientes"; a pending state without it tells nobody anything.
	PendingNote *string `json:"pending_note,omitempty"`
	// VaultPath is the task's folder relative to the vault root. KnowledgeRef
	// stays what it was: one curated document, not the whole directory.
	VaultPath *string `json:"vault_path,omitempty"`
	// ParentTaskID and ParentTaskSyncID place a task under another one. Both
	// are kept: the id for local integrity, the sync_id so the link survives
	// a trip through another machine.
	ParentTaskID     *int64  `json:"parent_task_id,omitempty"`
	ParentTaskSyncID *string `json:"parent_task_sync_id,omitempty"`
}

// UpsertTaskParams holds the optional fields of mem_task_upsert. A nil
// pointer means "omitted" and is left untouched on update.
type UpsertTaskParams struct {
	Project            string
	SyncID             *string
	JiraKey            *string
	SDDChange          *string
	Title              *string
	Kind               *string
	State              *string
	JiraStatus         *string
	JiraStatusCategory *string
	Branch             *string
	PRUrl              *string
	KnowledgeRef       *string
	Assignee           *string
	Slug               *string
	Summary            *string
	PendingNote        *string
	VaultPath          *string
	// ParentTask is a task reference in any of the forms ResolveTaskRef
	// accepts, scoped to the same project.
	ParentTask *string
}

// UpsertTaskResult is the outcome of UpsertTask.
type UpsertTaskResult struct {
	Task        Task
	Created     bool
	CardCreated bool
}

const taskSelectColumns = `id, sync_id, project, jira_key, sdd_change, title, kind, state,
	jira_status, jira_status_category, state_synced_at, branch, pr_url, knowledge_ref, assignee,
	created_at, updated_at, closed_at, slug, summary, pending_note, vault_path,
	parent_task_id, parent_task_sync_id`

// taskScanTargets lists the destinations taskSelectColumns scans into, in the
// same order. A query that appends columns of its own to that projection reuses
// this rather than spelling the task's fields a second time, so a column added
// to the table cannot end up scanned in one place and forgotten in the other.
func taskScanTargets(t *Task) []any {
	return []any{&t.ID, &t.SyncID, &t.Project, &t.JiraKey, &t.SDDChange, &t.Title, &t.Kind, &t.State,
		&t.JiraStatus, &t.JiraStatusCategory, &t.StateSyncedAt, &t.Branch, &t.PRUrl, &t.KnowledgeRef, &t.Assignee,
		&t.CreatedAt, &t.UpdatedAt, &t.ClosedAt, &t.Slug, &t.Summary, &t.PendingNote, &t.VaultPath,
		&t.ParentTaskID, &t.ParentTaskSyncID}
}

func scanTask(row interface{ Scan(dest ...any) error }) (Task, error) {
	var t Task
	err := row.Scan(taskScanTargets(&t)...)
	return t, err
}

func (s *Store) getTaskByID(id int64) (Task, error) {
	t, err := scanTask(s.db.QueryRow(`SELECT `+taskSelectColumns+` FROM tasks WHERE id = ? AND deleted_at IS NULL`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrUnknownTask
	}
	if err != nil {
		return Task{}, fmt.Errorf("engram-projects: get task: %w", err)
	}
	return t, nil
}

func isClosedState(state string) bool {
	return tasks.ClosedStates[state]
}

func strVal(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// UpsertTask creates or updates a task using the precedence documented in
// RFC §5.3: sync_id -> jira_key -> (project, sdd_change) -> new row.
func (s *Store) UpsertTask(p UpsertTaskParams) (UpsertTaskResult, error) {
	// The knowledge pointer is normalized before any lookup so a malformed
	// one never reaches the row: a task that already exists must not end up
	// updated on every other column and rejected on this one.
	normalizedRef, err := normalizeKnowledgeRefPtr(p.KnowledgeRef)
	if err != nil {
		return UpsertTaskResult{}, err
	}
	p.KnowledgeRef = normalizedRef

	var existingID int64
	var existingProject string
	found := false

	lookup := func(col, val string) error {
		err := s.db.QueryRow(`SELECT id, project FROM tasks WHERE `+col+` = ? AND deleted_at IS NULL`, val).
			Scan(&existingID, &existingProject)
		if err == nil {
			found = true
			return nil
		}
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}

	if p.SyncID != nil && strings.TrimSpace(*p.SyncID) != "" {
		if err := lookup("sync_id", *p.SyncID); err != nil {
			return UpsertTaskResult{}, err
		}
		if found && existingProject != p.Project {
			return UpsertTaskResult{}, &TaskKeyConflictError{JiraKey: *p.SyncID, ExistingProject: existingProject}
		}
	}
	if !found && p.JiraKey != nil && strings.TrimSpace(*p.JiraKey) != "" {
		if err := lookup("jira_key", *p.JiraKey); err != nil {
			return UpsertTaskResult{}, err
		}
		if found && existingProject != p.Project {
			return UpsertTaskResult{}, &TaskKeyConflictError{JiraKey: *p.JiraKey, ExistingProject: existingProject}
		}
	}
	if !found && p.SDDChange != nil && strings.TrimSpace(*p.SDDChange) != "" {
		var id int64
		err := s.db.QueryRow(`SELECT id FROM tasks WHERE project = ? AND sdd_change = ? AND deleted_at IS NULL`,
			p.Project, *p.SDDChange).Scan(&id)
		if err == nil {
			found = true
			existingID = id
			existingProject = p.Project
		} else if !errors.Is(err, sql.ErrNoRows) {
			return UpsertTaskResult{}, err
		}
	}
	// The slug comes last: it is unique only within a project, so it is the
	// weakest of the four identities and must never shadow a Jira key.
	if !found && p.Slug != nil && strings.TrimSpace(*p.Slug) != "" {
		var id int64
		err := s.db.QueryRow(`SELECT id FROM tasks WHERE project = ? AND slug = ? AND deleted_at IS NULL`,
			p.Project, strings.ToLower(strings.TrimSpace(*p.Slug))).Scan(&id)
		if err == nil {
			found = true
			existingID = id
			existingProject = p.Project
		} else if !errors.Is(err, sql.ErrNoRows) {
			return UpsertTaskResult{}, err
		}
	}

	cardCreated, err := s.ensureMinimalProjectCard(p.Project)
	if err != nil {
		return UpsertTaskResult{}, err
	}

	// The parent is resolved before anything is written: a reference that
	// names nothing, or names the task itself, must not leave the rest of the
	// upsert applied.
	var parentID *int64
	var parentSyncID *string
	if p.ParentTask != nil && strings.TrimSpace(*p.ParentTask) != "" {
		parent, err := s.ResolveTaskRef(p.Project, *p.ParentTask)
		if err != nil {
			return UpsertTaskResult{}, err
		}
		if found && parent.ID == existingID {
			return UpsertTaskResult{}, ErrTaskSelfParent
		}
		parentID = &parent.ID
		parentSyncID = &parent.SyncID
	}

	slug := p.Slug
	if slug != nil {
		lowered := strings.ToLower(strings.TrimSpace(*slug))
		slug = &lowered
	}

	now := s.nowUTC()

	if !found {
		if p.Title == nil || strings.TrimSpace(*p.Title) == "" {
			return UpsertTaskResult{}, &MissingFieldError{Field: "title"}
		}
		if p.Kind == nil || strings.TrimSpace(*p.Kind) == "" {
			return UpsertTaskResult{}, &MissingFieldError{Field: "kind"}
		}
		state := "open"
		if p.State != nil && *p.State != "" {
			state = *p.State
		} else if p.JiraStatus != nil {
			if derived, ok := tasks.DeriveState(*p.JiraStatus, strVal(p.JiraStatusCategory)); ok {
				state = derived
			}
		}
		var stateSyncedAt any
		if p.JiraStatus != nil {
			stateSyncedAt = now
		}
		var closedAt any
		if isClosedState(state) {
			closedAt = now
		}
		syncID := newSyncID("task")
		var insertedID int64
		if err := s.withTx(func(tx *sql.Tx) error {
			res, err := s.execHook(tx, `
				INSERT INTO tasks (sync_id, project, jira_key, sdd_change, title, kind, state, jira_status,
					jira_status_category, state_synced_at, branch, pr_url, knowledge_ref, assignee,
					created_at, updated_at, closed_at, slug, summary, pending_note, vault_path,
					parent_task_id, parent_task_sync_id)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				syncID, p.Project, nullableStr(p.JiraKey), nullableStr(p.SDDChange), *p.Title, *p.Kind, state,
				nullableStr(p.JiraStatus), nullableStr(p.JiraStatusCategory), stateSyncedAt, nullableStr(p.Branch),
				nullableStr(p.PRUrl), nullableStr(p.KnowledgeRef), nullableStr(p.Assignee), now, now, closedAt,
				nullableStr(slug), nullableStr(p.Summary), nullableStr(p.PendingNote), nullableStr(p.VaultPath),
				nullableInt64(parentID), nullableStr(parentSyncID))
			if err != nil {
				return fmt.Errorf("engram-projects: insert task: %w", err)
			}
			insertedID, err = res.LastInsertId()
			if err != nil {
				return err
			}
			return s.enqueueTaskTx(tx, insertedID)
		}); err != nil {
			return UpsertTaskResult{}, err
		}
		existingID = insertedID
	} else {
		sets := []string{"updated_at = ?"}
		args := []any{now}
		if p.Title != nil {
			sets = append(sets, "title = ?")
			args = append(args, *p.Title)
		}
		if p.Kind != nil {
			sets = append(sets, "kind = ?")
			args = append(args, *p.Kind)
		}
		var stateToSet *string
		if p.State != nil {
			stateToSet = p.State
		}
		if p.JiraStatus != nil {
			sets = append(sets, "jira_status = ?", "state_synced_at = ?")
			args = append(args, *p.JiraStatus, now)
			if stateToSet == nil {
				if derived, ok := tasks.DeriveState(*p.JiraStatus, strVal(p.JiraStatusCategory)); ok {
					stateToSet = &derived
				}
			}
		}
		if p.JiraStatusCategory != nil {
			sets = append(sets, "jira_status_category = ?")
			args = append(args, *p.JiraStatusCategory)
		}
		if stateToSet != nil {
			sets = append(sets, "state = ?")
			args = append(args, *stateToSet)
			if isClosedState(*stateToSet) {
				sets = append(sets, "closed_at = ?")
				args = append(args, now)
			} else {
				sets = append(sets, "closed_at = NULL")
			}
		}
		if p.Branch != nil {
			sets = append(sets, "branch = ?")
			args = append(args, *p.Branch)
		}
		if p.PRUrl != nil {
			sets = append(sets, "pr_url = ?")
			args = append(args, *p.PRUrl)
		}
		if p.KnowledgeRef != nil {
			sets = append(sets, "knowledge_ref = ?")
			args = append(args, *p.KnowledgeRef)
		}
		if p.Assignee != nil {
			sets = append(sets, "assignee = ?")
			args = append(args, *p.Assignee)
		}
		if slug != nil {
			sets = append(sets, "slug = ?")
			args = append(args, *slug)
		}
		if p.Summary != nil {
			sets = append(sets, "summary = ?")
			args = append(args, *p.Summary)
		}
		if p.PendingNote != nil {
			sets = append(sets, "pending_note = ?")
			args = append(args, *p.PendingNote)
		}
		if p.VaultPath != nil {
			sets = append(sets, "vault_path = ?")
			args = append(args, *p.VaultPath)
		}
		if parentID != nil {
			sets = append(sets, "parent_task_id = ?", "parent_task_sync_id = ?")
			args = append(args, *parentID, *parentSyncID)
		}
		args = append(args, existingID)
		if err := s.withTx(func(tx *sql.Tx) error {
			if _, err := s.execHook(tx, `UPDATE tasks SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
				return fmt.Errorf("engram-projects: update task: %w", err)
			}
			return s.enqueueTaskTx(tx, existingID)
		}); err != nil {
			return UpsertTaskResult{}, err
		}
	}

	task, err := s.getTaskByID(existingID)
	if err != nil {
		return UpsertTaskResult{}, err
	}
	return UpsertTaskResult{Task: task, Created: !found, CardCreated: cardCreated}, nil
}

// ErrTaskSelfParent is returned when a task is asked to be its own parent.
var ErrTaskSelfParent = errors.New("a task cannot be its own parent")

// TaskListFilter holds mem_task_list's filter parameters.
type TaskListFilter struct {
	State   string // "" or "active" -> every state that is not closed
	Kind    string
	JiraKey string
	Query   string
	// States narrows the list to an explicit set, for a caller that wants
	// more than one state but not all of them. It wins over State.
	States []string
	// IncludeArchived brings archived tasks back into a listing that would
	// otherwise leave them out. Archived work is kept, not shown by default:
	// it is history, and history is not a to-do list.
	IncludeArchived bool
	Limit           int
	Offset          int
	StaleAfterHours int
}

// TaskListItem is one row of mem_task_list's items array.
type TaskListItem struct {
	Task
	Observations int  `json:"observations"`
	Evidence     int  `json:"evidence"`
	StateStale   bool `json:"state_stale"`
}

// defaultTaskListLimit is the page size a listing takes when the caller names
// none. It is spelled once so ListTasksPage reports the same number the query
// actually applied.
const defaultTaskListLimit = 20

// taskListQuery reads one page of tasks and everything the page needs in a
// single round trip.
//
// The two counters are correlated subqueries rather than joins: a join to
// task_observations and another to evidence would multiply the rows against
// each other and force a GROUP BY over the whole task projection to undo the
// damage. Each subquery is driven by its own index on task_id, so it costs a
// lookup per row on the page — not per row in the table.
//
// total comes from COUNT(*) OVER (), which SQLite (3.25 and later) evaluates
// over the whole filtered set before LIMIT is applied. That is the number a
// pager needs, and it used to cost a second query that repeated the same
// predicate — and could disagree with the page whenever a write landed between
// the two.
const taskListQuery = `
SELECT %s,
       (SELECT COUNT(*) FROM task_observations o WHERE o.task_id = t.id),
       (SELECT COUNT(*) FROM evidence e WHERE e.task_id = t.id AND e.deleted_at IS NULL),
       COUNT(*) OVER ()
FROM tasks t
WHERE %s
ORDER BY t.updated_at DESC
LIMIT ? OFFSET ?`

// ListTasks lists tasks for a project applying TaskListFilter (RFC §5.4).
func (s *Store) ListTasks(project string, f TaskListFilter) ([]TaskListItem, int, error) {
	where := []string{"t.project = ?", "t.deleted_at IS NULL"}
	args := []any{project}

	// An explicitly named state, one or many, is always honoured as asked.
	// Only the default listing has an opinion: archived work is history, and
	// history does not belong in a list of what is open.
	switch {
	case len(f.States) > 0:
		placeholders := make([]string, 0, len(f.States))
		for _, state := range f.States {
			placeholders = append(placeholders, "?")
			args = append(args, state)
		}
		where = append(where, "t.state IN ("+strings.Join(placeholders, ",")+")")
	case f.State != "" && f.State != "active":
		where = append(where, "t.state = ?")
		args = append(args, f.State)
	case f.IncludeArchived:
		where = append(where, "t.state NOT IN ('done','cancelled')")
	default:
		where = append(where, "t.state NOT IN ('done','cancelled','archived')")
	}
	if f.Kind != "" {
		where = append(where, "t.kind = ?")
		args = append(args, f.Kind)
	}
	if f.JiraKey != "" {
		where = append(where, "t.jira_key = ?")
		args = append(args, f.JiraKey)
	}
	if strings.TrimSpace(f.Query) != "" {
		where = append(where, "t.id IN (SELECT rowid FROM tasks_fts WHERE tasks_fts MATCH ?)")
		args = append(args, sanitizeFTS(f.Query))
	}
	whereSQL := strings.Join(where, " AND ")

	limit := f.Limit
	if limit <= 0 {
		limit = defaultTaskListLimit
	}
	staleAfterHours := f.StaleAfterHours
	if staleAfterHours <= 0 {
		staleAfterHours = 24
	}

	rdb := s.readDB()
	query := fmt.Sprintf(taskListQuery, prefixColumns(taskSelectColumns, "t"), whereSQL)
	listArgs := append(append([]any{}, args...), limit, f.Offset)
	rows, err := s.queryHook(rdb, query, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("engram-projects: list tasks: %w", err)
	}
	defer rows.Close()

	items := make([]TaskListItem, 0, limit)
	total := 0
	for rows.Next() {
		var item TaskListItem
		dest := append(taskScanTargets(&item.Task), &item.Observations, &item.Evidence, &total)
		if err := rows.Scan(dest...); err != nil {
			return nil, 0, err
		}
		item.StateStale = isTaskStateStale(item.StateSyncedAt, staleAfterHours)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(items) > 0 {
		return items, total, nil
	}

	// An empty page carries no window function to read the total from. At
	// offset zero that is the honest answer — nothing matched. Past the end it
	// is not: the rows exist, this page just starts after them, and a pager
	// told "zero" has no way back. Only that case pays for a second query.
	if f.Offset <= 0 {
		return items, 0, nil
	}
	if err := s.queryRowHook(rdb, `SELECT COUNT(*) FROM tasks t WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("engram-projects: count tasks: %w", err)
	}
	return items, total, nil
}

// ListTasksPage is ListTasks with the page's own shape reported alongside it,
// so a caller that paginates does not have to remember which default limit the
// store applied.
func (s *Store) ListTasksPage(project string, f TaskListFilter) (Page[TaskListItem], error) {
	items, total, err := s.ListTasks(project, f)
	if err != nil {
		return Page[TaskListItem]{}, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = defaultTaskListLimit
	}
	return Page[TaskListItem]{Items: items, Total: total, Limit: limit, Offset: f.Offset}, nil
}

func isTaskStateStale(stateSyncedAt *string, staleAfterHours int) bool {
	if stateSyncedAt == nil || strings.TrimSpace(*stateSyncedAt) == "" {
		return true
	}
	t, err := parseObservationTime(*stateSyncedAt)
	if err != nil {
		return true
	}
	return time.Since(t) > time.Duration(staleAfterHours)*time.Hour
}

var (
	taskSyncIDRefPattern = regexp.MustCompile(`^task-[0-9a-f]{16}$`)
	jiraKeyRefPattern    = regexp.MustCompile(`^[A-Z][A-Z0-9]+-[0-9]+$`)
	localIDRefPattern    = regexp.MustCompile(`^#([0-9]+)$`)
	changeRefRefPattern  = regexp.MustCompile(`^change:([a-z0-9][a-z0-9-]*)$`)
)

// ResolveTaskRef resolves a task reference string, scoped to project, using
// one of the four forms documented in RFC §5.0: jira_key, sync_id, "#id", or
// "change:sdd_change".
func (s *Store) ResolveTaskRef(project, ref string) (Task, error) {
	ref = strings.TrimSpace(ref)
	switch {
	case taskSyncIDRefPattern.MatchString(ref):
		return s.getTaskByProjectColumn(project, "sync_id", ref)
	case jiraKeyRefPattern.MatchString(ref):
		return s.getTaskByProjectColumn(project, "jira_key", ref)
	case localIDRefPattern.MatchString(ref):
		m := localIDRefPattern.FindStringSubmatch(ref)
		id, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			return Task{}, ErrUnknownTask
		}
		t, err := s.getTaskByID(id)
		if err != nil {
			return Task{}, err
		}
		if t.Project != project {
			return Task{}, ErrUnknownTask
		}
		return t, nil
	case changeRefRefPattern.MatchString(ref):
		m := changeRefRefPattern.FindStringSubmatch(ref)
		return s.getTaskByProjectColumn(project, "sdd_change", m[1])
	default:
		return Task{}, ErrUnknownTask
	}
}

func (s *Store) getTaskByProjectColumn(project, col, val string) (Task, error) {
	t, err := scanTask(s.db.QueryRow(
		`SELECT `+taskSelectColumns+` FROM tasks WHERE project = ? AND `+col+` = ? AND deleted_at IS NULL`,
		project, val))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrUnknownTask
	}
	if err != nil {
		return Task{}, fmt.Errorf("engram-projects: resolve task ref: %w", err)
	}
	return t, nil
}

// ─── mem_task_link ───────────────────────────────────────────────────────────

// LinkTaskObservationParams holds mem_task_link's input, already resolved to
// concrete Task/Observation rows by the MCP handler.
type LinkTaskObservationParams struct {
	Task          Task
	ObservationID int64
	Role          string // "" triggers topic_key-based default
	KnowledgeRef  *string
	GraphRef      *string
	GraphCommit   *string
	RunbookID     *string
	JiraRef       *string
}

// ObservationRefOut is one row of mem_task_link's `refs` output array.
type ObservationRefOut struct {
	RefKind     string  `json:"ref_kind"`
	Ref         string  `json:"ref"`
	GraphCommit *string `json:"graph_commit"`
}

// LinkTaskObservationResult is the outcome of LinkTaskObservation.
type LinkTaskObservationResult struct {
	Linked            bool
	TaskSyncID        string
	ObservationSyncID string
	Role              string
	RefsAdded         int
	Refs              []ObservationRefOut
}

// LinkTaskObservation links an observation to a task and optionally records
// observation_refs (RFC §5.5).
func (s *Store) LinkTaskObservation(p LinkTaskObservationParams) (LinkTaskObservationResult, error) {
	obs, err := s.GetObservation(p.ObservationID)
	if err != nil {
		return LinkTaskObservationResult{}, ErrUnknownObservation
	}
	obsProject := ""
	if obs.Project != nil {
		obsProject, _ = NormalizeProject(*obs.Project)
	}
	taskProject, _ := NormalizeProject(p.Task.Project)
	if obsProject != taskProject {
		return LinkTaskObservationResult{}, ErrCrossProjectLink
	}

	role := strings.TrimSpace(p.Role)
	if role == "" {
		role = "context"
		if obs.TopicKey != nil {
			switch {
			case strings.HasPrefix(*obs.TopicKey, "incident/"):
				role = "root_cause"
			case strings.HasPrefix(*obs.TopicKey, "evidence/"):
				role = "evidence"
			}
		}
	}

	// Every rejection has to happen before the first INSERT. A graph_ref
	// without its commit used to be caught after the link row was already
	// written, so the caller got the error while the link existed with the
	// default role — and a corrected retry hit INSERT OR IGNORE and kept
	// that wrong role forever.
	type refCandidate struct {
		kind        string
		ref         string
		graphCommit *string
	}
	var candidates []refCandidate
	if p.KnowledgeRef != nil && strings.TrimSpace(*p.KnowledgeRef) != "" {
		knowledgeRef, err := NormalizeKnowledgeRef(*p.KnowledgeRef)
		if err != nil {
			return LinkTaskObservationResult{}, err
		}
		candidates = append(candidates, refCandidate{"knowledge", knowledgeRef, nil})
	}
	if p.GraphRef != nil && strings.TrimSpace(*p.GraphRef) != "" {
		if p.GraphCommit == nil || strings.TrimSpace(*p.GraphCommit) == "" {
			return LinkTaskObservationResult{}, ErrGraphCommitRequired
		}
		candidates = append(candidates, refCandidate{"graph", *p.GraphRef, p.GraphCommit})
	}
	if p.RunbookID != nil && strings.TrimSpace(*p.RunbookID) != "" {
		candidates = append(candidates, refCandidate{"runbook", *p.RunbookID, nil})
	}
	if p.JiraRef != nil && strings.TrimSpace(*p.JiraRef) != "" {
		candidates = append(candidates, refCandidate{"jira", *p.JiraRef, nil})
	}

	now := s.nowUTC()
	result := LinkTaskObservationResult{
		TaskSyncID:        p.Task.SyncID,
		ObservationSyncID: obs.SyncID,
		Role:              role,
	}

	// The link, its references, and the mutations that replicate them all
	// commit together: a partially replicated link would be indistinguishable
	// from a lost one on the other side.
	if err := s.withTx(func(tx *sql.Tx) error {
		result.RefsAdded = 0
		result.Refs = nil
		res, err := s.execHook(tx, `
			INSERT OR IGNORE INTO task_observations (task_id, observation_id, task_sync_id, observation_sync_id, role, linked_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			p.Task.ID, p.ObservationID, p.Task.SyncID, obs.SyncID, role, now)
		if err != nil {
			return fmt.Errorf("engram-projects: link task observation: %w", err)
		}
		affected, _ := res.RowsAffected()
		result.Linked = affected > 0
		if affected > 0 {
			if err := s.enqueueTaskLinkTx(tx, taskProject, p.Task.SyncID, obs.SyncID); err != nil {
				return err
			}
		}

		for _, c := range candidates {
			res, err := s.execHook(tx, `
				INSERT OR IGNORE INTO observation_refs (observation_sync_id, ref_kind, ref, graph_commit, created_at)
				VALUES (?, ?, ?, ?, ?)`,
				obs.SyncID, c.kind, c.ref, nullableStr(c.graphCommit), now)
			if err != nil {
				return fmt.Errorf("engram-projects: add observation ref: %w", err)
			}
			if affected, _ := res.RowsAffected(); affected > 0 {
				result.RefsAdded++
				result.Refs = append(result.Refs, ObservationRefOut{RefKind: c.kind, Ref: c.ref, GraphCommit: c.graphCommit})
				if err := s.enqueueObservationRefTx(tx, taskProject, obs.SyncID, c.kind, c.ref); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return LinkTaskObservationResult{}, err
	}

	return result, nil
}

// TaskCounts holds the per-task counters returned alongside a single task
// (backs GET /projects/{slug}/tasks/{task}). ListTasks computes the same two
// numbers per row; this is the single-task equivalent.
type TaskCounts struct {
	Observations int `json:"observations"`
	Evidence     int `json:"evidence"`
}

// TaskCounts counts the observations linked to a task and the evidence
// registered against it.
func (s *Store) TaskCounts(taskID int64) (TaskCounts, error) {
	var c TaskCounts
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM task_observations WHERE task_id = ?`, taskID,
	).Scan(&c.Observations); err != nil {
		return c, fmt.Errorf("engram-projects: count task observations: %w", err)
	}
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM evidence WHERE task_id = ? AND deleted_at IS NULL`, taskID,
	).Scan(&c.Evidence); err != nil {
		return c, fmt.Errorf("engram-projects: count task evidence: %w", err)
	}
	return c, nil
}

// TaskStateStale reports whether a task's Jira mirror is older than
// staleAfterHours, using the same rule ListTasks applies to every row.
func TaskStateStale(stateSyncedAt *string, staleAfterHours int) bool {
	if staleAfterHours <= 0 {
		staleAfterHours = 24
	}
	return isTaskStateStale(stateSyncedAt, staleAfterHours)
}

// ─── TUI Tasks tab (rfc-tui.md §4.3, §9.2) ─────────────────────────────────

// GetTask returns one task by its numeric id, regardless of project. The id
// is the tasks table's own primary key, so unlike ResolveTaskRef this needs
// no project to scope the lookup — the TUI's Tasks tab already resolved the
// id from a project-scoped ListTasks call before it ever reaches here.
func (s *Store) GetTask(id int64) (Task, error) {
	return s.getTaskByID(id)
}

// ErrInvalidTaskState is returned by UpdateTaskStateMirror when state is not
// one of the values the tasks.state CHECK constraint accepts. It is distinct
// from the rfc-engram-projects.md §5.0 sentinel errors above: this one guards
// a write rfc-tui.md §9.2 adds for the TUI, not an engram-projects tool.
var ErrInvalidTaskState = errors.New("invalid task state")

// mirrorableTaskStates lists every value the tasks.state CHECK constraint
// accepts (internal/store/projects_schema.go), reusing the internal/tasks
// list so the two never drift apart.
var mirrorableTaskStates = func() map[string]bool {
	states := make(map[string]bool, len(tasks.AllStates))
	for _, state := range tasks.AllStates {
		states[state] = true
	}
	return states
}()

// UpdateTaskStateMirror sets a task's local state mirror from the TUI
// (rfc-tui.md §9.2, ADR-028: "el cambio de state es espejo"). Jira remains
// the source of truth (D-02): this never talks to Jira and never touches
// jira_status, jira_status_category or state_synced_at — the columns the
// sync pipeline reads to detect drift between the mirror and the real Jira
// status. closed_at is cleared when the mirror moves a task out of a closed
// state, because the tasks table's CHECK constraint requires closed_at IS
// NULL outside ('done','cancelled'); it is never set by this path when
// moving a task into one of those two states, since only a real Jira
// transition — not a local guess — knows the true closing time.
func (s *Store) UpdateTaskStateMirror(id int64, state string) error {
	if !mirrorableTaskStates[state] {
		return fmt.Errorf("%w: %q", ErrInvalidTaskState, state)
	}
	now := s.nowUTC()
	closesTask := 0
	if isClosedState(state) {
		closesTask = 1
	}
	return s.withTx(func(tx *sql.Tx) error {
		res, err := s.execHook(tx, `
			UPDATE tasks SET state = ?, updated_at = ?,
				closed_at = CASE WHEN ? THEN closed_at ELSE NULL END
			WHERE id = ? AND deleted_at IS NULL`,
			state, now, closesTask, id)
		if err != nil {
			return fmt.Errorf("engram-projects: update task state mirror: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("engram-projects: update task state mirror: %w", err)
		}
		if affected == 0 {
			return ErrUnknownTask
		}
		return nil
	})
}
