package app

import (
	"github.com/Gentleman-Programming/engram/internal/tui/tabs"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// Update routes a message.
//
// Keys reach only the active tab, after the root has taken its global
// bindings. Everything else — window size, data loads, timers — is broadcast
// to every tab, so a command that finishes while the user is elsewhere still
// reaches the tab that issued it.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, globalKeys.Quit) {
			return m, tea.Quit
		}
		return m.updateActive(msg)

	case tabs.NavigateMsg:
		return m.activate(msg.Target)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m.broadcast(msg)
	}

	return m.broadcast(msg)
}

// updateActive forwards a message to the active tab only.
func (m Model) updateActive(msg tea.Msg) (tea.Model, tea.Cmd) {
	tab := m.tab(m.active)
	if tab == nil {
		return m, nil
	}
	updated, cmd := tab.Update(msg)
	return m.withTab(m.active, updated), cmd
}

// broadcast forwards a message to every registered tab.
func (m Model) broadcast(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmds := make([]tea.Cmd, 0, len(registered))
	for _, id := range registered {
		tab := m.tab(id)
		if tab == nil {
			continue
		}
		updated, cmd := tab.Update(msg)
		m = m.withTab(id, updated)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

// activate switches to target and reloads whatever it shows. Navigating to an
// ID this build implements no tab for leaves the workspace where it is.
func (m Model) activate(target tabs.ID) (tea.Model, tea.Cmd) {
	tab := m.tab(target)
	if tab == nil {
		return m, nil
	}
	m.active = target
	return m, tab.Refresh()
}
