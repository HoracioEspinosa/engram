package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// ObservationRoles are the roles an observation can play for the task it is
// linked to, exactly as the task_observations CHECK constraint spells them.
var ObservationRoles = []string{"context", "decision", "root_cause", "evidence", "summary"}

// DefaultObservationRole is the role a link takes when the caller names none.
const DefaultObservationRole = "context"

// ObservationLink is the optional second half of AddObservationLinked: the
// task a new observation belongs to and the graph node it describes.
type ObservationLink struct {
	// Task is the task to link the observation to. A nil Task links nothing,
	// which is how an observation that only carries a graph reference is
	// written.
	Task *Task
	// Role is the task_observations role. Empty means DefaultObservationRole.
	Role string
	// GraphRef is the graph node the observation describes. Empty writes no
	// graph reference.
	GraphRef string
	// GraphCommit is the commit GraphRef was resolved against. It is required
	// whenever GraphRef is set: the same node name means something different
	// in a later graph, so a reference without its commit points nowhere.
	GraphCommit string
}

// AddObservationLinkedResult reports what AddObservationLinked wrote. The
// observation id is the one AddObservation would have returned.
type AddObservationLinkedResult struct {
	ObservationID     int64
	ObservationSyncID string
	// LinkedTaskSyncID is the task the observation was linked to, empty when
	// the call carried no task.
	LinkedTaskSyncID string
	// Role is the role the link was written with, empty when nothing was
	// linked.
	Role string
	// RefsAdded counts the observation_refs rows this call created. A
	// reference that already existed counts zero.
	RefsAdded int
}

// AddObservationLinked writes an observation together with its task link and
// its graph reference, in one transaction.
//
// Writing the three separately loses either half on failure: an observation
// whose link was rejected is invisible work the caller retries and duplicates,
// and a link whose observation never landed is a dangling row. Every rejection
// is decided before the first INSERT, and the transaction carries the rest.
func (s *Store) AddObservationLinked(p AddObservationParams, link *ObservationLink) (AddObservationLinkedResult, error) {
	var result AddObservationLinkedResult
	if link == nil {
		link = &ObservationLink{}
	}

	observationProject, _ := NormalizeProject(p.Project)
	linkProject := observationProject

	role := strings.TrimSpace(link.Role)
	if link.Task != nil {
		linkProject, _ = NormalizeProject(link.Task.Project)
		if linkProject != observationProject {
			return result, ErrCrossProjectLink
		}
		if role == "" {
			role = DefaultObservationRole
		}
		if !validObservationRole(role) {
			return result, fmt.Errorf("engram-projects: unknown observation role %q, want one of %s", role, strings.Join(ObservationRoles, ", "))
		}
	}

	graphRef := strings.TrimSpace(link.GraphRef)
	graphCommit := strings.TrimSpace(link.GraphCommit)
	if graphRef != "" {
		if graphCommit == "" {
			return result, ErrGraphCommitRequired
		}
		if !isFullGitSHA(graphCommit) {
			return result, ErrGraphCommitNotFullSHA
		}
	}

	now := s.nowUTC()
	if err := s.withTx(func(tx *sql.Tx) error {
		result = AddObservationLinkedResult{}
		observationID, err := s.addObservationTx(tx, p)
		if err != nil {
			return err
		}
		obs, err := s.getObservationTx(tx, observationID)
		if err != nil {
			return err
		}
		result.ObservationID = observationID
		result.ObservationSyncID = obs.SyncID

		if link.Task != nil {
			res, err := s.execHook(tx, `
				INSERT OR IGNORE INTO task_observations (task_id, observation_id, task_sync_id, observation_sync_id, role, linked_at)
				VALUES (?, ?, ?, ?, ?, ?)`,
				link.Task.ID, observationID, link.Task.SyncID, obs.SyncID, role, now)
			if err != nil {
				return fmt.Errorf("engram-projects: link task observation: %w", err)
			}
			result.LinkedTaskSyncID = link.Task.SyncID
			result.Role = role
			if affected, _ := res.RowsAffected(); affected > 0 {
				if err := s.enqueueTaskLinkTx(tx, linkProject, link.Task.SyncID, obs.SyncID); err != nil {
					return err
				}
			}
		}

		if graphRef != "" {
			// ON CONFLICT DO NOTHING rather than INSERT OR IGNORE: the
			// reference is idempotent, but a malformed one must be reported,
			// not swallowed along with the duplicate.
			res, err := s.execHook(tx, `
				INSERT INTO observation_refs (observation_sync_id, ref_kind, ref, graph_commit, created_at)
				VALUES (?, 'graph', ?, ?, ?)
				ON CONFLICT (observation_sync_id, ref_kind, ref) DO NOTHING`,
				obs.SyncID, graphRef, graphCommit, now)
			if err != nil {
				return fmt.Errorf("engram-projects: add observation ref: %w", err)
			}
			if affected, _ := res.RowsAffected(); affected > 0 {
				result.RefsAdded++
				if err := s.enqueueObservationRefTx(tx, linkProject, obs.SyncID, "graph", graphRef); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return AddObservationLinkedResult{}, err
	}
	return result, nil
}

// isFullGitSHA reports whether commit is the 40-hex-character form the schema
// stores. An abbreviated SHA is ambiguous and stops resolving as a repository
// grows, so a graph reference only accepts the full one.
func isFullGitSHA(commit string) bool {
	if len(commit) != 40 {
		return false
	}
	for _, r := range commit {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func validObservationRole(role string) bool {
	for _, known := range ObservationRoles {
		if role == known {
			return true
		}
	}
	return false
}
