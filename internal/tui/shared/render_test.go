package shared

import (
	"strings"
	"testing"

	"github.com/Gentleman-Programming/engram/internal/store"
	"github.com/Gentleman-Programming/engram/internal/tui/theme"
)

func TestObservationListItemRendersBothLines(t *testing.T) {
	st := theme.Default()
	project := "engram"

	line := ObservationListItem(st, ObservationLine{
		ID:        42,
		Type:      "bugfix",
		Title:     "Title here",
		Content:   "content line 1\ncontent line 2",
		CreatedAt: "2026-01-01",
		Project:   &project,
		State:     store.ObservationStateActive,
		Pinned:    true,
		Selected:  true,
	})

	if !strings.Contains(line, "▸") {
		t.Fatal("selected item should include cursor marker")
	}
	if !strings.Contains(line, "Title here") {
		t.Fatal("line should include title")
	}
	if !strings.Contains(line, "content line 1 content line 2") {
		t.Fatal("content preview should be rendered on second line")
	}
	if !strings.Contains(line, "engram") {
		t.Fatal("project label should be rendered when project is set")
	}
	if !strings.Contains(line, "pinned") {
		t.Fatal("pinned item should include pin indicator")
	}
	if strings.Count(line, "\n") != 2 {
		t.Fatalf("an item with content should render two lines, got %q", line)
	}
}

func TestObservationListItemWithoutContentRendersOneLine(t *testing.T) {
	line := ObservationListItem(theme.Default(), ObservationLine{ID: 1, Type: "note", Title: "no body"})

	if strings.Count(line, "\n") != 1 {
		t.Fatalf("an item with no content should render one line, got %q", line)
	}
}

func TestObservationListItemBadgesNeedsReview(t *testing.T) {
	line := ObservationListItem(theme.Default(), ObservationLine{
		ID:    43,
		Type:  "decision",
		Title: "Needs review",
		State: store.ObservationStateNeedsReview,
	})

	if !strings.Contains(line, store.ObservationStateNeedsReview) {
		t.Fatal("stale item should include needs_review badge")
	}
}

func TestObservationListItemUnselectedHasNoCursor(t *testing.T) {
	line := ObservationListItem(theme.Default(), ObservationLine{ID: 1, Type: "note", Title: "plain"})

	if strings.Contains(line, "▸") {
		t.Fatal("an unselected item should not carry the cursor marker")
	}
}

func TestObservationState(t *testing.T) {
	st := theme.Default()

	if got := ObservationState(st, store.ObservationStateActive); !strings.Contains(got, store.ObservationStateActive) {
		t.Fatalf("active state = %q", got)
	}
	if got := ObservationState(st, store.ObservationStateNeedsReview); !strings.Contains(got, store.ObservationStateNeedsReview) {
		t.Fatalf("needs_review state = %q", got)
	}
}

func TestMenuMarksTheCursor(t *testing.T) {
	out := Menu(theme.Default(), []string{"first", "second", "third"}, 1)

	if !strings.Contains(out, "▸ second") {
		t.Fatalf("cursor should mark the second item, got %q", out)
	}
	if strings.Contains(out, "▸ first") || strings.Contains(out, "▸ third") {
		t.Fatal("only the item under the cursor carries the marker")
	}
	if lines := strings.Count(out, "\n"); lines != 3 {
		t.Fatalf("menu lines = %d, want one per item", lines)
	}
}

func TestMenuWithAnOutOfRangeCursorMarksNothing(t *testing.T) {
	out := Menu(theme.Default(), []string{"first", "second"}, 9)

	if strings.Contains(out, "▸") {
		t.Fatal("a cursor past the end should mark nothing rather than panic")
	}
}

func TestRangeIndicator(t *testing.T) {
	st := theme.Default()

	if got := RangeIndicator(st, "showing", 1, 3, 4); !strings.Contains(got, "showing 1-3 of 4") {
		t.Fatalf("indicator = %q", got)
	}
	if got := RangeIndicator(st, "line", 3, 20, 41); !strings.Contains(got, "line 3-20 of 41") {
		t.Fatalf("indicator = %q", got)
	}
	if got := RangeIndicator(st, "showing", 1, 1, 1); !strings.HasPrefix(got, "\n") {
		t.Fatalf("indicator should start on its own line, got %q", got)
	}
}

func TestVisibleItems(t *testing.T) {
	tests := []struct {
		name                             string
		height, chrome, perItem, minimum int
		want                             int
	}{
		{name: "two-line rows on a tall terminal", height: 40, chrome: 8, perItem: 2, minimum: 3, want: 16},
		{name: "one-line rows on a tall terminal", height: 40, chrome: 8, perItem: 1, minimum: 5, want: 32},
		{name: "floor applies when the terminal is short", height: 8, chrome: 8, perItem: 2, minimum: 3, want: 3},
		{name: "floor applies when the arithmetic goes negative", height: 2, chrome: 12, perItem: 2, minimum: 3, want: 3},
		{name: "exactly at the floor", height: 14, chrome: 8, perItem: 2, minimum: 3, want: 3},
		{name: "one row below the floor", height: 12, chrome: 8, perItem: 2, minimum: 3, want: 3},
		{name: "one row above the floor", height: 16, chrome: 8, perItem: 2, minimum: 3, want: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := VisibleItems(tt.height, tt.chrome, tt.perItem, tt.minimum); got != tt.want {
				t.Fatalf("VisibleItems(%d, %d, %d, %d) = %d, want %d",
					tt.height, tt.chrome, tt.perItem, tt.minimum, got, tt.want)
			}
		})
	}
}

func TestFormatReviewDate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "sqlite timestamp", in: "2027-02-03 04:05:06", want: "2027-02-03"},
		{name: "rfc3339", in: "2027-02-03T04:05:06Z", want: "2027-02-03"},
		{name: "rfc3339 nano", in: "2027-02-03T04:05:06.123456789Z", want: "2027-02-03"},
		{name: "bare date", in: "2027-02-03", want: "2027-02-03"},
		{name: "surrounding whitespace", in: "  2027-02-03  ", want: "2027-02-03"},
		{name: "unparseable but long enough to cut", in: "not-a-date-at-all", want: "not-a-date"},
		{name: "unparseable and short", in: "soon", want: "soon"},
		{name: "empty", in: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatReviewDate(tt.in); got != tt.want {
				t.Fatalf("FormatReviewDate(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestLocalTimeHonoursTheConfiguredZone(t *testing.T) {
	t.Setenv("ENGRAM_TIMEZONE", "America/Bogota")

	if got := LocalTime("2026-05-04 12:00:00"); got != "2026-05-04 07:00:00" {
		t.Fatalf("LocalTime = %q, want the timestamp converted to the configured zone", got)
	}
}

func TestLocalTimeLeavesUnparseableInputAlone(t *testing.T) {
	if got := LocalTime("whenever"); got != "whenever" {
		t.Fatalf("LocalTime = %q, want the input unchanged", got)
	}
}
