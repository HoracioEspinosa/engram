package memory

import (
	"errors"
	"fmt"
	"time"

	"github.com/HoracioEspinosa/engram/internal/setup"
	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// ─── Update ──────────────────────────────────────────────────────────────────

// Update advances the Memory tab. Key messages arrive only while the tab is
// active; ctrl+c is consumed by the root before it gets here.
func (m Model) Update(msg tea.Msg) (tabs.Tab, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil

	case tea.KeyMsg:
		// If search input is focused, let it handle most keys
		if m.Screen == ScreenSearch && m.SearchInput.Focused() {
			return m.handleSearchInputKeys(msg)
		}
		return m.handleKeyPress(msg.String())

	// ─── Data loaded messages ────────────────────────────────────────────
	case updateCheckMsg:
		m.UpdateStatus = msg.result.Status
		m.UpdateMsg = msg.result.Message
		return m, nil

	case statsLoadedMsg:
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.Stats = msg.stats
		return m, nil

	case searchResultsMsg:
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.SearchResults = msg.results
		m.SearchQuery = msg.query
		m.Screen = ScreenSearchResults
		m.Cursor = 0
		m.Scroll = 0
		return m, nil

	case recentObservationsMsg:
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.RecentObservations = msg.observations
		return m, nil

	case observationDetailMsg:
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.SelectedObservation = msg.observation
		m.Screen = ScreenObservationDetail
		m.DetailScroll = 0
		return m, nil

	case timelineMsg:
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.Timeline = msg.timeline
		m.Screen = ScreenTimeline
		m.Scroll = 0
		return m, nil

	case recentSessionsMsg:
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.Sessions = msg.sessions
		if m.Screen == ScreenSessions {
			if len(m.Sessions) == 0 {
				m.Cursor = 0
				m.Scroll = 0
			} else if m.Cursor >= len(m.Sessions) {
				m.Cursor = len(m.Sessions) - 1
				if m.Scroll > m.Cursor {
					m.Scroll = m.Cursor
				}
			}
		}
		return m, nil

	case sessionObservationsMsg:
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.SessionObservations = msg.observations
		m.Screen = ScreenSessionDetail
		m.Cursor = 0
		m.SessionDetailScroll = 0
		return m, nil

	case sessionDeletedMsg:
		m = m.resetSessionDeleteState()
		if msg.err != nil {
			m.ErrorMsg = sessionDeleteErrorMessage(msg.sessionID, msg.err)
			return m, nil
		}
		m.ErrorMsg = ""
		return m, loadRecentSessions(m.reader)

	case setupInstallMsg:
		m.SetupInstalling = false
		if msg.err != nil {
			m.SetupDone = true
			m.SetupError = msg.err.Error()
			return m, nil
		}
		m.SetupResult = msg.result
		m.SetupError = ""
		// For claude-code, show allowlist prompt before marking done
		if msg.result != nil && msg.result.Agent == "claude-code" {
			m.SetupAllowlistPrompt = true
			return m, nil
		}
		m.SetupDone = true
		return m, nil

	case shared.CopiedMsg:
		// Emit the OSC 52 sequence to stdout so the terminal copies the content,
		// set the feedback label, and schedule its removal after 2 seconds.
		m.CopyFeedback = "✓ Copied!"
		return m, tea.Batch(
			tea.Println(msg.Sequence),
			shared.ClearFeedbackAfter(2*time.Second),
		)

	case shared.ClearFeedbackMsg:
		m.CopyFeedback = ""
		return m, nil

	case spinner.TickMsg:
		// Only forward spinner ticks when we're actually installing
		if m.SetupInstalling {
			var cmd tea.Cmd
			m.SetupSpinner, cmd = m.SetupSpinner.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	return m, nil
}

// ─── Key Press Router ────────────────────────────────────────────────────────

func (m Model) handleKeyPress(key string) (tabs.Tab, tea.Cmd) {
	// Clear error on any keypress
	m.ErrorMsg = ""

	switch m.Screen {
	case ScreenDashboard:
		return m.handleDashboardKeys(key)
	case ScreenSearch:
		return m.handleSearchKeys(key)
	case ScreenSearchResults:
		return m.handleSearchResultsKeys(key)
	case ScreenRecent:
		return m.handleRecentKeys(key)
	case ScreenObservationDetail:
		return m.handleObservationDetailKeys(key)
	case ScreenTimeline:
		return m.handleTimelineKeys(key)
	case ScreenSessions:
		return m.handleSessionsKeys(key)
	case ScreenSessionDetail:
		return m.handleSessionDetailKeys(key)
	case ScreenSetup:
		return m.handleSetupKeys(key)
	}
	return m, nil
}

// ─── Dashboard ───────────────────────────────────────────────────────────────

var dashboardMenuItems = []string{
	"Search memories",
	"Recent observations",
	"Browse sessions",
	"Setup agent plugin",
	"Cloud sync settings",
	"Quit",
}

func (m Model) handleDashboardKeys(key string) (tabs.Tab, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
		}
	case "down", "j":
		if m.Cursor < len(dashboardMenuItems)-1 {
			m.Cursor++
		}
	case "enter", " ":
		return m.handleDashboardSelection()
	case "s", "/":
		m.PrevScreen = ScreenDashboard
		m.Screen = ScreenSearch
		m.Cursor = 0
		m.SearchInput.SetValue("")
		m.SearchInput.Focus()
		return m, nil
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleDashboardSelection() (tabs.Tab, tea.Cmd) {
	switch m.Cursor {
	case 0: // Search
		m.PrevScreen = ScreenDashboard
		m.Screen = ScreenSearch
		m.Cursor = 0
		m.SearchInput.SetValue("")
		m.SearchInput.Focus()
		return m, nil
	case 1: // Recent observations
		m.PrevScreen = ScreenDashboard
		m.Screen = ScreenRecent
		m.Cursor = 0
		m.Scroll = 0
		return m, loadRecentObservations(m.reader)
	case 2: // Sessions
		m.PrevScreen = ScreenDashboard
		m.Screen = ScreenSessions
		m.Cursor = 0
		m.Scroll = 0
		return m, loadRecentSessions(m.reader)
	case 3: // Setup
		m.PrevScreen = ScreenDashboard
		m.Screen = ScreenSetup
		m.Cursor = 0
		m.SetupAgents = setup.SupportedAgents()
		m.SetupResult = nil
		m.SetupError = ""
		m.SetupDone = false
		m.SetupInstalling = false
		m.SetupInstallingName = ""
		return m, nil
	case 4: // Cloud sync settings
		// Cloud lives in its own tab; ask the root to activate it. The cursor
		// is reset here so returning to the dashboard lands on the first item,
		// exactly as leaving any other screen does.
		m.PrevScreen = ScreenDashboard
		m.Cursor = 0
		return m, tabs.Navigate(tabs.Cloud)
	case 5: // Quit
		return m, tea.Quit
	}
	return m, nil
}

// ─── Search Input ────────────────────────────────────────────────────────────

func (m Model) handleSearchInputKeys(msg tea.KeyMsg) (tabs.Tab, tea.Cmd) {
	switch msg.String() {
	case "enter":
		query := m.SearchInput.Value()
		if query != "" {
			m.SearchInput.Blur()
			return m, searchMemories(m.reader, query)
		}
		return m, nil
	case "esc":
		m.SearchInput.Blur()
		m.Screen = ScreenDashboard
		m.Cursor = 0
		return m, nil
	}

	// Let the text input component handle everything else
	var cmd tea.Cmd
	m.SearchInput, cmd = m.SearchInput.Update(msg)
	return m, cmd
}

func (m Model) handleSearchKeys(key string) (tabs.Tab, tea.Cmd) {
	switch key {
	case "esc", "q":
		m.Screen = ScreenDashboard
		m.Cursor = 0
		return m, nil
	case "i", "/":
		m.SearchInput.Focus()
		return m, nil
	}
	return m, nil
}

// ─── Search Results ──────────────────────────────────────────────────────────

func (m Model) handleSearchResultsKeys(key string) (tabs.Tab, tea.Cmd) {
	visibleItems := shared.VisibleItems(m.Height, searchResultsChrome, observationItemLines, minVisibleItems)

	switch key {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
			// Scroll up if cursor goes above visible area
			if m.Cursor < m.Scroll {
				m.Scroll = m.Cursor
			}
		}
	case "down", "j":
		if m.Cursor < len(m.SearchResults)-1 {
			m.Cursor++
			// Scroll down if cursor goes below visible area
			if m.Cursor >= m.Scroll+visibleItems {
				m.Scroll = m.Cursor - visibleItems + 1
			}
		}
	case "enter":
		if len(m.SearchResults) > 0 && m.Cursor < len(m.SearchResults) {
			obsID := m.SearchResults[m.Cursor].ID
			m.PrevScreen = ScreenSearchResults
			return m, loadObservationDetail(m.reader, obsID)
		}
	case "c":
		if len(m.SearchResults) > 0 && m.Cursor < len(m.SearchResults) {
			return m, shared.Copy(m.SearchResults[m.Cursor].Content)
		}
	case "t":
		// Timeline for selected result
		if len(m.SearchResults) > 0 && m.Cursor < len(m.SearchResults) {
			obsID := m.SearchResults[m.Cursor].ID
			m.PrevScreen = ScreenSearchResults
			return m, loadTimeline(m.reader, obsID)
		}
	case "/", "s":
		m.PrevScreen = ScreenSearchResults
		m.Screen = ScreenSearch
		m.SearchInput.Focus()
		return m, nil
	case "esc", "q":
		m.PrevScreen = ScreenDashboard
		m.Screen = ScreenSearch
		m.Cursor = 0
		m.Scroll = 0
		m.SearchInput.Focus()
		return m, nil
	}
	return m, nil
}

// ─── Recent Observations ─────────────────────────────────────────────────────

func (m Model) handleRecentKeys(key string) (tabs.Tab, tea.Cmd) {
	visibleItems := shared.VisibleItems(m.Height, recentChrome, observationItemLines, minVisibleItems)

	switch key {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
			if m.Cursor < m.Scroll {
				m.Scroll = m.Cursor
			}
		}
	case "down", "j":
		if m.Cursor < len(m.RecentObservations)-1 {
			m.Cursor++
			if m.Cursor >= m.Scroll+visibleItems {
				m.Scroll = m.Cursor - visibleItems + 1
			}
		}
	case "enter":
		if len(m.RecentObservations) > 0 && m.Cursor < len(m.RecentObservations) {
			obsID := m.RecentObservations[m.Cursor].ID
			m.PrevScreen = ScreenRecent
			return m, loadObservationDetail(m.reader, obsID)
		}
	case "c":
		if len(m.RecentObservations) > 0 && m.Cursor < len(m.RecentObservations) {
			return m, shared.Copy(m.RecentObservations[m.Cursor].Content)
		}
	case "t":
		if len(m.RecentObservations) > 0 && m.Cursor < len(m.RecentObservations) {
			obsID := m.RecentObservations[m.Cursor].ID
			m.PrevScreen = ScreenRecent
			return m, loadTimeline(m.reader, obsID)
		}
	case "esc", "q":
		m.Screen = ScreenDashboard
		m.Cursor = 0
		m.Scroll = 0
		return m, loadStats(m.reader)
	}
	return m, nil
}

// ─── Observation Detail ──────────────────────────────────────────────────────

func (m Model) handleObservationDetailKeys(key string) (tabs.Tab, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.DetailScroll > 0 {
			m.DetailScroll--
		}
	case "down", "j":
		m.DetailScroll++
	case "c":
		if m.SelectedObservation != nil {
			return m, shared.Copy(m.SelectedObservation.Content)
		}
	case "t":
		// View timeline for this observation
		if m.SelectedObservation != nil {
			return m, loadTimeline(m.reader, m.SelectedObservation.ID)
		}
	case "esc", "q":
		m.Screen = m.PrevScreen
		m.Cursor = 0
		m.DetailScroll = 0
		return m, m.refreshScreen(m.PrevScreen)
	}
	return m, nil
}

// ─── Timeline ────────────────────────────────────────────────────────────────

func (m Model) handleTimelineKeys(key string) (tabs.Tab, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.Scroll > 0 {
			m.Scroll--
		}
	case "down", "j":
		m.Scroll++
	case "esc", "q":
		m.Screen = m.PrevScreen
		m.Cursor = 0
		m.Scroll = 0
		return m, m.refreshScreen(m.PrevScreen)
	}
	return m, nil
}

// ─── Sessions ────────────────────────────────────────────────────────────────

func (m Model) handleSessionsKeys(key string) (tabs.Tab, tea.Cmd) {
	switch m.SessionDeleteState {
	case SessionDeleteStateDeleting:
		return m, nil
	case SessionDeleteStatePrompt:
		switch key {
		case "y", "Y":
			if m.SessionDeleteID == "" {
				m = m.resetSessionDeleteState()
				return m, nil
			}
			sessionID := m.SessionDeleteID
			m.SessionDeleteState = SessionDeleteStateDeleting
			return m, deleteSession(m.reader, sessionID)
		case "n", "N", "esc":
			m = m.resetSessionDeleteState()
			return m, nil
		}
		return m, nil
	}

	visibleItems := shared.VisibleItems(m.Height, sessionsChrome, sessionItemLines, minVisibleSessions)

	switch key {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
			if m.Cursor < m.Scroll {
				m.Scroll = m.Cursor
			}
		}
	case "down", "j":
		if m.Cursor < len(m.Sessions)-1 {
			m.Cursor++
			if m.Cursor >= m.Scroll+visibleItems {
				m.Scroll = m.Cursor - visibleItems + 1
			}
		}
	case "enter":
		if len(m.Sessions) > 0 && m.Cursor < len(m.Sessions) {
			m.SelectedSessionIdx = m.Cursor
			m.PrevScreen = ScreenSessions
			sessionID := m.Sessions[m.Cursor].ID
			return m, loadSessionObservations(m.reader, sessionID)
		}
	case "d", "D":
		if len(m.Sessions) > 0 && m.Cursor < len(m.Sessions) {
			session := m.Sessions[m.Cursor]
			m.SessionDeleteState = SessionDeleteStatePrompt
			m.SessionDeleteID = session.ID
			m.SessionDeleteProject = session.Project
		}
	case "esc", "q":
		m.Screen = ScreenDashboard
		m.Cursor = 0
		m.Scroll = 0
		m = m.resetSessionDeleteState()
		return m, loadStats(m.reader)
	}
	return m, nil
}

// ─── Session Detail ──────────────────────────────────────────────────────────

func (m Model) handleSessionDetailKeys(key string) (tabs.Tab, tea.Cmd) {
	visibleItems := shared.VisibleItems(m.Height, sessionDetailChrome, observationItemLines, minVisibleItems)

	switch key {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
			if m.Cursor < m.SessionDetailScroll {
				m.SessionDetailScroll = m.Cursor
			}
		}
	case "down", "j":
		if m.Cursor < len(m.SessionObservations)-1 {
			m.Cursor++
			if m.Cursor >= m.SessionDetailScroll+visibleItems {
				m.SessionDetailScroll = m.Cursor - visibleItems + 1
			}
		}
	case "enter":
		if len(m.SessionObservations) > 0 && m.Cursor < len(m.SessionObservations) {
			obsID := m.SessionObservations[m.Cursor].ID
			m.PrevScreen = ScreenSessionDetail
			return m, loadObservationDetail(m.reader, obsID)
		}
	case "c":
		if len(m.SessionObservations) > 0 && m.Cursor < len(m.SessionObservations) {
			return m, shared.Copy(m.SessionObservations[m.Cursor].Content)
		}
	case "t":
		if len(m.SessionObservations) > 0 && m.Cursor < len(m.SessionObservations) {
			obsID := m.SessionObservations[m.Cursor].ID
			m.PrevScreen = ScreenSessionDetail
			return m, loadTimeline(m.reader, obsID)
		}
	case "esc", "q":
		m.Screen = ScreenSessions
		m.Cursor = m.SelectedSessionIdx
		m.SessionDetailScroll = 0
		return m, loadRecentSessions(m.reader)
	}
	return m, nil
}

// ─── Setup ───────────────────────────────────────────────────────────────────

func (m Model) handleSetupKeys(key string) (tabs.Tab, tea.Cmd) {
	// While installing, block all keys
	if m.SetupInstalling {
		return m, nil
	}

	// Allowlist prompt: y/n
	if m.SetupAllowlistPrompt {
		switch key {
		case "y", "Y":
			m.SetupAllowlistPrompt = false
			m.SetupDone = true
			if err := addClaudeCodeAllowlistFn(); err != nil {
				m.SetupAllowlistError = err.Error()
			} else {
				m.SetupAllowlistApplied = true
			}
			return m, nil
		case "n", "N", "esc":
			m.SetupAllowlistPrompt = false
			m.SetupDone = true
			return m, nil
		}
		return m, nil
	}

	// After install completed, any key goes back
	if m.SetupDone {
		switch key {
		case "esc", "q", "enter":
			m.Screen = ScreenDashboard
			m.Cursor = 0
			m.SetupDone = false
			m.SetupResult = nil
			m.SetupError = ""
			m.SetupAllowlistApplied = false
			m.SetupAllowlistError = ""
			return m, loadStats(m.reader)
		}
		return m, nil
	}

	switch key {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
		}
	case "down", "j":
		if m.Cursor < len(m.SetupAgents)-1 {
			m.Cursor++
		}
	case "enter":
		if len(m.SetupAgents) > 0 && m.Cursor < len(m.SetupAgents) {
			agent := m.SetupAgents[m.Cursor]
			m.SetupInstalling = true
			m.SetupInstallingName = agent.Name
			return m, tea.Batch(m.SetupSpinner.Tick, installAgent(agent.Name))
		}
	case "esc", "q":
		m.Screen = ScreenDashboard
		m.Cursor = 0
		return m, loadStats(m.reader)
	}
	return m, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func (m Model) resetSessionDeleteState() Model {
	m.SessionDeleteState = SessionDeleteStateNone
	m.SessionDeleteID = ""
	m.SessionDeleteProject = ""
	return m
}

func sessionDeleteErrorMessage(sessionID string, err error) string {
	if errors.Is(err, store.ErrSessionHasObservations) {
		return fmt.Sprintf("Cannot delete session %q: it still has observations. Delete or move observations first.", sessionID)
	}
	if errors.Is(err, store.ErrSessionNotFound) {
		return fmt.Sprintf("Cannot delete session %q: session not found.", sessionID)
	}
	return fmt.Sprintf("Failed to delete session %q: %v", sessionID, err)
}

// refreshScreen returns the appropriate data-loading Cmd for a given screen.
// Used when navigating back so lists show fresh data from the DB.
func (m Model) refreshScreen(screen Screen) tea.Cmd {
	switch screen {
	case ScreenDashboard:
		return loadStats(m.reader)
	case ScreenRecent:
		return loadRecentObservations(m.reader)
	case ScreenSessions:
		return loadRecentSessions(m.reader)
	default:
		return nil
	}
}
