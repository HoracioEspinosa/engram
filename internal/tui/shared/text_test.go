package shared

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{name: "unchanged", in: "short", max: 10, want: "short"},
		{name: "replaces newlines", in: "a\nb", max: 10, want: "a b"},
		{name: "truncated", in: "abcdefghijklmnopqrstuvwxyz", max: 5, want: "abcd…"},
		{name: "spanish accents", in: "Decisión de arquitectura", max: 8, want: "Decisió…"},
		{name: "emoji count two cells each", in: "🐛🔧🚀✨🎉💡", max: 5, want: "🐛🔧…"},
		{name: "mixed ascii and multibyte", in: "café☕latte", max: 5, want: "café…"},
		{name: "cjk counts two cells per ideograph", in: "決定を記録する", max: 6, want: "決定…"},
		{name: "cjk that fits is left alone", in: "決定", max: 4, want: "決定"},
		{name: "a private use glyph of one cell keeps its budget", in: "main-branch", max: 5, want: "mai…"},
		{name: "an sgr sequence costs no cells", in: "\x1b[31mdanger\x1b[0m", max: 6, want: "\x1b[31mdanger\x1b[0m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Truncate(tt.in, tt.max)
			if got != tt.want {
				t.Fatalf("Truncate() = %q, want %q", got, tt.want)
			}
			if w := ansi.StringWidth(got); w > tt.max {
				t.Fatalf("Truncate() width = %d cells, over the %d-cell budget", w, tt.max)
			}
		})
	}
}

func TestTruncateNeverCutsAnSGRSequenceOpen(t *testing.T) {
	got := Truncate("\x1b[31mdangerous title\x1b[0m", 6)
	if w := ansi.StringWidth(got); w > 6 {
		t.Fatalf("width = %d cells, want at most 6", w)
	}
	if !ansi.HasCsiPrefix([]byte(got)) {
		t.Fatalf("Truncate() = %q, want the colour it opened with preserved", got)
	}
}
