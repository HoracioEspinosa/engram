package app

import (
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// TestQAndEscGoHomeFromEveryTabsRootScreen pins the generalized "q"/"Esc":
// on a tab's root screen either key returns to Home. Tasks, Evidence,
// Runbooks and Settings each route their root screen's "esc"/"q" through
// tabs.Home(), and this holds that contract against changes to how
// updateActive dispatches around CapturingText.
func TestQAndEscGoHomeFromEveryTabsRootScreen(t *testing.T) {
	cases := []struct {
		name   string
		active tabs.ID
		key    string
	}{
		{"tasks-list-q", tabs.Tasks, "q"},
		{"tasks-list-esc", tabs.Tasks, "esc"},
		{"evidence-list-q", tabs.Evidence, "q"},
		{"evidence-list-esc", tabs.Evidence, "esc"},
		{"runbooks-index-q", tabs.Runbooks, "q"},
		{"runbooks-index-esc", tabs.Runbooks, "esc"},
		{"settings-list-q", tabs.Settings, "q"},
		{"settings-list-esc", tabs.Settings, "esc"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
			m.project = "nextcloud"
			m.active = tc.active
			// New opens the project tree when no project resolves; this
			// case's premise is being on tc.active's root screen already,
			// so that is set explicitly rather than relied on as a default.
			m.tree.open = false

			var msg tea.KeyMsg
			if tc.key == "esc" {
				msg = tea.KeyMsg{Type: tea.KeyEsc}
			} else {
				msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}
			}

			m, cmd := step(t, m, msg)
			if cmd == nil {
				t.Fatalf("%v's root screen should emit tabs.Home() on %q", tc.active, tc.key)
			}
			m, _ = step(t, m, cmd())
			if m.active != tabs.Home {
				t.Fatalf("active = %v, want Home: a project is active, so home is its own tab", m.active)
			}
		})
	}
}

// TestQDoesNotQuitFromAnOverlay pins that "q" belongs to the screen, not to
// the chrome. Home answers neither "q" nor "esc": it is where the other
// tabs send the reader back to, so there is nothing further back to go.
func TestQDoesNotQuitFromAnOverlay(t *testing.T) {
	// The project tree overlay does not answer "q": with it open the letter is
	// a filter candidate, and quitting from the one screen that can give the
	// workspace a project would strand the reader.
	tree := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	if _, cmd := step(t, tree, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}); cmd != nil {
		t.Fatal("q on the project tree should not quit: esc closes the overlay and ctrl+c leaves")
	}
}

// TestMemorysOwnDashboardQuitsDirectlyNotHome documents a deliberate
// exception rather than a bug: the Memory tab's own ScreenDashboard lists
// "Quit" as its sixth menu item, so "q" there is that menu shortcut and not
// the chrome's generalized "back one level". Folding it into tabs.Home()
// would repurpose a menu action the chrome does not own.
func TestMemorysOwnDashboardQuitsDirectlyNotHome(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.project = "nextcloud"
	m.active = tabs.Memory
	// New opens the project tree without a resolvable project; this case's
	// premise is being on Memory's own dashboard already, so that is set
	// explicitly rather than relied on as New's default.
	m.tree.open = false
	m.tree.open = false

	if _, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}); cmd == nil {
		t.Fatal("q on Memory's own dashboard should still quit directly, unchanged by this task")
	}
}
