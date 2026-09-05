package data

import "github.com/Gentleman-Programming/engram/internal/store"

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
