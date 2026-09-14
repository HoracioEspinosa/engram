package app

import "github.com/charmbracelet/bubbles/key"

// globalKeyMap holds the bindings the root consumes before the active tab is
// offered a key (rfc-tui.md §7.1). Every field but Quit is suspended while
// the active tab reports CapturingText() — see updateActive.
type globalKeyMap struct {
	// Quit leaves the TUI from anywhere, including a focused text input.
	Quit key.Binding
	// ProjectSelector opens S1. It lives on a modifier because the letter
	// keys belong to the screens: "p" is a page key in a paginated list and
	// the copy-path action on Evidence's detail, and a global that moved
	// between screens to make room was a rule nobody could remember.
	ProjectSelector key.Binding
	// Dashboard opens S2, the active project's Project Dashboard.
	Dashboard key.Binding
	// SwitchTab activates Memory, Tasks, Evidence, Runbooks or Cloud
	// directly by digit.
	SwitchTab key.Binding
	// NextTab and PrevTab cycle registered, wrapping at either end.
	NextTab key.Binding
	PrevTab key.Binding
	// Refresh reloads the active screen.
	Refresh key.Binding
	// Help toggles the "?" overlay (rfc-tui.md §7.1), built from
	// globalHelpBindings plus the active screen's own Help().
	Help key.Binding
}

var globalKeys = globalKeyMap{
	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
	ProjectSelector: key.NewBinding(
		key.WithKeys("ctrl+p"),
		key.WithHelp("ctrl+p", "project"),
	),
	Dashboard: key.NewBinding(
		key.WithKeys("0"),
		key.WithHelp("0", "dashboard"),
	),
	SwitchTab: key.NewBinding(
		key.WithKeys("1", "2", "3", "4", "5"),
		key.WithHelp("1-5", "tabs"),
	),
	NextTab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "next tab"),
	),
	PrevTab: key.NewBinding(
		key.WithKeys("shift+tab"),
		key.WithHelp("shift+tab", "previous tab"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
}

// globalHelpBindings is what the "?" overlay shows for every screen,
// regardless of which one is active — the chrome rfc-tui.md §7.1 owns, as
// opposed to whatever the active screen's own Help() adds on top.
func globalHelpBindings() []key.Binding {
	return []key.Binding{
		globalKeys.SwitchTab,
		globalKeys.NextTab,
		globalKeys.Dashboard,
		globalKeys.ProjectSelector,
		globalKeys.Refresh,
		globalKeys.Help,
		globalKeys.Quit,
	}
}
