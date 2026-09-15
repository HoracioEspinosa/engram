package app

import (
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// threeTreeNodes gives the project tree something to jump across: a cursor
// at 0 has nowhere interesting to go with "g", so these tests need at least
// three rows to tell "G" (last) apart from "moved by one".
func threeTreeNodes() []data.ProjectNode {
	return []data.ProjectNode{
		{Slug: "nextcloud"},
		{Slug: "middleware"},
		{Slug: "portal"},
	}
}

// TestGAndCapitalGJumpToTheEndsOfTheProjectTree pins that "g" and "G" go to
// the start and the end of a list, applied to the ctrl+p tree — the root's
// own overlay, not a tabs.Tab, so this is not covered by any per-tab
// Help/CapturingText test.
func TestGAndCapitalGJumpToTheEndsOfTheProjectTree(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m = m.WithProjectTree(&data.FakeProjectTree{Tree: threeTreeNodes()})
	m, _ = step(t, m, treeLoadedMsg{roots: threeTreeNodes()})
	m.tree.cursor = 1

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if m.tree.cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (the last row)", m.tree.cursor)
	}

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if m.tree.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (the first row)", m.tree.cursor)
	}
}

// TestGAndCapitalGAreSuspendedByTheProjectTreesFilterInput pins the same
// textinput-suspension rule as every tab's CapturingText: typing "g" or "G"
// into the filter box must reach the box, not the overlay's list-navigation
// handler. (A cursor assertion would be a false signal here: filtering to
// zero matches clamps the cursor to 0 on its own, whether or not "g" ever
// reached moveCursorToStart — the letter landing in the input is what
// actually distinguishes the two.)
func TestGAndCapitalGAreSuspendedByTheProjectTreesFilterInput(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m = m.WithProjectTree(&data.FakeProjectTree{Tree: threeTreeNodes()})
	m, _ = step(t, m, treeLoadedMsg{roots: threeTreeNodes()})
	m.tree.filterInput.Focus()

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if got := m.tree.filterInput.Value(); got != "g" {
		t.Fatalf("filter input = %q, want the letter to have reached it", got)
	}
}
