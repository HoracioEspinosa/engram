package shared

import (
	"strings"

	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// statusGap separates two segments of the same group.
const statusGap = "  "

// Segment is one item of the status bar.
//
// Priority decides what survives a narrow terminal: the lowest priority gives
// ground first, so the caller ranks its segments by how much the reader would
// miss them rather than by where they sit on the line.
type Segment struct {
	// Icon is drawn before Text, separated by a space. It is a glyph from
	// theme.Icons, never a literal — the vocabulary lives in one place.
	Icon string
	// Text is the segment's content.
	Text string
	// Short is what Text falls back to before the segment is dropped
	// outright: a breadcrumb keeps the child when its parents no longer fit.
	// Empty means the segment has no shorter form and is elided instead.
	Short string
	// Style paints the rendered segment.
	Style lipgloss.Style
	// Priority orders the give-ground: the lowest goes first, the highest
	// never gives at all.
	Priority int
	// MinWidth is how far the segment may be elided before it is worth
	// nothing and is dropped instead.
	MinWidth int
}

// plain is the segment's rendered text with its icon, before styling.
func (s Segment) plain() string {
	switch {
	case s.Icon == "":
		return s.Text
	case s.Text == "":
		return s.Icon
	default:
		return s.Icon + " " + s.Text
	}
}

// StatusBar renders the workspace's bottom line: left aligned to the left
// edge, right aligned to the right, padded to exactly width cells.
//
// When the two groups do not fit, the bar gives ground one segment at a time,
// always the longest of whatever has the lowest priority left:
//
//  1. it swaps to Short, when the segment declares a shorter form;
//  2. otherwise it elides down to MinWidth, because shortening one item costs
//     the reader less than losing a whole one;
//  3. otherwise it is dropped.
//
// The last segment standing is truncated rather than allowed to wrap: a
// status bar that took two rows would push the screen above it out of frame.
func StatusBar(st theme.Styles, width int, left, right []Segment) string {
	if width <= 0 {
		return ""
	}

	l := append([]Segment(nil), left...)
	r := append([]Segment(nil), right...)

	for overflow := statusOverflow(width, l, r); overflow > 0; overflow = statusOverflow(width, l, r) {
		if !giveGround(&l, &r, overflow) {
			break
		}
	}

	return renderStatusBar(st, width, l, r)
}

// statusOverflow is how many cells the two groups exceed width by, zero when
// they fit.
func statusOverflow(width int, left, right []Segment) int {
	need := groupWidth(left) + groupWidth(right)
	if len(left) > 0 && len(right) > 0 {
		need += len(statusGap)
	}
	if need <= width {
		return 0
	}
	return need - width
}

// groupWidth is the rendered width of one group, separators included.
func groupWidth(segs []Segment) int {
	total := 0
	for i, s := range segs {
		if i > 0 {
			total += len(statusGap)
		}
		total += ansi.StringWidth(s.plain())
	}
	return total
}

// giveGround shortens or drops exactly one segment and reports whether it
// managed to. It picks the longest of the lowest-priority segments left: the
// priority says what the reader can spare, the length says which one of those
// buys back the most.
func giveGround(left, right *[]Segment, overflow int) bool {
	group, idx := nextToGive(left, right)
	if group == nil {
		return false
	}

	seg := (*group)[idx]
	current := ansi.StringWidth(seg.plain())

	// A shorter form spends nothing but the detail the caller marked as
	// optional.
	if seg.Short != "" && seg.Short != seg.Text {
		short := seg
		short.Text, short.Short = seg.Short, ""
		if ansi.StringWidth(short.plain()) < current {
			(*group)[idx] = short
			return true
		}
	}

	// Elide, but only down to MinWidth and only while something is left. The
	// icon folds into the elided text: what matters at this point is that the
	// segment keeps its cell budget, not that the glyph survives.
	if target := max(seg.MinWidth, current-overflow); target < current && target > 0 {
		elided := seg
		elided.Icon, elided.Short = "", ""
		elided.Text = Truncate(seg.plain(), target)
		if ansi.StringWidth(elided.plain()) < current {
			(*group)[idx] = elided
			return true
		}
	}

	// Nothing left to shorten: the segment goes. The last one standing is
	// kept and clipped by the renderer instead — an empty status bar tells
	// the reader less than a truncated one.
	if len(*left)+len(*right) == 1 {
		return false
	}
	*group = append((*group)[:idx], (*group)[idx+1:]...)
	return true
}

// nextToGive returns the group and index of the segment that gives ground
// next: the longest of those tied for the lowest priority.
func nextToGive(left, right *[]Segment) (*[]Segment, int) {
	var best *[]Segment
	bestIdx := 0
	for _, group := range []*[]Segment{left, right} {
		for i, s := range *group {
			if best == nil {
				best, bestIdx = group, i
				continue
			}
			cur := (*best)[bestIdx]
			sameRank := s.Priority == cur.Priority && ansi.StringWidth(s.plain()) > ansi.StringWidth(cur.plain())
			if s.Priority < cur.Priority || sameRank {
				best, bestIdx = group, i
			}
		}
	}
	return best, bestIdx
}

// renderStatusBar lays the surviving segments out on one line of exactly
// width cells.
func renderStatusBar(st theme.Styles, width int, left, right []Segment) string {
	leftText := joinSegments(left)
	rightText := joinSegments(right)

	gap := width - ansi.StringWidth(leftText) - ansi.StringWidth(rightText)
	if gap < 0 {
		// Even after giving ground the line is too long: clip rather than
		// wrap, keeping the left group — the one the reader anchors on.
		if leftText != "" {
			return st.StatusBar.Render(Truncate(leftText, width))
		}
		return st.StatusBar.Render(Truncate(rightText, width))
	}

	line := leftText + strings.Repeat(" ", gap) + rightText
	return st.StatusBar.Render(line)
}

// joinSegments renders one group, each segment in its own style.
func joinSegments(segs []Segment) string {
	parts := make([]string, 0, len(segs))
	for _, s := range segs {
		text := s.plain()
		if text == "" {
			continue
		}
		parts = append(parts, s.Style.Render(text))
	}
	return strings.Join(parts, statusGap)
}
