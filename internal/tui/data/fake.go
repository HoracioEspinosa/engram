package data

import (
	"errors"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// FakeMemory is an in-memory MemoryReader for tests: set the fields you care
// about, leave the rest zero.
//
// Err short-circuits every method, which is how a test drives the error banner
// without a broken database.
type FakeMemory struct {
	StatsResult     *store.Stats
	SearchResults   []store.SearchResult
	Observations    []store.Observation
	ObservationByID map[int64]*store.Observation
	TimelineResult  *store.TimelineResult
	Sessions        []store.SessionSummary
	SessionObs      map[string][]store.Observation

	// Err is returned by every method when set.
	Err error

	// Queries and DeletedSessions record what the tab asked for, so a test can
	// assert on the call and not only on the rendered result.
	Queries         []string
	DeletedSessions []string
}

var _ MemoryReader = (*FakeMemory)(nil)

func (f *FakeMemory) Stats() (*store.Stats, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.StatsResult, nil
}

func (f *FakeMemory) Search(query string, _ store.SearchOptions) ([]store.SearchResult, error) {
	f.Queries = append(f.Queries, query)
	if f.Err != nil {
		return nil, f.Err
	}
	return f.SearchResults, nil
}

func (f *FakeMemory) RecentObservations(limit int) ([]store.Observation, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return capObservations(f.Observations, limit), nil
}

func (f *FakeMemory) Observation(id int64) (*store.Observation, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.ObservationByID[id], nil
}

func (f *FakeMemory) Timeline(_ int64, _, _ int) (*store.TimelineResult, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.TimelineResult, nil
}

func (f *FakeMemory) RecentSessions(limit int) ([]store.SessionSummary, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	if limit > 0 && len(f.Sessions) > limit {
		return f.Sessions[:limit], nil
	}
	return f.Sessions, nil
}

func (f *FakeMemory) SessionObservations(sessionID string, limit int) ([]store.Observation, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return capObservations(f.SessionObs[sessionID], limit), nil
}

func (f *FakeMemory) DeleteSession(sessionID string) error {
	if f.Err != nil {
		return f.Err
	}
	f.DeletedSessions = append(f.DeletedSessions, sessionID)
	return nil
}

func capObservations(obs []store.Observation, limit int) []store.Observation {
	if limit > 0 && len(obs) > limit {
		return obs[:limit]
	}
	return obs
}

// FakeProject is an in-memory ProjectReader for tests: set the fields you care
// about, leave the rest zero.
//
// Err short-circuits every method, which is how a test drives the error banner
// without a broken database.
type FakeProject struct {
	Cards             []store.ProjectCardListItem
	CardBySlug        map[string]store.ProjectCard
	HealthBySlug      map[string]ProjectHealth
	TasksBySlug       map[string][]store.TaskListItem
	StaleRunbooksSlug map[string][]store.RunbookIndexRow
	EvidenceBySlug    map[string][]store.EvidenceListItem

	// Err is returned by every method when set.
	Err error
}

var _ ProjectReader = (*FakeProject)(nil)

func (f *FakeProject) ListCards() ([]store.ProjectCardListItem, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Cards, nil
}

func (f *FakeProject) Card(slug string) (store.ProjectCard, error) {
	if f.Err != nil {
		return store.ProjectCard{}, f.Err
	}
	card, ok := f.CardBySlug[slug]
	if !ok {
		return store.ProjectCard{}, errors.New("no such project")
	}
	return card, nil
}

func (f *FakeProject) Health(slug string) (ProjectHealth, error) {
	if f.Err != nil {
		return ProjectHealth{}, f.Err
	}
	// A project with no counters is not a missing project: it is a project
	// whose numbers are all zero, the same way one with no tasks returns an
	// empty slice rather than an error. Only Card reports a slug that does
	// not exist, because the card is what identifies the project at all.
	return f.HealthBySlug[slug], nil
}

func (f *FakeProject) RecentTasks(slug string, limit int) ([]store.TaskListItem, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	tasks := f.TasksBySlug[slug]
	if limit > 0 && len(tasks) > limit {
		return tasks[:limit], nil
	}
	return tasks, nil
}

func (f *FakeProject) StaleRunbooks(slug string, limit int) ([]store.RunbookIndexRow, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	runbooks := f.StaleRunbooksSlug[slug]
	if limit > 0 && len(runbooks) > limit {
		return runbooks[:limit], nil
	}
	return runbooks, nil
}

func (f *FakeProject) LatestEvidence(slug string, limit int) ([]store.EvidenceListItem, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	evidence := f.EvidenceBySlug[slug]
	if limit > 0 && len(evidence) > limit {
		return evidence[:limit], nil
	}
	return evidence, nil
}

// FakeTask is an in-memory TaskReader for tests: set the fields you care
// about, leave the rest zero.
//
// Err short-circuits every method, which is how a test drives the error
// banner without a broken database. UpdateStateCalls and LinkCalls record
// what the tab asked to write, so a test can assert on the call and not only
// on the screen it produced.
type FakeTask struct {
	ItemsByProject  map[string][]store.TaskListItem
	DetailByID      map[int64]TaskDetail
	ContextPackByID map[int64]string

	// Err is returned by every method when set.
	Err error

	// LastListFilter records the filter the tab most recently asked for.
	LastListFilter   store.TaskListFilter
	UpdateStateCalls []struct {
		ID    int64
		State string
	}
	LinkCalls []struct {
		TaskID        int64
		ObservationID int64
	}
}

var _ TaskReader = (*FakeTask)(nil)

func (f *FakeTask) ListTasks(taskProject string, filter store.TaskListFilter) ([]store.TaskListItem, error) {
	f.LastListFilter = filter
	if f.Err != nil {
		return nil, f.Err
	}
	items := f.ItemsByProject[taskProject]
	if filter.Query != "" {
		var matched []store.TaskListItem
		for _, it := range items {
			if strings.Contains(strings.ToLower(it.Title), strings.ToLower(filter.Query)) {
				matched = append(matched, it)
			}
		}
		items = matched
	}
	// Mirrors store.ListTasks's own default: an unset limit still caps the
	// page at 20, it does not mean "everything" (see internal/store's
	// projects_tasks.go ListTasks). A fake that returned every row here
	// would let a caller's pagination logic go untested.
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := filter.Offset
	if offset > len(items) {
		offset = len(items)
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end], nil
}

func (f *FakeTask) Task(id int64) (TaskDetail, error) {
	if f.Err != nil {
		return TaskDetail{}, f.Err
	}
	d, ok := f.DetailByID[id]
	if !ok {
		return TaskDetail{}, errors.New("no such task")
	}
	return d, nil
}

func (f *FakeTask) UpdateState(id int64, state string) error {
	f.UpdateStateCalls = append(f.UpdateStateCalls, struct {
		ID    int64
		State string
	}{ID: id, State: state})
	if f.Err != nil {
		return f.Err
	}
	return nil
}

func (f *FakeTask) LinkObservation(taskID, observationID int64) error {
	f.LinkCalls = append(f.LinkCalls, struct {
		TaskID        int64
		ObservationID int64
	}{TaskID: taskID, ObservationID: observationID})
	if f.Err != nil {
		return f.Err
	}
	return nil
}

func (f *FakeTask) ContextPack(taskID int64) (string, error) {
	if f.Err != nil {
		return "", f.Err
	}
	return f.ContextPackByID[taskID], nil
}
