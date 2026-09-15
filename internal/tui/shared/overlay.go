package shared

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// escape is the byte every ANSI sequence starts with. Composite looks for it
// to decide whether a seam needs a reset at all.
const escape = '\x1b'

// Composite draws panel over base at cell column x and row y, and returns the
// result. It is the layering primitive bubbles does not ship in the v1 line
// this fork stays on: an overlay that replaced the body outright threw away
// the context the user opened it from, and there is no reason a help panel
// should erase the list it is describing.
//
// Cells are terminal cells, not bytes and not runes: a base line styled with
// SGR sequences, or holding double-width text, is cut on the same grid the
// terminal will draw it on.
//
// A panel row that falls outside base's own rows is dropped rather than
// appended — an overlay cannot paint where the frame does not reach. A panel
// that starts left of column zero is clipped on that side instead. Base lines
// shorter than x are padded with spaces, so a panel can sit past the ragged
// right edge of a body without pulling its own row back to the left.
func Composite(base, panel string, x, y int) string {
	if panel == "" {
		return base
	}

	baseLines := strings.Split(base, "\n")
	for i, panelLine := range strings.Split(panel, "\n") {
		row := y + i
		if row < 0 || row >= len(baseLines) {
			continue
		}
		baseLines[row] = compositeLine(baseLines[row], panelLine, x)
	}
	return strings.Join(baseLines, "\n")
}

// compositeLine replaces the cells [x, x+width(panelLine)) of baseLine with
// panelLine, keeping whatever base cells sit either side of it.
func compositeLine(baseLine, panelLine string, x int) string {
	if x < 0 {
		// The panel hangs off the left edge: drop the cells that would land
		// at a negative column rather than shifting the whole row right.
		panelLine = ansi.Cut(panelLine, -x, ansi.StringWidth(panelLine))
		x = 0
	}

	panelWidth := ansi.StringWidth(panelLine)
	if panelWidth == 0 {
		return baseLine
	}

	baseWidth := ansi.StringWidth(baseLine)

	left := ansi.Cut(baseLine, 0, min(x, baseWidth))
	if baseWidth < x {
		left += strings.Repeat(" ", x-baseWidth)
	}

	right := ""
	if end := x + panelWidth; end < baseWidth {
		right = ansi.Cut(baseLine, end, baseWidth)
	}

	// The seams carry a reset only when there is styling to close. A frame
	// rendered without a terminal has no SGR in it at all — lipgloss degrades
	// to plain text — and an unconditional reset would be the only escape on
	// that screen, which is exactly what a portable golden must not contain.
	return left + resetAfter(left) + panelLine + resetAfter(panelLine) + right
}

// resetAfter returns the SGR reset when s leaves styling open, and nothing
// when s carries no escape sequence at all.
func resetAfter(s string) string {
	if strings.ContainsRune(s, escape) {
		return "\x1b[0m"
	}
	return ""
}

// Centered draws panel over base in the middle of a width × height area.
// A panel larger than the area is pinned to the top-left corner and clipped
// by Composite rather than centred off-screen.
func Centered(base, panel string, width, height int) string {
	x := (width - lipgloss.Width(panel)) / 2
	y := (height - lipgloss.Height(panel)) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return Composite(base, panel, x, y)
}
