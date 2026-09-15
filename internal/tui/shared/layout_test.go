package shared

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/x/ansi"
)

func TestBreakpointForTheThreeWidths(t *testing.T) {
	cases := []struct {
		width int
		want  Breakpoint
	}{
		{width: 0, want: Compact},
		{width: 60, want: Compact},
		{width: 79, want: Compact},
		{width: 80, want: Single},
		{width: 100, want: Single},
		{width: 119, want: Single},
		{width: 120, want: Split},
		{width: 200, want: Split},
	}
	for _, tc := range cases {
		if got := BreakpointFor(tc.width); got != tc.want {
			t.Fatalf("BreakpointFor(%d) = %d, want %d", tc.width, got, tc.want)
		}
	}
}

func TestLayoutGivesOnePaneBelowTheSplitWidth(t *testing.T) {
	for _, width := range []int{60, 80, 119} {
		r := Layout(width, 24)
		if r.HasDetail() {
			t.Fatalf("%d columns opened a detail pane: %+v", width, r)
		}
		if got := r.Master.Dx(); got != width {
			t.Fatalf("%d columns gave the master %d cells, want the whole width", width, got)
		}
	}
}

func TestLayoutSplitsTwoPanesThatNeverOverlapOrOverflow(t *testing.T) {
	for _, width := range []int{120, 160, 200} {
		r := Layout(width, 40)
		if !r.HasDetail() {
			t.Fatalf("%d columns opened no detail pane", width)
		}
		if r.Master.Max.X > r.Detail.Min.X {
			t.Fatalf("%d columns: the panes overlap: %+v", width, r)
		}
		if r.Detail.Max.X > width {
			t.Fatalf("%d columns: the detail pane runs off the screen: %+v", width, r)
		}
		if total := r.Master.Dx() + r.Detail.Dx(); total > width {
			t.Fatalf("%d columns: the panes take %d cells together", width, total)
		}
		if r.Master.Dx() <= r.Detail.Dx() {
			t.Fatalf("%d columns: the detail pane is not the smaller half: %+v", width, r)
		}
	}
}

func TestLayoutOfNothing(t *testing.T) {
	if r := Layout(0, 10); r.HasDetail() || !r.Master.Empty() {
		t.Fatalf("a zero width produced %+v", r)
	}
	if r := Layout(120, 0); r.HasDetail() || !r.Master.Empty() {
		t.Fatalf("a zero height produced %+v", r)
	}
}

func TestSolveColumnsNeverExceedsItsBudget(t *testing.T) {
	cols := []Column{
		{Min: 8, Max: 12, Weight: 0},
		{Min: 10, Weight: 3},
		{Min: 6, Weight: 1},
		{Min: 8, Max: 8},
	}
	for _, total := range []int{20, 40, 78, 118, 200} {
		widths := SolveColumns(total, 1, cols)
		sum := len(cols) - 1
		for _, w := range widths {
			if w < 0 {
				t.Fatalf("total %d produced a negative width: %v", total, widths)
			}
			sum += w
		}
		if sum > total {
			t.Fatalf("total %d: the row takes %d cells: %v", total, sum, widths)
		}
	}
}

func TestSolveColumnsPaysEveryMinimumThenSharesByWeight(t *testing.T) {
	cols := []Column{
		{Min: 10, Weight: 3},
		{Min: 10, Weight: 1},
		{Min: 10},
	}
	widths := SolveColumns(62, 1, cols)

	if widths[2] != 10 {
		t.Fatalf("a weightless column grew to %d, want its minimum", widths[2])
	}
	if widths[0] <= widths[1] {
		t.Fatalf("the heavier column is not the wider one: %v", widths)
	}
	if got := widths[0] + widths[1] + widths[2] + 2; got != 62 {
		t.Fatalf("the row takes %d cells, want the whole 62: %v", got, widths)
	}
}

func TestSolveColumnsRespectsAMaximum(t *testing.T) {
	cols := []Column{{Min: 4, Max: 6, Weight: 1}, {Min: 4, Weight: 1}}
	widths := SolveColumns(60, 1, cols)

	if widths[0] != 6 {
		t.Fatalf("a capped column grew to %d, want 6", widths[0])
	}
	if widths[1] != 53 {
		t.Fatalf("the cap's remainder did not go to the other column: %v", widths)
	}
}

func TestSolveColumnsShrinksTheWidestWhenNothingFits(t *testing.T) {
	cols := []Column{{Min: 30}, {Min: 10}, {Min: 6}}
	widths := SolveColumns(44, 1, cols)

	sum := 2
	for _, w := range widths {
		sum += w
	}
	if sum > 44 {
		t.Fatalf("the row still takes %d cells: %v", sum, widths)
	}
	if widths[1] != 10 || widths[2] != 6 {
		t.Fatalf("the narrow columns gave ground before the widest: %v", widths)
	}
	if widths[0] != 26 {
		t.Fatalf("the widest column gave %d cells, want the whole excess: %v", 30-widths[0], widths)
	}

	// Squeezed hard enough, every column gives: the row degrades evenly
	// rather than erasing the two narrow fields to keep the wide one whole.
	tight := SolveColumns(20, 1, cols)
	for i, w := range tight {
		if w <= 0 {
			t.Fatalf("column %d was erased instead of shrunk: %v", i, tight)
		}
	}
}

func TestSolveColumnsOfNothing(t *testing.T) {
	if got := SolveColumns(80, 1, nil); len(got) != 0 {
		t.Fatalf("SolveColumns with no columns = %v", got)
	}
}

func TestCellAndFieldHoldTheirWidth(t *testing.T) {
	if got := Cell("abcdefgh", 4); got != "abcd" {
		t.Fatalf("Cell = %q, want the value cut to four cells", got)
	}
	if got := Cell("ab", 4); got != "ab  " {
		t.Fatalf("Cell = %q, want the value padded to four cells", got)
	}
	if got := Field("abcdefgh", 4); got != "abc…" {
		t.Fatalf("Field = %q, want the cut marked", got)
	}
	if got := Cell("abc", 0); got != "" {
		t.Fatalf("Cell at zero width = %q", got)
	}
	if got := Field("abc", 0); got != "" {
		t.Fatalf("Field at zero width = %q", got)
	}
}

func TestSplitPanesDrawsBothPanesWithoutOverflowing(t *testing.T) {
	st := theme.New(theme.KoiPond())
	r := Layout(120, 40)

	out := ansi.Strip(SplitPanes(st, r, "master row", "detail text"))
	if !strings.Contains(out, "master row") || !strings.Contains(out, "detail text") {
		t.Fatalf("SplitPanes lost a pane:\n%s", out)
	}
	for i, line := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(line); w > 120 {
			t.Fatalf("line %d is %d cells wide:\n%s", i+1, w, out)
		}
	}
}

func TestSplitPanesReturnsTheMasterWhenThereIsNoDetail(t *testing.T) {
	st := theme.New(theme.KoiPond())

	// No second pane at this width.
	if got := SplitPanes(st, Layout(80, 24), "master", "detail"); got != "master" {
		t.Fatalf("a single-column layout rendered %q", got)
	}
	// A pane with nothing to say is not drawn empty.
	if got := SplitPanes(st, Layout(120, 40), "master", ""); got != "master" {
		t.Fatalf("an empty detail rendered %q", got)
	}
}
