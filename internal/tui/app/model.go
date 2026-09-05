// Package app is the root of the TUI workspace: it owns which tab is active,
// consumes the global key bindings, fans messages out to every tab and draws
// the application frame around whichever tab is showing.
//
// Dependency direction is one-way. app knows tabs/*, theme, data and shared;
// no tab knows app, and no tab knows another tab. Cross-tab navigation travels
// as tabs.NavigateMsg, which is declared in tabs rather than here precisely so
// a tab can emit it without importing the root.
package app

import (
	"github.com/Gentleman-Programming/engram/internal/store"
	"github.com/Gentleman-Programming/engram/internal/tui/data"
	"github.com/Gentleman-Programming/engram/internal/tui/tabs"
	"github.com/Gentleman-Programming/engram/internal/tui/tabs/cloud"
	"github.com/Gentleman-Programming/engram/internal/tui/tabs/memory"
	"github.com/Gentleman-Programming/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// registered lists the tabs this build implements, in tab-bar order. The IDs
// tabs declares but that no sub-model implements yet are simply absent.
var registered = []tabs.ID{tabs.Memory, tabs.Cloud}

// Model is the root workspace model.
//
// Sub-models are typed fields rather than a map so the whole model stays a
// value: copying a Model copies every tab with it, which is what lets Update
// return a new root without the old one observing the change.
type Model struct {
	styles  theme.Styles
	version string

	width  int
	height int

	active tabs.ID
	memory memory.Model
	cloud  cloud.Model
}

// New builds the TUI bound to the engram store.
func New(s *store.Store, version string) Model {
	return Model{
		styles:  theme.Default(),
		version: version,
		active:  tabs.Memory,
		memory:  memory.New(data.NewMemoryReader(s), version),
		cloud:   cloud.New(),
	}
}

// Init loads every tab's first screen and switches the terminal to the
// alternate screen buffer.
func (m Model) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(registered)+1)
	for _, id := range registered {
		if tab := m.tab(id); tab != nil {
			cmds = append(cmds, tab.Init())
		}
	}
	cmds = append(cmds, tea.EnterAltScreen)
	return tea.Batch(cmds...)
}

// tab returns the sub-model registered for id, or nil when this build
// implements no tab for it.
func (m Model) tab(id tabs.ID) tabs.Tab {
	switch id {
	case tabs.Memory:
		return m.memory
	case tabs.Cloud:
		return m.cloud
	}
	return nil
}

// withTab returns a copy of m with id's sub-model replaced. A tab of an
// unexpected concrete type is dropped rather than stored, which keeps a
// mis-registered tab from corrupting the root.
func (m Model) withTab(id tabs.ID, t tabs.Tab) Model {
	switch id {
	case tabs.Memory:
		if updated, ok := t.(memory.Model); ok {
			m.memory = updated
		}
	case tabs.Cloud:
		if updated, ok := t.(cloud.Model); ok {
			m.cloud = updated
		}
	}
	return m
}
