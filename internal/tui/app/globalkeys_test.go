package app

import (
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs/tasks"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// TestDigitKeysSwitchTabsAndRefresh pins §6.9's bar: each digit activates the
// tab that owns that slot and triggers its Refresh(), from any other tab. The
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
// TestShiftTabGoesToThePreviousRegisteredTab pin rfc-tui.md §7.1's
// "Tab / Shift+Tab | Pestaña siguiente / anterior", cycling through
// registered (Home, Memory, Tasks, Evidence, Benchmarks, Runbooks,
// Graph, Settings) and wrapping at
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

// TestDigitKeysAreSuspendedWhileTheActiveTabIsCapturingText pins rfc-tui.md
// §7.1: "cuando un textinput tiene el foco... las teclas globales se
// suspenden". A digit typed into the Tasks search box must reach the input,
// never switch tabs.
func TestDigitKeysAreSuspendedWhileTheActiveTabIsCapturingText(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.tree.open = false // T-10.02: New alone no longer guarantees this
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
	m.tree.open = false // T-10.02: New alone no longer guarantees this
	m.active = tabs.Runbooks
	m.runbooks.Searching = true
	m.runbooks.SearchInput.Focus()

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.active != tabs.Runbooks {
		t.Fatalf("active = %v, want Runbooks: Tab must not fire while the search box is focused", m.active)
	}
}

// TestDigitKeysDoNothingOnTheProjectTree pins that rfc-tui.md §7.3's
// navigation diagram draws no edge from S1 through a digit: the overlay has
// the keyboard while it is open, so digits are inert there instead of
// switching to a tab nobody can see.
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

// The following five tests are the "no new key steals one a screen already
// used" check rfc-tui.md's acceptance criterion asks for, one case per tab:
// none of the five uses a digit locally (measured with `rg -n 'case "'
// internal/tui/tabs/*/update.go` — see this task's report), so the digit
// must always resolve to a tab switch and never leak into the active tab's
// own handling.

func TestDigitDoesNotLeakIntoMemorysOwnHandling(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.tree.open = false // T-10.02: New alone no longer guarantees this
	m.active = tabs.Memory

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	if m.active != tabs.Evidence {
		t.Fatalf("active = %v, want Evidence: Memory has no local use for \"3\"", m.active)
	}
}

func TestDigitDoesNotLeakIntoTasksOwnHandling(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.tree.open = false // T-10.02: New alone no longer guarantees this
	m.active = tabs.Tasks

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})
	if m.active != tabs.Runbooks {
		t.Fatalf("active = %v, want Runbooks: Tasks has no local use for \"5\"", m.active)
	}
}

func TestDigitDoesNotLeakIntoEvidencesOwnHandling(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.tree.open = false // T-10.02: New alone no longer guarantees this
	m.active = tabs.Evidence

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if m.active != tabs.Tasks {
		t.Fatalf("active = %v, want Tasks: Evidence has no local use for \"2\"", m.active)
	}
}

func TestDigitDoesNotLeakIntoRunbooksOwnHandling(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.tree.open = false // T-10.02: New alone no longer guarantees this
	m.active = tabs.Runbooks

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	if m.active != tabs.Memory {
		t.Fatalf("active = %v, want Memory: Runbooks has no local use for \"1\"", m.active)
	}
}

func TestDigitDoesNotLeakIntoCloudsOwnHandling(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.tree.open = false // T-10.02: New alone no longer guarantees this
	m.active = tabs.Settings

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if m.active != tabs.Tasks {
		t.Fatalf("active = %v, want Tasks: Cloud has no local use for \"2\"", m.active)
	}
}

// TestZeroAndPAreAlsoSuspendedWhileCapturingText pins a fix bundled with
// this task rather than invented separately for it: "0" and "p" were
// already global before this row (rfc-tui.md §7.1 predates it), but
// updateActive matched them with no textinput guard at all — unlike "r",
// which every tab's own Update already gates behind its own focus check
// before this task. Restructuring that same switch to add "1"…"5" made
// leaving "0"/"p" unguarded next to a guarded "2" indefensible, so both now
// share the guard. See this task's report for the literal `go test` output
// this reproduced before the fix.
func TestZeroAndPAreAlsoSuspendedWhileCapturingText(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.project = "nextcloud"
	m.tree.open = false // T-10.02: New alone no longer guarantees this
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
