package data

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/project"
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

// TestSQLiteRunbookReaderCoversTheContract is the RunbookReader counterpart
// of TestSQLiteEvidenceReaderCoversTheContract: it drives NewRunbookReader
// against a real store instead of data.FakeRunbook, which is what every
// Runbooks Update test uses. seedProject already flags RB-900 as stale for
// "acme"; a second project's runbook (unflagged) proves both the "a" (all
// projects) toggle and the plain single-project scope actually filter,
// rather than a fake echoing back whatever the test seeded.
func TestSQLiteRunbookReaderCoversTheContract(t *testing.T) {
	s := newTestStore(t)
	const slug = "acme"
	seedProject(t, s, slug)

	if _, err := s.SyncRunbookIndex(store.RunbookIndexSyncParams{
		Source: "knowledge-mcp",
		Entries: []store.RunbookIndexEntryInput{
			{
				ID: "RB-901", VaultPath: "Runbooks/RB-901.md", Title: "Unrelated middleware issue",
				Service: "middleware", Category: "registration", Status: "verified",
				Symptoms: []string{"BSS state desync between systems"},
			},
		},
	}); err != nil {
		t.Fatalf("SyncRunbookIndex (second project): %v", err)
	}

	r := NewRunbookReader(s)

	scoped, err := r.ListRunbooks(slug, false)
	if err != nil {
		t.Fatalf("ListRunbooks (scoped): %v", err)
	}
	if len(scoped) != 1 || scoped[0].ID != "RB-900" || !scoped[0].Stale {
		t.Fatalf("ListRunbooks(%q, false) = %+v, want only the stale RB-900", slug, scoped)
	}

	all, err := r.ListRunbooks(slug, true)
	if err != nil {
		t.Fatalf("ListRunbooks (all projects): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListRunbooks(%q, true) = %+v, want both RB-900 and RB-901", slug, all)
	}

	found, err := r.SearchRunbooks(slug, false, "stale runbook", 10)
	if err != nil {
		t.Fatalf("SearchRunbooks (scoped): %v", err)
	}
	if len(found) != 1 || found[0].ID != "RB-900" {
		t.Fatalf("SearchRunbooks(%q, false, ...) = %+v, want only RB-900", slug, found)
	}

	crossProject, err := r.SearchRunbooks(slug, true, "desync", 10)
	if err != nil {
		t.Fatalf("SearchRunbooks (all projects): %v", err)
	}
	if len(crossProject) != 1 || crossProject[0].ID != "RB-901" {
		t.Fatalf("SearchRunbooks(%q, true, %q, ...) = %+v, want RB-901 even though it belongs to middleware",
			slug, "desync", crossProject)
	}
}

func TestSQLiteRunbookReaderWithoutAStoreReportsIt(t *testing.T) {
	r := NewRunbookReader(nil)

	if _, err := r.ListRunbooks("acme", false); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("ListRunbooks error = %v, want ErrStoreUnavailable", err)
	}
	if _, err := r.SearchRunbooks("acme", false, "preview", 10); !errors.Is(err, ErrStoreUnavailable) {
		t.Errorf("SearchRunbooks error = %v, want ErrStoreUnavailable", err)
	}
}

func TestFakeRunbookFiltersByProjectAndAllToggle(t *testing.T) {
	acme := store.RunbookIndexRow{ID: "RB-900", Project: "acme", Title: "Stale runbook"}
	other := store.RunbookIndexRow{ID: "RB-901", Project: "middleware", Title: "Unrelated"}
	f := &FakeRunbook{ItemsByProject: map[string][]store.RunbookIndexRow{
		"acme":       {acme},
		"middleware": {other},
	}}

	scoped, err := f.ListRunbooks("acme", false)
	if err != nil {
		t.Fatalf("ListRunbooks: %v", err)
	}
	if len(scoped) != 1 || scoped[0].ID != "RB-900" {
		t.Fatalf("scoped = %+v, want only RB-900", scoped)
	}
	if f.LastListAll {
		t.Fatalf("LastListAll = true, want false to have been recorded")
	}

	all, err := f.ListRunbooks("acme", true)
	if err != nil {
		t.Fatalf("ListRunbooks (all): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("all = %+v, want both rows", all)
	}
	if !f.LastListAll {
		t.Fatalf("LastListAll = false, want true to have been recorded")
	}
}

func TestFakeRunbookSearchMatchesTitleAndSymptoms(t *testing.T) {
	byTitle := store.RunbookIndexRow{ID: "RB-900", Project: "acme", Title: "Preview endpoint slow"}
	bySymptom := store.RunbookIndexRow{ID: "RB-901", Project: "acme", Title: "Unrelated", Symptoms: []string{"returns HTTP 503"}}
	noMatch := store.RunbookIndexRow{ID: "RB-902", Project: "acme", Title: "Nothing in common"}
	f := &FakeRunbook{ItemsByProject: map[string][]store.RunbookIndexRow{
		"acme": {byTitle, bySymptom, noMatch},
	}}

	results, err := f.SearchRunbooks("acme", false, "503", 10)
	if err != nil {
		t.Fatalf("SearchRunbooks: %v", err)
	}
	if len(results) != 1 || results[0].ID != "RB-901" {
		t.Fatalf("results = %+v, want only the symptom match RB-901", results)
	}
	if f.LastSearch.Project != "acme" || f.LastSearch.Query != "503" || f.LastSearch.Limit != 10 {
		t.Fatalf("LastSearch = %+v, want the issued filter recorded", f.LastSearch)
	}
}

func TestFakeRunbookErrShortCircuits(t *testing.T) {
	boom := errors.New("boom")
	f := &FakeRunbook{Err: boom}

	if _, err := f.ListRunbooks("acme", false); !errors.Is(err, boom) {
		t.Errorf("ListRunbooks error = %v", err)
	}
	if _, err := f.SearchRunbooks("acme", false, "q", 10); !errors.Is(err, boom) {
		t.Errorf("SearchRunbooks error = %v", err)
	}
}

// TestFakeProjectReturnsWhatItWasGiven is FakeProject's counterpart of
// TestFakeMemoryReturnsWhatItWasGiven: nothing exercised FakeProject's own
// methods before this task (the Selector and Dashboard Update tests all
// build data.FakeProject directly and read the fields back, never through
// the interface methods themselves), which is why the package's coverage
// left every one of them at 0%.
func TestFakeProjectReturnsWhatItWasGiven(t *testing.T) {
	card := store.ProjectCard{Slug: "acme", DisplayName: "Acme"}
	health := ProjectHealth{ProjectCardCounts: store.ProjectCardCounts{Observations: 3}}
	tasks := []store.TaskListItem{
		{Task: store.Task{ID: 1}}, {Task: store.Task{ID: 2}}, {Task: store.Task{ID: 3}},
	}
	stale := []store.RunbookIndexRow{{ID: "RB-900"}, {ID: "RB-901"}}
	ev := []store.EvidenceListItem{{Evidence: store.Evidence{Path: "a.png"}}}

	f := &FakeProject{
		Cards:             []store.ProjectCardListItem{{ProjectCard: card}},
		CardBySlug:        map[string]store.ProjectCard{"acme": card},
		HealthBySlug:      map[string]ProjectHealth{"acme": health},
		TasksBySlug:       map[string][]store.TaskListItem{"acme": tasks},
		StaleRunbooksSlug: map[string][]store.RunbookIndexRow{"acme": stale},
		EvidenceBySlug:    map[string][]store.EvidenceListItem{"acme": ev},
	}

	cards, err := f.ListCards()
	if err != nil || len(cards) != 1 || cards[0].Slug != "acme" {
		t.Fatalf("ListCards = %+v, err %v", cards, err)
	}

	gotCard, err := f.Card("acme")
	if err != nil || gotCard.Slug != "acme" {
		t.Fatalf("Card = %+v, err %v", gotCard, err)
	}
	if _, err := f.Card("missing"); err == nil {
		t.Fatal("Card for an unseeded slug should report an error, not the zero card")
	}

	gotHealth, err := f.Health("acme")
	if err != nil || gotHealth.Observations != 3 {
		t.Fatalf("Health = %+v, err %v", gotHealth, err)
	}
	// A project with no seeded counters is not a missing project (readers.go's
	// own doc comment): the zero ProjectHealth, not an error.
	if h, err := f.Health("missing"); err != nil || h != (ProjectHealth{}) {
		t.Fatalf("Health for an unseeded slug = %+v, err %v, want the zero value and no error", h, err)
	}

	limited, err := f.RecentTasks("acme", 2)
	if err != nil || len(limited) != 2 {
		t.Fatalf("RecentTasks(limit=2) = %+v, err %v", limited, err)
	}
	unlimited, err := f.RecentTasks("acme", 0)
	if err != nil || len(unlimited) != 3 {
		t.Fatalf("RecentTasks(limit=0) = %+v, err %v, want every seeded row", unlimited, err)
	}

	staleLimited, err := f.StaleRunbooks("acme", 1)
	if err != nil || len(staleLimited) != 1 || staleLimited[0].ID != "RB-900" {
		t.Fatalf("StaleRunbooks(limit=1) = %+v, err %v", staleLimited, err)
	}

	evGot, err := f.LatestEvidence("acme", 10)
	if err != nil || len(evGot) != 1 || evGot[0].Path != "a.png" {
		t.Fatalf("LatestEvidence = %+v, err %v", evGot, err)
	}
}

func TestFakeProjectErrShortCircuits(t *testing.T) {
	boom := errors.New("boom")
	f := &FakeProject{Err: boom}

	if _, err := f.ListCards(); !errors.Is(err, boom) {
		t.Errorf("ListCards error = %v", err)
	}
	if _, err := f.Card("acme"); !errors.Is(err, boom) {
		t.Errorf("Card error = %v", err)
	}
	if _, err := f.Health("acme"); !errors.Is(err, boom) {
		t.Errorf("Health error = %v", err)
	}
	if _, err := f.RecentTasks("acme", 10); !errors.Is(err, boom) {
		t.Errorf("RecentTasks error = %v", err)
	}
	if _, err := f.StaleRunbooks("acme", 10); !errors.Is(err, boom) {
		t.Errorf("StaleRunbooks error = %v", err)
	}
	if _, err := f.LatestEvidence("acme", 10); !errors.Is(err, boom) {
		t.Errorf("LatestEvidence error = %v", err)
	}
}

// TestFakeTaskReturnsWhatItWasGiven is FakeTask's counterpart of
// TestFakeProjectReturnsWhatItWasGiven, covering the pagination default
// ListTasks documents (an unset limit still caps the page at 20, mirroring
// store.ListTasks) and its query filter, neither of which any tabs/tasks
// test drives through the interface method itself.
func TestFakeTaskReturnsWhatItWasGiven(t *testing.T) {
	items := make([]store.TaskListItem, 0, 25)
	for i := 0; i < 25; i++ {
		title := "other task"
		if i == 7 {
			title = "needle task"
		}
		items = append(items, store.TaskListItem{Task: store.Task{ID: int64(i), Title: title}})
	}
	f := &FakeTask{
		ItemsByProject:  map[string][]store.TaskListItem{"acme": items},
		DetailByID:      map[int64]TaskDetail{1: {Task: store.Task{ID: 1, Title: "one"}}},
		ContextPackByID: map[int64]string{1: "# pack"},
	}

	defaultPage, err := f.ListTasks("acme", store.TaskListFilter{})
	if err != nil || len(defaultPage) != 20 {
		t.Fatalf("ListTasks with no limit = %d items, err %v, want the default 20-row page", len(defaultPage), err)
	}

	filtered, err := f.ListTasks("acme", store.TaskListFilter{Query: "needle"})
	if err != nil || len(filtered) != 1 || filtered[0].ID != 7 {
		t.Fatalf("ListTasks(query=needle) = %+v, err %v", filtered, err)
	}
	if f.LastListFilter.Query != "needle" {
		t.Fatalf("LastListFilter = %+v, want the issued query recorded", f.LastListFilter)
	}

	paged, err := f.ListTasks("acme", store.TaskListFilter{Limit: 5, Offset: 22})
	if err != nil || len(paged) != 3 {
		t.Fatalf("ListTasks(limit=5, offset=22) = %d items, err %v, want the 3 remaining rows", len(paged), err)
	}

	detail, err := f.Task(1)
	if err != nil || detail.Task.Title != "one" {
		t.Fatalf("Task(1) = %+v, err %v", detail, err)
	}
	if _, err := f.Task(999); err == nil {
		t.Fatal("Task for an unseeded id should report an error")
	}

	if err := f.UpdateState(1, "review"); err != nil {
		t.Fatalf("UpdateState: %v", err)
	}
	if len(f.UpdateStateCalls) != 1 || f.UpdateStateCalls[0].State != "review" {
		t.Fatalf("UpdateStateCalls = %+v, want the call recorded", f.UpdateStateCalls)
	}

	if err := f.LinkObservation(1, 42); err != nil {
		t.Fatalf("LinkObservation: %v", err)
	}
	if len(f.LinkCalls) != 1 || f.LinkCalls[0].ObservationID != 42 {
		t.Fatalf("LinkCalls = %+v, want the call recorded", f.LinkCalls)
	}

	pack, err := f.ContextPack(1)
	if err != nil || pack != "# pack" {
		t.Fatalf("ContextPack(1) = %q, err %v", pack, err)
	}
	if pack, err := f.ContextPack(999); err != nil || pack != "" {
		t.Fatalf("ContextPack for an unseeded id = %q, err %v, want the empty string and no error", pack, err)
	}
}

func TestFakeTaskErrShortCircuitsButStillRecordsTheAttemptedWrite(t *testing.T) {
	boom := errors.New("boom")
	f := &FakeTask{Err: boom}

	if _, err := f.ListTasks("acme", store.TaskListFilter{}); !errors.Is(err, boom) {
		t.Errorf("ListTasks error = %v", err)
	}
	if _, err := f.Task(1); !errors.Is(err, boom) {
		t.Errorf("Task error = %v", err)
	}
	if _, err := f.ContextPack(1); !errors.Is(err, boom) {
		t.Errorf("ContextPack error = %v", err)
	}

	// UpdateState and LinkObservation record the attempted write before
	// returning Err, the same convention FakeMemory.DeleteSession follows in
	// reverse (it records only on success): a test asserting on "what did the
	// tab try to write" must see the attempt even when the store call it
	// stands in for was going to fail.
	if err := f.UpdateState(1, "review"); !errors.Is(err, boom) {
		t.Errorf("UpdateState error = %v", err)
	}
	if len(f.UpdateStateCalls) != 1 {
		t.Fatalf("UpdateStateCalls = %+v, want the attempt recorded even on failure", f.UpdateStateCalls)
	}
	if err := f.LinkObservation(1, 2); !errors.Is(err, boom) {
		t.Errorf("LinkObservation error = %v", err)
	}
	if len(f.LinkCalls) != 1 {
		t.Fatalf("LinkCalls = %+v, want the attempt recorded even on failure", f.LinkCalls)
	}
}

// TestTaskKeyFollowsJiraThenSDDThenSyncID pins data.TaskKey's delegation to
// project.TaskKey: 0% coverage here was not a missing branch, only a
// function nothing in this package's own tests called directly (every tab
// test imports and asserts on project.TaskKey's behaviour indirectly through
// rendered view text instead).
func TestTaskKeyFollowsJiraThenSDDThenSyncID(t *testing.T) {
	jiraKey := "ACME-9"
	if got := TaskKey(store.Task{JiraKey: &jiraKey, SyncID: "sync-1"}); got != "ACME-9" {
		t.Errorf("TaskKey (jira set) = %q, want ACME-9", got)
	}

	sdd := "add-thing"
	if got := TaskKey(store.Task{SDDChange: &sdd, SyncID: "sync-1"}); got != "add-thing" {
		t.Errorf("TaskKey (no jira, sdd set) = %q, want add-thing", got)
	}

	if got := TaskKey(store.Task{SyncID: "sync-1"}); got != "sync-1" {
		t.Errorf("TaskKey (neither jira nor sdd) = %q, want sync-1", got)
	}
}

// TestJiraURLBuildsTheBrowseLink pins data.JiraURL's delegation to
// project.JiraBaseURL(): jiraBaseURL itself is resolved once at package init
// from ENGRAM_JIRA_BASE_URL (see internal/project/contextpack.go), so a test
// cannot t.Setenv it into a different value here — asserting the
// concatenation against the real base this process resolved is still a real
// assertion, not a call with nothing checked.
func TestJiraURLBuildsTheBrowseLink(t *testing.T) {
	got := JiraURL("ACME-1")
	want := project.JiraBaseURL() + "ACME-1"
	if got != want {
		t.Fatalf("JiraURL(%q) = %q, want %q", "ACME-1", got, want)
	}
}

// TestSQLiteProjectReaderHealthReportsAClosedStore, ...Task... and
// ...LinkObservation... close the store before calling, which fails every
// query the sqlite adapter issues. That reaches each function's FIRST error
// branch (ProjectCardCounts in Health, GetTask in Task, GetTask in
// LinkObservation and ContextPack). It deliberately does not reach the
// SECOND error branch each of those functions also has (ProjectSyncSummary
// failing after ProjectCardCounts already succeeded, TaskObservationsForTask
// or ListEvidence failing after GetTask already succeeded, LinkTaskObservation
// failing after GetTask already succeeded): forcing only the second query in
// a chain to fail would need a store double narrower than *store.Store, and
// this task did not build one for four branches that already sit well clear
// of the package's 80% target.
func TestSQLiteProjectReaderHealthReportsAClosedStore(t *testing.T) {
	s := newTestStore(t)
	if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: "acme"}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := NewProjectReader(s).Health("acme"); err == nil {
		t.Fatal("Health against a closed store should report an error")
	}
}

func TestSQLiteTaskReaderTaskAndLinkObservationReportAClosedStore(t *testing.T) {
	s := newTestStore(t)
	const slug = "acme"
	task := seedProject(t, s, slug)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	r := NewTaskReader(s)
	if _, err := r.Task(task.ID); err == nil {
		t.Fatal("Task against a closed store should report an error")
	}
	if err := r.LinkObservation(task.ID, 1); err == nil {
		t.Fatal("LinkObservation against a closed store should report an error")
	}
	if _, err := r.ContextPack(task.ID); err == nil {
		t.Fatal("ContextPack against a closed store should report an error")
	}
}
