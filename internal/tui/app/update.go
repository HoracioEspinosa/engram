package app

import (
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"

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
		if m.showHelp {
			// While the "?" overlay (rfc-tui.md §7.1) is open, every key but
			// the three that close it is swallowed here, before it ever
			// reaches updateActive: a "j" meant for reading help must not
			// move a list's cursor on the screen underneath.
			switch msg.String() {
			case "?", "esc", "q":
				m.showHelp = false
			}
			return m, nil
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
		if msg.Target == tabs.Memory && msg.Query != "" {
			// Runbooks' "t" (rfc-tui.md §3.1 S8/S9): open Memory pre-searched
			// for this runbook's executions instead of landing on whatever
			// screen Memory last showed.
			m.active = tabs.Memory
			m.screen = screenTab
			return m, m.memory.SearchFor(msg.Query)
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

// digitTabs maps rfc-tui.md §7.1's "1"…"5" to the tab each activates, in tab
// bar order.
var digitTabs = map[string]tabs.ID{
	"1": tabs.Memory,
	"2": tabs.Tasks,
	"3": tabs.Evidence,
	"4": tabs.Runbooks,
	"5": tabs.Cloud,
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
			// Every global key below is suspended while the active tab is
			// capturing text (rfc-tui.md §7.1: "cuando un textinput tiene el
			// foco, las teclas globales se suspenden salvo Ctrl+C y Esc" —
			// Ctrl+C is handled in Update, before updateActive is ever
			// called, and Esc is not one of these keys at all).
			if tab := m.tab(m.active); tab == nil || !tab.CapturingText() {
				if handled, model, cmd := m.matchGlobal(keyMsg); handled {
					return model, cmd
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

// matchGlobal answers the key bindings the root owns, for every screen.
//
// Every one of them goes through key.Matches against globalKeys: the keymap
// is the declaration of what the root answers to, so a binding changed there
// changes the behaviour, the "?" overlay and the footer at once. A literal
// comparison here would let the three drift apart, which is what the keymap
// existed to prevent.
//
// handled is false when the key belongs to the screen below, which is then
// left to decide what it means.
func (m Model) matchGlobal(msg tea.KeyMsg) (handled bool, model tea.Model, cmd tea.Cmd) {
	switch {
	case key.Matches(msg, globalKeys.ProjectSelector):
		m.screen = screenSelector
		return true, m, loadSelector(m.projects)

	case key.Matches(msg, globalKeys.Dashboard):
		if m.project == "" {
			// Nothing to show a dashboard for. Swallow it anyway: the
			// digit is the root's, and handing it to a tab would make it
			// mean something else on one screen.
			return true, m, nil
		}
		m.screen = screenDashboard
		return true, m, loadDashboard(m.projects, m.project)

	case key.Matches(msg, globalKeys.SwitchTab):
		if target, ok := digitTabs[msg.String()]; ok {
			model, cmd = m.activate(target)
			return true, model, cmd
		}
		return true, m, nil

	case key.Matches(msg, globalKeys.NextTab):
		model, cmd = m.activateRelative(1)
		return true, model, cmd

	case key.Matches(msg, globalKeys.PrevTab):
		model, cmd = m.activateRelative(-1)
		return true, model, cmd

	case key.Matches(msg, globalKeys.Refresh):
		return true, m, m.refreshActiveScreen()

	case key.Matches(msg, globalKeys.Help):
		m.showHelp = true
		return true, m, nil
	}
	return false, m, nil
}

// refreshActiveScreen returns the command that reloads whatever is on
// display. Every screen owns a Refresh of its own; the root decides when it
// runs, so "r" means the same thing everywhere instead of being reimplemented
// once per screen.
func (m Model) refreshActiveScreen() tea.Cmd {
	switch m.screen {
	case screenDashboard:
		return loadDashboard(m.projects, m.project)
	case screenSelector:
		return loadSelector(m.projects)
	}
	if tab := m.tab(m.active); tab != nil {
		return tab.Refresh()
	}
	return nil
}

// activateRelative moves delta slots through registered, wrapping at either
// end, and activates whatever tab lands there (rfc-tui.md §7.1's
// "Tab / Shift+Tab | Pestaña siguiente / anterior"). delta is +1 for Tab, -1
// for Shift+Tab; registered is never empty, so the modulo below always has a
// tab to land on.
func (m Model) activateRelative(delta int) (tea.Model, tea.Cmd) {
	current := -1
	for i, id := range registered {
		if id == m.active {
			current = i
			break
		}
	}
	// Not on a registered tab at all (e.g. the Dashboard): Tab starts the
	// cycle at the first tab, Shift+Tab at the last one.
	if current == -1 {
		if delta > 0 {
			return m.activate(registered[0])
		}
		return m.activate(registered[len(registered)-1])
	}
	next := (current + delta + len(registered)) % len(registered)
	return m.activate(registered[next])
}

// updateDashboard handles key presses while the dashboard is active. The
// Dashboard has no text input of its own, so unlike updateActive's tab
// branch none of these need a CapturingText guard.
func (m Model) updateDashboard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if handled, model, cmd := m.matchGlobal(msg); handled {
		return model, cmd
	}

	switch msg.String() {
	case "j":
		// The blocks are stacked, so the vertical pair moves between them.
		// h/l are reserved for horizontal focus and mean nothing here.
		m.dashboard = m.dashboard.moveCursor(1)
		return m, nil
	case "k":
		m.dashboard = m.dashboard.moveCursor(-1)
		return m, nil
	case "g":
		m.dashboard = m.dashboard.moveCursorTo(dashBlockTasks)
		return m, nil
	case "G":
		m.dashboard = m.dashboard.moveCursorTo(dashBlockCount - 1)
		return m, nil
	case "enter":
		// activate() already knows which tabs this build registers, so route
		// through it instead of guessing here: a block whose tab does not
		// exist yet leaves the dashboard exactly as it was.
		return m.activate(m.dashboard.cursor.target())
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

	// The Selector answers two of the root's bindings and no more: there is
	// no project yet to number tabs for. Both go through the keymap so the
	// footer and the "?" overlay keep naming the keys that actually work.
	switch {
	case key.Matches(msg, globalKeys.Refresh):
		return m, loadSelector(m.projects)
	case key.Matches(msg, globalKeys.Help):
		m.showHelp = true
		return m, nil
	}

	switch msg.String() {
	case "j":
		m.selector = m.selector.moveCursor(1)
		return m, nil
	case "k":
		m.selector = m.selector.moveCursor(-1)
		return m, nil
	case "g":
		m.selector = m.selector.moveCursorToStart()
		return m, nil
	case "G":
		m.selector = m.selector.moveCursorToEnd()
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
			// dashboard is: neither Tasks', Evidence's nor Runbooks'
			// Refresh() takes a project parameter of its own (tabs.Tab is a
			// project-agnostic contract), so each has to already know the
			// new slug before it is ever activated.
			m.tasks = m.tasks.WithProject(selected.Slug)
			m.evidence = m.evidence.WithProject(selected.Slug)
			m.runbooks = m.runbooks.WithProject(selected.Slug)
			m.memory = m.memory.WithProject(selected.Slug)
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
