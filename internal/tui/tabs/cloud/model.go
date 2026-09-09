// Package cloud is the Cloud workspace tab: the sync configuration menu.
//
// It reads nothing and writes nothing yet — the menu items are the entry
// points the cloud sync work will hang off. What it does own is its own
// cursor, so switching tabs no longer perturbs the Memory tab's selection.
package cloud

import (
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// menuItems are the cloud sync entry points, in display order. "Back" is last
// so the escape hatch sits where every other menu in the TUI puts it.
var menuItems = []string{
	"Configure server",
	"View status",
	"Enroll projects",
	"Back",
}

// Model is the Cloud tab's state.
type Model struct {
	styles theme.Styles

	// Cursor is the highlighted menu item.
	Cursor int
}

// New creates the Cloud tab.
func New() Model {
	return Model{styles: theme.Default()}
}

// Title is the label the tab bar shows for this tab.
func (Model) Title() string { return "Cloud" }

// Init has nothing to load: the menu is static.
func (Model) Init() tea.Cmd { return nil }

// Refresh has nothing to reload.
func (Model) Refresh() tea.Cmd { return nil }
