package app

import (
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs/tasks"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// TestDigitKeysSwitchTabsAndRefresh pins rfc-tui.md §7.1: "1"…"5" activate
// Memory, Tasks, Evidence, Runbooks and Cloud respectively and trigger the
// target's Refresh(), from any other tab.
func TestDigitKeysSwitchTabsAndRefresh(t *testing.T) {
	cases := []struct {
		digit string
		want  tabs.ID
	}{
		{"1", tabs.Memory},
		{"2", tabs.Tasks},
		{"3", tabs.Evidence},
		{"4", tabs.Runbooks},
		{"5", tabs.Cloud},
	}

	for _, tc := range cases {
		t.Run(tc.digit, func(t *testing.T) {
			m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
			// New now opens the project tree without a resolvable project
			// (T-10.02); every case here assumes it is already on a tab.
			m.screen, m.tree.open = screenTab, false
			// Start on a different tab than the target so the assertion means
			// something even for "1" (already Memory's own digit).
			m.active = tabs.Cloud
			if tc.want == tabs.Cloud {
				m.active = tabs.Memory
			}

			m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.digit)})
			if m.active != tc.want {
				t.Fatalf("active = %v, want %v", m.active, tc.want)
			}
			if m.screen != screenTab {
				t.Fatalf("screen = %v, want screenTab", m.screen)
			}
			// Cloud's Refresh() has nothing to reload (its own menu is
			// static — TestNavigateSwitchesTabsAndRefreshesTheTarget pins
			// the same nil for NavigateMsg), so only the other four tabs
			// are expected to issue a load.
			if cmd == nil && tc.want != tabs.Cloud {
				t.Fatal("switching tabs should reload the target")
			}
		})
	}
}

// TestDigitKeysSwitchTabsFromTheDashboardToo pins the Dashboard's own footer
// (rfc-tui.md §5's S2 wireframe: "1-5 tabs"): the digits work from the
// Project Dashboard exactly like they do from any tab.
func TestDigitKeysSwitchTabsFromTheDashboardToo(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.project = "nextcloud"
	m.screen, m.tree.open = screenDashboard, false

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	if m.active != tabs.Evidence || m.screen != screenTab {
		t.Fatalf("active = %v screen = %v, want Evidence/screenTab", m.active, m.screen)
	}
	if cmd == nil {
		t.Fatal("switching tabs from the dashboard should reload the target")
	}
}

// TestTabKeyAdvancesToTheNextRegisteredTab and
// TestShiftTabGoesToThePreviousRegisteredTab pin rfc-tui.md §7.1's
// "Tab / Shift+Tab | Pestaña siguiente / anterior", cycling through
// registered (Memory, Tasks, Evidence, Runbooks, Cloud) and wrapping at
// either end.
func TestTabKeyAdvancesToTheNextRegisteredTab(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen, m.tree.open = screenTab, false // T-10.02: New alone no longer guarantees this
	m.active = tabs.Cloud                    // last in registered order

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.active != tabs.Memory {
		t.Fatalf("active = %v, want Memory (wrapping past Cloud)", m.active)
	}
	if cmd == nil {
		t.Fatal("advancing tabs should reload the target")
	}
}

func TestShiftTabGoesToThePreviousRegisteredTab(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen, m.tree.open = screenTab, false // T-10.02: New alone no longer guarantees this
	m.active = tabs.Memory                   // first in registered order

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.active != tabs.Cloud {
		t.Fatalf("active = %v, want Cloud (wrapping before Memory)", m.active)
	}
	// Cloud's Refresh() has nothing to reload (see
	// TestDigitKeysSwitchTabsAndRefresh's "5" case), so no cmd assertion here.
}

// TestDigitKeysAreSuspendedWhileTheActiveTabIsCapturingText pins rfc-tui.md
// §7.1: "cuando un textinput tiene el foco... las teclas globales se
// suspenden". A digit typed into the Tasks search box must reach the input,
// never switch tabs.
func TestDigitKeysAreSuspendedWhileTheActiveTabIsCapturingText(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen, m.tree.open = screenTab, false // T-10.02: New alone no longer guarantees this
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
	m.screen, m.tree.open = screenTab, false // T-10.02: New alone no longer guarantees this
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
	m.screen, m.tree.open = screenTab, false // T-10.02: New alone no longer guarantees this
	m.active = tabs.Memory

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	if m.active != tabs.Evidence {
		t.Fatalf("active = %v, want Evidence: Memory has no local use for \"3\"", m.active)
	}
}

func TestDigitDoesNotLeakIntoTasksOwnHandling(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen, m.tree.open = screenTab, false // T-10.02: New alone no longer guarantees this
	m.active = tabs.Tasks

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
	if m.active != tabs.Runbooks {
		t.Fatalf("active = %v, want Runbooks: Tasks has no local use for \"4\"", m.active)
	}
}

func TestDigitDoesNotLeakIntoEvidencesOwnHandling(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen, m.tree.open = screenTab, false // T-10.02: New alone no longer guarantees this
	m.active = tabs.Evidence

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5")})
	if m.active != tabs.Cloud {
		t.Fatalf("active = %v, want Cloud: Evidence has no local use for \"5\"", m.active)
	}
}

func TestDigitDoesNotLeakIntoRunbooksOwnHandling(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen, m.tree.open = screenTab, false // T-10.02: New alone no longer guarantees this
	m.active = tabs.Runbooks

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	if m.active != tabs.Memory {
		t.Fatalf("active = %v, want Memory: Runbooks has no local use for \"1\"", m.active)
	}
}

func TestDigitDoesNotLeakIntoCloudsOwnHandling(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen, m.tree.open = screenTab, false // T-10.02: New alone no longer guarantees this
	m.active = tabs.Cloud

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
	m.screen, m.tree.open = screenTab, false // T-10.02: New alone no longer guarantees this
	m.active = tabs.Tasks
	m.tasks.Searching = true
	m.tasks.SearchInput.Focus()

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("0")})
	if m.screen == screenDashboard {
		t.Fatal("\"0\" typed into a focused search box must not switch to the dashboard")
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
