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

// Column declares one column of a list row for the elastic solver.
//
// A row built from fixed constants is only correct at the width whoever wrote
// the constants had in mind: at 80 columns the same row runs off the screen,
// and at 200 it leaves half the terminal empty. A column says what it needs
// and how greedy it is instead, and the solver decides.
type Column struct {
	// Min is the width below which the column says nothing useful.
	Min int
	// Max caps a column whose content never grows — a date, a kind, a
	// fixed-length badge. Zero means no cap.
	Max int
	// Weight shares out whatever is left once every Min is paid. Zero keeps
	// the column at its Min, which is what a date column wants.
	Weight int
}

// SolveColumns spreads total cells across cols, with gap cells between each
// pair, and returns one width per column.
//
// Every column is paid its Min first. What is left over is shared by weight,
// and a column that hits its Max hands the remainder back to the others. When
// even the Mins do not fit, the widest columns give first — a row that has to
// lose something loses it from the field with the most to spare.
func SolveColumns(total, gap int, cols []Column) []int {
	widths := make([]int, len(cols))
	if len(cols) == 0 {
		return widths
	}

	budget := total - gap*(len(cols)-1)
	if budget < 0 {
		budget = 0
	}

	needed := 0
	for i, c := range cols {
		widths[i] = max(c.Min, 0)
		needed += widths[i]
	}

	if needed > budget {
		shrinkColumns(widths, needed-budget)
		return widths
	}

	spare := budget - needed
	for spare > 0 {
		totalWeight := 0
		for i, c := range cols {
			if c.Weight > 0 && (c.Max <= 0 || widths[i] < c.Max) {
				totalWeight += c.Weight
			}
		}
		if totalWeight == 0 {
			break
		}

		given := 0
		for i, c := range cols {
			if c.Weight <= 0 || (c.Max > 0 && widths[i] >= c.Max) {
				continue
			}
			share := spare * c.Weight / totalWeight
			if share == 0 {
				// Rounding left this column nothing; give it one cell so a
				// remainder of a few cells still lands somewhere instead of
				// looping forever.
				share = 1
			}
			if c.Max > 0 && widths[i]+share > c.Max {
				share = c.Max - widths[i]
			}
			if share > spare-given {
				share = spare - given
			}
			widths[i] += share
			given += share
			if given == spare {
				break
			}
		}
		if given == 0 {
			break
		}
		spare -= given
	}

	return widths
}

// shrinkColumns takes excess cells back, always from the column that is
// currently widest, so the row degrades evenly instead of erasing one field.
func shrinkColumns(widths []int, excess int) {
	for excess > 0 {
		widest := 0
		for i, w := range widths {
			if w > widths[widest] {
				widest = i
			}
		}
		if widths[widest] <= 0 {
			return
		}
		widths[widest]--
		excess--
	}
}

// Cell renders one solved column: cut to its width, padded to it, so the
// columns to its right stay in line whatever the content was.
func Cell(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return PadCells(CutCells(s, width), width)
}

// Field is Cell for prose: the cut is marked with an ellipsis, because a
// title that simply stops reads as a different title.
func Field(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return PadCells(Truncate(s, width), width)
}
