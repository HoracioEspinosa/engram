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

// ─── Model ───────────────────────────────────────────────────────────────────

type Model struct {
	reader     data.MemoryReader
	styles     theme.Styles
	Version    string
	Screen     Screen
	PrevScreen Screen
	Width      int
	Height     int
	Cursor     int
	Scroll     int

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

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = styles.Spinner

	return Model{
		reader:       r,
		styles:       styles,
		Version:      version,
		Screen:       ScreenDashboard,
		SearchInput:  ti,
		SetupSpinner: sp,
	}
}

// Title is the label the tab bar shows for this tab.
func (Model) Title() string { return "Memory" }

// Init loads the dashboard: the counters and the update check behind its
// banner.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		loadStats(m.reader),
		checkForUpdate(m.Version),
	)
}

// Refresh reloads the data behind the current screen. Screens that hold no
// list — search, detail, timeline, setup — have nothing to reload.
func (m Model) Refresh() tea.Cmd {
	return m.refreshScreen(m.Screen)
}

// ─── Commands (data loading) ─────────────────────────────────────────────────

func checkForUpdate(v string) tea.Cmd {
	return func() tea.Msg {
		return updateCheckMsg{result: version.CheckLatest(v)}
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
