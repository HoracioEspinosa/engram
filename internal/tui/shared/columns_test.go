package shared

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestPadCells(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{name: "ascii pads to the column width", in: "koi", n: 6, want: "koi   "},
		{name: "an exact fit is untouched", in: "koi", n: 3, want: "koi"},
		{name: "cjk counts two cells per ideograph", in: "決定", n: 6, want: "決定  "},
		{name: "an emoji counts two cells", in: "🐛fix", n: 8, want: "🐛fix   "},
		{name: "an accent counts one cell", in: "café", n: 6, want: "café  "},
		{name: "an sgr sequence costs no cells", in: "\x1b[31mred\x1b[0m", n: 5, want: "\x1b[31mred\x1b[0m  "},
		{name: "an overlong value is left alone", in: "koi-workspace", n: 4, want: "koi-workspace"},
		{name: "a non positive width pads nothing", in: "koi", n: 0, want: "koi"},
		{name: "an empty value fills the column", in: "", n: 3, want: "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PadCells(tt.in, tt.n)
			if got != tt.want {
				t.Fatalf("PadCells() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPadCellsAlignsColumnsOfMixedWidths(t *testing.T) {
	rows := []string{"decisión", "決定を記録", "\x1b[31mred\x1b[0m", "🐛🔧", ""}
	for _, row := range rows {
		padded := PadCells(CutCells(row, 12), 12)
		if w := ansi.StringWidth(padded); w != 12 {
			t.Fatalf("row %q rendered %d cells, want 12", row, w)
		}
	}
}

func TestCutCells(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{name: "a value that fits is untouched", in: "koi", n: 6, want: "koi"},
		{name: "ascii is cut at the budget", in: "koi-workspace", n: 3, want: "koi"},
		{name: "cjk never leaves half an ideograph", in: "決定を記録", n: 5, want: "決定"},
		{name: "a non positive width cuts everything", in: "koi", n: 0, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CutCells(tt.in, tt.n)
			if got != tt.want {
				t.Fatalf("CutCells() = %q, want %q", got, tt.want)
			}
			if w := ansi.StringWidth(got); w > tt.n && tt.n > 0 {
				t.Fatalf("CutCells() width = %d cells, over the %d-cell budget", w, tt.n)
			}
		})
	}
}

func TestCutCellsAddsNoEllipsis(t *testing.T) {
	got := CutCells("koi-workspace", 6)
	if strings.ContainsRune(got, '…') {
		t.Fatalf("CutCells() = %q, want a plain cut — Truncate is what marks the cut", got)
	}
}
