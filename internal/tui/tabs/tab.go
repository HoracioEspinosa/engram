// Package tabs declares the contract every workspace tab implements and the
// messages a tab uses to reach the root model.
//
// The root owns which tab is active and forwards messages to it; a tab never
// reaches into another tab's state. Cross-tab navigation travels as a
// NavigateMsg through the root, which is why this package holds no reference
// to any concrete tab.
package tabs

import tea "github.com/charmbracelet/bubbletea"

// ID identifies a workspace tab. The order is the order of the tab bar.
type ID int

const (
	// Memory is the observation, session and timeline workspace.
	Memory ID = iota
	// Tasks, Evidence and Runbooks are declared so navigation targets and the
	// tab-bar order are fixed once; the root reports an unimplemented target
	// rather than switching to it.
	Tasks
	Evidence
	Runbooks
	// Cloud is the sync configuration workspace.
	Cloud
)

// String names the tab for logs and test failures.
func (id ID) String() string {
	switch id {
	case Memory:
		return "memory"
	case Tasks:
		return "tasks"
	case Evidence:
		return "evidence"
	case Runbooks:
		return "runbooks"
	case Cloud:
		return "cloud"
	}
	return "unknown"
}

// Tab is an isolated Elm sub-model.
//
// Key messages arrive only while the tab is active, after the root has taken
// its global bindings. Every other message is broadcast to all tabs, so a load
// that finishes after the user switched tabs still reaches its owner; a tab
// ignores what it does not recognise.
//
// Terminal size arrives as a tea.WindowSizeMsg like any other broadcast
// message, so a tab that needs its geometry stores it in Update rather than
// receiving it as a View argument.
type Tab interface {
	// Init returns the command that loads the tab's first screen.
	Init() tea.Cmd
	// Update advances the tab and returns it; the concrete type is preserved
	// so the root can store it back into its typed field.
	Update(msg tea.Msg) (Tab, tea.Cmd)
	// View renders the tab body. The root wraps it in the application frame.
	View() string
	// Title is the label the tab bar shows.
	Title() string
	// Refresh reloads the data behind the current screen. The root calls it
	// when the tab becomes active.
	Refresh() tea.Cmd
}

// NavigateMsg asks the root to activate another tab. A tab emits it instead of
// switching itself, which is what keeps tabs from importing each other.
type NavigateMsg struct {
	Target ID
}

// Navigate returns the command that emits a NavigateMsg for target.
func Navigate(target ID) tea.Cmd {
	return func() tea.Msg {
		return NavigateMsg{Target: target}
	}
}

// HomeMsg asks the root to go home: to the dashboard if a project is active,
// or to the Memory tab otherwise.
type HomeMsg struct{}

// Home returns the command that emits a HomeMsg.
func Home() tea.Cmd {
	return func() tea.Msg {
		return HomeMsg{}
	}
}
