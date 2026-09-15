package store

import (
	"fmt"
	"strings"
)

// TaskObservationDetail is one row of a task's linked observations, as
// needed by internal/project.BuildContextPack (RFC §5.10, section 5).
type TaskObservationDetail struct {
	Role        string      `json:"role"`
	LinkedAt    string      `json:"linked_at"`
	Observation Observation `json:"observation"`
}

// rolePriority orders task_observations rows the way the context pack wants
// them: root_cause, decision, summary, evidence, context, then linked_at DESC.
var rolePriority = map[string]int{
	"root_cause": 0,
	"decision":   1,
	"summary":    2,
	"evidence":   3,
	"context":    4,
}

// TaskObservationsForTask returns every observation linked to taskID,
// ordered by role priority and then by link recency.
func (s *Store) TaskObservationsForTask(taskID int64) ([]TaskObservationDetail, error) {
	rows, err := s.db.Query(`
		SELECT link.role, link.linked_at, `+observationSelectColumns+`
		FROM task_observations link
		JOIN observations o ON o.id = link.observation_id
		WHERE link.task_id = ? AND o.deleted_at IS NULL
		ORDER BY link.linked_at DESC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var details []TaskObservationDetail
	for rows.Next() {
		var d TaskObservationDetail
		if err := rows.Scan(&d.Role, &d.LinkedAt,
			&d.Observation.ID, &d.Observation.SyncID, &d.Observation.SessionID, &d.Observation.Type,
			&d.Observation.Title, &d.Observation.Content, &d.Observation.ToolName, &d.Observation.Project,
			&d.Observation.Scope, &d.Observation.TopicKey, &d.Observation.RevisionCount, &d.Observation.DuplicateCount,
			&d.Observation.LastSeenAt, &d.Observation.ReviewAfter, &d.Observation.Pinned, &d.Observation.CreatedAt,
			&d.Observation.UpdatedAt, &d.Observation.DeletedAt,
		); err != nil {
			return nil, err
		}
		details = append(details, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Stable sort by role priority (linked_at DESC already applied by the query).
	for i := 1; i < len(details); i++ {
		for j := i; j > 0 && rolePriority[details[j-1].Role] > rolePriority[details[j].Role]; j-- {
			details[j-1], details[j] = details[j], details[j-1]
		}
	}
	return details, nil
}

// ObservationsByTopicKeyPrefix backfills the context pack's observations
// section (RFC §5.10, section 5) with observations whose topic_key starts
// with prefix, e.g. "sdd/proj-10336/".
func (s *Store) ObservationsByTopicKeyPrefix(project, prefix string, limit int) ([]Observation, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.Query(`
		SELECT `+observationSelectColumns+`
		FROM observations o
		WHERE LOWER(o.project) = ? AND o.topic_key LIKE ? AND o.deleted_at IS NULL
		ORDER BY o.updated_at DESC LIMIT ?`, project, prefix+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Observation
	for rows.Next() {
		var o Observation
		if err := rows.Scan(&o.ID, &o.SyncID, &o.SessionID, &o.Type, &o.Title, &o.Content, &o.ToolName,
			&o.Project, &o.Scope, &o.TopicKey, &o.RevisionCount, &o.DuplicateCount, &o.LastSeenAt,
			&o.ReviewAfter, &o.Pinned, &o.CreatedAt, &o.UpdatedAt, &o.DeletedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ObservationRefRow is one observation_refs row, grouped by ref_kind for the
// context pack's `refs` section.
//
// ObservationID and ObservationSyncID say which observation the reference hangs
// off. They are filled by the project-scoped listing, where a row on its own
// would be unattributable; ObservationRefsFor is already asked per observation,
// so it leaves them at zero and the fields drop out of its envelope.
type ObservationRefRow struct {
	RefKind           string `json:"ref_kind"`
	Ref               string `json:"ref"`
	GraphCommit       string `json:"graph_commit,omitempty"`
	ObservationID     int64  `json:"observation_id,omitempty"`
	ObservationSyncID string `json:"observation_sync_id,omitempty"`
}

// ListObservationRefs pages through the references a project's observations
// carry, newest first, optionally narrowed to one ref_kind.
//
// observation_refs is keyed by observation_sync_id and knows nothing about
// projects, so the join against observations is the only thing that can answer
// "which of this project's observations point at the graph?". The project is
// compared folded to lower case, like every other project filter here, because
// rows written before project names were lowercased are still in the table.
func (s *Store) ListObservationRefs(project, kind string, limit, offset int) (Page[ObservationRefRow], error) {
	limit = clampPageLimit(limit, defaultObservationRefsLimit, maxObservationRefsLimit)
	if offset < 0 {
		offset = 0
	}

	args := []any{strings.ToLower(strings.TrimSpace(project))}
	kindFilter := ""
	if k := strings.TrimSpace(kind); k != "" {
		kindFilter = " AND r.ref_kind = ?"
		args = append(args, k)
	}
	args = append(args, limit, offset)

	rows, err := s.readDB().Query(`
		SELECT r.ref_kind, r.ref, r.graph_commit, o.id, o.sync_id, COUNT(*) OVER() AS total
		FROM observation_refs r
		JOIN observations o ON o.sync_id = r.observation_sync_id
		WHERE lower(o.project) = ? AND o.deleted_at IS NULL`+kindFilter+`
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return Page[ObservationRefRow]{}, fmt.Errorf("engram-projects: list observation refs: %w", err)
	}
	defer rows.Close()

	page := Page[ObservationRefRow]{Items: []ObservationRefRow{}, Limit: limit, Offset: offset}
	for rows.Next() {
		var r ObservationRefRow
		var graphCommit *string
		if err := rows.Scan(&r.RefKind, &r.Ref, &graphCommit, &r.ObservationID, &r.ObservationSyncID, &page.Total); err != nil {
			return Page[ObservationRefRow]{}, fmt.Errorf("engram-projects: scan observation ref: %w", err)
		}
		if graphCommit != nil {
			r.GraphCommit = *graphCommit
		}
		page.Items = append(page.Items, r)
	}
	if err := rows.Err(); err != nil {
		return Page[ObservationRefRow]{}, fmt.Errorf("engram-projects: list observation refs: %w", err)
	}
	return page, nil
}

const (
	defaultObservationRefsLimit = 50
	maxObservationRefsLimit     = 500
)

// clampPageLimit keeps a caller's page size inside what a listing will serve,
// taking the default when nothing was asked for.
func clampPageLimit(limit, def, max int) int {
	if limit <= 0 {
		return def
	}
	if limit > max {
		return max
	}
	return limit
}

// ObservationRefsFor returns every observation_refs row for the given
// observation sync_ids, in insertion order.
func (s *Store) ObservationRefsFor(observationSyncIDs []string) ([]ObservationRefRow, error) {
	if len(observationSyncIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(observationSyncIDs))
	args := make([]any, len(observationSyncIDs))
	for i, id := range observationSyncIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := s.db.Query(`
		SELECT ref_kind, ref, graph_commit FROM observation_refs
		WHERE observation_sync_id IN (`+strings.Join(placeholders, ",")+`)
		ORDER BY id ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ObservationRefRow
	for rows.Next() {
		var r ObservationRefRow
		var graphCommit *string
		if err := rows.Scan(&r.RefKind, &r.Ref, &graphCommit); err != nil {
			return nil, err
		}
		if graphCommit != nil {
			r.GraphCommit = *graphCommit
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
