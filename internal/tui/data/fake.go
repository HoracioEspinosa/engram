package data

import (
	"errors"

	"github.com/Gentleman-Programming/engram/internal/store"
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
