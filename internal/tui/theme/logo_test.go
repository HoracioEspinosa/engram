package theme

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestWordmarkIsFiveRowsInEveryMode ties the wordmark to the gradient: the
// palette carries exactly five stops, one per row, and a wordmark of any
// other height would leave a row uncoloured or a stop unused.
func TestWordmarkIsFiveRowsInEveryMode(t *testing.T) {
	for _, mode := range allModes {
		t.Run(string(mode), func(t *testing.T) {
			rows := Wordmark(mode)
			if len(rows) != logoRows {
				t.Fatalf("Wordmark has %d rows, want %d — one per LogoGradient stop", len(rows), logoRows)
			}
		})
	}
}

// TestWordmarkRowsShareOneWidth is what lets the wordmark sit inside a border
// without the frame stepping in and out: every row measures the same, and
// that measure is the declared width.
func TestWordmarkRowsShareOneWidth(t *testing.T) {
	for _, mode := range allModes {
		t.Run(string(mode), func(t *testing.T) {
			for i, row := range Wordmark(mode) {
				if width := ansi.StringWidth(row); width != WordmarkWidth {
					t.Errorf("row %d is %d cells wide, want the declared %d: %q", i, width, WordmarkWidth, row)
				}
			}
		})
	}
}

// TestWordmarkFitsANarrowTerminal keeps the mark inside the budget the
// eighty-column layout leaves for it: forty cells, border and padding still
// to come.
func TestWordmarkFitsANarrowTerminal(t *testing.T) {
	const budget = 40
	if WordmarkWidth > budget {
		t.Fatalf("WordmarkWidth = %d, want at most %d", WordmarkWidth, budget)
	}
}

// TestWordmarkIsBuiltOnlyFromSingleCellCharacters is the reason the mark is
// drawn rather than written: an emoji hands its cell width to the terminal
// emulator instead of taking it from the font, so one in the wordmark makes
// the frame ragged on exactly the terminals it was supposed to look good on.
//
// The check is a whitelist rather than an emoji blacklist, because the
// blacklist is the part that goes out of date: every rune here must be a space,
// an ascii byte, the block element the letterforms are drawn with, or one of
// the private use codepoints icons.go already measures.
func TestWordmarkIsBuiltOnlyFromSingleCellCharacters(t *testing.T) {
	for _, mode := range allModes {
		for i, row := range Wordmark(mode) {
			for _, r := range row {
				switch {
				case r >= 0x20 && r <= 0x7e:
				case r == wordmarkFill:
				case inPrivateUse(r):
				case ansi.StringWidth(string(r)) == 1:
				default:
					t.Errorf("%s row %d carries U+%04X, which is neither ascii, a block cell, nor a patched icon",
						mode, i, r)
				}
			}
		}
	}
}

// TestWordmarkASCIIStaysASCII proves the last-resort variant really is one: a
// terminal that cannot draw block elements gets a mark made of bytes it can.
func TestWordmarkASCIIStaysASCII(t *testing.T) {
	for i, row := range Wordmark(IconModeASCII) {
		for _, r := range row {
			if r > 0x7e {
				t.Errorf("the ascii wordmark row %d carries U+%04X, which is not ascii", i, r)
			}
		}
	}
}

// TestWordmarkDiffersByMode catches the variants collapsing into one: three
// modes that draw the same rows would mean the decoration never changed and
// the mode argument is doing nothing.
func TestWordmarkDiffersByMode(t *testing.T) {
	unicode := strings.Join(Wordmark(IconModeUnicode), "\n")
	nerd := strings.Join(Wordmark(IconModeNerd), "\n")
	asciiRows := strings.Join(Wordmark(IconModeASCII), "\n")

	if unicode == nerd {
		t.Error("the unicode and nerd wordmarks are identical: the patched glyphs are not being used")
	}
	if unicode == asciiRows {
		t.Error("the unicode and ascii wordmarks are identical: the fallback is not a fallback")
	}
}

// TestWordmarkSpellsTheName is the check that survives a refactor of the
// letterforms: whatever the mark is made of, the shape of the letters has to
// carry six of them, in the six columns the name has.
func TestWordmarkSpellsTheName(t *testing.T) {
	rows := Wordmark(IconModeASCII)
	// The letters occupy everything after the decoration column, and each is
	// five cells wide with one cell between them.
	const lettersStart = wordmarkDecorationWidth
	body := make([]string, len(rows))
	for i, row := range rows {
		body[i] = row[lettersStart:]
	}
	got := len(strings.Split(body[0], " ")) // six letter blocks, five separators
	if got < len(wordmarkLetters) {
		t.Fatalf("the top row splits into %d blocks, want at least the %d letters of the name",
			got, len(wordmarkLetters))
	}
	for i, letter := range wordmarkLetters {
		if len(letter) != logoRows {
			t.Errorf("letter %d has %d rows, want %d", i, len(letter), logoRows)
		}
		for j, line := range letter {
			if len([]rune(line)) != wordmarkLetterWidth {
				t.Errorf("letter %d row %d is %d cells, want %d", i, j, len([]rune(line)), wordmarkLetterWidth)
			}
		}
	}
}

// TestRenderWordmarkPaintsOneGradientStopPerRow is the wordmark's whole
// visual point: each row goes through its own stop, shiro at the top through
// to water at the bottom. It compares against what the row's own gradient
// style produces rather than against an escape sequence, because a test
// process has no terminal and lipgloss degrades colour to nothing — an escape
// this asserted would be absent for a reason that has nothing to do with the
// gradient.
func TestRenderWordmarkPaintsOneGradientStopPerRow(t *testing.T) {
	styles := New(KoiPond())
	rendered := styles.RenderWordmark("engram test")

	for i, row := range Wordmark(styles.Icons.Mode()) {
		painted := styles.LogoGradient[i].Render(row)
		if !strings.Contains(rendered, painted) {
			t.Errorf("row %d does not go through gradient stop %d (%s)",
				i, i, styles.Palette.LogoGradient[i])
		}
	}
	if !strings.Contains(ansi.Strip(rendered), "engram test") {
		t.Error("the tagline did not survive rendering")
	}
}

// TestRenderWordmarkOmitsAnEmptyTagline keeps the frame from growing two blank
// rows for a caller that has nothing to say underneath the mark.
func TestRenderWordmarkOmitsAnEmptyTagline(t *testing.T) {
	var drawn int
	for _, line := range strings.Split(ansi.Strip(New(KoiPond()).RenderWordmark("")), "\n") {
		// The frame carries a bottom margin, which renders as a row of
		// spaces rather than as an empty string. Counting what has ink in it
		// is what separates the mark from its spacing.
		if strings.TrimSpace(line) != "" {
			drawn++
		}
	}
	// The five wordmark rows plus the border's own two.
	if drawn != logoRows+2 {
		t.Fatalf("an empty tagline drew %d rows, want %d", drawn, logoRows+2)
	}
}

// TestRenderWordmarkFitsItsFrame keeps the framed mark inside the narrow
// geometry: the border and its padding are the only cells the wordmark's own
// width does not account for.
func TestRenderWordmarkFitsItsFrame(t *testing.T) {
	const narrow = 80
	styles := New(KoiPond())
	for _, mode := range allModes {
		rendered := styles.WithIcons(mode).RenderWordmark("engram 0.0.0")
		for i, line := range strings.Split(ansi.Strip(rendered), "\n") {
			if width := ansi.StringWidth(line); width > narrow {
				t.Errorf("%s: framed line %d is %d cells wide, wider than an %d column terminal",
					mode, i, width, narrow)
			}
		}
	}
}
