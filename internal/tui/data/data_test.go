package data

import (
	"errors"
	"testing"

	"github.com/Gentleman-Programming/engram/internal/store"
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
