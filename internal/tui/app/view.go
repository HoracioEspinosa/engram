package app

// View draws the application frame around the active tab's body.
func (m Model) View() string {
	tab := m.tab(m.active)
	if tab == nil {
		return m.styles.App.Render("Unknown tab")
	}
	return m.styles.App.Render(tab.View())
}
