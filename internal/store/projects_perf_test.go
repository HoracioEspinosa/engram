package store

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// countingReadHooks records every statement the store sends through the read
// seams and then runs it unchanged, so a test can count what a listing costs in
// round trips without changing what the listing returns.
func countingReadHooks(s *Store, statements *[]string) {
	hooks := defaultStoreHooks()
	innerQuery := hooks.query
	innerQueryRow := hooks.queryRow
	hooks.query = func(db queryer, query string, args ...any) (*sql.Rows, error) {
		*statements = append(*statements, query)
		return innerQuery(db, query, args...)
	}
	hooks.queryRow = func(db rowQueryer, query string, args ...any) *sql.Row {
		*statements = append(*statements, query)
		return innerQueryRow(db, query, args...)
	}
	s.hooks = hooks
}

// seedCountedTasks fills project with n tasks, giving task i exactly i linked
// observations and i evidence rows, so a counter that reads the wrong row is
// visible as a wrong number rather than as a coincidence.
func seedCountedTasks(t *testing.T, s *Store, project string, n int) []Task {
	t.Helper()
	if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{Slug: project}); err != nil {
		t.Fatalf("UpsertProjectCard(%q): %v", project, err)
	}
	sessionID := "session-" + project
	if err := s.CreateSession(sessionID, project, ""); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	out := make([]Task, 0, n)
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("PERF-%d", i)
		title := fmt.Sprintf("counted task %d", i)
		kind := "bugfix"
		res, err := s.UpsertTask(UpsertTaskParams{
			Project: project, JiraKey: &key, Title: &title, Kind: &kind,
		})
		if err != nil {
			t.Fatalf("UpsertTask(%q): %v", key, err)
		}
		for o := 0; o < i; o++ {
			obsID, err := s.AddObservation(AddObservationParams{
				SessionID: sessionID,
				Type:      "discovery",
				Project:   project,
				Title:     fmt.Sprintf("%s note %d", key, o),
				Content:   fmt.Sprintf("content for %s note %d", key, o),
			})
			if err != nil {
				t.Fatalf("AddObservation: %v", err)
			}
			if _, err := s.LinkTaskObservation(LinkTaskObservationParams{
				Task: res.Task, ObservationID: obsID,
			}); err != nil {
				t.Fatalf("LinkTaskObservation: %v", err)
			}
		}
		for e := 0; e < i; e++ {
			if _, _, _, err := s.AddEvidence(AddEvidenceParams{
				Task:   res.Task,
				Path:   fmt.Sprintf("%s/%s/evidences/shot-%d.png", project, key, e),
				SHA256: fmt.Sprintf("%064x", i*1000+e+1),
				Kind:   "png",
				Proves: fmt.Sprintf("%s capture %d", key, e),
			}); err != nil {
				t.Fatalf("AddEvidence: %v", err)
			}
		}
		out = append(out, res.Task)
	}
	return out
}

func TestListTasksIssuesOneQuery(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	const project = "koi-perf"
	seedCountedTasks(t, s, project, 5)

	var statements []string
	countingReadHooks(s, &statements)

	items, total, err := s.ListTasks(project, TaskListFilter{Limit: 3})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(statements) != 1 {
		t.Fatalf("expected exactly 1 query, got %d:\n%s", len(statements), strings.Join(statements, "\n---\n"))
	}
	if total != 5 {
		t.Fatalf("expected the total to cover the whole filtered set (5), got %d", total)
	}
	if len(items) != 3 {
		t.Fatalf("expected the page to honour the limit (3), got %d", len(items))
	}

	// The seed gives task i exactly i observations and i evidence rows, so a
	// counter that read another task's rows shows up as the wrong number rather
	// than as a plausible one.
	for _, item := range items {
		if item.JiraKey == nil {
			t.Fatalf("expected every seeded task to carry a jira key, got %+v", item)
		}
		var i int
		if _, err := fmt.Sscanf(*item.JiraKey, "PERF-%d", &i); err != nil {
			t.Fatalf("unexpected jira key %q: %v", *item.JiraKey, err)
		}
		if item.Observations != i {
			t.Errorf("%s: expected %d observations, got %d", *item.JiraKey, i, item.Observations)
		}
		if item.Evidence != i {
			t.Errorf("%s: expected %d evidence rows, got %d", *item.JiraKey, i, item.Evidence)
		}
		if item.Title != fmt.Sprintf("counted task %d", i) {
			t.Errorf("%s: expected the full task projection, got title %q", *item.JiraKey, item.Title)
		}
		if !item.StateStale {
			t.Errorf("%s: expected state_stale=true when state_synced_at was never set", *item.JiraKey)
		}
	}
}

func TestListTasksPageReportsTotalAndOffset(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	const project = "koi-page"
	seedCountedTasks(t, s, project, 5)

	page, err := s.ListTasksPage(project, TaskListFilter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListTasksPage: %v", err)
	}
	if page.Total != 5 {
		t.Errorf("expected total 5, got %d", page.Total)
	}
	if page.Limit != 2 || page.Offset != 2 {
		t.Errorf("expected limit=2 offset=2, got limit=%d offset=%d", page.Limit, page.Offset)
	}
	if len(page.Items) != 2 {
		t.Fatalf("expected 2 items on the page, got %d", len(page.Items))
	}

	// A page past the end still reports how many rows the filter matched:
	// a pager that is told zero has no way back.
	beyond, err := s.ListTasksPage(project, TaskListFilter{Limit: 2, Offset: 99})
	if err != nil {
		t.Fatalf("ListTasksPage(offset=99): %v", err)
	}
	if beyond.Total != 5 {
		t.Errorf("expected total 5 past the end, got %d", beyond.Total)
	}
	if len(beyond.Items) != 0 {
		t.Errorf("expected no items past the end, got %d", len(beyond.Items))
	}

	// The default limit is the one ListTasks already applied.
	def, err := s.ListTasksPage(project, TaskListFilter{})
	if err != nil {
		t.Fatalf("ListTasksPage(default): %v", err)
	}
	if def.Limit != defaultTaskListLimit {
		t.Errorf("expected the default limit %d, got %d", defaultTaskListLimit, def.Limit)
	}
}
