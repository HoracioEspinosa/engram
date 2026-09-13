package app

// View draws the application frame around the active screen's body. The
// persistent tab bar (rfc-tui.md §5, §7) sits above the Dashboard and every
// tab; the Selector keeps its own distinct header instead (§5's S1
// wireframe), since there is no project yet to number tabs for.
func (m Model) View() string {
	var body string

	switch m.screen {
	case screenDashboard:
		body = m.viewTabBar() + "\n" + m.viewDashboard()
	case screenSelector:
		body = m.viewSelector()
	default:
		tab := m.tab(m.active)
		if tab == nil {
			body = m.viewTabBar() + "\n" + "Unknown tab"
		} else {
			body = m.viewTabBar() + "\n" + tab.View()
		}
	}

	return m.styles.App.Render(body)
}
