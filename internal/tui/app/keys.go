package app

import "github.com/charmbracelet/bubbles/key"

// globalKeyMap holds the bindings the root consumes before the active tab is
// offered a key. Every field but Quit is suspended while the active tab
// reports CapturingText() — see updateActive.
type globalKeyMap struct {
	// Quit leaves the TUI from anywhere, including a focused text input.
	Quit key.Binding
	// Search opens the workspace search palette. Like the tree it lives on a
	// modifier: "/" belongs to whichever screen has a search of its own, and
	// the palette borrows that key only where nothing else claims it.
	Search key.Binding
	// ProjectSelector opens the project tree. It lives on a modifier because
	// the letter keys belong to the screens: "p" is a page key in a paginated
	// list and the copy-path action on the evidence detail screen, and a
	// global that moved between screens to make room was a rule nobody could
	// remember.
	ProjectSelector key.Binding
	// SwitchTab activates any tab directly by its digit. Home is "0": it is
	// a tab like the rest, not a screen the root draws itself, so it needs
	// no binding of its own.
	SwitchTab key.Binding
	// NextTab and PrevTab cycle registered, wrapping at either end.
	NextTab key.Binding
	PrevTab key.Binding
	// Refresh reloads the active screen.
	Refresh key.Binding
	// ThemePicker opens the theme overlay. It belongs here, alongside the
	// rest of the chrome's keys, because it works on every screen: the
	// overlay owns what happens once it is open, not the key that opens it.
	ThemePicker key.Binding
	// Help toggles the "?" overlay, built from globalHelpBindings plus the
	// active screen's own Help().
	Help key.Binding
}

var globalKeys = globalKeyMap{
	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
	Search: key.NewBinding(
		key.WithKeys("ctrl+k"),
		key.WithHelp("ctrl+k", "search"),
	),
	ProjectSelector: key.NewBinding(
		key.WithKeys("ctrl+p"),
		key.WithHelp("ctrl+p", "project"),
	),
	SwitchTab: key.NewBinding(
		key.WithKeys("0", "1", "2", "3", "4", "5", "6", "7"),
		key.WithHelp("0-7", "tabs"),
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
	ThemePicker: key.NewBinding(
		key.WithKeys("ctrl+t"),
		key.WithHelp("ctrl+t", "theme"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
}

// nativeSelection names the gesture that gets the terminal's own text
// selection back.
//
// Turning the mouse on takes it away: every drag becomes an event the program
// consumes, so a reader who wants to copy a path off the screen finds their
// usual gesture doing nothing. Every terminal worth the name still selects and
// copies on shift+drag, and that is only discoverable if somebody writes it
// down.
//
// It is declared as a binding so the "?" overlay lists it in the grid rather
// than in a line of prose of its own: the panel is composited over the screen
// it describes, and every row or column it grows by is part of that screen the
// reader loses. Both halves are kept inside the widths the grid already has.
// Nothing ever matches against it — no terminal emits "shift+drag" as a key,
// and the pointer arrives as a tea.MouseMsg — which is why it is not a field
// of globalKeyMap.
var nativeSelection = key.NewBinding(
	key.WithKeys("shift+drag"),
	key.WithHelp("shift", "select"),
)

// globalHelpBindings is what the "?" overlay shows for every screen,
// regardless of which one is active — the chrome the root owns, as opposed
// to whatever the active screen's own Help() adds on top.
func globalHelpBindings() []key.Binding {
	return []key.Binding{
		globalKeys.SwitchTab,
		globalKeys.NextTab,
		globalKeys.Search,
		globalKeys.ProjectSelector,
		globalKeys.Refresh,
		globalKeys.ThemePicker,
		nativeSelection,
		globalKeys.Help,
		globalKeys.Quit,
	}
}
