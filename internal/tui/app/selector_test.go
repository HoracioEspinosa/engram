package app

import (
	"strings"
	"testing"

	"github.com/Gentleman-Programming/engram/internal/store"
	"github.com/Gentleman-Programming/engram/internal/tui/data"
	"github.com/Gentleman-Programming/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

func testSelectorCards() []store.ProjectCardListItem {
	return []store.ProjectCardListItem{
		{
			ProjectCard: store.ProjectCard{Slug: "nextcloud", DisplayName: "Nextcloud (clarodrive)"},
			Counts:      &store.ProjectCardCounts{Observations: 1204, TasksActive: 7, RunbooksStale: 2},
		},
		{
			ProjectCard: store.ProjectCard{Slug: "portal", DisplayName: "Portal (Angular 16)"},
			Counts:      &store.ProjectCardCounts{Observations: 97, TasksActive: 1},
		},
	}
}

// TestPKeyOpensSelectorAndLoadsCards checks the global "p" binding works from
// the tab the workspace opens on, with no project active yet — the state
// every real session starts from.
func TestPKeyOpensSelectorAndLoadsCards(t *testing.T) {
	m := New(nil, "", theme.New(theme.CatppuccinMocha()), "")
	fake := &data.FakeProject{Cards: testSelectorCards()}
	m.projects = fake
	m.selector = newSelectorModel(fake)

	m, cmd := step(t, m, tea.KeyMsg{Runes: []rune("p"), Type: tea.KeyRunes})
	if m.screen != screenSelector {
		t.Fatalf("screen = %v, want screenSelector", m.screen)
	}
	if cmd == nil {
		t.Fatal("opening the selector should load the card list")
	}

	m, _ = step(t, m, cmd())
	if !m.selector.loaded {
		t.Fatal("the selector should be marked loaded after its list arrives")
	}
	if len(m.selector.filtered) != 2 {
		t.Fatalf("filtered cards = %d, want 2", len(m.selector.filtered))
	}
}

func TestSelectorFilterNarrowsBySlugOrName(t *testing.T) {
	m := New(nil, "", theme.New(theme.CatppuccinMocha()), "")
	fake := &data.FakeProject{Cards: testSelectorCards()}
	m.selector = newSelectorModel(fake).applyLoaded(selectorLoadedMsg{cards: testSelectorCards()})
	m.screen = screenSelector

	m, _ = step(t, m, tea.KeyMsg{Runes: []rune("/"), Type: tea.KeyRunes})
	if !m.selector.filterInput.Focused() {
		t.Fatal("/ should focus the filter input")
	}

	for _, r := range "portal" {
		m, _ = step(t, m, tea.KeyMsg{Runes: []rune{r}, Type: tea.KeyRunes})
	}
	if len(m.selector.filtered) != 1 || m.selector.filtered[0].Slug != "portal" {
		t.Fatalf("filtered = %+v, want only portal", m.selector.filtered)
	}

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.selector.filterInput.Focused() {
		t.Fatal("esc should blur the filter input")
	}
	if m.selector.filterInput.Value() != "" {
		t.Fatal("esc should clear the filter")
	}
	if len(m.selector.filtered) != 2 {
		t.Fatalf("clearing the filter should restore every card, got %d", len(m.selector.filtered))
	}
}

// TestGlobalKeysReachTheFilterInputWhileItIsFocused is the regression case
// rfc-tui.md §7.1 calls out by name: typing a digit meant for the filter
// query must not be swallowed as a tab-switch key.
func TestGlobalKeysReachTheFilterInputWhileItIsFocused(t *testing.T) {
	m := New(nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.selector = newSelectorModel(nil).applyLoaded(selectorLoadedMsg{cards: testSelectorCards()})
	m.screen = screenSelector
	m.selector.filterInput.Focus()

	m, _ = step(t, m, tea.KeyMsg{Runes: []rune("5"), Type: tea.KeyRunes})
	if m.screen != screenSelector {
		t.Fatal("a digit typed into the filter must not switch tabs")
	}
	if m.selector.filterInput.Value() != "5" {
		t.Fatalf("filter value = %q, want the digit to have been typed", m.selector.filterInput.Value())
	}
}

func TestSelectorEnterActivatesProjectAndOpensDashboard(t *testing.T) {
	m := New(nil, "", theme.New(theme.CatppuccinMocha()), "")
	fake := &data.FakeProject{
		Cards:      testSelectorCards(),
		CardBySlug: map[string]store.ProjectCard{"portal": {Slug: "portal", DisplayName: "Portal"}},
	}
	m.projects = fake
	m.selector = newSelectorModel(fake).applyLoaded(selectorLoadedMsg{cards: testSelectorCards()})
	m.selector = m.selector.moveCursor(1) // portal
	m.screen = screenSelector

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.project != "portal" {
		t.Fatalf("project = %q, want portal", m.project)
	}
	if m.screen != screenDashboard {
		t.Fatalf("screen = %v, want screenDashboard", m.screen)
	}
	if cmd == nil {
		t.Fatal("activating a project should load its Dashboard")
	}
}

func TestSelectorEnterWithNoRowsSelectedIsANoOp(t *testing.T) {
	m := New(nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen = screenSelector // never loaded: filtered is empty

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.screen != screenSelector {
		t.Fatal("enter with nothing loaded should not leave the selector")
	}
	if cmd != nil {
		t.Fatal("enter with nothing selected should not produce a command")
	}
}

func TestSelectorEscNeverQuitsAndCancelsToTheDashboardOnlyWithAProject(t *testing.T) {
	// No project active yet: esc has nowhere to go back to, so it must do
	// nothing rather than exit — rfc-tui.md §7.1, "Esc never quits".
	m := New(nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen = screenSelector

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.screen != screenSelector {
		t.Fatal("esc with no project active should not leave the selector")
	}
	if cmd != nil {
		t.Fatal("esc should not quit")
	}

	// A project already active: esc cancels back to its Dashboard.
	m.project = "nextcloud"
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.screen != screenDashboard {
		t.Fatalf("screen = %v, want screenDashboard once a project is active", m.screen)
	}
}

func TestSelectorQQuits(t *testing.T) {
	m := New(nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen = screenSelector

	_, cmd := step(t, m, tea.KeyMsg{Runes: []rune("q"), Type: tea.KeyRunes})
	if cmd == nil {
		t.Fatal("q should quit the selector")
	}
}

func TestSelectorRRefreshesTheList(t *testing.T) {
	m := New(nil, "", theme.New(theme.CatppuccinMocha()), "")
	fake := &data.FakeProject{Cards: testSelectorCards()}
	m.projects = fake
	m.screen = screenSelector

	_, cmd := step(t, m, tea.KeyMsg{Runes: []rune("r"), Type: tea.KeyRunes})
	if cmd == nil {
		t.Fatal("r should reload the selector's card list")
	}
}

func TestSelectorAppliedLoadedErrorIsShown(t *testing.T) {
	m := New(nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen = screenSelector

	boom := &data.FakeProject{Err: errBoom}
	m.selector = newSelectorModel(boom)
	m.selector = m.selector.applyLoaded(selectorLoadedMsg{err: errBoom})

	if m.selector.err == "" {
		t.Fatal("a failed load should record its error")
	}
	if !strings.Contains(m.View(), m.selector.err) {
		t.Fatal("the selector view should render the load error")
	}
}

func TestSelectorArrowKeysMoveTheCursor(t *testing.T) {
	m := New(nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen = screenSelector
	m.selector = newSelectorModel(nil).applyLoaded(selectorLoadedMsg{cards: testSelectorCards()})

	m, _ = step(t, m, tea.KeyMsg{Runes: []rune("j"), Type: tea.KeyRunes})
	if m.selector.cursor != 1 {
		t.Fatalf("cursor = %d, want 1 after j", m.selector.cursor)
	}
	m, _ = step(t, m, tea.KeyMsg{Runes: []rune("k"), Type: tea.KeyRunes})
	if m.selector.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 after k", m.selector.cursor)
	}
}

func TestSelectorFilterEnterBlursWithoutClearing(t *testing.T) {
	m := New(nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen = screenSelector
	m.selector = newSelectorModel(nil).applyLoaded(selectorLoadedMsg{cards: testSelectorCards()})
	m.selector.filterInput.Focus()
	m.selector.filterInput.SetValue("port")
	m.selector = m.selector.applyFilter()

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.selector.filterInput.Focused() {
		t.Fatal("enter should blur the filter input")
	}
	if m.selector.filterInput.Value() != "port" {
		t.Fatal("enter should keep the typed filter, unlike esc")
	}
	if len(m.selector.filtered) != 1 {
		t.Fatalf("filtered = %d, want the filter to still apply", len(m.selector.filtered))
	}
}

func TestSelectorMoveCursorClampsAtTheEdges(t *testing.T) {
	sel := newSelectorModel(nil).applyLoaded(selectorLoadedMsg{cards: testSelectorCards()})

	sel = sel.moveCursor(-5)
	if sel.cursor != 0 {
		t.Fatalf("cursor = %d, want clamped to 0", sel.cursor)
	}
	sel = sel.moveCursor(5)
	if sel.cursor != len(sel.filtered)-1 {
		t.Fatalf("cursor = %d, want clamped to the last row", sel.cursor)
	}
}

func TestSelectorMoveCursorOnAnEmptyListIsANoOp(t *testing.T) {
	sel := newSelectorModel(nil)
	sel = sel.moveCursor(1)
	if sel.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 on an empty list", sel.cursor)
	}
}

func TestViewSelectorRendersLoadingThenRows(t *testing.T) {
	m := New(nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen = screenSelector
	if out := m.View(); !strings.Contains(out, "Loading projects") {
		t.Fatalf("an unloaded selector should show a loading state, got:\n%s", out)
	}

	m.selector = newSelectorModel(nil).applyLoaded(selectorLoadedMsg{cards: testSelectorCards()})
	out := m.View()
	if !strings.Contains(out, "nextcloud") || !strings.Contains(out, "portal") {
		t.Fatalf("loaded selector should list every card, got:\n%s", out)
	}
	if !strings.Contains(out, "1204") {
		t.Fatalf("loaded selector should show real counters, got:\n%s", out)
	}
}

func TestViewSelectorRendersNoMatches(t *testing.T) {
	m := New(nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen = screenSelector
	m.selector = newSelectorModel(nil).applyLoaded(selectorLoadedMsg{cards: testSelectorCards()})
	m.selector.filterInput.SetValue("no-such-project")
	m.selector = m.selector.applyFilter()

	if out := m.View(); !strings.Contains(out, "No projects match") {
		t.Fatalf("an empty filter result should say so, got:\n%s", out)
	}
}

var errBoom = errBoomType{}

type errBoomType struct{}

func (errBoomType) Error() string { return "boom" }
