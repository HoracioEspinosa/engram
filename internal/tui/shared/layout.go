package shared

import (
	"image"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/lipgloss"
)

// Breakpoint names how much room a screen has to work with. It is the one
// place the widths are decided, so a tab asks what it may draw instead of
// hard-coding a column count it cannot honour at every size.
type Breakpoint int

const (
	// Compact is a terminal too narrow even for a comfortable single
	// column: rows shed their optional fields rather than wrapping.
	Compact Breakpoint = iota
	// Single fits one list at full width, with the detail reached by
	// pressing enter rather than shown alongside.
	Single
	// Split fits master and detail side by side.
	Split
)

const (
	// SingleWidth is where a single column stops being cramped.
	SingleWidth = 80
	// SplitWidth is where a second pane earns its keep: below it, halving
	// the width leaves neither pane able to show a path or a title whole.
	SplitWidth = 120

	// detailShare is the percentage of the usable width the detail pane
	// takes. The master keeps the larger half: it is the one being
	// navigated, and its columns are what the reader scans.
	detailShare = 45
	// paneGap separates the two panes.
	paneGap = 2
	// detailFrame is what the detail panel's border and padding spend before
	// its own text starts.
	detailFrame = 4
)

// BreakpointFor classifies a width.
func BreakpointFor(width int) Breakpoint {
	switch {
	case width >= SplitWidth:
		return Split
	case width >= SingleWidth:
		return Single
	default:
		return Compact
	}
}

// Regions is where a screen's two panes sit inside its own area, in cells.
// Detail is empty below the split breakpoint — there is no second pane to
// place, and a caller checks with HasDetail rather than comparing widths of
// its own.
//
// They are rectangles rather than two widths because the mouse needs them:
// resolving a click or a wheel event against a pane means asking which
// rectangle holds the point.
type Regions struct {
	Master image.Rectangle
	Detail image.Rectangle
}

// HasDetail reports whether this layout has a second pane at all.
func (r Regions) HasDetail() bool { return !r.Detail.Empty() }

// Layout divides a width × height area into a master pane and, at the split
// breakpoint, a detail pane beside it.
func Layout(width, height int) Regions {
	if width <= 0 || height <= 0 {
		return Regions{}
	}

	if BreakpointFor(width) != Split {
		return Regions{Master: image.Rect(0, 0, width, height)}
	}

	usable := width - paneGap
	detail := usable * detailShare / 100
	master := usable - detail

	return Regions{
		Master: image.Rect(0, 0, master, height),
		Detail: image.Rect(master+paneGap, 0, width, height),
	}
}

// Pane names which half of a master-detail screen has the keyboard. "h" and
// "l" move between them; below the split breakpoint there is only one, and
// the focus never leaves it.
type Pane int

const (
	// PaneMaster is the list.
	PaneMaster Pane = iota
	// PaneDetail is the panel beside it.
	PaneDetail
)

// FocusLeft answers "h": the master pane is always there, so it always wins.
func FocusLeft() Pane { return PaneMaster }

// FocusRight answers "l": it moves to the detail pane only when the layout
// has one, so the key is inert at a width that draws a single column rather
// than moving the focus somewhere invisible.
func FocusRight(r Regions) Pane {
	if !r.HasDetail() {
		return PaneMaster
	}
	return PaneDetail
}

// SplitPanes draws master and detail side by side inside r.
//
// The detail pane is separated by its border, not by a fill: the same
// translucent-terminal rule the overlay panel follows. Below the split
// breakpoint there is no second pane and the master is returned as it is.
func SplitPanes(st theme.Styles, r Regions, master, detail string) string {
	if !r.HasDetail() || detail == "" {
		return master
	}

	left := lipgloss.NewStyle().Width(r.Master.Dx()).MaxWidth(r.Master.Dx()).Render(master)
	// The panel's own border and padding come out of the pane's width, so
	// the two panes together never exceed the area they were given.
	inner := r.Detail.Dx() - detailFrame
	if inner < 1 {
		return master
	}
	right := st.Panel.Width(inner).MaxWidth(r.Detail.Dx()).Render(detail)

	return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", paneGap), right)
}
