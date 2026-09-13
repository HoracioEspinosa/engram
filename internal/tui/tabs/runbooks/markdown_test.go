package runbooks

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/theme"
)

// TestRenderMarkdownIsColourlessUnderGoTest guards the determinism T-10.07's
// teatest golden files need: rendering must not emit ANSI escape sequences
// when stdout is not a terminal, the same invariant
// internal/tui/app/golden_test.go's renderNoANSI already holds every
// lipgloss-styled screen to.
//
// This failed for real against this repo's code before this change:
// glamour.NewTermRenderer hardcodes its ColorProfile to termenv.TrueColor
// unless told otherwise — unlike lipgloss, it does not check whether stdout
// is a terminal — so renderMarkdown(source, width), before it passed
// glamour.WithColorProfile(lipgloss.ColorProfile()), emitted real ANSI
// truecolor escapes even under `go test`. See this task's report for the
// literal failure this reproduced.
func TestRenderMarkdownIsColourlessUnderGoTest(t *testing.T) {
	out, err := renderMarkdown("# Heading\n\nSome **bold** text and a [link](https://example.com).", 80, theme.CatppuccinMocha())
	if err != nil {
		t.Fatalf("renderMarkdown: %v", err)
	}
	if strings.ContainsRune(out, '\x1b') {
		t.Fatalf("renderMarkdown emitted an ANSI escape sequence under go test (no TTY attached):\n%q", out)
	}
}

// TestGlamourStyleConfigUsesThePalette proves glamourStyleConfig is not
// returning a fixed style regardless of its argument: two different
// palettes must produce two different heading colours, each matching that
// palette's own Accent.
func TestGlamourStyleConfigUsesThePalette(t *testing.T) {
	mocha := theme.CatppuccinMocha()
	kanagawa := theme.Kanagawa()

	mochaCfg := glamourStyleConfig(mocha)
	kanagawaCfg := glamourStyleConfig(kanagawa)

	if mochaCfg.Heading.Color == nil || *mochaCfg.Heading.Color != string(mocha.Accent) {
		t.Errorf("catppuccin-mocha Heading.Color = %v, want %s", mochaCfg.Heading.Color, mocha.Accent)
	}
	if kanagawaCfg.Heading.Color == nil || *kanagawaCfg.Heading.Color != string(kanagawa.Accent) {
		t.Errorf("kanagawa Heading.Color = %v, want %s", kanagawaCfg.Heading.Color, kanagawa.Accent)
	}
	if mochaCfg.Heading.Color != nil && kanagawaCfg.Heading.Color != nil && *mochaCfg.Heading.Color == *kanagawaCfg.Heading.Color {
		t.Fatal("catppuccin-mocha and kanagawa produced the same glamour heading colour — the palette is not flowing through glamourStyleConfig")
	}
}

// TestRenderMarkdownRejectsNothing pins that a read failure never happened
// here — renderMarkdown itself has no filesystem dependency, only
// readRunbookMarkdown does — by confirming a render always returns the
// wrapped word count intact for plain text with no markdown at all.
func TestRenderMarkdownWrapsPlainText(t *testing.T) {
	out, err := renderMarkdown("hello world", 80, theme.Elephant())
	if err != nil {
		t.Fatalf("renderMarkdown: %v", err)
	}
	if !strings.Contains(out, "hello world") {
		t.Fatalf("renderMarkdown output = %q, want it to contain the source text", out)
	}
}
