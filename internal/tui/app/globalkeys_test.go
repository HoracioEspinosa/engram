package app

import (
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs/tasks"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// TestDigitKeysSwitchTabsAndRefresh pins the tab bar: each digit activates
// the tab that owns that slot and triggers its Refresh(), from any other tab. The
// slots this build implements no tab for ("4", "6", "7") are covered by
// TestDigitKeysForAnUnimplementedSlotStayPut.
func TestDigitKeysSwitchTabsAndRefresh(t *testing.T) {
	cases := []struct {
		digit string
		want  tabs.ID
	}{
		{"0", tabs.Home},
		{"1", tabs.Memory},
		{"2", tabs.Tasks},
		{"3", tabs.Evidence},
		{"4", tabs.Benchmarks},
		{"5", tabs.Runbooks},
		{"6", tabs.Graph},
		{"7", tabs.Settings},
	}

	for _, tc := range cases {
		t.Run(tc.digit, func(t *testing.T) {
			m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
			// New opens the project tree without a resolvable project; every
			// case here assumes it is already on a tab.
			m = scoped(t, m, "nextcloud")
			// Start on a tab other than the target so the assertion means
			// something even for a digit that names the tab already active.
			m.active = tabs.Settings

			m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.digit)})
			if m.active != tc.want {
				t.Fatalf("active = %v, want %v", m.active, tc.want)
			}
			// Settings reads nothing: every row of it reads its own source
			// when drawn, so arriving there issues no load.
			if cmd == nil && tc.want != tabs.Settings {
				t.Fatal("switching tabs should reload the target")
			}
		})
	}
}

// TestDigitKeysSwitchTabsFromHomeToo pins that Home is not special: the
// digits work from it exactly like they do from any other tab.
func TestDigitKeysSwitchTabsFromHomeToo(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.project = "nextcloud"
	m.tree.open, m.active = false, tabs.Home

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	if m.active != tabs.Evidence {
		t.Fatalf("active = %v, want Evidence", m.active)
	}
	if cmd == nil {
		t.Fatal("switching tabs from Home should reload the target")
	}
}

// TestADigitOutsideTheBarIsSwallowed pins the other half of the bar's
// contract: the digits it owns are its own, and one it does not own reaches
// nothing at all rather than leaking into the tab on screen.
func TestADigitOutsideTheBarIsSwallowed(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.tree.open = false
	m.active = tabs.Tasks

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("9")})
	if m.active != tabs.Tasks {
		t.Fatalf("active = %v, want Tasks: \"9\" names no slot of the bar", m.active)
	}
	if cmd != nil {
		t.Fatal("a digit outside the bar should produce no command")
	}
}

// TestTabKeyAdvancesToTheNextRegisteredTab and
// TestShiftTabGoesToThePreviousRegisteredTab pin Tab as the next tab and
// Shift+Tab as the previous one, cycling through registered (Home, Memory,
// Tasks, Evidence, Benchmarks, Runbooks, Graph, Settings) and wrapping at
// either end.
func TestTabKeyAdvancesToTheNextRegisteredTab(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m = scoped(t, m, "nextcloud")
	m.active = tabs.Settings // last in registered order

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.active != tabs.Home {
		t.Fatalf("active = %v, want Home (wrapping past Settings)", m.active)
	}
	if cmd == nil {
		t.Fatal("advancing tabs should reload the target")
	}
}

func TestShiftTabGoesToThePreviousRegisteredTab(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.tree.open = false
	m.active = tabs.Home // first in registered order

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.active != tabs.Settings {
		t.Fatalf("active = %v, want Settings (wrapping before Home)", m.active)
	}
	// Settings reads nothing, so there is no command to assert on here.
}

// TestDigitKeysAreSuspendedWhileTheActiveTabIsCapturingText pins that a
// focused textinput suspends the global keys. A digit typed into the Tasks
// search box must reach the input, never switch tabs.
func TestDigitKeysAreSuspendedWhileTheActiveTabIsCapturingText(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.tree.open = false // New opens the tree when no project resolves
	m.active = tabs.Tasks
	m.tasks.Searching = true
	m.tasks.SearchInput.Focus()

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if m.active != tabs.Tasks {
		t.Fatalf("active = %v, want Tasks: a digit typed into the search box must not switch tabs", m.active)
	}
	if got := m.tasks.SearchInput.Value(); got != "2" {
		t.Fatalf("tasks search input = %q, want the digit to have reached it", got)
	}
}

// TestTabKeyIsSuspendedWhileTheActiveTabIsCapturingText pins the same
// suspension rule for Tab/Shift+Tab: unlike a digit, Tab has no textual
// value to type, but it must still not steal focus mid-search.
func TestTabKeyIsSuspendedWhileTheActiveTabIsCapturingText(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.tree.open = false // New opens the tree when no project resolves
	m.active = tabs.Runbooks
	m.runbooks.Searching = true
	m.runbooks.SearchInput.Focus()

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.active != tabs.Runbooks {
		t.Fatalf("active = %v, want Runbooks: Tab must not fire while the search box is focused", m.active)
	}
}

// TestDigitKeysDoNothingOnTheProjectTree pins that no digit leads out of the
// project tree: the overlay has the keyboard while it is open, so digits are
// inert there instead of switching to a tab nobody can see.
func TestDigitKeysDoNothingOnTheProjectTree(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.tree.open = true

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if !m.tree.open {
		t.Fatal("a digit should not close the project tree")
	}
	if cmd != nil {
		t.Fatal("a digit on the project tree should not produce a command")
	}
}

// The following five tests check that no global key steals one a screen
// already uses, one case per tab: none of the five uses a digit locally
// (measured with `rg -n 'case "' internal/tui/tabs/*/update.go`), so the
// digit must always resolve to a tab switch and never leak into the active
// tab's own handling.

func TestDigitDoesNotLeakIntoMemorysOwnHandling(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.tree.open = false // New opens the tree when no project resolves
	m.active = tabs.Memory

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	if m.active != tabs.Evidence {
		t.Fatalf("active = %v, want Evidence: Memory has no local use for \"3\"", m.active)
	}
}

func TestDigitDoesNotLeakIntoTasksOwnHandling(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.tree.open = false // New opens the tree when no project resolves
	m.active = tabs.Tasks

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})
	if m.active != tabs.Runbooks {
		t.Fatalf("active = %v, want Runbooks: Tasks has no local use for \"5\"", m.active)
	}
}

func TestDigitDoesNotLeakIntoEvidencesOwnHandling(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.tree.open = false // New opens the tree when no project resolves
	m.active = tabs.Evidence

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if m.active != tabs.Tasks {
		t.Fatalf("active = %v, want Tasks: Evidence has no local use for \"2\"", m.active)
	}
}

func TestDigitDoesNotLeakIntoRunbooksOwnHandling(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.tree.open = false // New opens the tree when no project resolves
	m.active = tabs.Runbooks

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	if m.active != tabs.Memory {
		t.Fatalf("active = %v, want Memory: Runbooks has no local use for \"1\"", m.active)
	}
}

func TestDigitDoesNotLeakIntoSettingsOwnHandling(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.tree.open = false // New opens the tree when no project resolves
	m.active = tabs.Settings

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if m.active != tabs.Tasks {
		t.Fatalf("active = %v, want Tasks: Cloud has no local use for \"2\"", m.active)
	}
}

// TestZeroAndPAreAlsoSuspendedWhileCapturingText pins that the global "0"
// and "p" share the textinput guard with every other digit: a focused search
// box keeps them all, so neither a tab switch nor the project tree fires
// while the user is typing.
func TestZeroAndPAreAlsoSuspendedWhileCapturingText(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.project = "nextcloud"
	m.tree.open = false // New opens the tree when no project resolves
	m.active = tabs.Tasks
	m.tasks.Searching = true
	m.tasks.SearchInput.Focus()

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("0")})
	if m.active == tabs.Home {
		t.Fatal("\"0\" typed into a focused search box must not switch to Home")
	}
	if got := m.tasks.SearchInput.Value(); got != "0" {
		t.Fatalf("tasks search input = %q, want the digit to have reached it", got)
	}

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if m.tree.open {
		t.Fatal("\"p\" typed into a focused search box must not open the project tree")
	}
	if got := m.tasks.SearchInput.Value(); got != "0p" {
		t.Fatalf("tasks search input = %q, want \"p\" to have reached it too", got)
	}
}

var _ = tasks.Model{} // keep the import honest if the cases above shrink
