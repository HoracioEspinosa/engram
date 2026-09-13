// Package data is the only part of the TUI that talks to the engram store.
//
// Tabs depend on the interfaces declared here, never on *store.Store, so a tab
// can be exercised against an in-memory fake and the storage layer can move
// without a view noticing.
package data

import (
	"errors"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// ErrStoreUnavailable is returned by an adapter built without a store, which
// happens when the TUI is constructed for a render-only test.
var ErrStoreUnavailable = errors.New("store is unavailable")

// ProjectHealth carries the counters and sync status for a project card.
type ProjectHealth struct {
	store.ProjectCardCounts
	Sync store.ProjectSyncSummary `json:"sync"`
}

// ProjectReader is the surface that the Selector and Dashboard screens need:
// the project list, individual cards, health counters, and the four blocks
// of data that make up the Dashboard (recent tasks, stale runbooks, latest
// evidence).
type ProjectReader interface {
	// ListCards returns every project card for the selector's list.
	ListCards() ([]store.ProjectCardListItem, error)
	// Card returns one project card by slug.
	Card(slug string) (store.ProjectCard, error)
	// Health returns the health counters and sync status for one project.
	Health(slug string) (ProjectHealth, error)
	// RecentTasks returns the most recent active tasks for the given project,
	// up to limit.
	RecentTasks(slug string, limit int) ([]store.TaskListItem, error)
	// StaleRunbooks returns the stale runbooks for the given project, up to limit.
	StaleRunbooks(slug string, limit int) ([]store.RunbookIndexRow, error)
	// LatestEvidence returns the most recent evidence entries for the given
	// project, up to limit.
	LatestEvidence(slug string, limit int) ([]store.EvidenceListItem, error)
}

// TaskDetail is the aggregate the Tasks tab's detail screen needs (S4): the
// task row plus its linked observations (task_observations, root_cause
// first) and its evidence.
type TaskDetail struct {
	Task         store.Task
	Observations []store.TaskObservationDetail
	Evidence     []store.EvidenceListItem
}

// TaskReader is the surface the Tasks tab needs: the filtered/searched list
// (S3), one task's aggregate detail (S4), the two writes ADR-028 allows from
// the TUI — the local `state` mirror and linking an observation — and the
// context pack (S5).
//
// ListTasks folds search into the same call rather than exposing a second
// SearchTasks method: store.TaskListFilter.Query already runs against
// tasks_fts (rfc-tui.md §9.2's "S3 search" query), so a second method would
// just be a thinner duplicate of this one.
type TaskReader interface {
	// ListTasks lists tasks for a project applying f (state, kind, query,
	// limit, offset), most recently updated first.
	ListTasks(project string, f store.TaskListFilter) ([]store.TaskListItem, error)
	// Task returns one task's aggregate detail by id.
	Task(id int64) (TaskDetail, error)
	// UpdateState sets a task's local state mirror. Jira remains the source
	// of truth (ADR-028): this never talks to Jira.
	UpdateState(id int64, state string) error
	// LinkObservation links an existing observation to a task by id.
	LinkObservation(taskID, observationID int64) error
	// ContextPack renders the markdown context pack for a task (delegates to
	// internal/project.BuildContextPack, the same function mem_context_pack
	// calls).
	ContextPack(taskID int64) (string, error)
}

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
