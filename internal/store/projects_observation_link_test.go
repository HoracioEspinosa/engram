package store

import (
	"errors"
	"testing"
)

// seedLinkableTask returns a store with one task and one open session, the
// minimum an observation needs before it can be linked to anything.
func seedLinkableTask(t *testing.T, project string) (*Store, Task) {
	t.Helper()
	s := newProjectsSchemaTestStore(t)
	r, err := s.UpsertTask(UpsertTaskParams{Project: project, JiraKey: strp("PROJ-1"), Title: strp("t"), Kind: strp("incident")})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	if err := s.CreateSession("s1", project, ""); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return s, r.Task
}

func countRows(t *testing.T, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

// TestAddObservationLinkedIsAtomic pins the whole point of the method: the
// observation and its link are one write. A link that cannot be made leaves no
// observation behind for a retry to duplicate.
func TestAddObservationLinkedIsAtomic(t *testing.T) {
	t.Run("a rejected link writes no observation", func(t *testing.T) {
		s, task := seedLinkableTask(t, "nextcloud")
		before := countRows(t, s, `SELECT COUNT(*) FROM observations`)

		_, err := s.AddObservationLinked(
			AddObservationParams{SessionID: "s1", Type: "bugfix", Title: "root cause", Content: "c", Project: "nextcloud"},
			&ObservationLink{Task: &task, GraphRef: "node:auth"},
		)
		if !errors.Is(err, ErrGraphCommitRequired) {
			t.Fatalf("expected ErrGraphCommitRequired, got %v", err)
		}
		if after := countRows(t, s, `SELECT COUNT(*) FROM observations`); after != before {
			t.Fatalf("a rejected link must leave no observation: %d -> %d", before, after)
		}
	})

	t.Run("a cross-project link writes no observation", func(t *testing.T) {
		s, task := seedLinkableTask(t, "nextcloud")
		before := countRows(t, s, `SELECT COUNT(*) FROM observations`)

		_, err := s.AddObservationLinked(
			AddObservationParams{SessionID: "s1", Type: "bugfix", Title: "elsewhere", Content: "c", Project: "other-project"},
			&ObservationLink{Task: &task},
		)
		if !errors.Is(err, ErrCrossProjectLink) {
			t.Fatalf("expected ErrCrossProjectLink, got %v", err)
		}
		if after := countRows(t, s, `SELECT COUNT(*) FROM observations`); after != before {
			t.Fatalf("a cross-project link must leave no observation: %d -> %d", before, after)
		}
	})

	t.Run("a linked observation commits both halves", func(t *testing.T) {
		s, task := seedLinkableTask(t, "nextcloud")

		got, err := s.AddObservationLinked(
			AddObservationParams{SessionID: "s1", Type: "bugfix", Title: "root cause", Content: "c", Project: "nextcloud"},
			&ObservationLink{Task: &task, Role: "root_cause"},
		)
		if err != nil {
			t.Fatalf("AddObservationLinked: %v", err)
		}
		if got.ObservationID == 0 || got.ObservationSyncID == "" {
			t.Fatalf("expected the observation identity back, got %+v", got)
		}
		if got.LinkedTaskSyncID != task.SyncID || got.Role != "root_cause" {
			t.Fatalf("expected the link reported back, got %+v", got)
		}
		if n := countRows(t, s, `SELECT COUNT(*) FROM task_observations WHERE observation_id = ? AND role = 'root_cause'`, got.ObservationID); n != 1 {
			t.Fatalf("expected one link row with the requested role, got %d", n)
		}
	})

	t.Run("no link behaves like AddObservation", func(t *testing.T) {
		s, _ := seedLinkableTask(t, "nextcloud")

		got, err := s.AddObservationLinked(
			AddObservationParams{SessionID: "s1", Type: "manual", Title: "plain", Content: "c", Project: "nextcloud"},
			nil,
		)
		if err != nil {
			t.Fatalf("AddObservationLinked: %v", err)
		}
		if got.LinkedTaskSyncID != "" || got.RefsAdded != 0 {
			t.Fatalf("expected no link, got %+v", got)
		}
		if n := countRows(t, s, `SELECT COUNT(*) FROM observations WHERE id = ?`, got.ObservationID); n != 1 {
			t.Fatalf("expected the observation to exist, got %d", n)
		}
	})
}

// TestAddObservationLinkedWritesGraphRef pins the graph half: a graph_ref
// lands in observation_refs with its commit, and the commit is mandatory
// because the same node name means something different in a later graph.
func TestAddObservationLinkedWritesGraphRef(t *testing.T) {
	s, task := seedLinkableTask(t, "nextcloud")

	got, err := s.AddObservationLinked(
		AddObservationParams{SessionID: "s1", Type: "architecture", Title: "auth node", Content: "c", Project: "nextcloud"},
		&ObservationLink{Task: &task, GraphRef: "node:auth", GraphCommit: "0123456789abcdef0123456789abcdef01234567"},
	)
	if err != nil {
		t.Fatalf("AddObservationLinked: %v", err)
	}
	if got.RefsAdded != 1 {
		t.Fatalf("refs_added = %d, want 1", got.RefsAdded)
	}
	var ref, commit string
	if err := s.db.QueryRow(
		`SELECT ref, ifnull(graph_commit,'') FROM observation_refs WHERE observation_sync_id = ? AND ref_kind = 'graph'`,
		got.ObservationSyncID,
	).Scan(&ref, &commit); err != nil {
		t.Fatalf("read observation_refs: %v", err)
	}
	if ref != "node:auth" || commit != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("stored ref = %q commit = %q", ref, commit)
	}

	// An abbreviated commit is refused outright rather than silently dropped
	// by the schema CHECK inside an idempotent insert.
	if _, err := s.AddObservationLinked(
		AddObservationParams{SessionID: "s1", Type: "architecture", Title: "short sha", Content: "c", Project: "nextcloud"},
		&ObservationLink{GraphRef: "node:auth", GraphCommit: "abc1234"},
	); !errors.Is(err, ErrGraphCommitNotFullSHA) {
		t.Fatalf("expected ErrGraphCommitNotFullSHA, got %v", err)
	}

	// A graph_ref without a task still records the reference: the graph
	// pointer belongs to the observation, not to the link.
	loose, err := s.AddObservationLinked(
		AddObservationParams{SessionID: "s1", Type: "architecture", Title: "store node", Content: "c", Project: "nextcloud"},
		&ObservationLink{GraphRef: "node:store", GraphCommit: "0123456789abcdef0123456789abcdef01234567"},
	)
	if err != nil {
		t.Fatalf("AddObservationLinked without a task: %v", err)
	}
	if loose.RefsAdded != 1 || loose.LinkedTaskSyncID != "" {
		t.Fatalf("expected a ref and no task link, got %+v", loose)
	}
}
