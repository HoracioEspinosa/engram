package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	if len(f.Queries()) != 1 || f.Queries()[0] != "needle" {
		t.Fatalf("recorded queries = %v", f.Queries())
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
	if len(f.DeletedSessions()) != 1 || f.DeletedSessions()[0] != "s2" {
		t.Fatalf("recorded deletes = %v", f.DeletedSessions())
	}
}

func strp(v string) *string { return &v }
func boolp(v bool) *bool    { return &v }

// seedProject populates slug with one active task, one evidence file attached
// to it, and one runbook flagged for review — one row in each table the
// project tree and the Home tab read counters from, so
// TestSQLiteProjectReaderCoversTheContract exercises every real query those
// screens depend on, not just the ones with an existing store-level test.
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
// Task alone cannot cover, then drives the only two writes the TUI makes
// (UpdateState, LinkObservation) and the context pack.
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
	if len(f.DeletedSessions()) != 0 {
		t.Fatalf("a failing delete must not be recorded, got %v", f.DeletedSessions())
	}
}

// TestSQLiteEvidenceReaderCoversTheContract is the EvidenceReader
// counterpart of TestSQLiteTaskReaderCoversTheContract: it drives
// NewEvidenceReader against a real store instead of data.FakeEvidence, which
// is what every Evidence Update test uses. seedProject's evidence.png is
// enough for the plain list; a second task's evidence pins the task_id
// filter the Evidence list's query needs (`e.task_id = ?2`), which no
// store-level test covers.
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
	if f.LastFilter().TaskID != 5 {
		t.Fatalf("LastFilter = %+v, want the task filter just issued recorded", f.LastFilter())
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
	if f.LastListAll() {
		t.Fatalf("LastListAll = true, want false to have been recorded")
	}

	all, err := f.ListRunbooks("acme", true)
	if err != nil {
		t.Fatalf("ListRunbooks (all): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("all = %+v, want both rows", all)
	}
	if !f.LastListAll() {
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
	if f.LastSearch().Project != "acme" || f.LastSearch().Query != "503" || f.LastSearch().Limit != 10 {
		t.Fatalf("LastSearch = %+v, want the issued filter recorded", f.LastSearch())
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
// TestFakeMemoryReturnsWhatItWasGiven: nothing else exercises FakeProject's
// own methods (the project tree and Home Update tests all build
// data.FakeProject directly and read the fields back, never through the
// interface methods themselves), so without this case the package's coverage
// leaves every one of them at 0%.
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
	if f.LastListFilter().Query != "needle" {
		t.Fatalf("LastListFilter = %+v, want the issued query recorded", f.LastListFilter())
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
	if len(f.UpdateStateCalls()) != 1 || f.UpdateStateCalls()[0].State != "review" {
		t.Fatalf("UpdateStateCalls = %+v, want the call recorded", f.UpdateStateCalls())
	}

	if err := f.LinkObservation(1, 42); err != nil {
		t.Fatalf("LinkObservation: %v", err)
	}
	if len(f.LinkCalls()) != 1 || f.LinkCalls()[0].ObservationID != 42 {
		t.Fatalf("LinkCalls = %+v, want the call recorded", f.LinkCalls())
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
	if len(f.UpdateStateCalls()) != 1 {
		t.Fatalf("UpdateStateCalls = %+v, want the attempt recorded even on failure", f.UpdateStateCalls())
	}
	if err := f.LinkObservation(1, 2); !errors.Is(err, boom) {
		t.Errorf("LinkObservation error = %v", err)
	}
	if len(f.LinkCalls()) != 1 {
		t.Fatalf("LinkCalls = %+v, want the attempt recorded even on failure", f.LinkCalls())
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
// a chain to fail would need a store double narrower than *store.Store, which
// is not worth building for four branches that already sit well clear of the
// package's 80% target.
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

// ─── New interfaces: fakes implement them, and pagination is honest ─────────

// TestFakeReadersImplementNewInterfaces is the runtime counterpart of the
// var _ X = (*FakeY)(nil) assertions declared next to each fake in fake.go:
// those already fail the build if a fake drifts from its interface, so this
// test exists to give that contract a name a coverage report and a CI
// failure can point to.
func TestFakeReadersImplementNewInterfaces(t *testing.T) {
	var (
		_ ProjectTreeReader  = (*FakeProjectTree)(nil)
		_ BenchmarkReader    = (*FakeBenchmark)(nil)
		_ GraphReader        = (*FakeGraph)(nil)
		_ GraphSyncer        = (*FakeGraph)(nil)
		_ ThemeReader        = (*FakeTheme)(nil)
		_ ThemeWriter        = (*FakeTheme)(nil)
		_ SettingsReader     = (*FakeSettings)(nil)
		_ SettingsWriter     = (*FakeSettings)(nil)
		_ GlobalSearcher     = (*FakeSearch)(nil)
		_ TaskPageReader     = (*FakeTask)(nil)
		_ EvidencePageReader = (*FakeEvidence)(nil)
		_ RunbookPageReader  = (*FakeRunbook)(nil)
		_ ScopedMemoryReader = (*FakeMemory)(nil)
	)
}

// TestFakePaginatedMethodsReportTotalBeyondThePage seeds more rows than one
// page holds for every new paginated fake method, so Total (the full match
// count) and len(Items) (the page) provably disagree — a fake that quietly
// capped Total at the page size would pass every other test in this file
// and still lie to a pager.
func TestFakePaginatedMethodsReportTotalBeyondThePage(t *testing.T) {
	tasks := make([]store.TaskListItem, 5)
	for i := range tasks {
		tasks[i] = store.TaskListItem{Task: store.Task{ID: int64(i + 1), Project: "acme", Title: fmt.Sprintf("task %d", i)}}
	}
	ft := &FakeTask{ItemsByProject: map[string][]store.TaskListItem{"acme": tasks}}
	tp, err := ft.ListTasksPage("acme", store.TaskListFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListTasksPage: %v", err)
	}
	if tp.Total != 5 || len(tp.Items) != 2 {
		t.Fatalf("ListTasksPage = %+v, want Total=5 len(Items)=2", tp)
	}
	if !tp.HasNext() || tp.HasPrev() {
		t.Fatalf("ListTasksPage paging flags wrong: HasNext=%v HasPrev=%v", tp.HasNext(), tp.HasPrev())
	}

	evidence := make([]store.EvidenceListItem, 4)
	for i := range evidence {
		evidence[i] = store.EvidenceListItem{Evidence: store.Evidence{ID: int64(i + 1), Category: "auth"}}
	}
	fe := &FakeEvidence{ItemsByProject: map[string][]store.EvidenceListItem{"acme": evidence}}
	ep, err := fe.ListEvidencePage("acme", store.EvidenceListFilter{Limit: 1})
	if err != nil {
		t.Fatalf("ListEvidencePage: %v", err)
	}
	if ep.Total != 4 || len(ep.Items) != 1 {
		t.Fatalf("ListEvidencePage = %+v, want Total=4 len(Items)=1", ep)
	}

	runbooks := make([]store.RunbookIndexRow, 3)
	for i := range runbooks {
		runbooks[i] = store.RunbookIndexRow{ID: fmt.Sprintf("RB-%03d", i+1), Project: "acme"}
	}
	fr := &FakeRunbook{ItemsByProject: map[string][]store.RunbookIndexRow{"acme": runbooks}}
	rp, err := fr.ListRunbooksPage("acme", false, RunbookFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListRunbooksPage: %v", err)
	}
	if rp.Total != 3 || len(rp.Items) != 2 {
		t.Fatalf("ListRunbooksPage = %+v, want Total=3 len(Items)=2", rp)
	}

	benches := make([]Benchmark, 6)
	for i := range benches {
		benches[i] = Benchmark{BenchmarkDelta: store.BenchmarkDelta{Benchmark: store.Benchmark{Metric: "p95"}}}
	}
	fb := &FakeBenchmark{ByProject: map[string][]Benchmark{"acme": benches}}
	bp, err := fb.ListBenchmarks("acme", BenchmarkFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListBenchmarks: %v", err)
	}
	if bp.Total != 6 || len(bp.Items) != 2 {
		t.Fatalf("ListBenchmarks = %+v, want Total=6 len(Items)=2", bp)
	}

	results := make([]store.SearchResult, 7)
	for i := range results {
		results[i] = store.SearchResult{Observation: store.Observation{ID: int64(i + 1), Project: strp("acme")}}
	}
	fm := &FakeMemory{SearchResults: results}
	sp, err := fm.SearchScoped("needle", ProjectScope{}, 3, 0)
	if err != nil {
		t.Fatalf("SearchScoped: %v", err)
	}
	if sp.Total != 7 || len(sp.Items) != 3 {
		t.Fatalf("SearchScoped = %+v, want Total=7 len(Items)=3", sp)
	}
}

// countingProjectTreeStore is a projectTreeStore that counts how many of its
// methods sqliteProjectTree.ProjectTree calls, so
// TestProjectTreeReaderIssuesAtMostTwoStoreCalls can assert on the count
// without instrumenting SQL (internal/store's own query hooks are
// unexported).
type countingProjectTreeStore struct {
	calls int
	tree  []store.ProjectTreeNode
}

func (c *countingProjectTreeStore) ProjectTree(root string, includeCounts bool) ([]store.ProjectTreeNode, error) {
	c.calls++
	return c.tree, nil
}
func (c *countingProjectTreeStore) GetProjectCard(slug string) (store.ProjectCard, error) {
	c.calls++
	return store.ProjectCard{Slug: slug}, nil
}
func (c *countingProjectTreeStore) ProjectCardCounts(slug string) (store.ProjectCardCounts, error) {
	c.calls++
	return store.ProjectCardCounts{}, nil
}
func (c *countingProjectTreeStore) ListProjectAliases(slug string) ([]store.ProjectAlias, error) {
	c.calls++
	return nil, nil
}
func (c *countingProjectTreeStore) SubtreeSlugs(root string) ([]string, error) {
	c.calls++
	return nil, nil
}
func (c *countingProjectTreeStore) ResolveProjectSlug(raw string) (store.ProjectResolution, error) {
	c.calls++
	return store.ProjectResolution{}, nil
}

func TestProjectTreeReaderIssuesAtMostTwoStoreCalls(t *testing.T) {
	backing := &countingProjectTreeStore{
		tree: []store.ProjectTreeNode{
			{ProjectCard: store.ProjectCard{Slug: "a"}, Counts: &store.ProjectCardCounts{}},
			{ProjectCard: store.ProjectCard{Slug: "b", ParentSlug: strp("a")}, Counts: &store.ProjectCardCounts{}},
		},
	}
	r := sqliteProjectTree{store: backing}

	nodes, err := r.ProjectTree()
	if err != nil {
		t.Fatalf("ProjectTree: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Slug != "a" || len(nodes[0].Children) != 1 || nodes[0].Children[0].Slug != "b" {
		t.Fatalf("ProjectTree forest = %+v, want a with b nested under it", nodes)
	}
	if backing.calls == 0 {
		t.Fatal("ProjectTree issued no store calls at all — the test isn't exercising anything")
	}
	if backing.calls > 2 {
		t.Fatalf("ProjectTree issued %d store calls, want at most 2", backing.calls)
	}
}

// ─── Project tree, real store ────────────────────────────────────────────────

func TestSQLiteProjectTreeReaderRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: "koi-garden"}); err != nil {
		t.Fatalf("UpsertProjectCard(koi-garden): %v", err)
	}
	if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: "koi-garden-pond-02"}); err != nil {
		t.Fatalf("UpsertProjectCard(koi-garden-pond-02): %v", err)
	}
	if err := s.SetProjectParent("koi-garden-pond-02", strp("koi-garden")); err != nil {
		t.Fatalf("SetProjectParent: %v", err)
	}
	if err := s.UpsertProjectAlias("koi_garden", "koi-garden", "manual"); err != nil {
		t.Fatalf("UpsertProjectAlias: %v", err)
	}

	r := NewProjectTreeReader(s)

	tree, err := r.ProjectTree()
	if err != nil {
		t.Fatalf("ProjectTree: %v", err)
	}
	var root *ProjectNode
	for i := range tree {
		if tree[i].Slug == "koi-garden" {
			root = &tree[i]
		}
	}
	if root == nil {
		t.Fatalf("koi-garden missing from the forest: %+v", tree)
	}
	if len(root.Children) != 1 || root.Children[0].Slug != "koi-garden-pond-02" {
		t.Fatalf("koi-garden's children = %+v, want koi-garden-pond-02 nested under it", root.Children)
	}
	if root.Children[0].Depth != 1 {
		t.Fatalf("koi-garden-pond-02 depth = %d, want 1", root.Children[0].Depth)
	}

	node, err := r.ProjectNode("koi-garden")
	if err != nil {
		t.Fatalf("ProjectNode: %v", err)
	}
	if len(node.Aliases) != 1 || node.Aliases[0] != "koi_garden" {
		t.Fatalf("ProjectNode aliases = %#v, want [koi_garden]", node.Aliases)
	}

	ancestors, err := r.Ancestors("koi-garden-pond-02")
	if err != nil {
		t.Fatalf("Ancestors: %v", err)
	}
	if len(ancestors) != 1 || ancestors[0].Slug != "koi-garden" {
		t.Fatalf("ancestors = %+v, want [koi-garden]", ancestors)
	}

	descendants, err := r.Descendants("koi-garden")
	if err != nil {
		t.Fatalf("Descendants: %v", err)
	}
	found := false
	for _, d := range descendants {
		if d == "koi-garden-pond-02" {
			found = true
		}
	}
	if !found {
		t.Fatalf("descendants = %v, want koi-garden-pond-02 among them", descendants)
	}

	slug, err := r.ResolveAlias("koi_garden")
	if err != nil {
		t.Fatalf("ResolveAlias: %v", err)
	}
	if slug != "koi-garden" {
		t.Fatalf("ResolveAlias(koi_garden) = %q, want koi-garden", slug)
	}
	if _, err := r.ResolveAlias("no-such-project-anywhere"); !errors.Is(err, ErrProjectUnresolved) {
		t.Fatalf("ResolveAlias(unknown) = %v, want ErrProjectUnresolved", err)
	}
}

// ─── Workspace search, real store ────────────────────────────────────────────

func TestSQLiteGlobalSearcherRoundTrip(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "acme")
	if err := s.CreateSession("acme-session", "acme", "/tmp/acme"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := s.AddObservation(store.AddObservationParams{
		SessionID: "acme-session", Type: "discovery", Title: "Needle finding",
		Content: "needle content for workspace search", Project: "acme", Scope: "project",
	}); err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	hits, err := NewGlobalSearcher(s).SearchWorkspace(SearchQuery{Text: "needle", LimitPerKind: 5})
	if err != nil {
		t.Fatalf("SearchWorkspace: %v", err)
	}
	found := false
	for _, h := range hits {
		if h.Kind == SearchKindObservation && h.Project == "acme" {
			found = true
		}
	}
	if !found {
		t.Fatalf("SearchWorkspace hits = %+v, want an observation hit for acme", hits)
	}
}

// ─── Benchmarks, real store ───────────────────────────────────────────────────

func TestSQLiteBenchmarkReaderRoundTrip(t *testing.T) {
	s := newTestStore(t)
	task := seedProject(t, s, "acme")

	if _, err := s.AddBenchmark(store.AddBenchmarkParams{
		Task: task, Name: "login", Metric: "p95", Unit: "ms", Value: 400, Baseline: true,
		CapturedAt: "2026-01-01 00:00:00",
	}); err != nil {
		t.Fatalf("AddBenchmark(baseline): %v", err)
	}
	if _, err := s.AddBenchmark(store.AddBenchmarkParams{
		Task: task, Name: "login", Metric: "p95", Unit: "ms", Value: 300,
		CapturedAt: "2026-02-01 00:00:00",
	}); err != nil {
		t.Fatalf("AddBenchmark(measurement): %v", err)
	}

	r := NewBenchmarkReader(s)

	page, err := r.ListBenchmarks("acme", BenchmarkFilter{})
	if err != nil {
		t.Fatalf("ListBenchmarks: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("ListBenchmarks items = %d, want 2", len(page.Items))
	}
	var measurement Benchmark
	for _, b := range page.Items {
		if !b.Baseline {
			measurement = b
		}
	}
	if measurement.SyncID == "" {
		t.Fatal("expected the non-baseline measurement among ListBenchmarks's items")
	}
	if measurement.Delta() == nil {
		t.Fatal("a measurement with a baseline must report a Delta")
	}
	improved := measurement.Improved()
	if improved == nil || !*improved {
		t.Fatalf("300ms against a 400ms lower-is-better baseline must read as improved, got %v", improved)
	}

	taskBenches, err := r.TaskBenchmarks(task.SyncID)
	if err != nil {
		t.Fatalf("TaskBenchmarks: %v", err)
	}
	if len(taskBenches) != 2 {
		t.Fatalf("TaskBenchmarks = %d, want 2", len(taskBenches))
	}

	history, err := r.MetricHistory("acme", "p95", 10)
	if err != nil {
		t.Fatalf("MetricHistory: %v", err)
	}
	if len(history) != 2 || history[0].CapturedAt >= history[1].CapturedAt {
		t.Fatalf("MetricHistory = %+v, want oldest first", history)
	}
}

// ─── Themes, real store ───────────────────────────────────────────────────────

func TestSQLiteThemeReaderWriterRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if err := s.SeedBuiltinTheme("koi-pond", store.ThemeVariantDark, json.RawMessage(`{"primary":"#ff9e5e"}`)); err != nil {
		t.Fatalf("SeedBuiltinTheme: %v", err)
	}

	writer := NewThemeWriter(s)
	reader := NewThemeReader(s)

	if err := writer.SaveTheme(store.ThemeRecord{
		Name: "my-theme", Variant: store.ThemeVariantDark, Palette: json.RawMessage(`{"primary":"#111111"}`),
	}); err != nil {
		t.Fatalf("SaveTheme: %v", err)
	}

	themes, err := reader.ListThemes()
	if err != nil {
		t.Fatalf("ListThemes: %v", err)
	}
	if len(themes) != 2 {
		t.Fatalf("ListThemes = %d, want 2 (koi-pond + my-theme)", len(themes))
	}

	mine, err := reader.Theme("my-theme")
	if err != nil {
		t.Fatalf("Theme(my-theme): %v", err)
	}
	if mine.Invalid != "" {
		t.Fatalf("my-theme should validate: %q", mine.Invalid)
	}

	if err := writer.SaveTheme(store.ThemeRecord{Name: "blank", Palette: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("SaveTheme(blank): %v", err)
	}
	blank, err := reader.Theme("blank")
	if err != nil {
		t.Fatalf("Theme(blank): %v", err)
	}
	if blank.Invalid == "" {
		t.Fatal("a palette with no color roles must be flagged Invalid")
	}

	if err := writer.DeleteTheme("my-theme"); err != nil {
		t.Fatalf("DeleteTheme: %v", err)
	}
	if _, err := reader.Theme("my-theme"); !errors.Is(err, store.ErrThemeNotFound) {
		t.Fatalf("Theme(my-theme) after delete = %v, want ErrThemeNotFound", err)
	}

	if err := writer.DeleteTheme("koi-pond"); !errors.Is(err, store.ErrBuiltinTheme) {
		t.Fatalf("DeleteTheme(koi-pond) = %v, want ErrBuiltinTheme", err)
	}
	if err := writer.ResetTheme("koi-pond", nil); err != nil {
		t.Fatalf("ResetTheme: %v", err)
	}
}

// ─── Settings, real store ─────────────────────────────────────────────────────

func TestSQLiteSettingsReaderWriterRoundTrip(t *testing.T) {
	s := newTestStore(t)
	writer := NewSettingsWriter(s)
	reader := NewSettingsReader(s)

	if err := writer.SetSetting("tui.theme", "koi-pond"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := writer.SetSetting("tui.icons", "nerd"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if err := writer.SetSetting("other.key", "x"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	value, ok, err := reader.Setting("tui.theme")
	if err != nil {
		t.Fatalf("Setting: %v", err)
	}
	if !ok || value != "koi-pond" {
		t.Fatalf("Setting(tui.theme) = (%q, %v), want (koi-pond, true)", value, ok)
	}

	_, ok, err = reader.Setting("tui.mouse")
	if err != nil {
		t.Fatalf("Setting: %v", err)
	}
	if ok {
		t.Fatal("Setting(tui.mouse) reported ok for a key that was never set")
	}

	scoped, err := reader.Settings("tui.")
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if len(scoped) != 2 || scoped["tui.theme"] != "koi-pond" || scoped["tui.icons"] != "nerd" {
		t.Fatalf("Settings(tui.) = %v, want exactly the two tui.* keys", scoped)
	}
}

// ─── Tasks, evidence, runbooks: new methods, real store ─────────────────────

func TestSQLiteTaskReaderNewMethodsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	const slug = "acme"
	task := seedProject(t, s, slug)
	if _, err := s.UpsertTask(store.UpsertTaskParams{
		Project: slug, Slug: strp("mantenimiento"), Title: strp("Vault-only task"), Kind: strp("spike"),
		VaultPath: strp(slug + "/mantenimiento"),
	}); err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}

	r := NewTaskPageReader(s)

	page, err := r.ListTasksPage(slug, store.TaskListFilter{Limit: 1})
	if err != nil {
		t.Fatalf("ListTasksPage: %v", err)
	}
	if page.Total != 2 || len(page.Items) != 1 {
		t.Fatalf("ListTasksPage = %+v, want Total=2 len(Items)=1", page)
	}

	detail, err := r.TaskBySlug(slug, "mantenimiento")
	if err != nil {
		t.Fatalf("TaskBySlug: %v", err)
	}
	if detail.Task.Title != "Vault-only task" {
		t.Fatalf("TaskBySlug found the wrong task: %+v", detail.Task)
	}
	if _, err := r.TaskBySlug(slug, "no-such-slug"); err == nil {
		t.Fatal("TaskBySlug should fail for an unknown slug")
	}

	vaultRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vaultRoot, slug, "mantenimiento"), 0o755); err != nil {
		t.Fatalf("mkdir vault dir: %v", err)
	}
	t.Setenv(vaultRootEnv, vaultRoot)

	dir, ok, err := r.VaultDir(detail.Task.ID)
	if err != nil {
		t.Fatalf("VaultDir: %v", err)
	}
	if !ok || dir != filepath.Join(vaultRoot, slug, "mantenimiento") {
		t.Fatalf("VaultDir = (%q, %v), want the seeded folder", dir, ok)
	}

	dir, ok, err = r.VaultDir(task.ID) // seedProject's task has no vault_path
	if err != nil {
		t.Fatalf("VaultDir(no vault_path): %v", err)
	}
	if ok {
		t.Fatalf("VaultDir reported ok for a task with no vault_path: %q", dir)
	}
}

func TestSQLiteEvidenceReaderNewMethodsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	const slug = "acme"
	task := seedProject(t, s, slug) // seeds one evidence row under the default category

	if _, _, _, err := s.AddEvidence(store.AddEvidenceParams{
		Task: task, Path: "second.png", SHA256: strings.Repeat("b", 64),
		Kind: "png", Proves: "also works", Category: "benchmarks",
	}); err != nil {
		t.Fatalf("AddEvidence: %v", err)
	}

	r := NewEvidencePageReader(s)

	page, err := r.ListEvidencePage(slug, store.EvidenceListFilter{Limit: 1})
	if err != nil {
		t.Fatalf("ListEvidencePage: %v", err)
	}
	if page.Total != 2 || len(page.Items) != 1 {
		t.Fatalf("ListEvidencePage = %+v, want Total=2 len(Items)=1", page.Page)
	}
	if page.TotalBytes < 0 {
		t.Fatalf("TotalBytes = %d, want >= 0", page.TotalBytes)
	}

	categories, err := r.Categories(slug)
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	if len(categories) < 2 {
		t.Fatalf("Categories = %+v, want at least 2 distinct categories", categories)
	}
}

func TestSQLiteRunbookReaderListRunbooksPageRoundTrip(t *testing.T) {
	s := newTestStore(t)
	const slug = "acme"
	seedProject(t, s, slug) // seeds RB-900, needs_review=true (stale)

	if _, err := s.SyncRunbookIndex(store.RunbookIndexSyncParams{
		Source: "knowledge-mcp",
		Entries: []store.RunbookIndexEntryInput{
			{
				ID: "RB-901", VaultPath: "Runbooks/RB-901.md", Title: "Fresh runbook",
				Service: slug, Category: "auth", Status: "verified",
			},
		},
	}); err != nil {
		t.Fatalf("SyncRunbookIndex: %v", err)
	}

	r := NewRunbookPageReader(s)
	stale := true
	page, err := r.ListRunbooksPage(slug, false, RunbookFilter{Stale: &stale})
	if err != nil {
		t.Fatalf("ListRunbooksPage: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != "RB-900" {
		t.Fatalf("ListRunbooksPage(stale) = %+v, want just RB-900", page.Items)
	}
}

// ─── Memory scoped methods, real store ───────────────────────────────────────

func TestSQLiteMemoryReaderScopedMethodsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateSession("acme-session", "acme", "/tmp/acme"); err != nil {
		t.Fatalf("CreateSession(acme): %v", err)
	}
	if err := s.CreateSession("other-session", "other", "/tmp/other"); err != nil {
		t.Fatalf("CreateSession(other): %v", err)
	}
	if _, err := s.AddObservation(store.AddObservationParams{
		SessionID: "acme-session", Type: "discovery", Title: "Acme finding",
		Content: "scoped needle content", Project: "acme", Scope: "project",
	}); err != nil {
		t.Fatalf("AddObservation(acme): %v", err)
	}
	if _, err := s.AddObservation(store.AddObservationParams{
		SessionID: "other-session", Type: "discovery", Title: "Other finding",
		Content: "scoped needle content", Project: "other", Scope: "project",
	}); err != nil {
		t.Fatalf("AddObservation(other): %v", err)
	}

	r := NewScopedMemoryReader(s)

	searchPage, err := r.SearchScoped("needle", ProjectScope{Project: "acme"}, 10, 0)
	if err != nil {
		t.Fatalf("SearchScoped: %v", err)
	}
	if len(searchPage.Items) == 0 {
		t.Fatal("SearchScoped(acme) found nothing")
	}
	for _, hit := range searchPage.Items {
		if hit.Project == nil || *hit.Project != "acme" {
			t.Fatalf("SearchScoped leaked a result from another project: %+v", hit)
		}
	}

	obsPage, err := r.RecentObservationsScoped(ProjectScope{Project: "acme"}, 10, 0)
	if err != nil {
		t.Fatalf("RecentObservationsScoped: %v", err)
	}
	for _, o := range obsPage.Items {
		if o.Project == nil || *o.Project != "acme" {
			t.Fatalf("RecentObservationsScoped leaked another project: %+v", o)
		}
	}

	sessPage, err := r.RecentSessionsScoped(ProjectScope{Project: "acme"}, 10, 0)
	if err != nil {
		t.Fatalf("RecentSessionsScoped: %v", err)
	}
	for _, sess := range sessPage.Items {
		if sess.Project != "acme" {
			t.Fatalf("RecentSessionsScoped leaked another project: %+v", sess)
		}
	}

	allPage, err := r.RecentObservationsScoped(ProjectScope{}, 10, 0)
	if err != nil {
		t.Fatalf("RecentObservationsScoped(unscoped): %v", err)
	}
	if len(allPage.Items) < 2 {
		t.Fatalf("RecentObservationsScoped(unscoped) = %d, want >= 2", len(allPage.Items))
	}
}

// ─── Graph, real store ────────────────────────────────────────────────────────

func TestSQLiteGraphReaderRoundTrip(t *testing.T) {
	s := newTestStore(t)
	const slug = "acme"
	seedProject(t, s, slug)

	// project_cards.graph_commit is CHECKed at exactly 40 characters, the
	// shape of a full git SHA.
	const commit = "a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4"
	summary := `{"source":["graphify-out/graph.json"],"built_at_commit":"` + commit + `","node_count":10,` +
		`"edge_count":20,"community_count":2,"god_nodes":[{"label":"Foo","edges":5,"file":"foo.go"}]}`
	if err := s.StampProjectGraph(slug, commit, "2026-09-14 10:00:00", &summary); err != nil {
		t.Fatalf("StampProjectGraph: %v", err)
	}
	if err := s.StampGraphStaleness(slug, "code_changed", 3, "2026-09-14 10:05:00"); err != nil {
		t.Fatalf("StampGraphStaleness: %v", err)
	}

	state, err := NewGraphReader(s).GraphState(slug)
	if err != nil {
		t.Fatalf("GraphState: %v", err)
	}
	if state.Commit != commit || state.Nodes != 10 || state.Edges != 20 || state.Communities != 2 {
		t.Fatalf("GraphState = %+v, want the stamped summary", state)
	}
	if len(state.GodNodes) != 1 || state.GodNodes[0].Label != "Foo" {
		t.Fatalf("GraphState god nodes = %+v", state.GodNodes)
	}
	if !state.Stale || state.StaleReason != "code_changed" || state.ChangedFiles != 3 {
		t.Fatalf("GraphState staleness = %+v, want code_changed/3", state)
	}
}

// TestSQLiteGraphReaderObservationRefsPagesGraphLinks pins what the Graph tab
// lists: the project's graph-linked observations, newest first, with the real
// total behind the page.
func TestSQLiteGraphReaderObservationRefsPagesGraphLinks(t *testing.T) {
	s := newTestStore(t)
	const commit = "a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4"
	if err := s.CreateSession("acme-session", "acme", "/tmp/acme"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	var ids []int64
	for _, ref := range []string{"pkg/a.First", "pkg/b.Second"} {
		res, err := s.AddObservationLinked(store.AddObservationParams{
			SessionID: "acme-session", Type: "decision", Title: ref, Content: ref, Project: "acme",
		}, &store.ObservationLink{GraphRef: ref, GraphCommit: commit})
		if err != nil {
			t.Fatalf("AddObservationLinked(%s): %v", ref, err)
		}
		ids = append(ids, res.ObservationID)
	}

	page, err := NewGraphReader(s).ObservationRefs("acme", 1, 0)
	if err != nil {
		t.Fatalf("ObservationRefs: %v", err)
	}
	if page.Total != 2 || len(page.Items) != 1 {
		t.Fatalf("page = %+v, want one of two", page)
	}
	if page.Items[0].ObservationID != ids[1] || page.Items[0].Ref != "pkg/b.Second" {
		t.Fatalf("first item = %+v, want the newest ref", page.Items[0])
	}
	if page.Items[0].RefKind != "graph" || page.Items[0].GraphCommit != commit {
		t.Fatalf("item = %+v, want a graph ref at the stamped commit", page.Items[0])
	}
	if !page.HasNext() {
		t.Fatal("a page of one out of two has a next page")
	}

	empty, err := NewGraphReader(s).ObservationRefs("nobody", 10, 0)
	if err != nil {
		t.Fatalf("ObservationRefs(unknown project): %v", err)
	}
	if empty.Total != 0 || len(empty.Items) != 0 {
		t.Fatalf("page = %+v, want an empty one", empty)
	}
}

// ─── Nil-store guards ─────────────────────────────────────────────────────────

// TestNewAdaptersWithoutAStoreReportErrStoreUnavailable is the new-interface
// counterpart of the existing TestSQLite*WithoutAStoreReportsIt tests: every
// adapter this package added must fail the same honest way a real store
// outage would, rather than panic on a nil pointer.
func TestNewAdaptersWithoutAStoreReportErrStoreUnavailable(t *testing.T) {
	ptr := NewProjectTreeReader(nil)
	if _, err := ptr.ProjectTree(); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("ProjectTree(nil) = %v, want ErrStoreUnavailable", err)
	}
	if _, err := ptr.ProjectNode("x"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("ProjectNode(nil) = %v, want ErrStoreUnavailable", err)
	}
	if _, err := ptr.Ancestors("x"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("Ancestors(nil) = %v, want ErrStoreUnavailable", err)
	}
	if _, err := ptr.Descendants("x"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("Descendants(nil) = %v, want ErrStoreUnavailable", err)
	}
	if _, err := ptr.ResolveAlias("x"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("ResolveAlias(nil) = %v, want ErrStoreUnavailable", err)
	}

	tr := NewTaskPageReader(nil)
	if _, err := tr.ListTasksPage("acme", store.TaskListFilter{}); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("ListTasksPage(nil) = %v, want ErrStoreUnavailable", err)
	}
	if _, err := tr.TaskBySlug("acme", "x"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("TaskBySlug(nil) = %v, want ErrStoreUnavailable", err)
	}
	if _, _, err := tr.VaultDir(1); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("VaultDir(nil) = %v, want ErrStoreUnavailable", err)
	}

	er := NewEvidencePageReader(nil)
	if _, err := er.ListEvidencePage("acme", store.EvidenceListFilter{}); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("ListEvidencePage(nil) = %v, want ErrStoreUnavailable", err)
	}
	if _, err := er.Categories("acme"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("Categories(nil) = %v, want ErrStoreUnavailable", err)
	}

	rr := NewRunbookPageReader(nil)
	if _, err := rr.ListRunbooksPage("acme", false, RunbookFilter{}); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("ListRunbooksPage(nil) = %v, want ErrStoreUnavailable", err)
	}

	mr := NewScopedMemoryReader(nil)
	if _, err := mr.SearchScoped("x", ProjectScope{}, 10, 0); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("SearchScoped(nil) = %v, want ErrStoreUnavailable", err)
	}
	if _, err := mr.RecentObservationsScoped(ProjectScope{}, 10, 0); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("RecentObservationsScoped(nil) = %v, want ErrStoreUnavailable", err)
	}
	if _, err := mr.RecentSessionsScoped(ProjectScope{}, 10, 0); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("RecentSessionsScoped(nil) = %v, want ErrStoreUnavailable", err)
	}

	br := NewBenchmarkReader(nil)
	if _, err := br.ListBenchmarks("acme", BenchmarkFilter{}); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("ListBenchmarks(nil) = %v, want ErrStoreUnavailable", err)
	}
	if _, err := br.TaskBenchmarks("sync-1"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("TaskBenchmarks(nil) = %v, want ErrStoreUnavailable", err)
	}
	if _, err := br.MetricHistory("acme", "p95", 10); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("MetricHistory(nil) = %v, want ErrStoreUnavailable", err)
	}

	gr := NewGraphReader(nil)
	if _, err := gr.GraphState("acme"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("GraphState(nil) = %v, want ErrStoreUnavailable", err)
	}
	if _, err := gr.ObservationRefs("acme", 10, 0); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("ObservationRefs(nil) = %v, want ErrStoreUnavailable", err)
	}
	gs := NewGraphSyncer(nil, "")
	if _, err := gs.SyncGraph("acme"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("SyncGraph(nil) = %v, want ErrStoreUnavailable", err)
	}

	thr := NewThemeReader(nil)
	if _, err := thr.ListThemes(); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("ListThemes(nil) = %v, want ErrStoreUnavailable", err)
	}
	if _, err := thr.Theme("koi-pond"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("Theme(nil) = %v, want ErrStoreUnavailable", err)
	}
	thw := NewThemeWriter(nil)
	if err := thw.SaveTheme(store.ThemeRecord{}); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("SaveTheme(nil) = %v, want ErrStoreUnavailable", err)
	}
	if err := thw.DeleteTheme("koi-pond"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("DeleteTheme(nil) = %v, want ErrStoreUnavailable", err)
	}
	if err := thw.ResetTheme("koi-pond", nil); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("ResetTheme(nil) = %v, want ErrStoreUnavailable", err)
	}

	sr := NewSettingsReader(nil)
	if _, _, err := sr.Setting("x"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("Setting(nil) = %v, want ErrStoreUnavailable", err)
	}
	if _, err := sr.Settings(""); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("Settings(nil) = %v, want ErrStoreUnavailable", err)
	}
	sw := NewSettingsWriter(nil)
	if err := sw.SetSetting("x", "y"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("SetSetting(nil) = %v, want ErrStoreUnavailable", err)
	}

	if _, err := NewGlobalSearcher(nil).SearchWorkspace(SearchQuery{}); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("SearchWorkspace(nil) = %v, want ErrStoreUnavailable", err)
	}
}

// ─── New fakes: behavior, not just the interface assertion ──────────────────

func TestFakeProjectTreeReturnsWhatItWasGiven(t *testing.T) {
	f := &FakeProjectTree{
		Tree:              []ProjectNode{{Slug: "a"}},
		NodeBySlug:        map[string]ProjectNode{"a": {Slug: "a", DisplayName: "A"}},
		AncestorsBySlug:   map[string][]ProjectNode{"b": {{Slug: "a"}}},
		DescendantsBySlug: map[string][]string{"a": {"a", "b"}},
		AliasResolution:   map[string]string{"koi_garden": "koi-garden"},
	}

	tree, err := f.ProjectTree()
	if err != nil || len(tree) != 1 || tree[0].Slug != "a" {
		t.Fatalf("ProjectTree = (%+v, %v)", tree, err)
	}
	node, err := f.ProjectNode("a")
	if err != nil || node.DisplayName != "A" {
		t.Fatalf("ProjectNode = (%+v, %v)", node, err)
	}
	if _, err := f.ProjectNode("missing"); err == nil {
		t.Fatal("ProjectNode(missing) should fail")
	}
	ancestors, err := f.Ancestors("b")
	if err != nil || len(ancestors) != 1 {
		t.Fatalf("Ancestors = (%+v, %v)", ancestors, err)
	}
	descendants, err := f.Descendants("a")
	if err != nil || len(descendants) != 2 {
		t.Fatalf("Descendants = (%+v, %v)", descendants, err)
	}
	slug, err := f.ResolveAlias("koi_garden")
	if err != nil || slug != "koi-garden" {
		t.Fatalf("ResolveAlias = (%q, %v)", slug, err)
	}
	if _, err := f.ResolveAlias("unknown"); !errors.Is(err, ErrProjectUnresolved) {
		t.Fatalf("ResolveAlias(unknown) = %v, want ErrProjectUnresolved", err)
	}
}

func TestFakeProjectTreeErrShortCircuits(t *testing.T) {
	f := &FakeProjectTree{Err: errors.New("boom")}
	if _, err := f.ProjectTree(); err == nil {
		t.Fatal("ProjectTree should fail")
	}
	if _, err := f.ProjectNode("a"); err == nil {
		t.Fatal("ProjectNode should fail")
	}
	if _, err := f.Ancestors("a"); err == nil {
		t.Fatal("Ancestors should fail")
	}
	if _, err := f.Descendants("a"); err == nil {
		t.Fatal("Descendants should fail")
	}
	if _, err := f.ResolveAlias("a"); err == nil {
		t.Fatal("ResolveAlias should fail")
	}
}

func TestFakeBenchmarkReturnsWhatItWasGiven(t *testing.T) {
	f := &FakeBenchmark{
		ByProject: map[string][]Benchmark{
			"acme": {
				{BenchmarkDelta: store.BenchmarkDelta{Benchmark: store.Benchmark{Metric: "p95"}}},
				{BenchmarkDelta: store.BenchmarkDelta{Benchmark: store.Benchmark{Metric: "rss"}}},
			},
		},
		ByTaskSyncID: map[string][]Benchmark{
			"task-1": {{BenchmarkDelta: store.BenchmarkDelta{Benchmark: store.Benchmark{Metric: "p95"}}}},
		},
	}

	page, err := f.ListBenchmarks("acme", BenchmarkFilter{Metric: "p95"})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("ListBenchmarks(metric filter) = (%+v, %v)", page, err)
	}
	if f.LastFilter().Metric != "p95" {
		t.Fatalf("LastFilter = %+v", f.LastFilter())
	}

	taskBenches, err := f.TaskBenchmarks("task-1")
	if err != nil || len(taskBenches) != 1 {
		t.Fatalf("TaskBenchmarks = (%+v, %v)", taskBenches, err)
	}

	history, err := f.MetricHistory("acme", "p95", 1)
	if err != nil || len(history) != 1 {
		t.Fatalf("MetricHistory = (%+v, %v)", history, err)
	}
}

func TestFakeBenchmarkErrShortCircuits(t *testing.T) {
	f := &FakeBenchmark{Err: errors.New("boom")}
	if _, err := f.ListBenchmarks("acme", BenchmarkFilter{}); err == nil {
		t.Fatal("ListBenchmarks should fail")
	}
	if _, err := f.TaskBenchmarks("t"); err == nil {
		t.Fatal("TaskBenchmarks should fail")
	}
	if _, err := f.MetricHistory("acme", "p95", 1); err == nil {
		t.Fatal("MetricHistory should fail")
	}
}

func TestFakeGraphReturnsWhatItWasGivenAndRecordsSyncCalls(t *testing.T) {
	f := &FakeGraph{
		StateByProject: map[string]GraphState{"acme": {Project: "acme", Nodes: 5}},
		RefsByProject:  map[string][]ObservationRef{"acme": {{ObservationID: 1}, {ObservationID: 2}}},
		SyncResult:     map[string]GraphState{"acme": {Project: "acme", Nodes: 6}},
	}
	state, err := f.GraphState("acme")
	if err != nil || state.Nodes != 5 {
		t.Fatalf("GraphState = (%+v, %v)", state, err)
	}
	refs, err := f.ObservationRefs("acme", 1, 0)
	if err != nil || len(refs.Items) != 1 || refs.Total != 2 {
		t.Fatalf("ObservationRefs = (%+v, %v)", refs, err)
	}
	synced, err := f.SyncGraph("acme")
	if err != nil || synced.Nodes != 6 {
		t.Fatalf("SyncGraph = (%+v, %v)", synced, err)
	}
	if len(f.SyncCalls()) != 1 || f.SyncCalls()[0] != "acme" {
		t.Fatalf("SyncCalls = %v", f.SyncCalls())
	}
}

func TestFakeGraphErrShortCircuits(t *testing.T) {
	f := &FakeGraph{Err: errors.New("boom")}
	if _, err := f.GraphState("acme"); err == nil {
		t.Fatal("GraphState should fail")
	}
	if _, err := f.ObservationRefs("acme", 1, 0); err == nil {
		t.Fatal("ObservationRefs should fail")
	}
	if _, err := f.SyncGraph("acme"); err == nil {
		t.Fatal("SyncGraph should fail")
	}
}

func TestFakeThemeReturnsWhatItWasGivenAndRecordsWrites(t *testing.T) {
	f := &FakeTheme{
		Themes: []ThemeRecord{{ThemeRecord: store.ThemeRecord{Name: "koi-pond"}}},
		ByName: map[string]ThemeRecord{"koi-pond": {ThemeRecord: store.ThemeRecord{Name: "koi-pond", Builtin: true}}},
	}
	themes, err := f.ListThemes()
	if err != nil || len(themes) != 1 {
		t.Fatalf("ListThemes = (%+v, %v)", themes, err)
	}
	theme, err := f.Theme("koi-pond")
	if err != nil || !theme.Builtin {
		t.Fatalf("Theme = (%+v, %v)", theme, err)
	}
	if _, err := f.Theme("missing"); !errors.Is(err, store.ErrThemeNotFound) {
		t.Fatalf("Theme(missing) = %v, want ErrThemeNotFound", err)
	}
	if err := f.SaveTheme(store.ThemeRecord{Name: "new"}); err != nil {
		t.Fatalf("SaveTheme: %v", err)
	}
	if err := f.DeleteTheme("koi-pond"); err != nil {
		t.Fatalf("DeleteTheme: %v", err)
	}
	if err := f.ResetTheme("koi-pond", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("ResetTheme: %v", err)
	}
	if len(f.Saved()) != 1 || len(f.Deleted()) != 1 || len(f.ResetCalls()) != 1 {
		t.Fatalf("writes not recorded: saved=%d deleted=%d reset=%d", len(f.Saved()), len(f.Deleted()), len(f.ResetCalls()))
	}
}

func TestFakeThemeErrShortCircuitsButStillRecordsTheAttemptedWrite(t *testing.T) {
	f := &FakeTheme{Err: errors.New("boom")}
	if _, err := f.ListThemes(); err == nil {
		t.Fatal("ListThemes should fail")
	}
	if _, err := f.Theme("x"); err == nil {
		t.Fatal("Theme should fail")
	}
	if err := f.SaveTheme(store.ThemeRecord{Name: "x"}); err == nil {
		t.Fatal("SaveTheme should fail")
	}
	if err := f.DeleteTheme("x"); err == nil {
		t.Fatal("DeleteTheme should fail")
	}
	if err := f.ResetTheme("x", nil); err == nil {
		t.Fatal("ResetTheme should fail")
	}
	if len(f.Saved()) != 1 || len(f.Deleted()) != 1 || len(f.ResetCalls()) != 1 {
		t.Fatalf("attempted writes not recorded despite the error: saved=%d deleted=%d reset=%d",
			len(f.Saved()), len(f.Deleted()), len(f.ResetCalls()))
	}
}

func TestFakeSettingsReturnsWhatItWasGivenAndRecordsWrites(t *testing.T) {
	f := &FakeSettings{Values: map[string]string{"tui.theme": "koi-pond"}}
	v, ok, err := f.Setting("tui.theme")
	if err != nil || !ok || v != "koi-pond" {
		t.Fatalf("Setting = (%q, %v, %v)", v, ok, err)
	}
	_, ok, err = f.Setting("tui.mouse")
	if err != nil || ok {
		t.Fatalf("Setting(unset) = (%v, %v)", ok, err)
	}
	all, err := f.Settings("tui.")
	if err != nil || len(all) != 1 {
		t.Fatalf("Settings = (%+v, %v)", all, err)
	}
	if err := f.SetSetting("tui.icons", "nerd"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if f.Value("tui.icons") != "nerd" || len(f.SetCalls()) != 1 {
		t.Fatalf("SetSetting did not update Values/SetCalls: %+v %v", f.Values, f.SetCalls())
	}
}

func TestFakeSettingsErrShortCircuits(t *testing.T) {
	f := &FakeSettings{Err: errors.New("boom")}
	if _, _, err := f.Setting("x"); err == nil {
		t.Fatal("Setting should fail")
	}
	if _, err := f.Settings(""); err == nil {
		t.Fatal("Settings should fail")
	}
	if err := f.SetSetting("x", "y"); err == nil {
		t.Fatal("SetSetting should fail")
	}
}

func TestFakeSearchFiltersByKindsAndRecordsTheQuery(t *testing.T) {
	f := &FakeSearch{Hits: []SearchHit{
		{Kind: SearchKindObservation, Title: "obs"},
		{Kind: SearchKindTask, Title: "task"},
	}}
	all, err := f.SearchWorkspace(SearchQuery{Text: "x"})
	if err != nil || len(all) != 2 {
		t.Fatalf("SearchWorkspace(no kinds) = (%+v, %v)", all, err)
	}
	only, err := f.SearchWorkspace(SearchQuery{Text: "x", Kinds: []SearchKind{SearchKindTask}})
	if err != nil || len(only) != 1 || only[0].Kind != SearchKindTask {
		t.Fatalf("SearchWorkspace(task only) = (%+v, %v)", only, err)
	}
	if f.LastQuery().Text != "x" {
		t.Fatalf("LastQuery = %+v", f.LastQuery())
	}
}

func TestFakeSearchErrShortCircuits(t *testing.T) {
	f := &FakeSearch{Err: errors.New("boom")}
	if _, err := f.SearchWorkspace(SearchQuery{}); err == nil {
		t.Fatal("SearchWorkspace should fail")
	}
}

func TestFakeTaskBySlugAndVaultDirReturnWhatTheyWereGiven(t *testing.T) {
	f := &FakeTask{
		DetailBySlug: map[string]TaskDetail{"acme/mantenimiento": {Task: store.Task{Title: "Vault task"}}},
		VaultDirByID: map[int64]string{1: "/vault/acme/mantenimiento"},
	}
	detail, err := f.TaskBySlug("acme", "mantenimiento")
	if err != nil || detail.Task.Title != "Vault task" {
		t.Fatalf("TaskBySlug = (%+v, %v)", detail, err)
	}
	if _, err := f.TaskBySlug("acme", "missing"); err == nil {
		t.Fatal("TaskBySlug(missing) should fail")
	}
	dir, ok, err := f.VaultDir(1)
	if err != nil || !ok || dir != "/vault/acme/mantenimiento" {
		t.Fatalf("VaultDir = (%q, %v, %v)", dir, ok, err)
	}
	if _, ok, err := f.VaultDir(2); err != nil || ok {
		t.Fatalf("VaultDir(unset) = (%v, %v)", ok, err)
	}

	errFake := &FakeTask{Err: errors.New("boom")}
	if _, err := errFake.TaskBySlug("acme", "x"); err == nil {
		t.Fatal("TaskBySlug should fail")
	}
	if _, _, err := errFake.VaultDir(1); err == nil {
		t.Fatal("VaultDir should fail")
	}
}

func TestFakeEvidenceCategoriesReturnsWhatItWasGiven(t *testing.T) {
	f := &FakeEvidence{ItemsByProject: map[string][]store.EvidenceListItem{"acme": {
		{Evidence: store.Evidence{Category: "auth"}},
		{Evidence: store.Evidence{Category: "auth"}},
		{Evidence: store.Evidence{Category: "benchmarks"}},
	}}}
	cats, err := f.Categories("acme")
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	if len(cats) != 2 || cats[0].Category != "auth" || cats[0].Count != 2 ||
		cats[1].Category != "benchmarks" || cats[1].Count != 1 {
		t.Fatalf("Categories = %+v", cats)
	}
	if _, err := (&FakeEvidence{Err: errors.New("boom")}).Categories("acme"); err == nil {
		t.Fatal("Categories should fail")
	}
}

func TestFakeMemoryScopedMethodsFilterAndPaginate(t *testing.T) {
	f := &FakeMemory{
		Observations: []store.Observation{{ID: 1, Project: strp("acme")}, {ID: 2, Project: strp("other")}},
		Sessions:     []store.SessionSummary{{ID: "s1", Project: "acme"}, {ID: "s2", Project: "other"}},
	}
	obsPage, err := f.RecentObservationsScoped(ProjectScope{Project: "acme"}, 10, 0)
	if err != nil || len(obsPage.Items) != 1 || obsPage.Items[0].Project == nil || *obsPage.Items[0].Project != "acme" {
		t.Fatalf("RecentObservationsScoped = (%+v, %v)", obsPage, err)
	}
	sessPage, err := f.RecentSessionsScoped(ProjectScope{Project: "acme"}, 10, 0)
	if err != nil || len(sessPage.Items) != 1 || sessPage.Items[0].Project != "acme" {
		t.Fatalf("RecentSessionsScoped = (%+v, %v)", sessPage, err)
	}
	if f.LastScope().Project != "acme" {
		t.Fatalf("LastScope = %+v", f.LastScope())
	}

	// Subtree widens the scope through SubtreeSlugs.
	f.SubtreeSlugs = map[string][]string{"acme": {"acme", "acme-child"}}
	f.Observations = append(f.Observations, store.Observation{ID: 3, Project: strp("acme-child")})
	widePage, err := f.RecentObservationsScoped(ProjectScope{Project: "acme", Subtree: true}, 10, 0)
	if err != nil || len(widePage.Items) != 2 {
		t.Fatalf("RecentObservationsScoped(subtree) = (%+v, %v)", widePage, err)
	}

	errFake := &FakeMemory{Err: errors.New("boom")}
	if _, err := errFake.RecentObservationsScoped(ProjectScope{}, 1, 0); err == nil {
		t.Fatal("RecentObservationsScoped should fail")
	}
	if _, err := errFake.RecentSessionsScoped(ProjectScope{}, 1, 0); err == nil {
		t.Fatal("RecentSessionsScoped should fail")
	}
	if _, err := errFake.SearchScoped("x", ProjectScope{}, 1, 0); err == nil {
		t.Fatal("SearchScoped should fail")
	}
}

// TestFakeSpiesAreSafeUnderConcurrency drives every fake from several
// goroutines at once, the way Bubble Tea runs a tab's Init and Refresh
// commands. It only fails under -race, which is the point: the records each
// fake keeps are written from command goroutines and read from the test's.
func TestFakeSpiesAreSafeUnderConcurrency(t *testing.T) {
	memory := &FakeMemory{Observations: []store.Observation{{ID: 1}}}
	tasks := &FakeTask{ItemsByProject: map[string][]store.TaskListItem{"acme": {{Task: store.Task{ID: 1}}}}}
	evidence := &FakeEvidence{ItemsByProject: map[string][]store.EvidenceListItem{"acme": {{Evidence: store.Evidence{ID: 1}}}}}
	runbooks := &FakeRunbook{ItemsByProject: map[string][]store.RunbookIndexRow{"acme": {{Title: "one"}}}}
	benchmarks := &FakeBenchmark{ByProject: map[string][]Benchmark{"acme": {{}}}}
	graph := &FakeGraph{StateByProject: map[string]GraphState{"acme": {}}}
	themes := &FakeTheme{}
	settings := &FakeSettings{}
	search := &FakeSearch{Hits: []SearchHit{{Kind: "task"}}}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = memory.Search("needle", store.SearchOptions{})
			_, _ = memory.SearchScoped("needle", ProjectScope{Project: "acme"}, 10, 0)
			_ = memory.DeleteSession("s1")
			_, _ = tasks.ListTasks("acme", store.TaskListFilter{Query: "a"})
			_, _ = tasks.ListTasksPage("acme", store.TaskListFilter{Query: "a"})
			_ = tasks.UpdateState(1, "review")
			_ = tasks.LinkObservation(1, 2)
			_, _ = evidence.ListEvidence("acme", store.EvidenceListFilter{})
			_, _ = evidence.ListEvidencePage("acme", store.EvidenceListFilter{})
			_, _ = runbooks.ListRunbooks("acme", true)
			_, _ = runbooks.ListRunbooksPage("acme", true, RunbookFilter{})
			_, _ = runbooks.SearchRunbooks("acme", true, "one", 5)
			_, _ = benchmarks.ListBenchmarks("acme", BenchmarkFilter{})
			_, _ = graph.SyncGraph("acme")
			_ = themes.SaveTheme(store.ThemeRecord{Name: "koi-pond"})
			_ = themes.DeleteTheme("koi-pond")
			_ = themes.ResetTheme("koi-pond", nil)
			_ = settings.SetSetting("tui.theme", "koi-pond")
			_, _, _ = settings.Setting("tui.theme")
			_, _ = search.SearchWorkspace(SearchQuery{Text: "x"})
		}()
	}

	// The test goroutine reads the records while the writers are still
	// running: an unguarded field races here, not only between two commands.
	for i := 0; i < 8; i++ {
		_ = memory.Queries()
		_ = memory.DeletedSessions()
		_ = memory.LastScope()
		_ = tasks.LastListFilter()
		_ = tasks.UpdateStateCalls()
		_ = tasks.LinkCalls()
		_ = evidence.LastFilter()
		_ = runbooks.LastListAll()
		_ = runbooks.LastSearch()
		_ = benchmarks.LastFilter()
		_ = graph.SyncCalls()
		_ = themes.Saved()
		_ = themes.Deleted()
		_ = themes.ResetCalls()
		_ = settings.SetCalls()
		_ = settings.Value("tui.theme")
		_ = search.LastQuery()
	}
	wg.Wait()

	if len(memory.Queries()) != 16 {
		t.Fatalf("recorded %d queries, want one per Search and SearchScoped call", len(memory.Queries()))
	}
	if len(tasks.UpdateStateCalls()) != 8 || len(tasks.LinkCalls()) != 8 {
		t.Fatalf("task calls = %d state, %d link, want 8 of each", len(tasks.UpdateStateCalls()), len(tasks.LinkCalls()))
	}
	if len(graph.SyncCalls()) != 8 || len(settings.SetCalls()) != 8 {
		t.Fatalf("sync=%d settings=%d, want 8 of each", len(graph.SyncCalls()), len(settings.SetCalls()))
	}
}

// TestSetErrFailsAFakeWhileItIsInUse covers the accessor a test needs when it
// breaks a reader after the program is already running: assigning Err
// directly would race with the command goroutine reading it.
func TestSetErrFailsAFakeWhileItIsInUse(t *testing.T) {
	f := &FakeSettings{}
	if _, _, err := f.Setting("tui.theme"); err != nil {
		t.Fatalf("Setting before SetErr = %v, want no error", err)
	}
	f.SetErr(errors.New("boom"))
	if _, _, err := f.Setting("tui.theme"); err == nil {
		t.Fatal("Setting after SetErr should fail")
	}
	if err := f.SetSetting("tui.theme", "koi-day"); err == nil {
		t.Fatal("SetSetting after SetErr should fail")
	}
}
