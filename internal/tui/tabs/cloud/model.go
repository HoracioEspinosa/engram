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

// WithStyles returns a copy of m painted with styles instead of the default
// theme.New built it with — app.New calls this once, right after New, so
// the tab renders under the same resolved palette as the workspace chrome
// around it (rfc-tui.md §8.2's --theme / ENGRAM_TUI_THEME / tui.theme).
func (m Model) WithStyles(styles theme.Styles) Model {
	m.styles = styles
	return m
}

// Styles exposes the tab's current style set for app-level tests that
// assert every tab paints with the same resolved palette instead of its own
// default.
func (m Model) Styles() theme.Styles { return m.styles }

// Title is the label the tab bar shows for this tab.
func (Model) Title() string { return "Cloud" }

// Init has nothing to load: the menu is static.
func (Model) Init() tea.Cmd { return nil }

// Refresh has nothing to reload.
func (Model) Refresh() tea.Cmd { return nil }
