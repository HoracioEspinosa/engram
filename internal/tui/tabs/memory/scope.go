package memory

import (
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"

	tea "github.com/charmbracelet/bubbletea"
)

// Scope is how wide the Memory tab reads: this project, its subtree, or the
// whole workspace.
//
// Memory used to answer only the last of the three. That is the right default
// for a workspace-wide recall tool and the wrong one for somebody reading a
// project's own history, which is the reason "a" cycles rather than toggles:
// the two useful narrowings — one project, one family — are different
// questions, and a two-state switch could only ever offer one of them.
type Scope int

const (
	// ScopeProject reads only the active project.
	ScopeProject Scope = iota
	// ScopeSubtree widens to the project and everything under it.
	ScopeSubtree
	// ScopeAll reads every project the workspace holds.
	ScopeAll
)

// ScopeSettingKey is where the chosen width is remembered, so a reader who
// works inside one project does not re-narrow Memory on every start.
const ScopeSettingKey = "tui.memory_scope"

// scopeNames are the values written to settings, indexed by Scope. They are
// the words `engram` would use on a command line, not display copy.
var scopeNames = [...]string{"project", "subtree", "all"}

// String names the scope for settings and for test failures.
func (s Scope) String() string {
	if s < 0 || int(s) >= len(scopeNames) {
		return scopeNames[ScopeAll]
	}
	return scopeNames[s]
}

// label is how the scope reads in the tab's title.
func (s Scope) label() string {
	switch s {
	case ScopeProject:
		return "project"
	case ScopeSubtree:
		return "subtree"
	}
	return "all"
}

// next cycles to the following scope, wrapping at the end.
func (s Scope) next() Scope {
	if s >= ScopeAll {
		return ScopeProject
	}
	return s + 1
}

// ParseScope reads a remembered scope back. Anything it does not recognise —
// a settings row written by a newer build, or by hand — falls back to reading
// everything, which is the answer that can never be wrong for the wrong
// reason: too much memory, never too little.
func ParseScope(raw string) Scope {
	for i, name := range scopeNames {
		if raw == name {
			return Scope(i)
		}
	}
	return ScopeAll
}

// WithScope returns a copy of m reading at the given width.
func (m Model) WithScope(s Scope) Model {
	m.Scope = s
	return m
}

// WithSettings binds the tab to the store it remembers the scope in. A tab
// built without one still cycles; the choice simply does not survive a
// restart.
func (m Model) WithSettings(w data.SettingsWriter) Model {
	m.settings = w
	return m
}

// projectScope turns the tab's own setting into the narrowing its reader
// takes.
//
// Without an active project there is nothing to narrow to, so every scope
// reads everything: a Memory tab that answered "nothing" because no project
// was resolved would look broken rather than scoped.
func (m Model) projectScope() data.ProjectScope {
	if m.project == "" || m.Scope == ScopeAll {
		return data.ProjectScope{}
	}
	return data.ProjectScope{Project: m.project, Subtree: m.Scope == ScopeSubtree}
}

// scopeSavedMsg reports the settings write, so a store that will not answer
// says so instead of losing the choice silently.
type scopeSavedMsg struct{ err error }

// TabOwner names the tab this message belongs to.
func (scopeSavedMsg) TabOwner() tabs.ID { return tabs.Memory }

// saveScope remembers the chosen width.
func saveScope(w data.SettingsWriter, s Scope) tea.Cmd {
	if w == nil {
		return nil
	}
	return func() tea.Msg {
		return scopeSavedMsg{err: w.SetSetting(ScopeSettingKey, s.String())}
	}
}

// cycleScope is what "a" runs: widen (or narrow back round), remember the
// choice, and reload whatever list is on screen through the new scope.
//
// The offsets go back to zero: page three of a list that just got narrower is
// a page that may no longer exist, and landing on an empty screen after a
// keypress reads as a failure rather than as a filter.
func (m Model) cycleScope() (tabs.Tab, tea.Cmd) {
	if m.project == "" {
		// Nothing to narrow to. The key is answered rather than passed on:
		// it means the same thing on this screen whether or not a project
		// happens to be active.
		return m, nil
	}

	m.Scope = m.Scope.next()
	remember := saveScope(m.settings, m.Scope)

	var reload tea.Cmd
	switch m.Screen {
	case ScreenSearchResults:
		m.SearchOffset = 0
		reload = searchMemories(m.reader, m.SearchQuery, m.projectScope(), 0)
	case ScreenRecent:
		m.RecentOffset = 0
		reload = loadRecentObservations(m.reader, m.projectScope(), 0)
	case ScreenSessions:
		reload = loadRecentSessions(m.reader, m.projectScope())
	}
	return m, tea.Batch(remember, reload)
}
