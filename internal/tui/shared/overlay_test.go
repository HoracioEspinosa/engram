package shared

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestCompositeKeepsTheBaseEitherSideOfThePanel(t *testing.T) {
	base := strings.Join([]string{
		"aaaaaaaaaa",
		"bbbbbbbbbb",
		"cccccccccc",
	}, "\n")

	got := Composite(base, "XX\nYY", 4, 1)

	want := strings.Join([]string{
		"aaaaaaaaaa",
		"bbbbXXbbbb",
		"ccccYYcccc",
	}, "\n")
	if got != want {
		t.Fatalf("Composite =\n%q\nwant\n%q", got, want)
	}
}

func TestCompositeCutsStyledBaseOnCellBoundaries(t *testing.T) {
	// A base row whose middle is styled: the cut has to land on cells, not
	// on bytes, or the panel eats part of an escape sequence and the rest of
	// the row turns into garbage.
	base := "aaa" + "\x1b[31m" + "bbbb" + "\x1b[0m" + "ccc"

	got := Composite(base, "XX", 4, 0)

	if w := ansi.StringWidth(got); w != 10 {
		t.Fatalf("width = %d, want the base's own 10 cells", w)
	}
	// Cells 0-3 are "aaab", 4-5 take the panel, 6-9 are "bccc".
	if plain := ansi.Strip(got); plain != "aaabXXbccc" {
		t.Fatalf("stripped = %q, want %q", plain, "aaabXXbccc")
	}
	if !strings.Contains(got, "\x1b[0m"+"XX") {
		t.Fatal("the seam before the panel does not close the base's own styling")
	}
}

func TestCompositeCarriesNoEscapeOverAPlainBase(t *testing.T) {
	// Under `go test` there is no terminal, so lipgloss emits no styling at
	// all. A reset written unconditionally would be the only escape on the
	// screen, and every golden asserts there is none.
	got := Composite("aaaaa\nbbbbb", "XX", 1, 0)
	if strings.ContainsRune(got, '\x1b') {
		t.Fatalf("plain base gained an escape sequence: %q", got)
	}
}

func TestCompositePadsShortBaseLines(t *testing.T) {
	base := "aaaaaaaaaa\nbb\ncccccccccc"

	got := Composite(base, "X\nX\nX", 5, 0)

	want := "aaaaaXaaaa\nbb   X\ncccccXcccc"
	if got != want {
		t.Fatalf("Composite =\n%q\nwant\n%q", got, want)
	}
}

func TestCompositeWithAPanelWiderThanTheBase(t *testing.T) {
	got := Composite("aaa", "XXXXXXX", 0, 0)
	if got != "XXXXXXX" {
		t.Fatalf("Composite = %q, want the panel to extend the row", got)
	}

	got = Composite("aaaaa", "XXXXXXX", 3, 0)
	if got != "aaaXXXXXXX" {
		t.Fatalf("Composite = %q, want the panel to run past the base's end", got)
	}
}

func TestCompositeClipsOutOfRangePlacements(t *testing.T) {
	base := "aaaaa\nbbbbb"

	// Below the last row: nothing to paint on.
	if got := Composite(base, "XX", 0, 9); got != base {
		t.Fatalf("a panel below the frame changed it: %q", got)
	}
	// Above the first row: only the rows that land inside are drawn.
	if got := Composite(base, "XX\nYY\nZZ", 0, -2); got != "ZZaaa\nbbbbb" {
		t.Fatalf("a panel above the frame = %q", got)
	}
	// Left of column zero: the panel is clipped, not shifted.
	if got := Composite(base, "XYZ", -2, 0); got != "Zaaaa\nbbbbb" {
		t.Fatalf("a panel left of the frame = %q", got)
	}
	// An empty panel is a no-op.
	if got := Composite(base, "", 0, 0); got != base {
		t.Fatalf("an empty panel changed the base: %q", got)
	}
}

func TestCenteredPutsThePanelInTheMiddle(t *testing.T) {
	base := strings.Join([]string{
		"..........",
		"..........",
		"..........",
		"..........",
		"..........",
	}, "\n")

	got := Centered(base, "XXXX\nXXXX", 10, 5)

	want := strings.Join([]string{
		"..........",
		"...XXXX...",
		"...XXXX...",
		"..........",
		"..........",
	}, "\n")
	if got != want {
		t.Fatalf("Centered =\n%q\nwant\n%q", got, want)
	}
}

func TestCenteredPinsAPanelLargerThanTheArea(t *testing.T) {
	base := "..\n.."

	got := Centered(base, "XXXX\nXXXX\nXXXX", 2, 2)

	if got != "XXXX\nXXXX" {
		t.Fatalf("Centered = %q, want the panel pinned to the corner and clipped", got)
	}
}
