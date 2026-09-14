package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The wordmark is drawn, not written: a picture character would hand its cell
// width to the terminal emulator, and a frame built around a mark that
// measures differently on two machines is ragged on one of them. Block
// elements and the patched icons in icons.go are the only characters here,
// and every one of them is a single cell.

// wordmarkLetterWidth is how many cells one letterform occupies. Every letter
// is the same width so the name can be laid out without measuring.
const wordmarkLetterWidth = 5

// wordmarkDecorationWidth is the gutter to the left of the name, where the koi
// swims between two lines of water.
const wordmarkDecorationWidth = 2

// WordmarkWidth is the cell width of every wordmark row: six letters, the five
// single-cell gaps between them, and the decoration gutter.
const WordmarkWidth = len(wordmarkLetters)*wordmarkLetterWidth + (len(wordmarkLetters) - 1) + wordmarkDecorationWidth

// wordmarkFill is the cell the letterforms are drawn with in the two modes
// that can draw one, and the byte that stands in for it in the third.
const (
	wordmarkFill      = '█'
	wordmarkASCIIFill = '#'
)

// wordmarkLetters spells "engram" as six five-by-five letterforms. They are
// written with the block cell they are drawn with, so what the source looks
// like is what the terminal shows.
var wordmarkLetters = [6][logoRows]string{
	{ // E
		"█████",
		"█    ",
		"████ ",
		"█    ",
		"█████",
	},
	{ // N
		"█   █",
		"██  █",
		"█ █ █",
		"█  ██",
		"█   █",
	},
	{ // G
		" ████",
		"█    ",
		"█  ██",
		"█   █",
		" ████",
	},
	{ // R
		"████ ",
		"█   █",
		"████ ",
		"█  █ ",
		"█   █",
	},
	{ // A
		" ███ ",
		"█   █",
		"█████",
		"█   █",
		"█   █",
	},
	{ // M
		"█   █",
		"██ ██",
		"█ █ █",
		"█   █",
		"█   █",
	},
}

// Wordmark returns the five rows of the koi wordmark, drawn for one icon
// mode. Every row is exactly WordmarkWidth cells, which is what lets a caller
// frame it without measuring and what keeps the frame square at eighty
// columns.
//
// The rows are returned uncoloured: RenderWordmark is what runs the gradient
// down them, and a caller that wants the raw shape — a test, a width
// check — should not have to strip escapes to get it.
func Wordmark(mode IconMode) []string {
	icons := Icons(mode)
	fill := string(wordmarkFill)
	if icons.Mode() == IconModeASCII {
		fill = string(wordmarkASCIIFill)
	}

	// The gutter: still water, the koi, still water. The top and bottom rows
	// stay clear so the mark reads as a name with a mark beside it rather
	// than as a column of symbols.
	gutter := [logoRows]string{
		"  ",
		icons.Glyph(IconWater) + " ",
		icons.Glyph(IconKoi) + " ",
		icons.Glyph(IconWater) + " ",
		"  ",
	}

	rows := make([]string, 0, logoRows)
	for row := 0; row < logoRows; row++ {
		var b strings.Builder
		b.WriteString(gutter[row])
		for i, letter := range wordmarkLetters {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(strings.ReplaceAll(letter[row], string(wordmarkFill), fill))
		}
		rows = append(rows, b.String())
	}
	return rows
}

// RenderWordmark draws the framed wordmark: the five rows, each in its own
// gradient stop, with the tagline underneath.
//
// The frame is a rounded border in Overlay and nothing else. There is no
// background fill anywhere in it, which is what lets the mark sit on a
// translucent terminal without punching an opaque rectangle through whatever
// is behind the window.
func (s Styles) RenderWordmark(tagline string) string {
	rows := Wordmark(s.Icons.Mode())
	painted := make([]string, 0, len(rows)+2)
	for i, row := range rows {
		painted = append(painted, s.LogoGradient[i].Render(row))
	}
	if tagline != "" {
		// The tagline carries a version and can be wider than the mark. The
		// two are centred on each other rather than left-aligned, so the
		// frame reads as a mark with a caption instead of as a block that
		// happens to start at the same column.
		painted = append(painted, "", s.LogoTagline.Render(tagline))
	}
	return s.LogoFrame.Render(lipgloss.JoinVertical(lipgloss.Center, painted...))
}

// wordmarkBorder is the frame the mark sits in. It is declared here rather
// than inline in New so the one place that decides what the mark looks like is
// the one file that draws it.
func wordmarkBorder() lipgloss.Border { return lipgloss.RoundedBorder() }
