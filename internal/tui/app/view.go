package app

import (
	"strings"

	"github.com/HoracioEspinosa/engram/internal/tui/shared"
)

// View draws the application frame around the active screen's body. The
// persistent tab bar (rfc-tui.md §5, §7) sits above the Dashboard and every
// tab; the Selector keeps its own distinct header instead (§5's S1
// wireframe), since there is no project yet to number tabs for.
//
// The footer belongs to the frame, not to the screen: it is rendered here
// from whatever the active screen declares in Help(), so a screen names its
// keys once and never prints them.
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

	if m.themePicker.open {
		// The theme overlay takes the screen the same way the "?" overlay
		// below does, and carries its own footer: the screen underneath is
		// not answering keys, so its hints would name keys that do nothing.
		return m.styles.App.Render(m.viewThemePicker())
	}

	if m.showHelp {
		// The "?" overlay (rfc-tui.md §7.1) replaces the body outright
		// rather than compositing over it: bubbles has no layering
		// primitive in the v1 line this fork stays on (rfc-tui.md §6), and
		// a full-screen swap keeps the overlay legible at 80 columns too.
		return m.styles.App.Render(m.viewHelpOverlay())
	}

	if hints := shared.HintsFrom(m.styles, m.activeScreenHelp(), m.width); hints != "" {
		body = strings.TrimRight(body, "\n") + "\n" + hints
	}

	return m.styles.App.Render(body)
}
