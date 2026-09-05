package data

import "github.com/Gentleman-Programming/engram/internal/store"

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
