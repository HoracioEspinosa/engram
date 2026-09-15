package app

import (
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// Settings keys the workspace remembers between runs.
//
// They are spelled here and again in cmd/engram, the way tui.theme already
// is: internal/tui is reached through one facade, and a settings key is not
// part of that facade's surface. The pair that reads them opens the workspace;
// the pair that writes them is here, where the change actually happens.
const (
	lastProjectSettingKey = "tui.last_project"
	lastTabSettingKey     = "tui.last_tab"
)

// settingsWriter is where the root writes what it should reopen on.
//
// It is the writer the theme picker was bound to, and deliberately not a
// second field of its own: cmd/engram opens one store and derives one settings
// surface over it, so a second field would be the same writer under another
// name and one more thing to forget to wire.
func (m Model) settingsWriter() data.SettingsWriter { return m.themePicker.settings }

// WithLastTab reopens the workspace on the tab a previous run left it on.
//
// An empty or unrecognised name leaves the workspace where New put it. A build
// that registers no tab for the remembered name is the interesting case: the
// name outlives the build that wrote it, so a workspace must never open on a
// tab it cannot draw.
func (m Model) WithLastTab(name string) Model {
	for _, id := range registered {
		if id.String() != name {
			continue
		}
		if m.tab(id) == nil {
			return m
		}
		m.active = id
		return m
	}
	return m
}

// WithIcons repaints the workspace in an icon vocabulary — the one
// theme.ResolveIconMode picked from the environment, the remembered setting
// and what the terminal can be trusted to draw.
//
// It goes through withStyles like a change of palette does, because it is one:
// a tab that missed the fan-out would keep drawing glyphs its terminal cannot
// render, which reads as a broken font rather than as a setting nobody
// applied.
func (m Model) WithIcons(mode theme.IconMode) Model {
	return m.withStyles(m.styles.WithIcons(mode))
}

// openTab activates target, records it as the tab to reopen on, and carries
// whatever command the caller was already issuing.
//
// The deep links into a tab set the active tab without going through
// activate(), so this is what keeps them from being the one way to change tabs
// that the workspace forgets.
func (m Model) openTab(target tabs.ID, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	m.active = target
	return m, tea.Batch(m.rememberTab(target), cmd)
}

// rememberTab returns the command that records target as the tab to reopen on.
//
// It is a command, never a write in Update: SetSetting goes to SQLite, and a
// synchronous write would stall the frame behind a disk that is busy every
// time the user pressed Tab.
func (m Model) rememberTab(target tabs.ID) tea.Cmd {
	return rememberSetting(m.settingsWriter(), lastTabSettingKey, target.String())
}

// rememberProject returns the command that records slug as the project to
// reopen on.
func (m Model) rememberProject(slug string) tea.Cmd {
	return rememberSetting(m.settingsWriter(), lastProjectSettingKey, slug)
}

// rememberSetting writes one remembered choice, off the frame.
//
// A failed write raises nothing. The workspace is already in exactly the state
// the user asked for — only the memory of it failed — and a notice on every
// tab switch would report a store problem five times a minute while saying
// nothing the next switch does not retry.
func rememberSetting(w data.SettingsWriter, key, value string) tea.Cmd {
	if w == nil || value == "" {
		return nil
	}
	return func() tea.Msg {
		_ = w.SetSetting(key, value)
		return nil
	}
}
