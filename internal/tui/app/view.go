package app

// View draws the application frame around the active screen's body.
func (m Model) View() string {
	var body string

	switch m.screen {
	case screenDashboard:
		body = m.viewDashboard()
	case screenSelector:
		body = m.viewSelector()
	default:
		tab := m.tab(m.active)
		if tab == nil {
			body = "Unknown tab"
		} else {
			body = tab.View()
		}
	}

	return m.styles.App.Render(body)
}
