// Package tabs declares the contract every workspace tab implements and the
// messages a tab uses to reach the root model.
//
// The root owns which tab is active and forwards messages to it; a tab never
// reaches into another tab's state. Cross-tab navigation travels as a
// NavigateMsg through the root, which is why this package holds no reference
// to any concrete tab.
package tabs

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// ID identifies a workspace tab. The order is the order of the tab bar.
type ID int

const (
	// Home is the active project seen whole: its card, its graph, and the
	// three lists that say what has been happening to it.
	Home ID = iota
	// Memory is the observation, session and timeline workspace.
	Memory
	// Tasks, Evidence, Benchmarks, Runbooks, Graph and Settings are declared
	// so navigation targets and the tab-bar order are fixed once; the root
	// reports an unimplemented target rather than switching to it.
	Tasks
	Evidence
	Benchmarks
	Runbooks
	Graph
	// Settings holds everything the workspace remembers about itself,
	// including the sync configuration that used to be a tab of its own.
	Settings
)

// String names the tab for logs and test failures.
func (id ID) String() string {
	switch id {
	case Home:
		return "home"
	case Memory:
		return "memory"
	case Tasks:
		return "tasks"
	case Evidence:
		return "evidence"
	case Benchmarks:
		return "benchmarks"
	case Runbooks:
		return "runbooks"
	case Graph:
		return "graph"
	case Settings:
		return "settings"
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
	// Help returns the key bindings the screen currently on display wants
	// listed in the "?" overlay (rfc-tui.md §7.1): whichever internal screen
	// is active, not a fixed per-tab list, so the overlay grows and shrinks
	// with what is actually on screen.
	Help() []key.Binding
	// CapturingText reports whether the tab currently has a text input
	// focused (a search box, the Tasks "link observation" prompt, ...). While
	// true, the root suspends every global key it would otherwise intercept
	// before the tab sees it — 1..5, Tab/Shift+Tab, "?", p, P and 0 — except
	// Ctrl+C, matching rfc-tui.md §7.1: "cuando un textinput tiene el foco,
	// las teclas globales se suspenden salvo Ctrl+C y Esc."
	CapturingText() bool
}

// Targeted is what a message implements when it belongs to exactly one tab.
//
// Everything used to be broadcast: a task list coming back woke Memory,
// Evidence, Runbooks and Cloud as well, each type-switching over a message
// it had no case for. That is five Update calls and five model copies for
// one row of data, on every load, on every tab.
//
// A message that names its owner is delivered to that tab alone. Only the
// two kinds that genuinely concern everyone — the terminal's size and a
// change of palette — are still broadcast.
type Targeted interface {
	// TabOwner is the tab this message was issued by and belongs to.
	TabOwner() ID
}

// NavigateMsg asks the root to activate another tab. A tab emits it instead of
// switching itself, which is what keeps tabs from importing each other.
//
// ObservationID, TaskID and Query are the cross-tab context v1 needs, each
// zero except for the one navigation it carries a deep link for:
//   - ObservationID: rfc-tui.md §3.1 S4 has Enter on a task's linked
//     observation open that observation's detail inside Memory.
//   - TaskID: §3.1 S4's "e" opens Evidence filtered to the task under view
//     (S6's task_id filter), and §3.1 S7's "Enter" opens that evidence
//     file's task inside Tasks — the same field serves both directions
//     because Target already says which one applies.
//   - Query: §3.1 S8/S9's "t" opens Memory pre-searched for
//     "runbook/RB-NNN", the executions recorded against that runbook
//     (D-09's `runbook/RB-NNN/exec/<task-key>` topic_key convention).
//   - EvidenceID and BenchmarkID are the same idea for the two kinds the
//     workspace search can land on directly: one file, one measurement.
//   - Slug rescopes the workspace before the target opens. A search that
//     crosses projects has to move the whole workspace, not only the tab:
//     a task from another project opened inside this one's Tasks list would
//     be a row nobody can find again.
type NavigateMsg struct {
	Target        ID
	ObservationID int64
	TaskID        int64
	EvidenceID    int64
	BenchmarkID   int64
	Slug          string
	Query         string
}

// Navigate returns the command that emits a NavigateMsg for target.
func Navigate(target ID) tea.Cmd {
	return func() tea.Msg {
		return NavigateMsg{Target: target}
	}
}

// NavigateToObservation returns the command that asks the root to open id's
// detail inside the Memory tab.
func NavigateToObservation(id int64) tea.Cmd {
	return func() tea.Msg {
		return NavigateMsg{Target: Memory, ObservationID: id}
	}
}

// NavigateToTaskEvidence returns the command that asks the root to open the
// Evidence tab filtered to taskID (rfc-tui.md §3.1 S4's "e" key).
func NavigateToTaskEvidence(taskID int64) tea.Cmd {
	return func() tea.Msg {
		return NavigateMsg{Target: Evidence, TaskID: taskID}
	}
}

// NavigateToTask returns the command that asks the root to open taskID's
// detail inside the Tasks tab (rfc-tui.md §3.1 S7's "Enter" key).
func NavigateToTask(taskID int64) tea.Cmd {
	return func() tea.Msg {
		return NavigateMsg{Target: Tasks, TaskID: taskID}
	}
}

// NavigateToMemorySearch returns the command that asks the root to open
// Memory pre-searched for query (rfc-tui.md §3.1 S8/S9's "t" key).
func NavigateToMemorySearch(query string) tea.Cmd {
	return func() tea.Msg {
		return NavigateMsg{Target: Memory, Query: query}
	}
}
