package app

import (
	"strings"

	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// View draws the application frame around the active tab's body. The
// persistent tab bar sits above every one of them.
//
// The status bar belongs to the frame, not to the tab: its hints are rendered
// here from whatever the active tab declares in Help(), so a tab names its
// keys once and never prints them.
func (m Model) View() string {
	body := m.viewTabBar() + "\n"
	if tab := m.tab(m.active); tab == nil {
		body += "Unknown tab"
	} else {
		body += tab.View()
	}

	if panel := m.viewOverlay(); panel != "" {
		return m.styles.App.Render(m.compose(m.overlayCanvas(body), panel))
	}

	return m.styles.App.Render(m.withStatusBar(body))
}

// viewOverlay renders whichever panel currently holds the workspace, or "" for
// none. The order is the order they take the keyboard in.
//
// Every overlay carries its own hints: the screen underneath is not answering
// keys while one is open, so its own would name keys that do nothing.
func (m Model) viewOverlay() string {
	switch {
	case m.themePicker.open:
		return m.viewThemePicker()
	case m.palette.open:
		return m.viewPalette()
	case m.tree.open:
		return m.viewProjectTree()
	case m.showHelp:
		return m.viewHelpOverlay()
	}
	return ""
}

// withStatusBar puts the frame's own bottom row under the body, with the body
// cut to whatever is left of the terminal above it.
func (m Model) withStatusBar(body string) string {
	bar := m.viewStatusBar()
	body = m.clipRows(strings.TrimRight(body, "\n"), m.bodyRows())
	if bar == "" {
		return body
	}
	return body + "\n" + bar
}

// bodyRows is how many rows the frame may spend on the tab bar and the screen
// under it: the terminal, less the app frame's own padding and less the status
// bar pinned to the bottom.
//
// A root that has not been told the terminal's size yet reports 0, which its
// callers read as "no ceiling": there is nothing to measure against, and a
// frame cut to a size nobody declared would be worse than one drawn whole.
func (m Model) bodyRows() int {
	rows := m.contentHeight()
	if rows <= 0 {
		return 0
	}
	if m.viewStatusBar() != "" {
		rows--
	}
	return rows
}

// tabRows is how many rows a tab's own screen may occupy: the frame's body
// less the tab bar drawn above it. It is what the root tells every tab the
// terminal is, so a tab solves its viewport against the room it has rather
// than against the whole screen.
func (m Model) tabRows() int {
	rows := m.bodyRows()
	if rows <= 0 {
		return 0
	}
	return max(rows-1, 0)
}

// clipRows caps body at rows terminal lines and marks the cut on the last one
// it keeps.
//
// Something has to do this, and the alternative does it far worse: bubbletea's
// renderer discards the rows a frame does not have room for in the order it
// writes them, which is from the top. A screen one row too tall therefore
// loses its tab bar, not its last list row, and the reader is left on a screen
// with no chrome to say where they are. Cutting here loses the bottom instead,
// which is where a list already says "there was more".
//
// rows at zero or less is a root with no terminal to measure against, and the
// body is returned whole.
func (m Model) clipRows(body string, rows int) string {
	if rows <= 0 {
		return body
	}
	lines := strings.Split(body, "\n")
	if len(lines) <= rows {
		return body
	}
	lines = lines[:rows]
	lines[rows-1] = m.markClipped(lines[rows-1])
	return strings.Join(lines, "\n")
}

// markClipped ends a row with the mark that says the screen continues past it,
// paying for the mark out of the row's own width rather than past it.
func (m Model) markClipped(line string) string {
	trimmed := strings.TrimRight(line, " ")
	if strings.HasSuffix(trimmed, theme.Ellipsis) {
		return trimmed
	}
	width := m.bodyWidth()
	if ansi.StringWidth(trimmed) >= width {
		trimmed = ansi.Truncate(trimmed, width-1, "")
	}
	return trimmed + theme.Ellipsis
}

// overlayCanvas is the base an overlay is composed over: the frame the reader
// would have seen anyway, grown to the terminal so a centred panel lands in
// the middle of the screen rather than in the middle of whatever the tab
// underneath happened to draw.
//
// The status bar stays pinned to the bottom row. It reports the project, the
// task and the sync state, which are the frame's context and not the screen's:
// an overlay that took them off the display would answer "what is this?" while
// hiding "where am I?".
func (m Model) overlayCanvas(body string) string {
	rows := m.bodyRows()
	body = m.clipRows(strings.TrimRight(body, "\n"), rows)

	if grow := rows - lipgloss.Height(body); grow > 0 {
		body += strings.Repeat("\n", grow)
	}

	bar := m.viewStatusBar()
	if bar == "" {
		return body
	}
	return body + "\n" + bar
}

// overlayOpen reports whether any overlay currently holds the workspace.
//
// The four are listed once, here, because they are modal as a group: a caller
// that asked about three of them would let the fourth pass the pointer or a
// key through to the body it is covering.
func (m Model) overlayOpen() bool {
	return m.themePicker.open || m.palette.open || m.tree.open || m.showHelp
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

// contentHeight is how many rows the frame's body may occupy: the terminal
// less the app frame's own vertical padding.
//
// A root that has not received a tea.WindowSizeMsg yet has no terminal to
// measure, so it reports 0 and its callers fall back to what the content
// itself needs — the same fallback bodyWidth makes for the width.
func (m Model) contentHeight() int {
	if m.height <= 0 {
		return 0
	}
	return max(m.height-m.styles.App.GetPaddingTop()-m.styles.App.GetPaddingBottom(), 0)
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

// overlayFrameRows is what an overlay's own content gives back to the frame
// around it: the app frame's two rows of padding, the tab bar and the status
// bar the panel must not cover, and the panel's own top and bottom border.
//
// It is the ceiling for an overlay holding a list, which would otherwise grow
// with the rows it holds. The panels with fixed content cede overlayChromeRows
// instead — more than this, because content that fits in a dozen rows has no
// reason to take the whole screen.
const overlayFrameRows = 6

// defaultOverlayRows is what such an overlay may spend on a root that has not
// been told the terminal's size yet, on the geometry defaultBodyWidth assumes.
const defaultOverlayRows = 24 - overlayFrameRows

// compose centres panel over body instead of replacing it. An overlay that
// swapped the whole screen threw away the very thing it was describing —
// the list, the detail, the tab the user pressed "?" on — and left them
// navigating back by memory.
//
// The panel is framed in Overlay and never filled: on a translucent terminal
// a panel is delimited by its border, so the cells Composite replaces carry
// the panel's own content and nothing else.
func (m Model) compose(body, panel string) string {
	framed := m.styles.Panel.Render(panel)

	// The extent the panel is centred in is the terminal's, less the frame's
	// own padding — not the body's rendered size. A panel centred on the body
	// drifts with every screen it opens over, and over a short one it lands on
	// the tab bar and the status bar instead of between them. The body still
	// widens it and the panel widens and heightens it, so an unsized root and
	// an oversized panel both still have somewhere to be drawn.
	//
	// The body's own height is deliberately not part of the vertical extent:
	// a screen taller than the terminal would push the panel down by half of
	// its excess, and what leaves the frame first is the panel's top border
	// and title.
	width := max(m.bodyWidth(), lipgloss.Width(body), lipgloss.Width(framed))
	height := max(m.contentHeight(), lipgloss.Height(framed))

	// Composite only paints on rows the base already has, so a body shorter
	// than the panel is grown first: an overlay clipped by whatever happened
	// to be underneath it would lose its own footer.
	if grow := height - lipgloss.Height(body); grow > 0 {
		body += strings.Repeat("\n", grow)
	}

	return shared.Centered(body, framed, width, height)
}
