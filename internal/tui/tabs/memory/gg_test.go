package memory

import (
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

func threeObservations() []store.Observation {
	return []store.Observation{
		{ID: 1, Type: "decision", Title: "one"},
		{ID: 2, Type: "decision", Title: "two"},
		{ID: 3, Type: "decision", Title: "three"},
	}
}

func gKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")} }
func GKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")} }

// TestGAndCapitalGOnSearchResults pins rfc-tui.md §7.1's "g/G van al inicio
// y al fin de cada lista", applied to S10's search results — the same job
// they already did on Tasks/Runbooks/Evidence's lists before this task.
func TestGAndCapitalGOnSearchResults(t *testing.T) {
	m := New(nil, "")
	m.Screen = ScreenSearchResults
	obs := threeObservations()
	results := make([]store.SearchResult, len(obs))
	for i, o := range obs {
		results[i] = store.SearchResult{Observation: o}
	}
	m.SearchResults = results
	m.Cursor = 1

	updated, _ := m.Update(GKey())
	got := updated.(Model)
	if got.Cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (the last result)", got.Cursor)
	}

	updated, _ = got.Update(gKey())
	got = updated.(Model)
	if got.Cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (the first result)", got.Cursor)
	}
}

// TestGAndCapitalGOnRecentObservations mirrors the same pin for S10's
// "recent observations" list.
func TestGAndCapitalGOnRecentObservations(t *testing.T) {
	m := New(nil, "")
	m.Screen = ScreenRecent
	m.RecentObservations = threeObservations()
	m.Cursor = 1

	updated, _ := m.Update(GKey())
	got := updated.(Model)
	if got.Cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (the last observation)", got.Cursor)
	}

	updated, _ = got.Update(gKey())
	got = updated.(Model)
	if got.Cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (the first observation)", got.Cursor)
	}
}

// TestGAndCapitalGOnSessions mirrors the same pin for S10's session list.
func TestGAndCapitalGOnSessions(t *testing.T) {
	m := New(nil, "")
	m.Screen = ScreenSessions
	m.Sessions = []store.SessionSummary{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	m.Cursor = 1

	updated, _ := m.Update(GKey())
	got := updated.(Model)
	if got.Cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (the last session)", got.Cursor)
	}

	updated, _ = got.Update(gKey())
	got = updated.(Model)
	if got.Cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (the first session)", got.Cursor)
	}
}

// TestGAndCapitalGDoNotInterfereWithTheDeletePrompt pins that "g"/"G" do not
// leak into the "y/n" delete confirmation's own key handling.
func TestGAndCapitalGDoNotInterfereWithTheDeletePrompt(t *testing.T) {
	m := New(nil, "")
	m.Screen = ScreenSessions
	m.Sessions = []store.SessionSummary{{ID: "a"}, {ID: "b"}}
	m.Cursor = 0
	m.SessionDeleteState = SessionDeleteStatePrompt
	m.SessionDeleteID = "a"

	updated, _ := m.Update(gKey())
	got := updated.(Model)
	if got.SessionDeleteState != SessionDeleteStatePrompt {
		t.Fatal("\"g\" should not dismiss the delete prompt")
	}
}
