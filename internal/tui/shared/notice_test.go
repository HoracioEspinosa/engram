package shared

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/x/ansi"
)

func TestNoticeCarriesItsSeverityGlyph(t *testing.T) {
	st := theme.New(theme.KoiPond())

	cases := []struct {
		name   string
		notice Notice
		icon   theme.Icon
	}{
		{name: "error", notice: Error("the store said no"), icon: theme.IconTaskCancelled},
		{name: "warning", notice: Warn("the value may be stale"), icon: theme.IconStale},
		{name: "info", notice: Info("copied"), icon: theme.IconFresh},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ansi.Strip(tc.notice.Render(st))
			if !strings.Contains(got, st.Icons.Glyph(tc.icon)+" ") {
				t.Fatalf("notice = %q, want it to carry its severity glyph", got)
			}
			if !strings.Contains(got, tc.notice.Text) {
				t.Fatalf("notice = %q, want it to carry its text", got)
			}
		})
	}
}

func TestAnEmptyNoticeRendersNothing(t *testing.T) {
	st := theme.New(theme.KoiPond())
	n := Notice{}
	if !n.Empty() {
		t.Fatal("a notice with no text reports itself as non-empty")
	}
	if got := n.Render(st); got != "" {
		t.Fatalf("an empty notice rendered %q", got)
	}
}

func TestADismissableNoticeSaysSo(t *testing.T) {
	st := theme.New(theme.KoiPond())
	n := Error("the store said no")
	n.Dismissable = true

	if got := ansi.Strip(n.Render(st)); !strings.Contains(got, "(esc)") {
		t.Fatalf("notice = %q, want it to name the key that clears it", got)
	}
}

func TestLoadingNamesWhatItIsWaitingFor(t *testing.T) {
	st := theme.New(theme.KoiPond())
	got := ansi.Strip(Loading(st, spinner.New(), "evidence"))

	if !strings.Contains(got, "loading evidence") {
		t.Fatalf("loading state = %q", got)
	}
}

func TestViewportWindowsContentAtAnOffset(t *testing.T) {
	content := "one\ntwo\nthree\nfour\nfive"

	got := Viewport(content, 20, 2, 1)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("viewport rendered %d rows, want the two it was given: %q", len(lines), got)
	}
	if !strings.Contains(lines[0], "two") || !strings.Contains(lines[1], "three") {
		t.Fatalf("viewport did not start at the offset: %q", got)
	}

	// No room to draw in: the content is handed back rather than swallowed.
	if got := Viewport(content, 0, 5, 0); got != content {
		t.Fatalf("a zero width returned %q", got)
	}
	if got := Viewport(content, 20, 0, 0); got != content {
		t.Fatalf("a zero height returned %q", got)
	}
}
