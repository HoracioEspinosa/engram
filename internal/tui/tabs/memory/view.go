package memory

import (
	"fmt"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/version"
)

// ─── Logo ────────────────────────────────────────────────────────────────────

func (m Model) renderLogo(version string) string {
	logoText := []string{
		`███████ ███    ██  ██████  ██████   █████  ███    ███ `,
		`██      ████   ██ ██       ██   ██ ██   ██ ████  ████ `,
		`█████   ██ ██  ██ ██   ███ ██████  ███████ ██ ████ ██ `,
		`██      ██  ██ ██ ██    ██ ██   ██ ██   ██ ██  ██  ██ `,
		`███████ ██   ████  ██████  ██   ██ ██   ██ ██      ██ `,
	}

	var b strings.Builder

	// Header line inside box (Cyber-Elephant Terminal)
	b.WriteString(m.styles.LogoAccent.Render(" 🐘 SYSTEM ONLINE ") + strings.Repeat(" ", 32) + m.styles.LogoAccent.Render(" MEM: OK 100% ") + "\n\n")

	// Logo body with gradient (logoText and the gradient are the same length)
	for i, line := range logoText {
		b.WriteString(" " + m.styles.LogoGradient[i].Render(line) + "\n")
	}
	b.WriteString("\n")

	// Footer inside box
	b.WriteString(m.styles.LogoTagline.Render(" > engram " + version + " — An elephant never forgets"))

	return m.styles.LogoFrame.Render(b.String()) + "\n"
}

// ─── View (screen router) ────────────────────────────────────────────────────

// View renders the active screen followed by the transient banners the tab
// owns: the error line and the clipboard confirmation. The root wraps the
// result in the application frame.
func (m Model) View() string {
	var content string

	switch m.Screen {
	case ScreenDashboard:
		content = m.viewDashboard()
	case ScreenSearch:
		content = m.viewSearch()
	case ScreenSearchResults:
		content = m.viewSearchResults()
	case ScreenRecent:
		content = m.viewRecent()
	case ScreenObservationDetail:
		content = m.viewObservationDetail()
	case ScreenTimeline:
		content = m.viewTimeline()
	case ScreenSessions:
		content = m.viewSessions()
	case ScreenSessionDetail:
		content = m.viewSessionDetail()
	case ScreenSetup:
		content = m.viewSetup()
	default:
		content = "Unknown screen"
	}

	// Show error if present
	if m.ErrorMsg != "" {
		content += "\n" + m.styles.Error.Render("Error: "+m.ErrorMsg)
	}

	// Show clipboard copy feedback if present
	if m.CopyFeedback != "" {
		content += "\n" + m.styles.Notice.Render(m.CopyFeedback)
	}

	return content
}

// ─── Dashboard ───────────────────────────────────────────────────────────────

func (m Model) viewDashboard() string {
	var b strings.Builder

	// Logo header
	b.WriteString(m.renderLogo(m.Version))
	b.WriteString("\n")

	// Update notification
	if m.UpdateMsg != "" {
		bannerStyle := m.styles.UpdateBanner
		if m.UpdateStatus == version.StatusCheckFailed {
			bannerStyle = m.styles.Error
		}
		b.WriteString(bannerStyle.Render(m.UpdateMsg))
		b.WriteString("\n\n")
	}

	// Stats card
	if m.Stats != nil {
		statsContent := fmt.Sprintf(
			"%s %s\n%s %s\n%s %s\n%s %s",
			m.styles.StatNumber.Render(fmt.Sprintf("%d", m.Stats.TotalSessions)),
			m.styles.StatLabel.Render("sessions"),
			m.styles.StatNumber.Render(fmt.Sprintf("%d", m.Stats.TotalObservations)),
			m.styles.StatLabel.Render("observations"),
			m.styles.StatNumber.Render(fmt.Sprintf("%d", m.Stats.TotalPrompts)),
			m.styles.StatLabel.Render("prompts"),
			m.styles.StatNumber.Render(fmt.Sprintf("%d", len(m.Stats.Projects))),
			m.styles.StatLabel.Render("projects"),
		)
		b.WriteString(m.styles.StatCard.Render(statsContent))
		b.WriteString("\n")

		if len(m.Stats.Projects) > 0 {
			b.WriteString(m.styles.Title.Render("  Projects"))
			b.WriteString("\n")

			limit := 5
			for i, p := range m.Stats.Projects {
				if i >= limit {
					break
				}
				b.WriteString(m.styles.ListItem.Render("• " + p))
				b.WriteString("\n")
			}

			if len(m.Stats.Projects) > limit {
				remaining := len(m.Stats.Projects) - limit
				b.WriteString(fmt.Sprintf("    %s\n", m.styles.Timestamp.Render(fmt.Sprintf("...and %d more projects", remaining))))
			}
			b.WriteString("\n")
		}
	} else {
		b.WriteString(m.styles.StatCard.Render("Loading stats..."))
		b.WriteString("\n")
	}

	// Menu
	b.WriteString(m.styles.Title.Render("  Actions"))
	b.WriteString("\n")
	b.WriteString(shared.Menu(m.styles, dashboardMenuItems, m.Cursor))

	// Help
	b.WriteString(m.styles.Help.Render("\n  j/k navigate • enter select • s search • q quit"))

	return b.String()
}

// ─── Search ──────────────────────────────────────────────────────────────────

func (m Model) viewSearch() string {
	var b strings.Builder

	b.WriteString(m.styles.Header.Render("  Search Memories"))
	b.WriteString("\n\n")

	b.WriteString(m.styles.SearchInput.Render(m.SearchInput.View()))
	b.WriteString("\n\n")

	b.WriteString(m.styles.Help.Render("  Type a query and press enter • esc go back"))

	return b.String()
}

// ─── Search Results ──────────────────────────────────────────────────────────

func (m Model) viewSearchResults() string {
	var b strings.Builder

	resultCount := len(m.SearchResults)
	header := fmt.Sprintf("  Search: %q — %d result", m.SearchQuery, resultCount)
	if resultCount != 1 {
		header += "s"
	}
	b.WriteString(m.styles.Header.Render(header))
	b.WriteString("\n")

	if resultCount == 0 {
		b.WriteString(m.styles.NoResults.Render("No memories found. Try a different query."))
		b.WriteString("\n\n")
		b.WriteString(m.styles.Help.Render("  / new search • esc back"))
		return b.String()
	}

	visibleItems := shared.VisibleItems(m.Height, searchResultsChrome, observationItemLines, minVisibleItems)

	end := m.Scroll + visibleItems
	if end > resultCount {
		end = resultCount
	}

	for i := m.Scroll; i < end; i++ {
		r := m.SearchResults[i]
		b.WriteString(m.renderObservationListItem(i, r.ID, r.Type, r.Title, r.Content, r.CreatedAt, r.Project, r.State(), r.ReviewAfter, r.Pinned))
	}

	// Scroll indicator
	if resultCount > visibleItems {
		b.WriteString(shared.RangeIndicator(m.styles, "showing", m.Scroll+1, end, resultCount))
	}

	b.WriteString(m.styles.Help.Render("\n  j/k navigate • enter detail • c copy • t timeline • / search • esc back"))

	return b.String()
}

// ─── Recent Observations ─────────────────────────────────────────────────────

func (m Model) viewRecent() string {
	var b strings.Builder

	count := len(m.RecentObservations)
	header := fmt.Sprintf("  Recent Observations — %d total", count)
	b.WriteString(m.styles.Header.Render(header))
	b.WriteString("\n")

	if count == 0 {
		b.WriteString(m.styles.NoResults.Render("No observations yet."))
		b.WriteString("\n\n")
		b.WriteString(m.styles.Help.Render("  esc back"))
		return b.String()
	}

	visibleItems := shared.VisibleItems(m.Height, recentChrome, observationItemLines, minVisibleItems)

	end := m.Scroll + visibleItems
	if end > count {
		end = count
	}

	for i := m.Scroll; i < end; i++ {
		o := m.RecentObservations[i]
		b.WriteString(m.renderObservationListItem(i, o.ID, o.Type, o.Title, o.Content, o.CreatedAt, o.Project, o.State(), o.ReviewAfter, o.Pinned))
	}

	if count > visibleItems {
		b.WriteString(shared.RangeIndicator(m.styles, "showing", m.Scroll+1, end, count))
	}

	b.WriteString(m.styles.Help.Render("\n  j/k navigate • enter detail • c copy • t timeline • esc back"))

	return b.String()
}

// ─── Observation Detail ──────────────────────────────────────────────────────

func (m Model) viewObservationDetail() string {
	var b strings.Builder

	if m.SelectedObservation == nil {
		b.WriteString(m.styles.Header.Render("  Observation Detail"))
		b.WriteString("\n")
		b.WriteString(m.styles.NoResults.Render("Loading..."))
		return b.String()
	}

	obs := m.SelectedObservation

	header := fmt.Sprintf("  Observation #%d", obs.ID)
	b.WriteString(m.styles.Header.Render(header))
	b.WriteString("\n")

	// Metadata rows
	b.WriteString(fmt.Sprintf("%s %s\n",
		m.styles.DetailLabel.Render("Type:"),
		m.styles.TypeBadge.Render(obs.Type)))

	b.WriteString(fmt.Sprintf("%s %s\n",
		m.styles.DetailLabel.Render("Title:"),
		m.styles.DetailValue.Bold(true).Render(obs.Title)))

	b.WriteString(fmt.Sprintf("%s %s\n",
		m.styles.DetailLabel.Render("Session:"),
		m.styles.ID.Render(obs.SessionID)))

	b.WriteString(fmt.Sprintf("%s %s\n",
		m.styles.DetailLabel.Render("Created:"),
		m.styles.Timestamp.Render(shared.LocalTime(obs.CreatedAt))))

	b.WriteString(fmt.Sprintf("%s %s\n",
		m.styles.DetailLabel.Render("State:"),
		shared.ObservationState(m.styles, obs.State())))

	b.WriteString(fmt.Sprintf("%s %s\n",
		m.styles.DetailLabel.Render("Pinned:"),
		m.styles.DetailValue.Render(fmt.Sprintf("%t", obs.Pinned))))

	if obs.ReviewAfter != nil {
		b.WriteString(fmt.Sprintf("%s %s\n",
			m.styles.DetailLabel.Render("Review:"),
			m.styles.Timestamp.Render(shared.FormatReviewDate(*obs.ReviewAfter))))
	}

	if obs.ToolName != nil {
		b.WriteString(fmt.Sprintf("%s %s\n",
			m.styles.DetailLabel.Render("Tool:"),
			m.styles.DetailValue.Render(*obs.ToolName)))
	}

	if obs.Project != nil {
		b.WriteString(fmt.Sprintf("%s %s\n",
			m.styles.DetailLabel.Render("Project:"),
			m.styles.Project.Render(*obs.Project)))
	}

	// Content section
	b.WriteString("\n")
	b.WriteString(m.styles.SectionHeading.Render("  Content"))
	b.WriteString("\n")

	// Wrap content based on terminal width
	wrapWidth := m.Width - detailWrapMargin
	if wrapWidth < minDetailWrap {
		wrapWidth = minDetailWrap
	}
	wrappedContent := m.styles.DetailContent.Width(wrapWidth).Render(obs.Content)

	// Split wrapped content into lines
	contentLines := strings.Split(wrappedContent, "\n")
	maxLines := m.Height - detailChrome
	if maxLines < minDetailLines {
		maxLines = minDetailLines
	}

	// Clamp scroll
	maxScroll := len(contentLines) - maxLines
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.DetailScroll > maxScroll {
		m.DetailScroll = maxScroll
	}

	end := m.DetailScroll + maxLines
	if end > len(contentLines) {
		end = len(contentLines)
	}

	for i := m.DetailScroll; i < end; i++ {
		b.WriteString(contentLines[i])
		b.WriteString("\n")
	}

	if len(contentLines) > maxLines {
		b.WriteString(shared.RangeIndicator(m.styles, "line", m.DetailScroll+1, end, len(contentLines)))
	}

	b.WriteString(m.styles.Help.Render("\n  j/k scroll • c copy • t timeline • esc back"))

	return b.String()
}

// ─── Timeline ────────────────────────────────────────────────────────────────

func (m Model) viewTimeline() string {
	var b strings.Builder

	if m.Timeline == nil {
		b.WriteString(m.styles.Header.Render("  Timeline"))
		b.WriteString("\n")
		b.WriteString(m.styles.NoResults.Render("Loading..."))
		return b.String()
	}

	tl := m.Timeline
	header := fmt.Sprintf("  Timeline — Observation #%d (%d total in session)", tl.Focus.ID, tl.TotalInRange)
	b.WriteString(m.styles.Header.Render(header))
	b.WriteString("\n")

	// Session info
	if tl.SessionInfo != nil {
		b.WriteString(fmt.Sprintf("  %s %s  %s %s\n\n",
			m.styles.DetailLabel.Render("Session:"),
			m.styles.ID.Render(tl.SessionInfo.ID),
			m.styles.DetailLabel.Render("Project:"),
			m.styles.Project.Render(tl.SessionInfo.Project)))
	}

	// Before entries
	if len(tl.Before) > 0 {
		b.WriteString(m.styles.SectionHeading.Render("  Before"))
		b.WriteString("\n")
		for _, e := range tl.Before {
			b.WriteString(fmt.Sprintf("  %s %s %s  %s\n",
				m.styles.TimelineConnector.Render("│"),
				m.styles.ID.Render(fmt.Sprintf("#%-4d", e.ID)),
				m.styles.TypeBadge.Render(fmt.Sprintf("[%-12s]", e.Type)),
				m.styles.TimelineItem.Render(shared.Truncate(e.Title, 60))))
		}
		b.WriteString(fmt.Sprintf("  %s\n", m.styles.TimelineConnector.Render("│")))
	}

	// Focus (highlighted)
	focusContent := fmt.Sprintf("  %s %s  %s\n  %s",
		m.styles.ID.Render(fmt.Sprintf("#%d", tl.Focus.ID)),
		m.styles.TypeBadge.Render("["+tl.Focus.Type+"]"),
		m.styles.Emphasis.Render(tl.Focus.Title),
		m.styles.DetailContent.Render(shared.Truncate(tl.Focus.Content, 120)))
	b.WriteString(m.styles.TimelineFocus.Render(focusContent))
	b.WriteString("\n")

	// After entries
	if len(tl.After) > 0 {
		b.WriteString(fmt.Sprintf("  %s\n", m.styles.TimelineConnector.Render("│")))
		b.WriteString(m.styles.SectionHeading.Render("  After"))
		b.WriteString("\n")
		for _, e := range tl.After {
			b.WriteString(fmt.Sprintf("  %s %s %s  %s\n",
				m.styles.TimelineConnector.Render("│"),
				m.styles.ID.Render(fmt.Sprintf("#%-4d", e.ID)),
				m.styles.TypeBadge.Render(fmt.Sprintf("[%-12s]", e.Type)),
				m.styles.TimelineItem.Render(shared.Truncate(e.Title, 60))))
		}
	}

	b.WriteString(m.styles.Help.Render("\n  j/k scroll • esc back"))

	return b.String()
}

// ─── Sessions ────────────────────────────────────────────────────────────────

func (m Model) viewSessions() string {
	var b strings.Builder

	count := len(m.Sessions)
	header := fmt.Sprintf("  Sessions — %d total", count)
	b.WriteString(m.styles.Header.Render(header))
	b.WriteString("\n")

	switch m.SessionDeleteState {
	case SessionDeleteStateDeleting:
		b.WriteString("\n")
		b.WriteString(m.styles.SectionHeading.Render("  Deleting Session"))
		b.WriteString("\n\n")
		b.WriteString(m.styles.DetailContent.Render(fmt.Sprintf("  Deleting session %q...", m.SessionDeleteID)))
		b.WriteString("\n")
		return b.String()
	case SessionDeleteStatePrompt:
		b.WriteString("\n")
		b.WriteString(m.styles.SectionHeading.Render("  Confirm Session Delete"))
		b.WriteString("\n\n")
		b.WriteString(m.styles.DetailContent.Render(fmt.Sprintf("  Delete session %q from project %q?", m.SessionDeleteID, m.SessionDeleteProject)))
		b.WriteString("\n")
		b.WriteString(m.styles.Timestamp.Render("  Sessions with observations cannot be deleted; Engram will refuse unsafe deletes."))
		b.WriteString("\n\n")
		b.WriteString(m.styles.Help.Render("  [y] Delete  [n] Cancel  [esc] Cancel"))
		return b.String()
	}

	if count == 0 {
		b.WriteString(m.styles.NoResults.Render("No sessions yet."))
		b.WriteString("\n\n")
		b.WriteString(m.styles.Help.Render("  esc back"))
		return b.String()
	}

	visibleItems := shared.VisibleItems(m.Height, sessionsChrome, sessionItemLines, minVisibleSessions)

	end := m.Scroll + visibleItems
	if end > count {
		end = count
	}

	for i := m.Scroll; i < end; i++ {
		s := m.Sessions[i]
		cursor := "  "
		style := m.styles.ListItem
		if i == m.Cursor {
			cursor = "▸ "
			style = m.styles.ListSelected
		}

		summary := ""
		if s.Summary != nil {
			summary = shared.Truncate(*s.Summary, 50)
		}

		line := fmt.Sprintf("%s%s  %s  %s obs  %s",
			cursor,
			m.styles.Project.Render(fmt.Sprintf("%-20s", s.Project)),
			m.styles.Timestamp.Render(shared.LocalTime(s.StartedAt)),
			m.styles.StatNumber.Render(fmt.Sprintf("%d", s.ObservationCount)),
			style.Render(summary))

		b.WriteString(line)
		b.WriteString("\n")
	}

	if count > visibleItems {
		b.WriteString(shared.RangeIndicator(m.styles, "showing", m.Scroll+1, end, count))
	}

	b.WriteString(m.styles.Help.Render("\n  j/k navigate • enter view session • d delete • esc back"))

	return b.String()
}

// ─── Session Detail ──────────────────────────────────────────────────────────

func (m Model) viewSessionDetail() string {
	var b strings.Builder

	if m.SelectedSessionIdx >= len(m.Sessions) {
		b.WriteString(m.styles.Header.Render("  Session Detail"))
		b.WriteString("\n")
		b.WriteString(m.styles.NoResults.Render("Session not found."))
		return b.String()
	}

	sess := m.Sessions[m.SelectedSessionIdx]
	header := fmt.Sprintf("  Session: %s — %s", sess.Project, shared.LocalTime(sess.StartedAt))
	b.WriteString(m.styles.Header.Render(header))
	b.WriteString("\n")

	// Session metadata
	if sess.Summary != nil {
		b.WriteString(fmt.Sprintf("  %s %s\n\n",
			m.styles.DetailLabel.Render("Summary:"),
			m.styles.DetailValue.Render(*sess.Summary)))
	}

	count := len(m.SessionObservations)
	b.WriteString(m.styles.SectionHeading.Render(fmt.Sprintf("  Observations (%d)", count)))
	b.WriteString("\n")

	if count == 0 {
		b.WriteString(m.styles.NoResults.Render("No observations in this session."))
		b.WriteString("\n\n")
		b.WriteString(m.styles.Help.Render("  esc back"))
		return b.String()
	}

	visibleItems := shared.VisibleItems(m.Height, sessionDetailChrome, observationItemLines, minVisibleItems)

	end := m.SessionDetailScroll + visibleItems
	if end > count {
		end = count
	}

	for i := m.SessionDetailScroll; i < end; i++ {
		o := m.SessionObservations[i]
		b.WriteString(m.renderObservationListItem(i, o.ID, o.Type, o.Title, o.Content, o.CreatedAt, o.Project, o.State(), o.ReviewAfter, o.Pinned))
	}

	if count > visibleItems {
		b.WriteString(shared.RangeIndicator(m.styles, "showing", m.SessionDetailScroll+1, end, count))
	}

	b.WriteString(m.styles.Help.Render("\n  j/k navigate • enter detail • c copy • t timeline • esc back"))

	return b.String()
}

// ─── Setup ───────────────────────────────────────────────────────────────────

func (m Model) viewSetup() string {
	var b strings.Builder

	b.WriteString(m.styles.Header.Render("  Setup — Install Agent Plugin"))
	b.WriteString("\n")

	// Show spinner while installing
	if m.SetupInstalling {
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("  %s Installing %s plugin...\n",
			m.SetupSpinner.View(),
			m.styles.Emphasis.Render(m.SetupInstallingName)))
		b.WriteString("\n")

		switch m.SetupInstallingName {
		case "opencode":
			b.WriteString(m.styles.Timestamp.Render("  Copying plugin file to plugins directory"))
		case "claude-code":
			b.WriteString(m.styles.Timestamp.Render("  Running claude plugin marketplace add + install"))
		}

		b.WriteString("\n")
		return b.String()
	}

	// Show allowlist prompt after successful claude-code install
	if m.SetupAllowlistPrompt && m.SetupResult != nil {
		successMsg := fmt.Sprintf("Installed %s plugin", m.SetupResult.Agent)
		b.WriteString(fmt.Sprintf("\n  %s %s\n\n",
			m.styles.SuccessInline.Render("✓"),
			m.styles.SuccessInline.Render(successMsg)))

		b.WriteString(m.styles.SectionHeading.Render("  Permissions Allowlist"))
		b.WriteString("\n\n")
		b.WriteString(m.styles.DetailContent.Render("  Add engram tools to ~/.claude/settings.json allowlist?"))
		b.WriteString("\n")
		b.WriteString(m.styles.Timestamp.Render("  This prevents Claude Code from asking permission on every tool call."))
		b.WriteString("\n\n")
		b.WriteString(m.styles.Help.Render("  [y] Yes  [n] No"))
		return b.String()
	}

	// Show result after install
	if m.SetupDone {
		if m.SetupError != "" {
			b.WriteString(m.styles.Error.Render("  ✗ Installation failed: " + m.SetupError))
			b.WriteString("\n\n")
		} else if m.SetupResult != nil {
			successMsg := fmt.Sprintf("Installed %s plugin", m.SetupResult.Agent)
			if m.SetupResult.Files > 0 {
				successMsg += fmt.Sprintf(" (%d files)", m.SetupResult.Files)
			}
			b.WriteString(fmt.Sprintf("  %s %s\n",
				m.styles.SuccessInline.Render("✓"),
				m.styles.SuccessInline.Render(successMsg)))
			b.WriteString(fmt.Sprintf("  %s %s\n\n",
				m.styles.DetailLabel.Render("Location:"),
				m.styles.Project.Render(m.SetupResult.Destination)))

			// Post-install instructions
			switch m.SetupResult.Agent {
			case "opencode":
				b.WriteString(m.styles.SectionHeading.Render("  Next Steps"))
				b.WriteString("\n")
				b.WriteString(m.styles.DetailContent.Render("1. Restart OpenCode"))
				b.WriteString("\n")
				b.WriteString(m.styles.DetailContent.Render("2. Plugin is auto-loaded from ~/.config/opencode/plugins/"))
				b.WriteString("\n")
				b.WriteString(m.styles.DetailContent.Render("3. Make sure 'engram' is in your MCP config (opencode.json)"))
				b.WriteString("\n")
			case "claude-code":
				b.WriteString(m.styles.SectionHeading.Render("  Next Steps"))
				b.WriteString("\n")
				if m.SetupAllowlistApplied {
					b.WriteString(fmt.Sprintf("  %s %s\n",
						m.styles.SuccessInline.Render("✓"),
						m.styles.DetailContent.Render("Engram tools added to allowlist")))
				} else if m.SetupAllowlistError != "" {
					b.WriteString(fmt.Sprintf("  %s %s\n",
						m.styles.DangerInline.Render("✗"),
						m.styles.DetailContent.Render("Allowlist update failed: "+m.SetupAllowlistError)))
					b.WriteString(m.styles.DetailContent.Render("  Add manually to permissions.allow in ~/.claude/settings.json"))
					b.WriteString("\n")
				}
				b.WriteString(m.styles.DetailContent.Render("1. Restart Claude Code — the plugin is active immediately"))
				b.WriteString("\n")
				b.WriteString(m.styles.DetailContent.Render("2. Verify with: claude plugin list"))
				b.WriteString("\n")
			}
		}

		b.WriteString(m.styles.Help.Render("\n  enter/esc back to dashboard"))
		return b.String()
	}

	// Agent selection
	b.WriteString("\n")
	b.WriteString(m.styles.Title.Render("  Select an agent to set up"))
	b.WriteString("\n\n")

	for i, agent := range m.SetupAgents {
		if i == m.Cursor {
			b.WriteString(m.styles.MenuSelected.Render("▸ " + agent.Description))
		} else {
			b.WriteString(m.styles.MenuItem.Render("  " + agent.Description))
		}
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("      %s %s\n\n",
			m.styles.DetailLabel.Render("Install to:"),
			m.styles.Timestamp.Render(agent.InstallDir)))
	}

	b.WriteString(m.styles.Help.Render("\n  j/k navigate • enter install • esc back"))

	return b.String()
}

// ─── Shared Renderers ────────────────────────────────────────────────────────

// renderObservationListItem binds one observation to the tab's cursor and
// styles and renders it as a two-line row.
//
// reviewAfter is part of the caller's data but not of the row: the list shows
// only the needs_review badge, and the deadline itself belongs to the detail
// screen.
func (m Model) renderObservationListItem(index int, id int64, obsType, title, content, createdAt string, project *string, state string, reviewAfter *string, pinned bool) string {
	return shared.ObservationListItem(m.styles, shared.ObservationLine{
		ID:        id,
		Type:      obsType,
		Title:     title,
		Content:   content,
		CreatedAt: createdAt,
		Project:   project,
		State:     state,
		Pinned:    pinned,
		Selected:  index == m.Cursor,
	})
}
