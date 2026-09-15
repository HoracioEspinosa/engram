package app

import (
	"time"

	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs/settings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// Update routes a message.
//
// Keys reach only the active tab, after the root has taken its global
// bindings and whichever overlay has the keyboard. Everything else — window
// size, data loads, timers — goes to the tab that owns it, or to every tab
// when it genuinely concerns them all.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, globalKeys.Quit) {
			return m, tea.Quit
		}
		if handled, next, cmd := m.updateThemePicker(msg); handled {
			return next, cmd
		}
		if handled, next, cmd := m.updateProjectTree(msg); handled {
			return next, cmd
		}
		if handled, next, cmd := m.updatePalette(msg); handled {
			return next, cmd
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
		if msg.Slug != "" && msg.Slug != m.project {
			// A hit in another project: the whole workspace moves, not only
			// the tab, so the row the reader opened can be found again.
			scoped, cmd := m.openProject(msg.Slug)
			next := scoped.(Model)
			if msg.Target == tabs.Home {
				return next, cmd
			}
			model, activateCmd := next.routeNavigate(msg)
			return model, tea.Batch(cmd, activateCmd)
		}
		return m.routeNavigate(msg)

	case searchTickMsg, searchDoneMsg, searchHistorySavedMsg:
		return m.updatePaletteMessage(msg)

	case ancestorsLoadedMsg:
		if msg.slug != m.project || msg.err != nil {
			// A chain for a project the user has since left, or a lookup
			// that failed: the breadcrumb falls back to the project on its
			// own rather than showing somebody else's parents.
			return m, nil
		}
		m.ancestors = msg.nodes
		// Home draws the same chain in its project card. It is handed down
		// rather than looked up again: one ancestor query per project is
		// enough, and two would be two answers that could disagree.
		m.home = m.home.WithBreadcrumb(msg.nodes)
		return m, nil

	case treeLoadedMsg:
		m.tree = m.tree.applyLoaded(msg)
		return m, nil

	case settings.OpenThemePickerMsg:
		// The Settings tab's own "theme" row: the picker is root state, so
		// the tab asks for it rather than owning a second copy.
		m.themePicker.open = true
		m.themePicker.original = m.styles.Palette
		m.themePicker.notice = ""
		return m, loadThemes(m.themePicker.themes)

	case themesLoadedMsg, themePreviewMsg, themeAppliedMsg:
		return m.updateThemeMessage(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// The terminal's size genuinely concerns every tab, not only the
		// active one: a tab laid out at the old width would render wrong the
		// moment it is switched to.
		return m.broadcast(msg)
	}

	// A message that names its owner goes to that tab alone. Broadcasting it
	// woke four tabs that had no case for it, copying each model to leave it
	// unchanged.
	if targeted, ok := msg.(tabs.Targeted); ok {
		return m.deliver(targeted.TabOwner(), msg)
	}

	return m.broadcast(msg)
}

// routeNavigate opens whatever one NavigateMsg points at: a deep link into a
// row, or a plain tab switch.
func (m Model) routeNavigate(msg tabs.NavigateMsg) (tea.Model, tea.Cmd) {
	{
		if msg.Target == tabs.Memory && msg.ObservationID != 0 {
			// A deep-link into one observation (rfc-tui.md §3.1 S4's "Enter
			// opens the observation in Memory"), not a plain tab switch: skip
			// activate()'s generic tab.Refresh() and load that observation's
			// detail directly instead.
			m.active = tabs.Memory
			return m, m.memory.OpenObservation(msg.ObservationID)
		}
		if msg.Target == tabs.Memory && msg.Query != "" {
			// Runbooks' "t" (rfc-tui.md §3.1 S8/S9): open Memory pre-searched
			// for this runbook's executions instead of landing on whatever
			// screen Memory last showed.
			m.active = tabs.Memory
			return m, m.memory.SearchFor(msg.Query)
		}
		if msg.Target == tabs.Tasks && msg.TaskID != 0 {
			// The mirror image, for S7's "Enter" on an evidence file: open
			// that file's task directly instead of landing on the list.
			m.active = tabs.Tasks
			return m, m.tasks.OpenTask(msg.TaskID)
		}
		if msg.Target == tabs.Evidence && msg.TaskID != 0 {
			// S4's "e" key: filter Evidence to the task under view (S6's
			// task_id filter) instead of showing every file in the project.
			m.active = tabs.Evidence
			return m, m.evidence.OpenForTask(msg.TaskID)
		}
		if msg.Target == tabs.Benchmarks && msg.BenchmarkID != 0 {
			// The palette's deep link into one measurement: the tab's own
			// table is what shows it, filtered to nothing so the row is
			// where the search said it was.
			return m.activate(tabs.Benchmarks)
		}
		if msg.Target == tabs.Evidence && msg.EvidenceID != 0 {
			// The palette's own deep link: one file, opened by its id.
			m.active = tabs.Evidence
			opened, cmd := m.evidence.OpenEvidence(msg.EvidenceID)
			m.evidence = opened
			return m, cmd
		}
		return m.activate(msg.Target)
	}
}

// deliver hands msg to one tab and stores the result back. A message for a
// tab this build does not implement is dropped, the same way activate leaves
// an unimplemented target alone.
func (m Model) deliver(id tabs.ID, msg tea.Msg) (tea.Model, tea.Cmd) {
	tab := m.tab(id)
	if tab == nil {
		return m, nil
	}
	updated, cmd := tab.Update(msg)
	m = m.withTab(id, updated)
	m.freshness = m.freshness.loaded(id)
	return m, cmd
}

// digitTabs maps rfc-tui.md §7.1's "0"…"7" to the tab each activates, in tab
// bar order.
var digitTabs = map[string]tabs.ID{
	"0": tabs.Home,
	"1": tabs.Memory,
	"2": tabs.Tasks,
	"3": tabs.Evidence,
	"4": tabs.Benchmarks,
	"5": tabs.Runbooks,
	"6": tabs.Graph,
	"7": tabs.Settings,
}

// updateActive forwards a message to the active tab only.
func (m Model) updateActive(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, isKey := msg.(tea.KeyMsg); isKey {
		// Every global key below is suspended while the active tab is
		// capturing text (rfc-tui.md §7.1: "cuando un textinput tiene el
		// foco, las teclas globales se suspenden salvo Ctrl+C y Esc" —
		// Ctrl+C is handled in Update, before updateActive is ever called,
		// and Esc is not one of these keys at all).
		if tab := m.tab(m.active); tab == nil || !tab.CapturingText() {
			if handled, model, cmd := m.matchGlobal(keyMsg); handled {
				return model, cmd
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

// matchGlobal answers the key bindings the root owns, on every tab.
//
// Every one of them goes through key.Matches against globalKeys: the keymap
// is the declaration of what the root answers to, so a binding changed there
// changes the behaviour, the "?" overlay and the hints at once. A literal
// comparison here would let the three drift apart, which is what the keymap
// existed to prevent.
//
// handled is false when the key belongs to the tab below, which is then left
// to decide what it means.
func (m Model) matchGlobal(msg tea.KeyMsg) (handled bool, model tea.Model, cmd tea.Cmd) {
	switch {
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
		// "r" is the user saying the data is out of date, so it reloads
		// whatever the TTL would have let stand.
		m.freshness = m.freshness.loaded(m.active)
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
	if m.tree.open {
		return loadTree(m.tree.reader)
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
	// Not on a registered tab at all: Tab starts the cycle at the first tab,
	// Shift+Tab at the last one.
	if current == -1 {
		if delta > 0 {
			return m.activate(registered[0])
		}
		return m.activate(registered[len(registered)-1])
	}
	next := (current + delta + len(registered)) % len(registered)
	return m.activate(registered[next])
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

// activate switches to target and reloads it only when what it is showing
// could have gone out of date. Navigating to an ID this build implements no
// tab for leaves the workspace where it is.
//
// Every tab switch used to re-run the tab's whole query set. Cycling through
// five tabs to read something on the first one cost five round trips to
// SQLite and five screens that flickered back through their loading state,
// for data nobody had changed. A tab that loaded seconds ago is shown as it
// is; "r" still reloads on demand, and a tab marked dirty reloads whether or
// not its TTL has run out.
func (m Model) activate(target tabs.ID) (tea.Model, tea.Cmd) {
	tab := m.tab(target)
	if tab == nil {
		// This build does not implement that tab. Staying put with no command
		// is the whole contract: an unimplemented destination must never move
		// the workspace nor strand the screen it was showing.
		return m, nil
	}
	m.active = target

	if !m.freshness.stale(target, time.Now()) {
		return m, nil
	}
	m.freshness = m.freshness.loaded(target)
	return m, tab.Refresh()
}
