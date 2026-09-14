// Package memory is the Memory workspace tab: the dashboard, search, recent
// observations, observation detail, timeline, sessions, session detail and the
// agent setup screens.
//
// It is an isolated Elm sub-model:
//   - screen constants are a local iota; the root does not know them
//   - one Model struct holds all of the tab's state
//   - Update type-switches, per-screen key handlers return (tabs.Tab, tea.Cmd)
//   - vim keys (j/k) navigate, PrevScreen walks back
//
// Data reaches the tab through data.MemoryReader, never through *store.Store,
// and styling through theme.Styles.
package memory

import (
	"github.com/HoracioEspinosa/engram/internal/setup"
	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"
	"github.com/HoracioEspinosa/engram/internal/version"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// ─── Screens ─────────────────────────────────────────────────────────────────

type Screen int

const (
	ScreenDashboard Screen = iota
	ScreenSearch
	ScreenSearchResults
	ScreenRecent
	ScreenObservationDetail
	ScreenTimeline
	ScreenSessions
	ScreenSessionDetail
	ScreenSetup
)

type SessionDeleteState int

const (
	SessionDeleteStateNone SessionDeleteState = iota
	SessionDeleteStatePrompt
	SessionDeleteStateDeleting
)

// ─── Custom Messages ─────────────────────────────────────────────────────────

type updateCheckMsg struct {
	result version.CheckResult
}

type statsLoadedMsg struct {
	stats *store.Stats
	err   error
}

type searchResultsMsg struct {
	results []store.SearchResult
	query   string
	err     error
}

type recentObservationsMsg struct {
	observations []store.Observation
	err          error
}

type observationDetailMsg struct {
	observation *store.Observation
	err         error
}

type timelineMsg struct {
	timeline *store.TimelineResult
	err      error
}

type recentSessionsMsg struct {
	sessions []store.SessionSummary
	err      error
}

type sessionObservationsMsg struct {
	observations []store.Observation
	err          error
}

type sessionDeletedMsg struct {
	sessionID string
	err       error
}

type setupInstallMsg struct {
	result *setup.Result
	err    error
}

type linkTaskResultsMsg struct {
	results []store.TaskListItem
	err     error
}

type observationLinkedToTaskMsg struct {
	taskID int64
	err    error
}

// ─── Model ───────────────────────────────────────────────────────────────────

// UpdateChecker reports whether a newer release exists. It is a seam around
// version.CheckLatest, whose default reaches GitHub over the network: a
// golden suite that drives this tab through a real program otherwise renders
// a banner whose text — and whose very presence — depends on whether an HTTP
// response landed before the program quit.
type UpdateChecker func(current string) version.CheckResult

// defaultUpdateChecker is the seam's production implementation. It is named
// at package scope because New's own "version" parameter shadows the package
// this refers to.
var defaultUpdateChecker UpdateChecker = version.CheckLatest

type Model struct {
	reader      data.MemoryReader
	tasks       data.TaskReader
	project     string
	styles      theme.Styles
	updateCheck UpdateChecker
	Version     string
	Screen      Screen
	PrevScreen  Screen
	Width       int
	Height      int
	Cursor      int
	Scroll      int

	// Update notification
	UpdateStatus version.CheckStatus
	UpdateMsg    string

	// Error display
	ErrorMsg string

	// Dashboard
	Stats *store.Stats

	// Search
	SearchInput   textinput.Model
	SearchQuery   string
	SearchResults []store.SearchResult

	// Link to task (rfc-tui.md §7.2's "L", the only addition rfc-tui.md §5
	// lists for the memory screens): a task-search overlay available from
	// Search Results, Recent and Observation Detail. Selecting a result
	// writes the task_observations row mem_task_link writes over MCP today,
	// then navigates to that task's detail (rfc-tui.md §7.3's
	// "MEM -->|L link| TD"). It renders through shared.Menu, the same
	// component the Tasks tab's state picker uses (ADR-051 §4).
	Linking     bool
	LinkObsID   int64
	LinkQuery   textinput.Model
	LinkResults []store.TaskListItem
	LinkCursor  int

	// Recent observations
	RecentObservations []store.Observation

	// Observation detail
	SelectedObservation *store.Observation
	DetailScroll        int

	// Timeline
	Timeline *store.TimelineResult

	// Sessions
	Sessions             []store.SessionSummary
	SelectedSessionIdx   int
	SessionObservations  []store.Observation
	SessionDetailScroll  int
	SessionDeleteState   SessionDeleteState
	SessionDeleteID      string
	SessionDeleteProject string

	// Clipboard feedback
	CopyFeedback string // "✓ Copied!" or "" — shown for 2 s after copy

	// Setup
	SetupAgents           []setup.Agent
	SetupResult           *setup.Result
	SetupError            string
	SetupDone             bool
	SetupInstalling       bool
	SetupInstallingName   string // agent name being installed (for display)
	SetupAllowlistPrompt  bool   // true = showing y/n prompt for allowlist
	SetupAllowlistApplied bool   // true = allowlist was added successfully
	SetupAllowlistError   string // error message if allowlist injection failed
	SetupSpinner          spinner.Model
}

// New creates the Memory tab bound to the given reader.
func New(r data.MemoryReader, version string) Model {
	styles := theme.Default()

	ti := textinput.New()
	ti.Placeholder = "Search memories..."
	ti.CharLimit = 256
	ti.Width = 60

	lq := textinput.New()
	lq.Placeholder = "Search tasks..."
	lq.CharLimit = 256
	lq.Width = 60

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = styles.Spinner

	return Model{
		reader:       r,
		styles:       styles,
		updateCheck:  defaultUpdateChecker,
		Version:      version,
		Screen:       ScreenDashboard,
		SearchInput:  ti,
		LinkQuery:    lq,
		SetupSpinner: sp,
	}
}

// WithUpdateChecker returns a copy of m that asks check instead of GitHub.
// A nil check keeps the default, so a caller can pass one through
// unconditionally.
func (m Model) WithUpdateChecker(check UpdateChecker) Model {
	if check != nil {
		m.updateCheck = check
	}
	return m
}

// WithStyles returns a copy of m painted with styles instead of the default
// theme.New built it with — app.New calls this once, right after New, so
// the tab renders under the same resolved palette as the workspace chrome
// around it (rfc-tui.md §8.2's --theme / ENGRAM_TUI_THEME / tui.theme). The
// spinner's own style is re-derived too: New bakes styles.Spinner into it at
// construction time, so leaving it alone here would strand the spinner on
// the palette New saw instead of the one the workspace resolved.
func (m Model) WithStyles(styles theme.Styles) Model {
	m.styles = styles
	m.SetupSpinner.Style = styles.Spinner
	return m
}

// WithTasks returns a copy of m able to look up and link tasks. The "L"
// picker needs a data.TaskReader to search tasks and write
// task_observations, a capability data.MemoryReader has no business
// exposing.
func (m Model) WithTasks(r data.TaskReader) Model {
	m.tasks = r
	return m
}

// WithProject sets the project the "L" picker scopes its task search to.
// Memory's own data — search, recent observations, sessions — spans every
// project, so nothing else about the tab reads this field.
func (m Model) WithProject(project string) Model {
	m.project = project
	return m
}

// Styles exposes the tab's current style set for app-level tests that
// assert every tab paints with the same resolved palette instead of its own
// default.
func (m Model) Styles() theme.Styles { return m.styles }

// Title is the label the tab bar shows for this tab.
func (Model) Title() string { return "Memory" }

// Init loads the dashboard: the counters and the update check behind its
// banner.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		loadStats(m.reader),
		checkForUpdate(m.updateCheck, m.Version),
	)
}

// Refresh reloads the data behind the current screen. Screens that hold no
// list — search, detail, timeline, setup — have nothing to reload.
func (m Model) Refresh() tea.Cmd {
	return m.refreshScreen(m.Screen)
}

// OpenObservation returns the command that loads id's detail. It is what the
// root drives when another tab asks to deep-link into an observation
// (rfc-tui.md §3.1 S4: Enter on a task's linked observation opens it here) —
// the same command loadObservationDetail already issues on the "enter" key
// from every list screen, exposed so a message from outside this package can
// trigger it too.
func (m Model) OpenObservation(id int64) tea.Cmd {
	return loadObservationDetail(m.reader, id)
}

// SearchFor returns the command that runs query and lands on the search
// results screen once it comes back — it is what the root drives when
// another tab asks Memory to open pre-searched (rfc-tui.md §3.1 S8/S9's "t":
// executions recorded against a runbook live at topic_key
// "runbook/RB-NNN/exec/<task-key>", so searching "runbook/RB-NNN" surfaces
// them). It issues the exact same searchMemories command the "/" key does
// from ScreenSearch — searchResultsMsg's handler in update.go already sets
// Screen to ScreenSearchResults regardless of who asked, the same way
// OpenObservation relies on observationDetailMsg's handler to switch screens.
func (m Model) SearchFor(query string) tea.Cmd {
	return searchMemories(m.reader, query)
}

// ─── Commands (data loading) ─────────────────────────────────────────────────

func checkForUpdate(check UpdateChecker, v string) tea.Cmd {
	return func() tea.Msg {
		if check == nil {
			check = defaultUpdateChecker
		}
		return updateCheckMsg{result: check(v)}
	}
}

func loadStats(r data.MemoryReader) tea.Cmd {
	return func() tea.Msg {
		stats, err := r.Stats()
		return statsLoadedMsg{stats: stats, err: err}
	}
}

func searchMemories(r data.MemoryReader, query string) tea.Cmd {
	return func() tea.Msg {
		results, err := r.Search(query, store.SearchOptions{Limit: 50})
		return searchResultsMsg{results: results, query: query, err: err}
	}
}

func loadRecentObservations(r data.MemoryReader) tea.Cmd {
	return func() tea.Msg {
		obs, err := r.RecentObservations(50)
		return recentObservationsMsg{observations: obs, err: err}
	}
}

func loadObservationDetail(r data.MemoryReader, id int64) tea.Cmd {
	return func() tea.Msg {
		obs, err := r.Observation(id)
		return observationDetailMsg{observation: obs, err: err}
	}
}

func loadTimeline(r data.MemoryReader, obsID int64) tea.Cmd {
	return func() tea.Msg {
		tl, err := r.Timeline(obsID, 10, 10)
		return timelineMsg{timeline: tl, err: err}
	}
}

func loadRecentSessions(r data.MemoryReader) tea.Cmd {
	return func() tea.Msg {
		sessions, err := r.RecentSessions(50)
		return recentSessionsMsg{sessions: sessions, err: err}
	}
}

func loadSessionObservations(r data.MemoryReader, sessionID string) tea.Cmd {
	return func() tea.Msg {
		obs, err := r.SessionObservations(sessionID, 200)
		return sessionObservationsMsg{observations: obs, err: err}
	}
}

func deleteSession(r data.MemoryReader, sessionID string) tea.Cmd {
	return func() tea.Msg {
		if r == nil {
			return sessionDeletedMsg{sessionID: sessionID, err: data.ErrStoreUnavailable}
		}
		err := r.DeleteSession(sessionID)
		return sessionDeletedMsg{sessionID: sessionID, err: err}
	}
}

func installAgent(agentName string) tea.Cmd {
	return func() tea.Msg {
		result, err := installAgentFn(agentName)
		return setupInstallMsg{result: result, err: err}
	}
}

var installAgentFn = setup.Install
var addClaudeCodeAllowlistFn = setup.AddClaudeCodeAllowlist

// searchTasksForLink returns the command that lists project's tasks
// matching query, for the "L" picker (rfc-tui.md §5).
func searchTasksForLink(r data.TaskReader, project, query string) tea.Cmd {
	return func() tea.Msg {
		results, err := r.ListTasks(project, store.TaskListFilter{Query: query, Limit: 20})
		return linkTaskResultsMsg{results: results, err: err}
	}
}

// linkObservationToTask returns the command that writes the
// task_observations row linking observationID to taskID — the same write
// data.TaskReader.LinkObservation performs for the Tasks tab's own "l" key,
// used here in the other direction.
func linkObservationToTask(r data.TaskReader, taskID, observationID int64) tea.Cmd {
	return func() tea.Msg {
		err := r.LinkObservation(taskID, observationID)
		return observationLinkedToTaskMsg{taskID: taskID, err: err}
	}
}
