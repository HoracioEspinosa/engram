package data

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()

	cfg, err := store.DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig: %v", err)
	}
	cfg.DataDir = t.TempDir()

	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seed(t *testing.T, s *store.Store) (sessionID string, obsID, secondObs int64) {
	t.Helper()

	sessionID = "session-1"
	if err := s.CreateSession(sessionID, "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := s.CreateSession("session-empty", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create empty session: %v", err)
	}

	var err error
	obsID, err = s.AddObservation(store.AddObservationParams{
		SessionID: sessionID,
		Type:      "bugfix",
		Title:     "Needle observation",
		Content:   "needle content for deterministic search",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add first observation: %v", err)
	}

	secondObs, err = s.AddObservation(store.AddObservationParams{
		SessionID: sessionID,
		Type:      "decision",
		Title:     "Second observation",
		Content:   "timeline sibling",
		Project:   "engram",
		Scope:     "project",
	})
	if err != nil {
		t.Fatalf("add second observation: %v", err)
	}
	return sessionID, obsID, secondObs
}

func TestSQLiteMemoryReaderCoversTheContract(t *testing.T) {
	s := newTestStore(t)
	sessionID, obsID, secondObs := seed(t, s)
	r := NewMemoryReader(s)

	stats, err := r.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats == nil || stats.TotalSessions < 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	results, err := r.Search("needle", store.SearchOptions{Limit: 50})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one search result")
	}

	recent, err := r.RecentObservations(50)
	if err != nil {
		t.Fatalf("RecentObservations: %v", err)
	}
	if len(recent) < 2 {
		t.Fatalf("recent observations = %d, want >= 2", len(recent))
	}

	obs, err := r.Observation(obsID)
	if err != nil {
		t.Fatalf("Observation: %v", err)
	}
	if obs == nil || obs.ID != obsID {
		t.Fatalf("unexpected observation: %+v", obs)
	}

	tl, err := r.Timeline(secondObs, 10, 10)
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	if tl == nil || tl.Focus.ID != secondObs {
		t.Fatalf("unexpected timeline focus: %+v", tl)
	}

	sessions, err := r.RecentSessions(50)
	if err != nil {
		t.Fatalf("RecentSessions: %v", err)
	}
	if len(sessions) < 2 {
		t.Fatalf("sessions = %d, want >= 2", len(sessions))
	}

	sessionObs, err := r.SessionObservations(sessionID, 200)
	if err != nil {
		t.Fatalf("SessionObservations: %v", err)
	}
	if len(sessionObs) < 2 {
		t.Fatalf("session observations = %d, want >= 2", len(sessionObs))
	}
}

func TestSQLiteRecentObservationsHonoursTheLimit(t *testing.T) {
	s := newTestStore(t)
	seed(t, s)

	recent, err := NewMemoryReader(s).RecentObservations(1)
	if err != nil {
		t.Fatalf("RecentObservations: %v", err)
	}
	if len(recent) != 1 {
		t.Fatalf("recent observations = %d, want 1", len(recent))
	}
}

func TestSQLiteDeleteSessionRefusesASessionWithObservations(t *testing.T) {
	s := newTestStore(t)
	sessionID, _, _ := seed(t, s)

	err := NewMemoryReader(s).DeleteSession(sessionID)
	if !errors.Is(err, store.ErrSessionHasObservations) {
		t.Fatalf("DeleteSession error = %v, want ErrSessionHasObservations", err)
	}
}

func TestSQLiteDeleteSessionRemovesAnEmptySession(t *testing.T) {
	s := newTestStore(t)
	seed(t, s)

	if err := NewMemoryReader(s).DeleteSession("session-empty"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
}

func TestSQLiteWithoutAStoreReportsItRatherThanDeleting(t *testing.T) {
	err := NewMemoryReader(nil).DeleteSession("session-1")
	if !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("DeleteSession error = %v, want ErrStoreUnavailable", err)
	}
}

func TestFakeMemoryReturnsWhatItWasGiven(t *testing.T) {
	obs := store.Observation{ID: 7, Title: "seeded"}
	f := &FakeMemory{
		StatsResult:     &store.Stats{TotalSessions: 3},
		SearchResults:   []store.SearchResult{{Observation: obs}},
		Observations:    []store.Observation{obs, {ID: 8}},
		ObservationByID: map[int64]*store.Observation{7: &obs},
		TimelineResult:  &store.TimelineResult{Focus: obs},
		Sessions:        []store.SessionSummary{{ID: "s1"}, {ID: "s2"}},
		SessionObs:      map[string][]store.Observation{"s1": {obs}},
	}

	if stats, _ := f.Stats(); stats.TotalSessions != 3 {
		t.Fatalf("stats = %+v", stats)
	}
	if results, _ := f.Search("needle", store.SearchOptions{}); len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if len(f.Queries) != 1 || f.Queries[0] != "needle" {
		t.Fatalf("recorded queries = %v", f.Queries)
	}
	if recent, _ := f.RecentObservations(1); len(recent) != 1 {
		t.Fatalf("recent = %d, want the limit to apply", len(recent))
	}
	if got, _ := f.Observation(7); got == nil || got.ID != 7 {
		t.Fatalf("observation = %+v", got)
	}
	if tl, _ := f.Timeline(7, 10, 10); tl == nil || tl.Focus.ID != 7 {
		t.Fatalf("timeline = %+v", tl)
	}
	if sessions, _ := f.RecentSessions(1); len(sessions) != 1 {
		t.Fatalf("sessions = %d, want the limit to apply", len(sessions))
	}
	if sessionObs, _ := f.SessionObservations("s1", 200); len(sessionObs) != 1 {
		t.Fatalf("session observations = %d, want 1", len(sessionObs))
	}
	if err := f.DeleteSession("s2"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if len(f.DeletedSessions) != 1 || f.DeletedSessions[0] != "s2" {
		t.Fatalf("recorded deletes = %v", f.DeletedSessions)
	}
}

func strp(v string) *string { return &v }
func boolp(v bool) *bool    { return &v }

// seedProject populates slug with one active task, one evidence file attached
// to it, and one runbook flagged for review — one row in each table the
// Selector (S1) and Dashboard (S2) read counters from (rfc-tui.md §3.1),
// so TestSQLiteProjectReaderCoversTheContract exercises every real query
// those screens depend on, not just the ones with an existing store-level
// test.
func seedProject(t *testing.T, s *store.Store, slug string) store.Task {
	t.Helper()

	if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: slug}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}
	taskResult, err := s.UpsertTask(store.UpsertTaskParams{
		Project: slug, JiraKey: strp("ACME-1"), Title: strp("Fix the thing"), Kind: strp("bugfix"),
	})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	if _, _, _, err := s.AddEvidence(store.AddEvidenceParams{
		Task: taskResult.Task, Path: "evidence.png",
		SHA256: strings.Repeat("a", 64), Kind: "png", Proves: "it works",
	}); err != nil {
		t.Fatalf("AddEvidence: %v", err)
	}
	if _, err := s.SyncRunbookIndex(store.RunbookIndexSyncParams{
		Source: "knowledge-mcp",
		Entries: []store.RunbookIndexEntryInput{
			{
				ID: "RB-900", VaultPath: "Runbooks/RB-900.md", Title: "Stale runbook",
				Service: slug, Category: "performance", Status: "verified", NeedsReview: boolp(true),
			},
		},
	}); err != nil {
		t.Fatalf("SyncRunbookIndex: %v", err)
	}
	return taskResult.Task
}

// TestSQLiteProjectReaderCoversTheContract is the ProjectReader counterpart
// of TestSQLiteMemoryReaderCoversTheContract: it drives NewProjectReader
// against a real store instead of data.FakeProject, which is what every
// Selector/Dashboard Update test uses. A fake proves the screen reacts
// correctly to a given count; only this test proves the count itself is
// real, read from project_cards/tasks/evidence/runbook_index rather than
// asserted by the test.
func TestSQLiteProjectReaderCoversTheContract(t *testing.T) {
	s := newTestStore(t)
	const slug = "acme"
	seedProject(t, s, slug)

	r := NewProjectReader(s)

	cards, err := r.ListCards()
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	var found *store.ProjectCardListItem
	for i := range cards {
		if cards[i].Slug == slug {
			found = &cards[i]
		}
	}
	if found == nil {
		t.Fatalf("ListCards missing %q, got %+v", slug, cards)
	}
	if found.Counts == nil {
		t.Fatal("ListCards should include real counters, not just the card")
	}

	card, err := r.Card(slug)
	if err != nil {
		t.Fatalf("Card: %v", err)
	}
	if card.Slug != slug {
		t.Fatalf("card = %+v", card)
	}

	health, err := r.Health(slug)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if health.Observations != 0 || health.TasksActive != 1 || health.Evidence != 1 || health.RunbooksStale != 1 {
		t.Fatalf("health = %+v, want 0 observations, 1 active task, 1 evidence file, 1 stale runbook", health)
	}

	tasks, err := r.RecentTasks(slug, 10)
	if err != nil {
		t.Fatalf("RecentTasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].JiraKey == nil || *tasks[0].JiraKey != "ACME-1" {
		t.Fatalf("tasks = %+v, want the seeded ACME-1", tasks)
	}

	stale, err := r.StaleRunbooks(slug, 10)
	if err != nil {
		t.Fatalf("StaleRunbooks: %v", err)
	}
	if len(stale) != 1 || stale[0].ID != "RB-900" {
		t.Fatalf("stale runbooks = %+v, want the seeded RB-900", stale)
	}

	evidence, err := r.LatestEvidence(slug, 10)
	if err != nil {
		t.Fatalf("LatestEvidence: %v", err)
	}
	if len(evidence) != 1 || evidence[0].Path != "evidence.png" {
		t.Fatalf("evidence = %+v, want the seeded evidence.png", evidence)
	}
}

func TestSQLiteProjectReaderRecentTasksHonoursTheLimit(t *testing.T) {
	s := newTestStore(t)
	const slug = "acme"
	if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: slug}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.UpsertTask(store.UpsertTaskParams{
			Project: slug, JiraKey: strp(fmt.Sprintf("ACME-%d", i+1)), Title: strp("t"), Kind: strp("bugfix"),
		}); err != nil {
			t.Fatalf("UpsertTask %d: %v", i, err)
		}
	}

	tasks, err := NewProjectReader(s).RecentTasks(slug, 2)
	if err != nil {
		t.Fatalf("RecentTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks = %d, want the limit of 2 honoured", len(tasks))
	}
}

func TestSQLiteProjectReaderWithoutAStoreReportsIt(t *testing.T) {
	r := NewProjectReader(nil)

	if _, err := r.ListCards(); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("ListCards error = %v, want ErrStoreUnavailable", err)
	}
	if _, err := r.Card("acme"); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("Card error = %v, want ErrStoreUnavailable", err)
	}
	if _, err := r.Health("acme"); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("Health error = %v, want ErrStoreUnavailable", err)
	}
	if _, err := r.RecentTasks("acme", 10); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("RecentTasks error = %v, want ErrStoreUnavailable", err)
	}
	if _, err := r.StaleRunbooks("acme", 10); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("StaleRunbooks error = %v, want ErrStoreUnavailable", err)
	}
	if _, err := r.LatestEvidence("acme", 10); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("LatestEvidence error = %v, want ErrStoreUnavailable", err)
	}
}

// TestSQLiteTaskReaderCoversTheContract exercises every TaskReader method
// against a real store: the seeded ACME-1 task (seedProject) already carries
// one evidence file, so this only adds the observation link ListTasks and
// Task alone cannot cover, then drives the two writes ADR-028 allows from the
// TUI (UpdateState, LinkObservation) and the context pack (S5).
func TestSQLiteTaskReaderCoversTheContract(t *testing.T) {
	s := newTestStore(t)
	const slug = "acme"
	task := seedProject(t, s, slug)

	if err := s.CreateSession("session-task", slug, "/tmp/acme"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	obsID, err := s.AddObservation(store.AddObservationParams{
		SessionID: "session-task", Type: "discovery", Title: "Root cause",
		Content: "race in writeStream", Project: slug, Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	if _, err := s.LinkTaskObservation(store.LinkTaskObservationParams{
		Task: task, ObservationID: obsID, Role: "root_cause",
	}); err != nil {
		t.Fatalf("LinkTaskObservation (seed): %v", err)
	}

	r := NewTaskReader(s)

	items, err := r.ListTasks(slug, store.TaskListFilter{})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(items) != 1 || items[0].JiraKey == nil || *items[0].JiraKey != "ACME-1" {
		t.Fatalf("ListTasks = %+v, want the seeded ACME-1", items)
	}

	detail, err := r.Task(task.ID)
	if err != nil {
		t.Fatalf("Task: %v", err)
	}
	if detail.Task.ID != task.ID {
		t.Fatalf("Task.Task.ID = %d, want %d", detail.Task.ID, task.ID)
	}
	if len(detail.Observations) != 1 || detail.Observations[0].Observation.ID != obsID {
		t.Fatalf("Observations = %+v, want the one linked in the seed", detail.Observations)
	}
	if len(detail.Evidence) != 1 || detail.Evidence[0].Path != "evidence.png" {
		t.Fatalf("Evidence = %+v, want the seeded evidence.png", detail.Evidence)
	}

	if err := r.UpdateState(task.ID, "review"); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	afterState, err := r.Task(task.ID)
	if err != nil {
		t.Fatalf("Task (after UpdateState): %v", err)
	}
	if afterState.Task.State != "review" {
		t.Fatalf("state = %q, want review", afterState.Task.State)
	}

	obsID2, err := s.AddObservation(store.AddObservationParams{
		SessionID: "session-task", Type: "decision", Title: "Fix rolled out",
		Content: "guard the write behind a mutex", Project: slug, Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation (second): %v", err)
	}
	if err := r.LinkObservation(task.ID, obsID2); err != nil {
		t.Fatalf("LinkObservation: %v", err)
	}
	afterLink, err := r.Task(task.ID)
	if err != nil {
		t.Fatalf("Task (after LinkObservation): %v", err)
	}
	if len(afterLink.Observations) != 2 {
		t.Fatalf("Observations after LinkObservation = %+v, want 2", afterLink.Observations)
	}

	pack, err := r.ContextPack(task.ID)
	if err != nil {
		t.Fatalf("ContextPack: %v", err)
	}
	if !strings.Contains(pack, "ACME-1") {
		t.Fatalf("context pack = %q, want it to mention ACME-1", pack)
	}
}

func TestSQLiteTaskReaderWithoutAStoreReportsIt(t *testing.T) {
	r := NewTaskReader(nil)

	if _, err := r.ListTasks("acme", store.TaskListFilter{}); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("ListTasks error = %v, want ErrStoreUnavailable", err)
	}
	if _, err := r.Task(1); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("Task error = %v, want ErrStoreUnavailable", err)
	}
	if err := r.UpdateState(1, "open"); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("UpdateState error = %v, want ErrStoreUnavailable", err)
	}
	if err := r.LinkObservation(1, 2); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("LinkObservation error = %v, want ErrStoreUnavailable", err)
	}
	if _, err := r.ContextPack(1); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("ContextPack error = %v, want ErrStoreUnavailable", err)
	}
}

func TestFakeMemoryErrShortCircuitsEveryCall(t *testing.T) {
	boom := errors.New("boom")
	f := &FakeMemory{Err: boom}

	if _, err := f.Stats(); !errors.Is(err, boom) {
		t.Errorf("Stats error = %v", err)
	}
	if _, err := f.Search("q", store.SearchOptions{}); !errors.Is(err, boom) {
		t.Errorf("Search error = %v", err)
	}
	if _, err := f.RecentObservations(10); !errors.Is(err, boom) {
		t.Errorf("RecentObservations error = %v", err)
	}
	if _, err := f.Observation(1); !errors.Is(err, boom) {
		t.Errorf("Observation error = %v", err)
	}
	if _, err := f.Timeline(1, 1, 1); !errors.Is(err, boom) {
		t.Errorf("Timeline error = %v", err)
	}
	if _, err := f.RecentSessions(10); !errors.Is(err, boom) {
		t.Errorf("RecentSessions error = %v", err)
	}
	if _, err := f.SessionObservations("s1", 10); !errors.Is(err, boom) {
		t.Errorf("SessionObservations error = %v", err)
	}
	if err := f.DeleteSession("s1"); !errors.Is(err, boom) {
		t.Errorf("DeleteSession error = %v", err)
	}
	if len(f.DeletedSessions) != 0 {
		t.Fatalf("a failing delete must not be recorded, got %v", f.DeletedSessions)
	}
}

// TestSQLiteEvidenceReaderCoversTheContract is the EvidenceReader
// counterpart of TestSQLiteTaskReaderCoversTheContract: it drives
// NewEvidenceReader against a real store instead of data.FakeEvidence, which
// is what every Evidence Update test uses. seedProject's evidence.png is
// enough for the plain list; a second task's evidence pins the task_id
// filter rfc-tui.md §9.2's S6 query needs (`e.task_id = ?2`), the one no
// existing store-level test covered before T-10.04.
func TestSQLiteEvidenceReaderCoversTheContract(t *testing.T) {
	s := newTestStore(t)
	const slug = "acme"
	task := seedProject(t, s, slug)

	other, err := s.UpsertTask(store.UpsertTaskParams{
		Project: slug, JiraKey: strp("ACME-2"), Title: strp("Something else"), Kind: strp("bugfix"),
	})
	if err != nil {
		t.Fatalf("UpsertTask (second): %v", err)
	}
	if _, _, _, err := s.AddEvidence(store.AddEvidenceParams{
		Task: other.Task, Path: "other.png",
		SHA256: strings.Repeat("b", 64), Kind: "png", Proves: "unrelated",
	}); err != nil {
		t.Fatalf("AddEvidence (second task): %v", err)
	}

	r := NewEvidenceReader(s)

	all, err := r.ListEvidence(slug, store.EvidenceListFilter{})
	if err != nil {
		t.Fatalf("ListEvidence: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListEvidence (unfiltered) = %+v, want the seeded evidence.png and other.png", all)
	}

	scoped, err := r.ListEvidence(slug, store.EvidenceListFilter{TaskID: task.ID})
	if err != nil {
		t.Fatalf("ListEvidence (task filter): %v", err)
	}
	if len(scoped) != 1 || scoped[0].Path != "evidence.png" || scoped[0].TaskID != task.ID {
		t.Fatalf("ListEvidence scoped to task %d = %+v, want only evidence.png", task.ID, scoped)
	}
}

func TestSQLiteEvidenceReaderWithoutAStoreReportsIt(t *testing.T) {
	r := NewEvidenceReader(nil)

	if _, err := r.ListEvidence("acme", store.EvidenceListFilter{}); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("ListEvidence error = %v, want ErrStoreUnavailable", err)
	}
}

func TestFakeEvidenceFiltersByTaskIDAndAttached(t *testing.T) {
	attached := store.EvidenceListItem{Evidence: store.Evidence{ID: 1, TaskID: 5, Path: "a.png", AttachedJira: true}}
	pending := store.EvidenceListItem{Evidence: store.Evidence{ID: 2, TaskID: 5, Path: "b.png"}}
	otherTask := store.EvidenceListItem{Evidence: store.Evidence{ID: 3, TaskID: 6, Path: "c.png"}}
	f := &FakeEvidence{ItemsByProject: map[string][]store.EvidenceListItem{
		"acme": {attached, pending, otherTask},
	}}

	all, err := f.ListEvidence("acme", store.EvidenceListFilter{})
	if err != nil {
		t.Fatalf("ListEvidence: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("unfiltered = %+v, want all 3 seeded rows", all)
	}

	byTask, err := f.ListEvidence("acme", store.EvidenceListFilter{TaskID: 5})
	if err != nil {
		t.Fatalf("ListEvidence (task filter): %v", err)
	}
	if len(byTask) != 2 {
		t.Fatalf("task filter = %+v, want the 2 rows under task 5", byTask)
	}
	if f.LastFilter.TaskID != 5 {
		t.Fatalf("LastFilter = %+v, want the task filter just issued recorded", f.LastFilter)
	}

	yes := true
	byAttached, err := f.ListEvidence("acme", store.EvidenceListFilter{AttachedJira: &yes})
	if err != nil {
		t.Fatalf("ListEvidence (attached filter): %v", err)
	}
	if len(byAttached) != 1 || byAttached[0].Path != "a.png" {
		t.Fatalf("attached filter = %+v, want only a.png", byAttached)
	}
}

func TestFakeEvidenceErrShortCircuits(t *testing.T) {
	boom := errors.New("boom")
	f := &FakeEvidence{Err: boom}

	if _, err := f.ListEvidence("acme", store.EvidenceListFilter{}); !errors.Is(err, boom) {
		t.Errorf("ListEvidence error = %v", err)
	}
}
