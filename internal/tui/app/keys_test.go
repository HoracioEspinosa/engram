package app

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs/evidence"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// keyMsg turns a binding's first key into the message a terminal would send.
func keyMsg(k string) tea.KeyMsg {
	switch k {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "ctrl+p":
		return tea.KeyMsg{Type: tea.KeyCtrlP}
	case "ctrl+t":
		return tea.KeyMsg{Type: tea.KeyCtrlT}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// onTab returns a model sitting on a tab with a project already selected, the
// state every global binding is meant to work from.
func onTab(t *testing.T, active tabs.ID) Model {
	t.Helper()
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	next, _ := m.openProject("nextcloud")
	m = next.(Model)
	m.tree.open = false
	m.active = active
	return m
}

// TestEveryGlobalKeyIsRoutedThroughTheKeymap pins the reason globalKeys
// exists: each binding is answered by matching against it, so changing a key
// there changes the behaviour, the "?" overlay and the footer at once. A
// binding the root declares but never matches is a key that does nothing.
func TestEveryGlobalKeyIsRoutedThroughTheKeymap(t *testing.T) {
	cases := []struct {
		name    string
		binding key.Binding
		assert  func(t *testing.T, before, after Model, cmd tea.Cmd)
	}{
		{
			name:    "quit",
			binding: globalKeys.Quit,
			assert: func(t *testing.T, _, _ Model, cmd tea.Cmd) {
				if cmd == nil {
					t.Fatal("quit should return a command")
				}
			},
		},
		{
			name:    "project tree",
			binding: globalKeys.ProjectSelector,
			assert: func(t *testing.T, _, after Model, cmd tea.Cmd) {
				if !after.tree.open {
					t.Fatal("ctrl+p should open the project tree overlay")
				}
				if cmd == nil {
					t.Fatal("opening the project tree should load the forest")
				}
			},
		},
		{
			name:    "switch tab",
			binding: globalKeys.SwitchTab,
			assert: func(t *testing.T, before, after Model, _ tea.Cmd) {
				// Every digit is one slot of the bar. It either activates the
				// tab behind that slot or, for a slot this build implements
				// no tab for, is swallowed; what it must never do is reach
				// the tab on screen.
				if after.active == before.active {
					return
				}
				for _, id := range digitTabs {
					if after.active == id {
						return
					}
				}
				t.Fatalf("a digit moved the workspace to %v, which no bar slot names", after.active)
			},
		},
		{
			name:    "next tab",
			binding: globalKeys.NextTab,
			assert: func(t *testing.T, before, after Model, _ tea.Cmd) {
				if after.active == before.active {
					t.Fatalf("active tab did not move from %v", before.active)
				}
			},
		},
		{
			name:    "previous tab",
			binding: globalKeys.PrevTab,
			assert: func(t *testing.T, before, after Model, _ tea.Cmd) {
				if after.active == before.active {
					t.Fatalf("active tab did not move from %v", before.active)
				}
			},
		},
		{
			name:    "theme picker",
			binding: globalKeys.ThemePicker,
			assert: func(t *testing.T, _, after Model, _ tea.Cmd) {
				if !after.themePicker.open {
					t.Fatal("the theme overlay should be open")
				}
			},
		},
		{
			name:    "help",
			binding: globalKeys.Help,
			assert: func(t *testing.T, _, after Model, _ tea.Cmd) {
				if !after.showHelp {
					t.Fatal("the help overlay should be open")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keys := tc.binding.Keys()
			if len(keys) == 0 {
				t.Fatalf("%s declares no key at all", tc.name)
			}
			for _, k := range keys {
				before := onTab(t, tabs.Tasks)
				after, cmd := step(t, before, keyMsg(k))
				tc.assert(t, before, after, cmd)
			}
		})
	}
}

// TestRefreshReachesTheActiveTab pins the binding the root declared and never
// used: "r" was reimplemented once per screen and never intercepted here, so
// the keymap said one thing and five screens each did their own.
func TestRefreshReachesTheActiveTab(t *testing.T) {
	for _, id := range []tabs.ID{tabs.Memory, tabs.Tasks, tabs.Evidence, tabs.Runbooks} {
		t.Run(id.String(), func(t *testing.T) {
			m := onTab(t, id)
			_, cmd := step(t, m, keyMsg(globalKeys.Refresh.Keys()[0]))
			if cmd == nil {
				t.Fatalf("refresh on the %s tab issued no reload", id)
			}
		})
	}
}

// TestRefreshReloadsTheDashboardAndTheProjectTree covers the two surfaces the
// root draws itself: they answer the same binding, not a literal of their own.
func TestRefreshReloadsTheDashboardAndTheProjectTree(t *testing.T) {
	refresh := keyMsg(globalKeys.Refresh.Keys()[0])

	m := onTab(t, tabs.Tasks)
	m.tree.open, m.active = false, tabs.Home
	if _, cmd := step(t, m, refresh); cmd == nil {
		t.Fatal("refresh on Home issued no reload")
	}

	m = onTab(t, tabs.Tasks)
	m.tree.open = true
	if _, cmd := step(t, m, refresh); cmd == nil {
		t.Fatal("refresh on the project tree issued no reload")
	}
}

// TestLowercasePNoLongerOpensTheSelector pins the move to ctrl+p. "p" is a
// page key in a paginated list and the copy-path action on Evidence's detail;
// a global that swapped itself to "P" on one screen to make room was a rule
// nobody could remember.
func TestLowercasePNoLongerOpensTheSelector(t *testing.T) {
	for _, k := range []string{"p", "P"} {
		t.Run(k, func(t *testing.T) {
			m, _ := step(t, onTab(t, tabs.Tasks), keyMsg(k))
			if m.tree.open {
				t.Fatalf("%q still opens the project tree", k)
			}
		})
	}
}

// TestLowercasePReachesEvidenceDetail is the other half: with the global out
// of the way, the screen's own "p" works without the swap.
func TestLowercasePReachesEvidenceDetail(t *testing.T) {
	m := onTab(t, tabs.Evidence)
	m.evidence.Screen = evidence.ScreenDetail

	after, _ := step(t, m, keyMsg("p"))
	if after.tree.open {
		t.Fatal("p on the evidence detail opened the project tree")
	}
}

// TestFooterMatchesHelp walks every screen the root can draw and checks that
// the footer says nothing the screen has not declared in Help(). The eight
// hand-written footers this replaces had already drifted: they named keys the
// screens no longer answered and omitted ones they did.
func TestFooterMatchesHelp(t *testing.T) {
	screens := []struct {
		name  string
		build func() Model
	}{
		{"project tree", func() Model { m := onTab(t, tabs.Memory); m.tree.open = true; return m }},
		{"home", func() Model { m := onTab(t, tabs.Memory); m.tree.open, m.active = false, tabs.Home; return m }},
		{"memory", func() Model { return onTab(t, tabs.Memory) }},
		{"tasks", func() Model { return onTab(t, tabs.Tasks) }},
		{"evidence", func() Model { return onTab(t, tabs.Evidence) }},
		{"runbooks", func() Model { return onTab(t, tabs.Runbooks) }},
		{"settings", func() Model { return onTab(t, tabs.Settings) }},
	}

	for _, sc := range screens {
		t.Run(sc.name, func(t *testing.T) {
			m := sc.build()
			bindings := m.activeScreenHelp()
			if len(bindings) == 0 {
				t.Skip("this screen declares no bindings of its own")
			}

			footer := ansi.Strip(shared.HintsFrom(m.styles, bindings, 0))
			line := strings.TrimSpace(footer[strings.LastIndex(footer, "\n")+1:])
			if line == "" {
				t.Fatal("screen declares bindings but renders no footer")
			}

			declared := make(map[string]bool, len(bindings))
			for _, b := range bindings {
				h := b.Help()
				if h.Key == "" || h.Desc == "" {
					continue
				}
				declared[h.Key+" "+h.Desc] = true
			}

			for _, hint := range strings.Split(line, "•") {
				hint = strings.TrimSpace(hint)
				if !declared[hint] {
					t.Fatalf("footer hint %q is not declared in Help()", hint)
				}
			}
		})
	}
}

// TestEveryScreenRendersItsFooter pins the frame's half of the contract: the
// root draws the footer, so no screen can lose it by forgetting to print one.
func TestEveryScreenRendersItsFooter(t *testing.T) {
	for _, id := range []tabs.ID{tabs.Memory, tabs.Tasks, tabs.Evidence, tabs.Runbooks} {
		t.Run(id.String(), func(t *testing.T) {
			m := onTab(t, id)
			m.width = 120

			view := ansi.Strip(m.View())
			first := m.activeScreenHelp()[0].Help()
			if !strings.Contains(view, first.Key+" "+first.Desc) {
				t.Fatalf("the %s tab renders no footer:\n%s", id, view)
			}
		})
	}
}
