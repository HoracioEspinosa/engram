package shared

import (
	"fmt"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"
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
}

// ObservationListItem renders one observation row, preview included. The
// returned string always ends in a newline, and carries a second one when the
// observation has content to preview.
func ObservationListItem(st theme.Styles, line ObservationLine) string {
	cursor := "  "
	titleStyle := st.ListItem
	if line.Selected {
		cursor = "▸ "
		titleStyle = st.ListSelected
	}

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

	rendered := fmt.Sprintf("%s%s %s%s%s %s%s  %s\n",
		cursor,
		st.ID.Render(fmt.Sprintf("#%-5d", line.ID)),
		st.TypeBadge.Render(fmt.Sprintf("[%-12s]", line.Type)),
		stateBadge,
		pinBadge,
		titleStyle.Render(Truncate(line.Title, 50)),
		project,
		st.Timestamp.Render(LocalTime(line.CreatedAt)))

	if preview := Truncate(line.Content, 80); preview != "" {
		rendered += st.ContentPreview.Render(preview) + "\n"
	}

	return rendered
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
			b.WriteString(st.MenuSelected.Render("▸ " + item))
		} else {
			b.WriteString(st.MenuItem.Render("  " + item))
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
