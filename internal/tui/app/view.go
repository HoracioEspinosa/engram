package app

import (
	"strings"

	"github.com/HoracioEspinosa/engram/internal/tui/shared"

	"github.com/charmbracelet/lipgloss"
)

// View draws the application frame around the active screen's body. The
// persistent tab bar (rfc-tui.md §5, §7) sits above the Dashboard and every
// tab.
//
// The footer belongs to the frame, not to the screen: it is rendered here
// from whatever the active screen declares in Help(), so a screen names its
// keys once and never prints them.
func (m Model) View() string {
	var body string

	if m.screen == screenDashboard {
		body = m.viewTabBar() + "\n" + m.viewDashboard()
	} else if tab := m.tab(m.active); tab == nil {
		body = m.viewTabBar() + "\n" + "Unknown tab"
	} else {
		body = m.viewTabBar() + "\n" + tab.View()
	}

	// Every overlay carries its own hints: the screen underneath is not
	// answering keys while one is open, so its own would name keys that do
	// nothing.
	if m.themePicker.open {
		return m.styles.App.Render(m.compose(body, m.viewThemePicker()))
	}

	if m.tree.open {
		return m.styles.App.Render(m.compose(body, m.viewProjectTree()))
	}

	if m.showHelp {
		return m.styles.App.Render(m.compose(body, m.viewHelpOverlay()))
	}

	if bar := m.viewStatusBar(); bar != "" {
		body = strings.TrimRight(body, "\n") + "\n" + bar
	}

	return m.styles.App.Render(body)
}

// bodyWidth is how many cells the frame's body may occupy: the terminal less
// the app frame's own padding. A root that has not received a
// tea.WindowSizeMsg yet assumes the width the wireframes were drawn at.
func (m Model) bodyWidth() int {
	if m.width <= 0 {
		return defaultBodyWidth
	}
	if w := m.width - bodyMargin; w >= minBodyWidth {
		return w
	}
	return minBodyWidth
}

// bodyMargin is what the app frame spends either side of the body,
// minBodyWidth the narrowest body worth laying out, and defaultBodyWidth
// what a root with no size yet assumes.
const (
	bodyMargin       = 4
	minBodyWidth     = 24
	defaultBodyWidth = 80
)

// overlayChromeCells and overlayChromeRows are what an overlay's own content
// must give back to whatever frames it: the app frame's horizontal padding,
// the panel's border and padding, and a margin either side so the screen
// underneath stays visible around it.
const (
	overlayChromeCells = 12
	overlayChromeRows  = 12
)

// compose centres panel over body instead of replacing it. An overlay that
// swapped the whole screen threw away the very thing it was describing —
// the list, the detail, the tab the user pressed "?" on — and left them
// navigating back by memory.
//
// The panel is framed in Overlay and never filled: §6.9's rule for a
// translucent terminal is that a panel is delimited by its border, so the
// cells Composite replaces carry the panel's own content and nothing else.
func (m Model) compose(body, panel string) string {
	framed := m.styles.Panel.Render(panel)

	// The frame's own padding is not part of body, so the area the panel is
	// centred in is the body's rendered extent rather than the terminal's —
	// widened to the panel when the screen underneath is the narrower of the
	// two.
	width := max(lipgloss.Width(body), lipgloss.Width(framed))
	height := max(lipgloss.Height(body), lipgloss.Height(framed))

	// Composite only paints on rows the base already has, so a body shorter
	// than the panel is grown first: an overlay clipped by whatever happened
	// to be underneath it would lose its own footer.
	if grow := height - lipgloss.Height(body); grow > 0 {
		body += strings.Repeat("\n", grow)
	}

	return shared.Centered(body, framed, width, height)
}
