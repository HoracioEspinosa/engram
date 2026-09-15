package shared

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/lipgloss"
)

// markerStyle wraps whatever it renders in a visible pair of delimiters, so a
// test can see how many segments Highlight produced. Colors are invisible
// under the Ascii profile a test binary renders in; a transform is not.
func markerStyle(open, close string) lipgloss.Style {
	return lipgloss.NewStyle().Transform(func(s string) string { return open + s + close })
}

func TestFuzzyWithoutATermKeepsEveryTargetInOrder(t *testing.T) {
	targets := []string{"clarodrive", "engram", "portal"}

	matches := Fuzzy("  ", targets)

	if len(matches) != len(targets) {
		t.Fatalf("Fuzzy with a blank term returned %d matches, want %d", len(matches), len(targets))
	}
	for i, m := range matches {
		if m.Index != i {
			t.Errorf("match %d has index %d, want %d", i, m.Index, i)
		}
		if m.MatchedIndexes != nil {
			t.Errorf("match %d carries matched indexes %v; nothing was typed to match", i, m.MatchedIndexes)
		}
	}
}

func TestFuzzyDropsWhatDoesNotMatchAndSaysWhereItDid(t *testing.T) {
	targets := []string{"clarodrive", "engram", "portal"}

	matches := Fuzzy("cld", targets)

	if len(matches) != 1 {
		t.Fatalf("Fuzzy(%q) returned %d matches, want 1: %#v", "cld", len(matches), matches)
	}
	if matches[0].Index != 0 {
		t.Fatalf("matched target index %d, want 0 (clarodrive)", matches[0].Index)
	}
	if len(matches[0].MatchedIndexes) != 3 {
		t.Fatalf("matched indexes %v, want three positions", matches[0].MatchedIndexes)
	}
	for _, i := range matches[0].MatchedIndexes {
		if i < 0 || i >= len("clarodrive") {
			t.Errorf("matched index %d is outside the target", i)
		}
	}
}

func TestMatchedByIndexesTheResultByTarget(t *testing.T) {
	targets := []string{"alpha", "beta", "gamma"}

	by := MatchedBy(Fuzzy("bt", targets))

	if _, ok := by[1]; !ok {
		t.Fatalf("beta (index 1) is missing from %v", by)
	}
	if _, ok := by[0]; ok {
		t.Errorf("alpha (index 0) should not have matched %q", "bt")
	}
}

func TestHighlightWithoutMatchesRendersThePlainString(t *testing.T) {
	st := theme.Default()

	got := Highlight(st.DetailValue, st.SearchHighlight, "clarodrive", nil)

	if got != st.DetailValue.Render("clarodrive") {
		t.Errorf("Highlight without matches = %q, want the base rendering", got)
	}
}

func TestHighlightStripsTheBoxModelOfTheStylesItApplies(t *testing.T) {
	st := theme.Default()

	// ListItem carries PaddingLeft(2): applied once per segment it would pay
	// that padding three times over and push the row past its column.
	got := Highlight(st.ListItem, st.SearchHighlight, "clarodrive", []int{2, 3, 4})

	if got != "clarodrive" {
		t.Errorf("Highlight with a padded base = %q, want the untouched text", got)
	}
}

func TestHighlightKeepsEveryRuneOfTheOriginal(t *testing.T) {
	st := theme.Default()

	got := Highlight(st.ListItem, st.SearchHighlight, "clarodrive", []int{0, 1, 5})

	// The test binary has no TTY, so lipgloss renders in the Ascii profile and
	// emits no escape sequences: the highlighted string is the original,
	// which is exactly the guarantee a column solved in cells needs.
	if strings.ContainsRune(got, 0x1b) {
		t.Fatalf("Highlight emitted ANSI under the Ascii profile: %q", got)
	}
	if got != "clarodrive" {
		t.Errorf("Highlight rebuilt the string as %q, want %q", got, "clarodrive")
	}
}

func TestHighlightGroupsConsecutiveRunsIntoOneSegment(t *testing.T) {
	// A style that marks its own extent proves the run grouping without
	// depending on a color profile: three consecutive matched runes must be
	// wrapped once, not three times.
	base := markerStyle("(", ")")
	hl := markerStyle("[", "]")

	got := Highlight(base, hl, "abcdef", []int{1, 2, 3})

	if want := "(a)[bcd](ef)"; got != want {
		t.Errorf("Highlight = %q, want %q", got, want)
	}
}

func TestHighlightMeasuresRunesNotBytes(t *testing.T) {
	base := markerStyle("(", ")")
	hl := markerStyle("[", "]")

	// "ñ" is two bytes and one rune: a byte-indexed highlight would split it.
	got := Highlight(base, hl, "añb", []int{1})

	if want := "(a)[ñ](b)"; got != want {
		t.Errorf("Highlight = %q, want %q", got, want)
	}
}
