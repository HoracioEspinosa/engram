package shared

import (
	"fmt"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/x/ansi"
)

const (
	// observationTypeCells is the width of the type badge shared by every
	// observation list, in terminal cells.
	observationTypeCells = 12
	// defaultTitleCells and defaultPreviewCells are what a row falls back to
	// when the screen has no width yet — the two constants this row carried
	// before it could measure.
	defaultTitleCells   = 50
	defaultPreviewCells = 80
	// minTitleCells is the narrowest title worth drawing.
	minTitleCells = 12
)

// ObservationLine is one row of the two-line observation list: an identity
// line with badges and a dimmed content preview underneath.
type ObservationLine struct {
	ID        int64
	Type      string
	Title     string
	Content   string
	CreatedAt string
	Project   *string
	State     string
	Pinned    bool
	// Selected draws the cursor marker and the highlighted title style.
	Selected bool
	// Width is the screen's own width in cells. The title and the preview
	// are cut to whatever is left of it once the fixed fields are paid, so
	// the row fits the terminal it is drawn in.
	Width int
}

// ObservationListItem renders one observation row, preview included. The
// returned string always ends in a newline, and carries a second one when the
// observation has content to preview.
func ObservationListItem(st theme.Styles, line ObservationLine) string {
	titleStyle := st.ListItem
	if line.Selected {
		titleStyle = st.ListSelected
	}
	cursor := RowCursor(st, line.Selected)

	project := ""
	if line.Project != nil {
		project = "  " + st.Project.Render(*line.Project)
	}

	stateBadge := ""
	if line.State == store.ObservationStateNeedsReview {
		stateBadge = " " + st.StateWarningBadge.Render("["+store.ObservationStateNeedsReview+"]")
	}
	pinBadge := ""
	if line.Pinned {
		pinBadge = " " + st.DetailValue.Render("[pinned]")
	}

	timestamp := LocalTime(line.CreatedAt)

	// The identity line's fixed parts are measured rather than counted: a
	// budget worked out by adding up constants drifts the moment a badge or
	// a separator changes, and the row that overflows is the one nobody
	// re-derived.
	scaffold := func() string {
		return fmt.Sprintf("%s%s %s%s%s %s%s  %s",
			cursor,
			fmt.Sprintf("#%-5d", line.ID),
			"["+PadCells(line.Type, observationTypeCells)+"]",
			stateBadge, pinBadge, "", project, timestamp)
	}

	width := line.Width
	if width <= 0 {
		// A screen that never learned its size keeps the widths the two-line
		// row was written against, rather than collapsing to nothing.
		width = defaultTitleCells + ansi.StringWidth(scaffold())
	}

	// A narrow terminal sheds the row's optional fields in order of what the
	// reader can spare, rather than squeezing the title — which is the point
	// of the row — down to nothing or letting it run off the screen.
	for _, drop := range []func(){
		func() { pinBadge = "" },
		func() { project = "" },
		func() { stateBadge = "" },
		func() { timestamp = LocalDate(line.CreatedAt) },
	} {
		if width-ansi.StringWidth(scaffold()) >= minTitleCells {
			break
		}
		drop()
	}

	titleWidth := width - ansi.StringWidth(scaffold())
	if titleWidth < minTitleCells {
		titleWidth = minTitleCells
	}

	rendered := fmt.Sprintf("%s%s %s%s%s %s%s  %s\n",
		cursor,
		st.ID.Render(fmt.Sprintf("#%-5d", line.ID)),
		st.TypeBadge.Render("["+PadCells(line.Type, observationTypeCells)+"]"),
		stateBadge,
		pinBadge,
		titleStyle.Render(Truncate(line.Title, titleWidth)),
		project,
		st.Timestamp.Render(timestamp))

	previewWidth := width - ansi.StringWidth(cursor)
	if line.Width <= 0 {
		previewWidth = defaultPreviewCells
	}
	if preview := Truncate(line.Content, previewWidth); preview != "" {
		rendered += st.ContentPreview.Render(preview) + "\n"
	}

	return rendered
}

// RowCursor is the two-cell marker drawn left of the selected row. The glyph
// comes from the icon vocabulary, so the marker follows the resolved icon
// mode like every other one.
func RowCursor(st theme.Styles, selected bool) string {
	if !selected {
		return "  "
	}
	return st.Icons.Glyph(theme.IconChevronRight) + " "
}

// ObservationState renders a lifecycle state, warning-coloured when the
// observation is waiting for a review.
func ObservationState(st theme.Styles, state string) string {
	if state == store.ObservationStateNeedsReview {
		return st.StateWarningBadge.Render(state)
	}
	return st.DetailValue.Render(state)
}

// Menu renders a vertical list of selectable items with a cursor on the
// current one. The returned string ends in a newline.
func Menu(st theme.Styles, items []string, cursor int) string {
	var b strings.Builder
	for i, item := range items {
		if i == cursor {
			b.WriteString(st.MenuSelected.Render(RowCursor(st, true) + item))
		} else {
			b.WriteString(st.MenuItem.Render(RowCursor(st, false) + item))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// RangeIndicator renders the "showing 1-6 of 7" footer of a scrolling list.
// The noun names what is being counted, so a line-scrolled detail view reads
// "line 3-20 of 41" with the same widget.
func RangeIndicator(st theme.Styles, noun string, from, to, total int) string {
	return fmt.Sprintf("\n  %s",
		st.Timestamp.Render(fmt.Sprintf("%s %d-%d of %d", noun, from, to, total)))
}

// VisibleItems is how many list rows fit in a viewport.
//
// chrome is the number of lines the surrounding header, footer and indicator
// consume; perItem is the height of one row; minimum is the floor applied when
// the terminal is too short to honour the arithmetic. Scroll handling in
// Update and the row window in View must be computed with the same call, or
// the cursor drifts out of the rendered window.
func VisibleItems(height, chrome, perItem, minimum int) int {
	n := (height - chrome) / perItem
	if n < minimum {
		return minimum
	}
	return n
}
