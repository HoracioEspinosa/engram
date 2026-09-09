package data

import "github.com/HoracioEspinosa/engram/internal/store"

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
