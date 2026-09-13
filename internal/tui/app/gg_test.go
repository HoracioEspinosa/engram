package app

import (
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// threeSelectorCards gives the selector something to jump across: a cursor
// at 0 has nowhere interesting to go with "g", so these tests need at least
// three rows to tell "G" (last) apart from "moved by one".
func threeSelectorCards() []store.ProjectCardListItem {
	return []store.ProjectCardListItem{
		{ProjectCard: store.ProjectCard{Slug: "nextcloud"}},
		{ProjectCard: store.ProjectCard{Slug: "middleware"}},
		{ProjectCard: store.ProjectCard{Slug: "portal"}},
	}
}

// TestGAndCapitalGJumpToTheEndsOfTheSelectorList pins rfc-tui.md §7.1's
// "g/G van al inicio y al fin de cada lista", applied to S1's project list —
// the root's own screen, not a tabs.Tab, so this is not covered by any
// per-tab Help/CapturingText test.
func TestGAndCapitalGJumpToTheEndsOfTheSelectorList(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	fake := &data.FakeProject{Cards: threeSelectorCards()}
	m.projects = fake
	m.selector = newSelectorModel(fake)
	m.screen = screenSelector
	m, _ = step(t, m, selectorLoadedMsg{cards: threeSelectorCards()})
	m.selector.cursor = 1

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if m.selector.cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (the last row)", m.selector.cursor)
	}

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if m.selector.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (the first row)", m.selector.cursor)
	}
}

// TestGAndCapitalGAreSuspendedByTheSelectorsFilterInput pins the same
// textinput-suspension rule as every tab's CapturingText: typing "g" or "G"
// into the filter box must reach the box, not the root's list-navigation
// handler. (A cursor assertion would be a false signal here: filtering to
// zero matches resets the cursor to 0 on its own, via applyFilter, whether
// or not "g" ever reached moveCursorToStart — the letter landing in the
// input is what actually distinguishes the two.)
func TestGAndCapitalGAreSuspendedByTheSelectorsFilterInput(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	fake := &data.FakeProject{Cards: threeSelectorCards()}
	m.projects = fake
	m.selector = newSelectorModel(fake)
	m.screen = screenSelector
	m, _ = step(t, m, selectorLoadedMsg{cards: threeSelectorCards()})
	m.selector.filterInput.Focus()

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if got := m.selector.filterInput.Value(); got != "g" {
		t.Fatalf("filter input = %q, want the letter to have reached it", got)
	}
}

// TestGAndCapitalGJumpToTheEndsOfTheDashboardBlocks pins the same rule
// applied to the Project Dashboard's three-block cursor (rfc-tui.md §5's
// S2: recent tasks, stale runbooks, latest evidence).
func TestGAndCapitalGJumpToTheEndsOfTheDashboardBlocks(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.project = "nextcloud"
	m.screen = screenDashboard
	m.dashboard = newDashboardModel(nil, "nextcloud")
	m.dashboard.cursor = dashBlockRunbooks

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if m.dashboard.cursor != dashBlockEvidence {
		t.Fatalf("cursor = %v, want the last block (evidence)", m.dashboard.cursor)
	}

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if m.dashboard.cursor != dashBlockTasks {
		t.Fatalf("cursor = %v, want the first block (tasks)", m.dashboard.cursor)
	}
}
