package data

import (
	"fmt"

	"github.com/HoracioEspinosa/engram/internal/project"
	"github.com/HoracioEspinosa/engram/internal/store"
)

// sqliteMemory adapts the engram SQLite store to MemoryReader.
//
// The TUI shares the single *store.Store that cmd/engram already opened; it
// does not open a connection of its own.
type sqliteMemory struct {
	store *store.Store
}

// NewMemoryReader wraps an engram store as the Memory tab's reader.
//
// A nil store yields a reader whose destructive call reports
// ErrStoreUnavailable; the queries assume a real store, exactly as the screens
// did before they were extracted.
func NewMemoryReader(s *store.Store) MemoryReader {
	return sqliteMemory{store: s}
}

func (r sqliteMemory) Stats() (*store.Stats, error) {
	return r.store.Stats()
}

func (r sqliteMemory) Search(query string, opts store.SearchOptions) ([]store.SearchResult, error) {
	return r.store.Search(query, opts)
}

func (r sqliteMemory) RecentObservations(limit int) ([]store.Observation, error) {
	return r.store.AllObservations("", "", limit)
}

func (r sqliteMemory) Observation(id int64) (*store.Observation, error) {
	return r.store.GetObservation(id)
}

func (r sqliteMemory) Timeline(id int64, before, after int) (*store.TimelineResult, error) {
	return r.store.Timeline(id, before, after)
}

func (r sqliteMemory) RecentSessions(limit int) ([]store.SessionSummary, error) {
	return r.store.AllSessions("", limit)
}

func (r sqliteMemory) SessionObservations(sessionID string, limit int) ([]store.Observation, error) {
	return r.store.SessionObservations(sessionID, limit)
}

func (r sqliteMemory) DeleteSession(sessionID string) error {
	if r.store == nil {
		return ErrStoreUnavailable
	}
	return r.store.DeleteSession(sessionID)
}

// sqliteProject adapts the engram SQLite store to ProjectReader.
//
// The TUI shares the single *store.Store that cmd/engram already opened; it
// does not open a connection of its own.
type sqliteProject struct {
	store *store.Store
}

// NewProjectReader wraps an engram store as the Selector and Dashboard screens'
// reader.
//
// A nil store yields a reader whose queries report ErrStoreUnavailable.
func NewProjectReader(s *store.Store) ProjectReader {
	return sqliteProject{store: s}
}

func (r sqliteProject) ListCards() ([]store.ProjectCardListItem, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	cards, _, err := r.store.ListProjectCards(true)
	return cards, err
}

func (r sqliteProject) Card(slug string) (store.ProjectCard, error) {
	if r.store == nil {
		return store.ProjectCard{}, ErrStoreUnavailable
	}
	return r.store.GetProjectCard(slug)
}

func (r sqliteProject) Health(slug string) (ProjectHealth, error) {
	if r.store == nil {
		return ProjectHealth{}, ErrStoreUnavailable
	}
	counts, err := r.store.ProjectCardCounts(slug)
	if err != nil {
		return ProjectHealth{}, err
	}
	sync, err := r.store.ProjectSyncSummary(slug)
	if err != nil {
		return ProjectHealth{}, err
	}
	return ProjectHealth{ProjectCardCounts: counts, Sync: sync}, nil
}

func (r sqliteProject) RecentTasks(slug string, limit int) ([]store.TaskListItem, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	tasks, _, err := r.store.ListTasks(slug, store.TaskListFilter{
		State:  "active",
		Limit:  limit,
		Offset: 0,
	})
	return tasks, err
}

func (r sqliteProject) StaleRunbooks(slug string, limit int) ([]store.RunbookIndexRow, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	staleTrue := true
	runbooks, _, err := r.store.ListRunbookIndex(slug, store.RunbookListFilter{
		Stale:  &staleTrue,
		Limit:  limit,
		Offset: 0,
	})
	return runbooks, err
}

func (r sqliteProject) LatestEvidence(slug string, limit int) ([]store.EvidenceListItem, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	evidence, _, _, err := r.store.ListEvidence(slug, store.EvidenceListFilter{
		TaskSyncID: "",
		Limit:      limit,
		Offset:     0,
	})
	return evidence, err
}

// sqliteTask adapts the engram SQLite store to TaskReader.
//
// The TUI shares the single *store.Store that cmd/engram already opened; it
// does not open a connection of its own.
type sqliteTask struct {
	store *store.Store
}

// NewTaskReader wraps an engram store as the Tasks tab's reader.
//
// A nil store yields a reader whose queries report ErrStoreUnavailable.
func NewTaskReader(s *store.Store) TaskReader {
	return sqliteTask{store: s}
}

func (r sqliteTask) ListTasks(taskProject string, f store.TaskListFilter) ([]store.TaskListItem, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	items, _, err := r.store.ListTasks(taskProject, f)
	return items, err
}

func (r sqliteTask) Task(id int64) (TaskDetail, error) {
	if r.store == nil {
		return TaskDetail{}, ErrStoreUnavailable
	}
	t, err := r.store.GetTask(id)
	if err != nil {
		return TaskDetail{}, err
	}
	observations, err := r.store.TaskObservationsForTask(id)
	if err != nil {
		return TaskDetail{}, err
	}
	evidence, _, _, err := r.store.ListEvidence(t.Project, store.EvidenceListFilter{TaskSyncID: t.SyncID})
	if err != nil {
		return TaskDetail{}, err
	}
	return TaskDetail{Task: t, Observations: observations, Evidence: evidence}, nil
}

func (r sqliteTask) UpdateState(id int64, state string) error {
	if r.store == nil {
		return ErrStoreUnavailable
	}
	return r.store.UpdateTaskStateMirror(id, state)
}

func (r sqliteTask) LinkObservation(taskID, observationID int64) error {
	if r.store == nil {
		return ErrStoreUnavailable
	}
	t, err := r.store.GetTask(taskID)
	if err != nil {
		return err
	}
	_, err = r.store.LinkTaskObservation(store.LinkTaskObservationParams{Task: t, ObservationID: observationID})
	return err
}

// ContextPack resolves the task by id to find its project, then delegates to
// internal/project.BuildContextPack — the same function mem_context_pack
// calls — addressing the task by its numeric id ("#<id>", ResolveTaskRef's
// local-id form) so the rendered pack always reflects the exact row S4 has
// on screen.
func (r sqliteTask) ContextPack(taskID int64) (string, error) {
	if r.store == nil {
		return "", ErrStoreUnavailable
	}
	t, err := r.store.GetTask(taskID)
	if err != nil {
		return "", err
	}
	_, markdown, err := project.BuildContextPack(r.store, t.Project, fmt.Sprintf("#%d", t.ID), project.DefaultContextPackOptions())
	return markdown, err
}

// sqliteEvidence adapts the engram SQLite store to EvidenceReader.
//
// The TUI shares the single *store.Store that cmd/engram already opened; it
// does not open a connection of its own.
type sqliteEvidence struct {
	store *store.Store
}

// NewEvidenceReader wraps an engram store as the Evidence tab's reader.
//
// A nil store yields a reader whose queries report ErrStoreUnavailable.
func NewEvidenceReader(s *store.Store) EvidenceReader {
	return sqliteEvidence{store: s}
}

func (r sqliteEvidence) ListEvidence(project string, f store.EvidenceListFilter) ([]store.EvidenceListItem, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	items, _, _, err := r.store.ListEvidence(project, f)
	return items, err
}

// sqliteRunbook adapts the engram SQLite store to RunbookReader.
//
// The TUI shares the single *store.Store that cmd/engram already opened; it
// does not open a connection of its own.
type sqliteRunbook struct {
	store *store.Store
}

// NewRunbookReader wraps an engram store as the Runbooks tab's reader.
//
// A nil store yields a reader whose queries report ErrStoreUnavailable.
func NewRunbookReader(s *store.Store) RunbookReader {
	return sqliteRunbook{store: s}
}

func (r sqliteRunbook) ListRunbooks(project string, all bool) ([]store.RunbookIndexRow, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	if all {
		project = ""
	}
	items, _, err := r.store.ListRunbookIndex(project, store.RunbookListFilter{})
	return items, err
}

func (r sqliteRunbook) SearchRunbooks(project string, all bool, query string, limit int) ([]store.RunbookIndexRow, error) {
	if r.store == nil {
		return nil, ErrStoreUnavailable
	}
	if all {
		project = ""
	}
	return r.store.SearchRunbookIndex(query, project, limit)
}

// TaskKey returns the label a task is identified by everywhere in the TUI:
// its Jira key, falling back to its SDD change slug, falling back to its
// sync_id. It is the same precedence internal/project.BuildContextPack uses
// for the context pack's own header, exported through data (rather than
// tabs/tasks importing internal/project directly) so every tab stays on the
// dependency rule in rfc-tui.md §4.1: a tab imports only data, theme and
// shared.
func TaskKey(t store.Task) string {
	return project.TaskKey(t)
}

// JiraURL returns the browse URL for a Jira key, using the same
// ENGRAM_JIRA_BASE_URL configuration internal/project.BuildContextPack reads
// for the context pack's own links.
func JiraURL(jiraKey string) string {
	return project.JiraBaseURL() + jiraKey
}
