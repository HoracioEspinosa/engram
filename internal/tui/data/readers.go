// Package data is the only part of the TUI that talks to the engram store.
//
// Tabs depend on the interfaces declared here, never on *store.Store, so a tab
// can be exercised against an in-memory fake and the storage layer can move
// without a view noticing.
package data

import (
	"errors"

	"github.com/Gentleman-Programming/engram/internal/store"
)

// ErrStoreUnavailable is returned by an adapter built without a store, which
// happens when the TUI is constructed for a render-only test.
var ErrStoreUnavailable = errors.New("store is unavailable")

// MemoryReader is the surface the Memory tab needs: the observation, session
// and timeline queries behind engram's memory screens, plus the one destructive
// operation those screens expose.
//
// It is named "reader" for the role it plays — every method but DeleteSession
// is a query — and DeleteSession lives here rather than in a second interface
// because the delete prompt is part of the same screen and splitting it would
// buy nothing but a second constructor argument.
type MemoryReader interface {
	// Stats returns the dashboard counters.
	Stats() (*store.Stats, error)
	// Search runs a full-text query over observations.
	Search(query string, opts store.SearchOptions) ([]store.SearchResult, error)
	// RecentObservations returns the newest observations across all projects.
	RecentObservations(limit int) ([]store.Observation, error)
	// Observation returns one observation by id.
	Observation(id int64) (*store.Observation, error)
	// Timeline returns the observations surrounding id inside its session.
	Timeline(id int64, before, after int) (*store.TimelineResult, error)
	// RecentSessions returns the newest sessions across all projects.
	RecentSessions(limit int) ([]store.SessionSummary, error)
	// SessionObservations returns the observations recorded in one session.
	SessionObservations(sessionID string, limit int) ([]store.Observation, error)
	// DeleteSession removes an empty session. Deleting a session that still
	// holds observations must fail rather than orphan them.
	DeleteSession(sessionID string) error
}
