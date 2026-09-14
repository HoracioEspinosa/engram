package app

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

// rootHelpKeyMap adapts two plain []key.Binding slices — the chrome's own
// global bindings and whichever screen is active's own — into help.KeyMap,
// so bubbles/help's FullHelpView renders them as two columns instead of one
// flat list.
type rootHelpKeyMap struct {
	global []key.Binding
	screen []key.Binding
}

func (k rootHelpKeyMap) ShortHelp() []key.Binding {
	return append(append([]key.Binding{}, k.global...), k.screen...)
}

func (k rootHelpKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.global, k.screen}
}

// selectorHelp lists S1's own bindings (rfc-tui.md §7.2). Both the "?"
// overlay and the footer are rendered from it, so what the screen answers to
// and what it advertises are one declaration.
func selectorHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("j/k", "move")),
		key.NewBinding(key.WithKeys("g", "G"), key.WithHelp("g/G", "top/bottom")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
		key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "sort by health")),
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	}
}

// dashboardHelp lists S2's own bindings (rfc-tui.md §7.2). The blocks are
// stacked, so only the vertical pair moves between them: h and l are reserved
// for horizontal focus and do nothing here.
func dashboardHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("j/k", "move block")),
		key.NewBinding(key.WithKeys("g", "G"), key.WithHelp("g/G", "top/bottom")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open block")),
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	}
}

// activeScreenHelp returns whichever screen is on display's own bindings:
// the Selector's and the Dashboard's own lists for those two root screens,
// or the active tab's Help() for every other one (rfc-tui.md §7.1: "lista
// los atajos que esa pantalla declara, no una lista fija").
func (m Model) activeScreenHelp() []key.Binding {
	switch m.screen {
	case screenSelector:
		return selectorHelp()
	case screenDashboard:
		return dashboardHelp()
	default:
		if tab := m.tab(m.active); tab != nil {
			return tab.Help()
		}
		return nil
	}
}

// viewHelpOverlay renders the "?" overlay (rfc-tui.md §7.1) with
// bubbles/help: the chrome's own global bindings alongside whichever screen
// is active's own, styled from the resolved palette rather than a default
// one.
func (m Model) viewHelpOverlay() string {
	h := help.New()
	h.ShowAll = true
	h.Styles.FullKey = m.styles.TabActive
	h.Styles.FullDesc = m.styles.DetailValue
	h.Styles.FullSeparator = m.styles.Help

	km := rootHelpKeyMap{global: globalHelpBindings(), screen: m.activeScreenHelp()}

	return m.styles.Title.Render("Help") + "\n" +
		h.View(km) + "\n" +
		m.styles.Help.Render("  ? / esc / q  close")
}
