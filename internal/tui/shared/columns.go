package shared

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Column layout in a terminal is measured in cells, and fmt's width verbs
// count runes. The two agree only on plain ASCII: an ideograph or an emoji
// takes two cells, a combining accent takes none of its own, and an SGR
// sequence takes none at all. A Japanese task title or a coloured value run
// through "%-12s" therefore pushes every column to its right out of line.
//
// PadCells and CutCells are the two halves of a fixed-width column. A field
// that can be arbitrarily long is written PadCells(CutCells(v, n), n); a field
// already capped by Truncate only needs the padding.

// PadCells pads s on the right with spaces until it occupies n terminal cells.
// A value that is already at or over the width is returned unchanged, the way
// fmt's "%-Ns" leaves an overlong argument alone — cutting is CutCells's job,
// and a caller that wants the cut says so.
func PadCells(s string, n int) string {
	width := ansi.StringWidth(s)
	if width >= n {
		return s
	}
	return s + strings.Repeat(" ", n-width)
}

// CutCells cuts s down to at most n terminal cells, without marking the cut.
// It never splits a wide character or an escape sequence. Use it for a column
// whose content is an identifier the reader scans rather than reads; Truncate
// is what marks the cut with an ellipsis, for prose.
func CutCells(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return ansi.Truncate(s, n, "")
}
