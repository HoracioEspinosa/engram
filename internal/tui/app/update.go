package app

import (
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs/evidence"

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
		if msg.Target == tabs.Memory && msg.ObservationID != 0 {
			// A deep-link into one observation (rfc-tui.md §3.1 S4's "Enter
			// opens the observation in Memory"), not a plain tab switch: skip
			// activate()'s generic tab.Refresh() and load that observation's
			// detail directly instead.
			m.active = tabs.Memory
			m.screen = screenTab
			return m, m.memory.OpenObservation(msg.ObservationID)
		}
		if msg.Target == tabs.Tasks && msg.TaskID != 0 {
			// The mirror image, for S7's "Enter" on an evidence file: open
			// that file's task directly instead of landing on the list.
			m.active = tabs.Tasks
			m.screen = screenTab
			return m, m.tasks.OpenTask(msg.TaskID)
		}
		if msg.Target == tabs.Evidence && msg.TaskID != 0 {
			// S4's "e" key: filter Evidence to the task under view (S6's
			// task_id filter) instead of showing every file in the project.
			m.active = tabs.Evidence
			m.screen = screenTab
			return m, m.evidence.OpenForTask(msg.TaskID)
		}
		return m.activate(msg.Target)

	case tabs.HomeMsg:
		if m.project != "" {
			// A project is active: go to the dashboard.
			m.screen = screenDashboard
			return m, loadDashboard(m.projects, m.project)
		}
		// No project active: go to the Memory tab.
		m.screen = screenTab
		return m.activate(tabs.Memory)

	case dashboardLoadedMsg:
		m.dashboard = m.dashboard.applyLoaded(msg)
		return m, nil

	case selectorLoadedMsg:
		m.selector = m.selector.applyLoaded(msg)
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m.broadcast(msg)
	}

	return m.broadcast(msg)
}

// updateActive forwards a message to the active tab only, or to the dashboard/
// selector if one of those screens is active.
func (m Model) updateActive(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, isKey := msg.(tea.KeyMsg)
	if isKey {
		switch m.screen {
		case screenDashboard:
			return m.updateDashboard(keyMsg)
		case screenSelector:
			return m.updateSelector(keyMsg)
		default:
			// Handle global keys from tabs (p = projects, 0 = dashboard).
			//
			// rfc-tui.md §7.2's footnote on S7 swaps this pair on purpose:
			// "p copia la ruta y el selector de proyecto se abre con P" —
			// Evidence's detail screen needs lowercase "p" for its own copy
			// action more than the global shortcut does, so on that one
			// screen the global binding moves to uppercase "P" instead and
			// lowercase falls through to the tab below.
			onEvidenceDetail := m.active == tabs.Evidence && m.evidence.Screen == evidence.ScreenDetail
			switch keyMsg.String() {
			case "p":
				if !onEvidenceDetail {
					m.screen = screenSelector
					return m, loadSelector(m.projects)
				}
			case "P":
				if onEvidenceDetail {
					m.screen = screenSelector
					return m, loadSelector(m.projects)
				}
			case "0":
				if m.project != "" {
					m.screen = screenDashboard
					return m, loadDashboard(m.projects, m.project)
				}
			}
		}
	}

	tab := m.tab(m.active)
	if tab == nil {
		return m, nil
	}
	updated, cmd := tab.Update(msg)
	return m.withTab(m.active, updated), cmd
}

// updateDashboard handles key presses while the dashboard is active.
func (m Model) updateDashboard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "l":
		// rfc-tui.md §7.2: h/l are the Dashboard's own aliases for j/k,
		// alongside the vim-style pair every other screen already uses.
		m.dashboard = m.dashboard.moveCursor(1)
		return m, nil
	case "k", "h":
		m.dashboard = m.dashboard.moveCursor(-1)
		return m, nil
	case "enter":
		// activate() already knows which tabs this build registers, so route
		// through it instead of guessing here: a block whose tab does not
		// exist yet leaves the dashboard exactly as it was.
		return m.activate(m.dashboard.cursor.target())
	case "0":
		return m, loadDashboard(m.projects, m.project)
	case "p":
		m.screen = screenSelector
		return m, loadSelector(m.projects)
	case "r":
		return m, loadDashboard(m.projects, m.project)
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

// updateSelector handles key presses while the selector is active.
func (m Model) updateSelector(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If the filter input is focused, only handle enter, esc, and pass everything else to the input
	if m.selector.filterInput.Focused() {
		switch msg.Type {
		case tea.KeyEnter:
			m.selector.filterInput.Blur()
			return m, nil
		case tea.KeyEsc:
			m.selector.filterInput.Blur()
			m.selector.filterInput.SetValue("")
			m.selector = m.selector.applyFilter()
			return m, nil
		default:
			// Pass all other keys to the filter input
			updated, cmd := m.selector.filterInput.Update(msg)
			m.selector.filterInput = updated
			m.selector = m.selector.applyFilter()
			return m, cmd
		}
	}

	switch msg.String() {
	case "j":
		m.selector = m.selector.moveCursor(1)
		return m, nil
	case "k":
		m.selector = m.selector.moveCursor(-1)
		return m, nil
	case "i":
		m.selector = m.selector.toggleHealthSort()
		return m, nil
	case "enter":
		selected := m.selector.selected()
		if selected != nil {
			m.project = selected.Slug
			m.screen = screenDashboard
			m.dashboard = newDashboardModel(m.projects, selected.Slug)
			// Every project-scoped tab is rebuilt here, the same way the
			// dashboard is: neither Tasks' nor Evidence's Refresh() takes a
			// project parameter of its own (tabs.Tab is a project-agnostic
			// contract), so each has to already know the new slug before it
			// is ever activated.
			m.tasks = m.tasks.WithProject(selected.Slug)
			m.evidence = m.evidence.WithProject(selected.Slug)
			return m, loadDashboard(m.projects, selected.Slug)
		}
		return m, nil
	case "/":
		m.selector.filterInput.Focus()
		// Don't return here; fall through to pass "/" to the input
	case "esc":
		if m.selector.filterInput.Focused() {
			m.selector.filterInput.Blur()
			m.selector.filterInput.SetValue("")
			m.selector = m.selector.applyFilter()
			return m, nil
		}
		if m.project != "" {
			m.screen = screenDashboard
			return m, nil
		}
		return m, nil
	case "r":
		return m, loadSelector(m.projects)
	case "q":
		return m, tea.Quit
	}

	return m, nil
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
		// This build does not implement that tab. Staying put with no command
		// is the whole contract: an unimplemented destination must never move
		// the workspace nor strand the screen it was showing.
		return m, nil
	}
	m.active = target
	m.screen = screenTab
	return m, tab.Refresh()
}
