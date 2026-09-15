package shared

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/x/ansi"
)

func footerStyles() theme.Styles { return theme.New(theme.CatppuccinMocha()) }

func binding(k, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(k), key.WithHelp(k, desc))
}

// footerLine is the rendered hint line without the blank margin the Help
// style puts above it, which is the theme's spacing rather than the footer's
// content.
func footerLine(rendered string) string {
	lines := strings.Split(ansi.Strip(rendered), "\n")
	return strings.TrimRight(lines[len(lines)-1], " ")
}

func TestHintsFromRendersEveryBinding(t *testing.T) {
	got := footerLine(HintsFrom(footerStyles(), []key.Binding{
		binding("j/k", "move"),
		binding("enter", "detail"),
		binding("esc", "back"),
	}, 0))

	want := "  j/k move • enter detail • esc back"
	if got != want {
		t.Fatalf("HintsFrom() = %q, want %q", got, want)
	}
}

func TestHintsFromSkipsWhatHasNothingToSay(t *testing.T) {
	disabled := binding("x", "disabled")
	disabled.SetEnabled(false)

	got := footerLine(HintsFrom(footerStyles(), []key.Binding{
		binding("j/k", "move"),
		disabled,
		key.NewBinding(key.WithKeys("z")),
		key.NewBinding(key.WithKeys("y"), key.WithHelp("", "no key")),
		binding("esc", "back"),
	}, 0))

	want := "  j/k move • esc back"
	if got != want {
		t.Fatalf("HintsFrom() = %q, want %q", got, want)
	}
}

func TestHintsFromIsEmptyWithoutBindings(t *testing.T) {
	if got := HintsFrom(footerStyles(), nil, 40); got != "" {
		t.Fatalf("HintsFrom() = %q, want an empty footer", got)
	}
}

func TestHintsFromTrimsToItsBudget(t *testing.T) {
	bindings := []key.Binding{
		binding("j/k", "move"),
		binding("enter", "detail"),
		binding("c", "copy key"),
		binding("o", "open jira"),
		binding("esc", "dashboard"),
	}

	const budget = 30
	plain := footerLine(HintsFrom(footerStyles(), bindings, budget))

	if w := ansi.StringWidth(plain); w > budget {
		t.Fatalf("footer is %d cells wide, over the %d-cell budget: %q", w, budget, plain)
	}
	if !strings.HasPrefix(plain, "  j/k move") {
		t.Fatalf("footer dropped the first hint instead of the last: %q", plain)
	}
	if !strings.HasSuffix(plain, "…") {
		t.Fatalf("footer trimmed silently, without saying more hints exist: %q", plain)
	}
}

func TestHintsFromKeepsOneHintEvenBelowItsBudget(t *testing.T) {
	got := footerLine(HintsFrom(footerStyles(), []key.Binding{
		binding("j/k", "move"),
		binding("enter", "detail"),
	}, 8))

	if got == "" {
		t.Fatal("a footer with almost no room should still name its first hint")
	}
	if !strings.Contains(got, "j/k") {
		t.Fatalf("HintsFrom() = %q, want the first hint kept", got)
	}
	if w := ansi.StringWidth(got); w > 8 {
		t.Fatalf("footer is %d cells wide, over the 8-cell budget: %q", w, got)
	}
}

func TestHintsFromMeasuresWideHintsInCells(t *testing.T) {
	const budget = 24
	got := footerLine(HintsFrom(footerStyles(), []key.Binding{
		binding("j/k", "移動する"),
		binding("enter", "詳細"),
		binding("esc", "戻る"),
	}, budget))

	if w := ansi.StringWidth(got); w > budget {
		t.Fatalf("footer is %d cells wide, over the %d-cell budget: %q", w, budget, got)
	}
}
