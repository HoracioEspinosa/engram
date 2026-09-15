package shared

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/x/ansi"
)

// statusFixture is the same bar at every width: a breadcrumb that can shed
// its parents, an active task, the screen's hints, the sync state that never
// gives ground, and the theme name.
func statusFixture() (left, right []Segment) {
	left = []Segment{
		{Text: "clarodrive › nextcloud › previews", Short: "previews", Priority: 3, MinWidth: 8},
		{Text: "CDBS-1010", Priority: 4, MinWidth: 9},
		{Text: "j/k move • enter detail • / search • r refresh", Priority: 1, MinWidth: 6},
	}
	right = []Segment{
		{Text: "sync: healthy", Priority: 9, MinWidth: 13},
		{Text: "koi-pond", Priority: 2, MinWidth: 8},
	}
	return left, right
}

func TestStatusBarFillsExactlyTheWidthItIsGiven(t *testing.T) {
	st := theme.New(theme.KoiPond())
	left, right := statusFixture()

	for _, width := range []int{80, 100, 120} {
		got := ansi.Strip(StatusBar(st, width, left, right))
		if strings.Contains(got, "\n") {
			t.Fatalf("width %d: the bar wrapped onto a second row:\n%s", width, got)
		}
		if w := ansi.StringWidth(got); w != width {
			t.Fatalf("width %d: rendered %d cells:\n%q", width, w, got)
		}
	}
}

func TestStatusBarKeepsEverythingWhenItFits(t *testing.T) {
	st := theme.New(theme.KoiPond())
	left, right := statusFixture()

	got := ansi.Strip(StatusBar(st, 120, left, right))
	for _, want := range []string{"clarodrive › nextcloud › previews", "CDBS-1010", "j/k move", "sync: healthy", "koi-pond"} {
		if !strings.Contains(got, want) {
			t.Fatalf("120 columns dropped %q:\n%s", want, got)
		}
	}
}

func TestStatusBarGivesGroundInPriorityOrder(t *testing.T) {
	st := theme.New(theme.KoiPond())
	left, right := statusFixture()

	at100 := ansi.Strip(StatusBar(st, 100, left, right))
	at80 := ansi.Strip(StatusBar(st, 80, left, right))

	// The hints are the lowest priority, so they are what shortens first.
	if strings.Contains(at100, "r refresh") {
		t.Fatalf("100 columns kept every hint instead of trimming the cheapest segment:\n%s", at100)
	}
	// Sync is the highest priority and is never trimmed at any width.
	for width, got := range map[int]string{100: at100, 80: at80} {
		if !strings.Contains(got, "sync: healthy") {
			t.Fatalf("%d columns trimmed the sync state:\n%s", width, got)
		}
	}
	// At 80 the cheaper segments have given enough that the breadcrumb is
	// still whole; squeeze harder and it falls back to the child rather than
	// disappearing.
	if !strings.Contains(at80, "previews") {
		t.Fatalf("80 columns lost the breadcrumb:\n%s", at80)
	}
	at50 := ansi.Strip(StatusBar(st, 50, left, right))
	if strings.Contains(at50, "clarodrive ›") {
		t.Fatalf("50 columns kept the full breadcrumb:\n%s", at50)
	}
	if !strings.Contains(at50, "previews") {
		t.Fatalf("50 columns lost the breadcrumb's child:\n%s", at50)
	}
	if !strings.Contains(at50, "sync: healthy") {
		t.Fatalf("50 columns trimmed the sync state:\n%s", at50)
	}
}

func TestStatusBarClipsRatherThanWrapsWhenNothingCanGive(t *testing.T) {
	st := theme.New(theme.KoiPond())
	left := []Segment{{Text: "an extremely long single segment nobody can shorten", Priority: 1, MinWidth: 100}}

	got := ansi.Strip(StatusBar(st, 20, left, nil))
	if strings.Contains(got, "\n") {
		t.Fatalf("the bar wrapped: %q", got)
	}
	if w := ansi.StringWidth(got); w > 20 {
		t.Fatalf("width = %d, want at most 20: %q", w, got)
	}
}

func TestStatusBarWithNoSegmentsOrNoWidth(t *testing.T) {
	st := theme.New(theme.KoiPond())

	if got := StatusBar(st, 0, []Segment{{Text: "x"}}, nil); got != "" {
		t.Fatalf("a zero width rendered %q, want nothing", got)
	}
	if got := ansi.Strip(StatusBar(st, 10, nil, nil)); ansi.StringWidth(got) != 10 {
		t.Fatalf("an empty bar rendered %q, want ten blank cells", got)
	}
}

func TestSegmentRendersItsIconBeforeItsText(t *testing.T) {
	st := theme.New(theme.KoiPond())
	seg := Segment{Icon: st.Icons.Glyph(theme.IconSyncHealthy), Text: "sync: healthy", Priority: 1}

	got := ansi.Strip(StatusBar(st, 40, []Segment{seg}, nil))
	if !strings.Contains(got, seg.Icon+" sync: healthy") {
		t.Fatalf("the icon is not drawn before the text: %q", got)
	}
}
