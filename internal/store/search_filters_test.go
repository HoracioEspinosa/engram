package store

import (
	"fmt"
	"testing"
)

// TestSearchPagedFiltersAndTotal pins the workspace filters mem_search exposes:
// a page that knows how big the whole answer is, and the four ways of narrowing
// it that a workspace asks for — the task, the graph node, the date range, and
// the project subtree.
func TestSearchPagedFiltersAndTotal(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	if err := s.CreateSession("s1", "koi-garden", ""); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	key := "PROJ-1"
	title := "drain the pond"
	kind := "incident"
	task, err := s.UpsertTask(UpsertTaskParams{Project: "koi-garden", JiraKey: &key, Title: &title, Kind: &kind})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}

	for i := 0; i < 5; i++ {
		if _, err := s.AddObservation(AddObservationParams{
			SessionID: "s1", Type: "manual", Title: fmt.Sprintf("pond filter note %d", i),
			Content: "the pond filter clogged", Project: "koi-garden",
		}); err != nil {
			t.Fatalf("AddObservation %d: %v", i, err)
		}
	}
	linked, err := s.AddObservationLinked(
		AddObservationParams{SessionID: "s1", Type: "bugfix", Title: "pond filter root cause", Content: "the pond filter clogged on silt", Project: "koi-garden"},
		&ObservationLink{Task: &task.Task, Role: "root_cause", GraphRef: "node:filter", GraphCommit: "0123456789abcdef0123456789abcdef01234567"},
	)
	if err != nil {
		t.Fatalf("AddObservationLinked: %v", err)
	}

	t.Run("total counts past the page", func(t *testing.T) {
		page, err := s.SearchPaged("pond filter", SearchOptions{Project: "koi-garden", Limit: 2})
		if err != nil {
			t.Fatalf("SearchPaged: %v", err)
		}
		if page.Total != 6 {
			t.Fatalf("total = %d, want 6", page.Total)
		}
		if len(page.Results) != 2 {
			t.Fatalf("page size = %d, want 2", len(page.Results))
		}
	})

	t.Run("offset pages without overlap", func(t *testing.T) {
		first, err := s.SearchPaged("pond filter", SearchOptions{Project: "koi-garden", Limit: 3})
		if err != nil {
			t.Fatalf("SearchPaged: %v", err)
		}
		second, err := s.SearchPaged("pond filter", SearchOptions{Project: "koi-garden", Limit: 3, Offset: 3})
		if err != nil {
			t.Fatalf("SearchPaged offset: %v", err)
		}
		if second.Offset != 3 {
			t.Fatalf("offset = %d, want 3", second.Offset)
		}
		seen := map[int64]bool{}
		for _, r := range first.Results {
			seen[r.ID] = true
		}
		for _, r := range second.Results {
			if seen[r.ID] {
				t.Fatalf("observation %d appears on both pages", r.ID)
			}
		}
	})

	t.Run("task keeps only the linked observation", func(t *testing.T) {
		page, err := s.SearchPaged("pond filter", SearchOptions{Project: "koi-garden", TaskSyncID: task.Task.SyncID})
		if err != nil {
			t.Fatalf("SearchPaged: %v", err)
		}
		if page.Total != 1 || len(page.Results) != 1 || page.Results[0].ID != linked.ObservationID {
			t.Fatalf("expected only the linked observation, got total=%d results=%d", page.Total, len(page.Results))
		}
	})

	t.Run("graph_ref keeps only the stamped observation", func(t *testing.T) {
		page, err := s.SearchPaged("pond filter", SearchOptions{Project: "koi-garden", GraphRef: "node:filter"})
		if err != nil {
			t.Fatalf("SearchPaged: %v", err)
		}
		if page.Total != 1 || len(page.Results) != 1 || page.Results[0].ID != linked.ObservationID {
			t.Fatalf("expected only the stamped observation, got total=%d results=%d", page.Total, len(page.Results))
		}
		empty, err := s.SearchPaged("pond filter", SearchOptions{Project: "koi-garden", GraphRef: "node:missing"})
		if err != nil {
			t.Fatalf("SearchPaged unknown ref: %v", err)
		}
		if empty.Total != 0 {
			t.Fatalf("an unknown graph ref must match nothing, got %d", empty.Total)
		}
	})

	t.Run("since and until bound the range", func(t *testing.T) {
		if _, err := s.db.Exec(`UPDATE observations SET created_at = '2020-01-01 00:00:00' WHERE id = ?`, linked.ObservationID); err != nil {
			t.Fatalf("backdate: %v", err)
		}
		old, err := s.SearchPaged("pond filter", SearchOptions{Project: "koi-garden", Until: "2020-06-01"})
		if err != nil {
			t.Fatalf("SearchPaged until: %v", err)
		}
		if old.Total != 1 || old.Results[0].ID != linked.ObservationID {
			t.Fatalf("until must keep only the backdated observation, got total=%d", old.Total)
		}
		recent, err := s.SearchPaged("pond filter", SearchOptions{Project: "koi-garden", Since: "2020-06-01"})
		if err != nil {
			t.Fatalf("SearchPaged since: %v", err)
		}
		if recent.Total != 5 {
			t.Fatalf("since must drop the backdated observation, got total=%d", recent.Total)
		}
	})

	t.Run("projects widens the scope to a subtree", func(t *testing.T) {
		if err := s.CreateSession("s2", "koi-garden-pond-01", ""); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		if _, err := s.AddObservation(AddObservationParams{
			SessionID: "s2", Type: "manual", Title: "child pond filter note",
			Content: "the pond filter clogged downstream", Project: "koi-garden-pond-01",
		}); err != nil {
			t.Fatalf("AddObservation child: %v", err)
		}
		parentOnly, err := s.SearchPaged("pond filter", SearchOptions{Project: "koi-garden"})
		if err != nil {
			t.Fatalf("SearchPaged parent: %v", err)
		}
		subtree, err := s.SearchPaged("pond filter", SearchOptions{Projects: []string{"koi-garden", "koi-garden-pond-01"}})
		if err != nil {
			t.Fatalf("SearchPaged subtree: %v", err)
		}
		if subtree.Total != parentOnly.Total+1 {
			t.Fatalf("subtree total = %d, parent total = %d", subtree.Total, parentOnly.Total)
		}
	})
}
