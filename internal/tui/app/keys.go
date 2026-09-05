package app

import "github.com/charmbracelet/bubbles/key"

// globalKeyMap holds the bindings the root consumes before the active tab is
// offered a key.
type globalKeyMap struct {
	// Quit leaves the TUI from anywhere, including a focused text input.
	Quit key.Binding
}

var globalKeys = globalKeyMap{
	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
}
