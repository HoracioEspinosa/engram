package runbooks

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
	glamourstyles "github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestRenderMarkdownIsColourlessUnderGoTest guards the determinism the
// teatest golden files need: rendering must not emit ANSI escape sequences
// when stdout is not a terminal, the same invariant
// internal/tui/app/golden_test.go's renderNoANSI already holds every
// lipgloss-styled screen to.
//
// The pin is not theoretical: glamour.NewTermRenderer hardcodes its
// ColorProfile to termenv.TrueColor unless told otherwise — unlike lipgloss,
// it does not check whether stdout is a terminal — so a renderMarkdown that
// stops passing glamour.WithColorProfile(lipgloss.ColorProfile()) emits real
// ANSI truecolor escapes even under `go test`.
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
// palette's own Secondary.
func TestGlamourStyleConfigUsesThePalette(t *testing.T) {
	mocha := theme.CatppuccinMocha()
	kanagawa := theme.Kanagawa()

	mochaCfg := glamourStyleConfig(mocha, termenv.TrueColor)
	kanagawaCfg := glamourStyleConfig(kanagawa, termenv.TrueColor)

	if mochaCfg.Heading.Color == nil || *mochaCfg.Heading.Color != string(mocha.Secondary) {
		t.Errorf("catppuccin-mocha Heading.Color = %v, want %s", mochaCfg.Heading.Color, mocha.Secondary)
	}
	if kanagawaCfg.Heading.Color == nil || *kanagawaCfg.Heading.Color != string(kanagawa.Secondary) {
		t.Errorf("kanagawa Heading.Color = %v, want %s", kanagawaCfg.Heading.Color, kanagawa.Secondary)
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

// chromaFixedBackground is the code-block background glamour's own dark
// scheme hardcodes. It is what a palette-driven config must no longer carry:
// a light palette paints its own foregrounds on top of it, and a dark grey
// under a dark grey is a block nobody can read.
const chromaFixedBackground = "#373737"

// chromaDefaultText is the plain-source colour of the same scheme: a light
// grey that a light palette leaves at 1.5:1 against its own ground.
const chromaDefaultText = "#C4C4C4"

// sgrBackground and sgrForeground spell a hex colour the way termenv writes
// it into a truecolor escape, so a test can look for a colour in the bytes a
// terminal actually receives.
func sgrBackground(hex string) string { return "48;2;" + sgrTriplet(hex) }

func sgrForeground(hex string) string { return "38;2;" + sgrTriplet(hex) }

func sgrTriplet(hex string) string {
	var r, g, b int
	fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b)
	return fmt.Sprintf("%d;%d;%d", r, g, b)
}

// colourCodes reduces a rendered frame to the distinct colour parameters it
// carries, in the order they first appear. A failure about which colours
// reached the terminal reads off a dozen codes; the frame itself is thousands
// of escape-laden bytes nobody reads.
func colourCodes(rendered string) []string {
	var codes []string
	seen := map[string]bool{}
	for _, run := range strings.Split(rendered, "\x1b[") {
		end := strings.IndexByte(run, 'm')
		if end <= 0 {
			continue
		}
		for _, part := range sgrColours(run[:end]) {
			if !seen[part] {
				seen[part] = true
				codes = append(codes, part)
			}
		}
	}
	return codes
}

// sgrColours pulls the colour parameters out of one SGR parameter string,
// keeping the 38/48 selectors together with the arguments they consume.
func sgrColours(params string) []string {
	fields := strings.Split(params, ";")
	var out []string
	for i := 0; i < len(fields); i++ {
		switch fields[i] {
		case "38", "48":
			if i+1 < len(fields) && fields[i+1] == "2" && i+4 < len(fields) {
				out = append(out, strings.Join(fields[i:i+5], ";"))
				i += 4
			} else if i+1 < len(fields) && fields[i+1] == "5" && i+2 < len(fields) {
				out = append(out, strings.Join(fields[i:i+3], ";"))
				i += 2
			}
		}
	}
	return out
}

// paletteHexes is the set of colours a palette carries, for asserting that a
// colour which reached the terminal came from the palette and nowhere else.
func paletteHexes(p theme.Palette) map[string]bool {
	out := map[string]bool{}
	for _, c := range []lipgloss.Color{
		p.Base, p.Surface, p.Overlay, p.Text, p.Subtext,
		p.Primary, p.Secondary, p.Accent, p.Highlight,
		p.Success, p.Warning, p.Danger, p.Info,
	} {
		out[string(c)] = true
	}
	return out
}

// codeBlockFixture is a runbook with one fenced block, so a test exercises the
// chroma path rather than glamour's plain-text fallback.
const codeBlockFixture = "# Title\n\n```go\n// restart the pool\nfunc main() { restart(\"pool\", 3) }\n```\n"

// truecolor pins the profile for a test that reads the escape sequences a
// terminal would receive. Under `go test` lipgloss resolves to Ascii, which is
// exactly the profile that emits nothing to read.
func truecolor(t *testing.T) {
	t.Helper()
	restore := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })
}

// TestCodeBlockChromaEntriesComeFromThePalette pins that every token type
// glamour styles is answered from the palette, with none of glamour's own
// fixed colours surviving.
//
// The assertion is structural rather than a second contrast measurement on
// purpose. A code block is drawn straight onto Base with no panel under it, so
// the plane its colours are read against is exactly the one
// TestRegisteredPalettesAreLegible already audits every role against. Proving
// each token holds a palette role inherits that coverage; re-deriving WCAG
// here would mean copying theme's own debt list into this package, where it
// would drift from the original at the first change of hue.
func TestCodeBlockChromaEntriesComeFromThePalette(t *testing.T) {
	for _, builtin := range theme.Builtins() {
		t.Run(builtin.Name, func(t *testing.T) {
			hexes := paletteHexes(builtin.Palette)
			entries := chromaStyleEntries(builtin.Palette)
			if len(entries) == 0 {
				t.Fatal("the palette produced no syntax scheme at all")
			}
			for token, entry := range entries {
				colour, _, _ := strings.Cut(entry, " ")
				if colour == "" {
					t.Errorf("token %v carries no colour", token)
					continue
				}
				if !hexes[colour] {
					t.Errorf("token %v is %s, which is not one of the palette's colours", token, colour)
				}
				if strings.Contains(entry, "bg:") {
					t.Errorf("token %v declares a background; chroma's TTY formatters strip it", token)
				}
			}
			if got := entries[chroma.Text]; !strings.HasPrefix(got, string(builtin.Palette.Text)) {
				t.Errorf("the plain-text token is %q, want the palette's Text %s", got, builtin.Palette.Text)
			}
		})
	}
}

// TestChromaSchemeAvoidsTheRolesWithContrastDebt pins the one constraint the
// role mapping is not free to ignore: Info carries known debt against Base in
// an inherited palette, and a code block has no panel to lift it.
func TestChromaSchemeAvoidsTheRolesWithContrastDebt(t *testing.T) {
	for _, builtin := range theme.Builtins() {
		t.Run(builtin.Name, func(t *testing.T) {
			info := string(builtin.Palette.Info)
			for token, entry := range chromaStyleEntries(builtin.Palette) {
				if colour, _, _ := strings.Cut(entry, " "); colour == info {
					t.Errorf("token %v uses Info, a role with contrast debt against Base", token)
				}
			}
		})
	}
}

// TestGlamoursOwnSchemeIsNeverMutated guards the package global this config is
// copied from. ansi.StyleConfig holds Chroma behind a pointer, so a struct
// copy still shares that scheme: writing through it would repaint every
// palette at once and whichever theme was configured last would own them all.
func TestGlamoursOwnSchemeIsNeverMutated(t *testing.T) {
	for _, builtin := range theme.Builtins() {
		glamourStyleConfig(builtin.Palette, termenv.TrueColor)
	}
	got := glamourstyles.DarkStyleConfig.CodeBlock.Chroma.Background.BackgroundColor
	if got == nil || *got != chromaFixedBackground {
		t.Fatalf("glamour's own scheme was mutated: its code-block background is now %v", got)
	}
}

// TestEachPaletteRegistersItsOwnChromaStyle is the reason the scheme travels by
// name instead of through CodeBlock.Chroma: glamour registers that field under
// one fixed name and only when the name is free, so two palettes would share
// one style and the second would silently inherit the first.
func TestEachPaletteRegistersItsOwnChromaStyle(t *testing.T) {
	seen := map[string]string{}
	for _, builtin := range theme.Builtins() {
		name := ensureChromaStyle(builtin.Palette)
		if other, clash := seen[name]; clash {
			t.Fatalf("%s and %s registered the same style %q", other, builtin.Name, name)
		}
		seen[name] = builtin.Name
		if styles.Get(name).Name != name {
			t.Fatalf("%s asked for style %q and chroma fell back to %q", builtin.Name, name, styles.Get(name).Name)
		}
	}
}

// TestCodeBlockStyleIsNotFrozenByTheFirstPalette renders two palettes in one
// process, which is what a theme swap does. A block that keeps the first
// palette's colours is the registry bug this design exists to avoid.
func TestCodeBlockStyleIsNotFrozenByTheFirstPalette(t *testing.T) {
	truecolor(t)

	// The keyword is what pins this. Its colour is written by chroma from the
	// registered scheme, and it is the palette's Primary — a role the block's
	// own indentation never uses, so finding it there means the scheme, not
	// the surrounding config, answered.
	for _, p := range []theme.Palette{theme.KoiDay(), theme.KoiPond()} {
		out, err := renderMarkdown(codeBlockFixture, 80, p)
		if err != nil {
			t.Fatalf("renderMarkdown(%s): %v", p.Name, err)
		}
		block := codeBlockLines(out)
		if block == "" {
			t.Fatal("the fixture rendered no code block at all")
		}
		if want := sgrForeground(string(p.Primary)); !strings.Contains(block, want) {
			t.Fatalf("%s drew its keywords in %v, not in its own Primary %s — the scheme is frozen to an earlier palette",
				p.Name, colourCodes(block), p.Primary)
		}
	}
}

// TestCodeBlockColoursAreThePalettes reads the escape sequences a terminal
// would receive for the code block itself and holds every one of them to the
// palette.
//
// The assertion is scoped to the block's own lines because the H1 above it
// legitimately fills a background, and it allows one unit per channel because
// the two writers disagree by that much: chroma writes the hex verbatim, while
// glamour hands its colours to termenv, which round-trips them through a
// colour space and can land a unit off. A unit is not a colour anyone can
// name; a different role is, and that is what this catches.
func TestCodeBlockColoursAreThePalettes(t *testing.T) {
	truecolor(t)

	for _, builtin := range theme.Builtins() {
		t.Run(builtin.Name, func(t *testing.T) {
			out, err := renderMarkdown(codeBlockFixture, 80, builtin.Palette)
			if err != nil {
				t.Fatalf("renderMarkdown: %v", err)
			}

			block := codeBlockLines(out)
			if block == "" {
				t.Fatal("the fixture rendered no code block at all")
			}
			codes := colourCodes(block)
			if len(codes) == 0 {
				t.Fatal("the code block carries no colour at all")
			}
			for _, code := range codes {
				if strings.HasPrefix(code, "48;") {
					t.Errorf("the code block fills background %s; chroma's formatters strip backgrounds, so nothing should", code)
					continue
				}
				if !nearAPaletteColour(builtin.Palette, code) {
					t.Errorf("foreground %s belongs to no palette role; all: %v", code, codes)
				}
			}
			if strings.Contains(block, sgrForeground(chromaDefaultText)) {
				t.Errorf("the code block still carries glamour's own text colour; all: %v", codes)
			}
			if strings.Contains(block, sgrBackground(chromaFixedBackground)) {
				t.Errorf("the code block still carries glamour's fixed background; all: %v", codes)
			}
		})
	}
}

// codeBlockLines returns the rendered lines that carry codeBlockFixture's
// source, which is the part of the frame chroma wrote.
func codeBlockLines(rendered string) string {
	var out []string
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "restart") {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// nearAPaletteColour reports whether a truecolor parameter run names one of
// the palette's roles, within the one unit per channel termenv's round trip
// can cost.
func nearAPaletteColour(p theme.Palette, code string) bool {
	got, ok := rgbOfSGR(code)
	if !ok {
		return false
	}
	for hex := range paletteHexes(p) {
		want, ok := rgbOfSGR(sgrForeground(hex))
		if !ok {
			continue
		}
		if abs(got[0]-want[0]) <= 1 && abs(got[1]-want[1]) <= 1 && abs(got[2]-want[2]) <= 1 {
			return true
		}
	}
	return false
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// TestRenderMarkdownIsSafeUnderConcurrency covers the registry chroma exposes
// as a plain map. Markdown is loaded from a tea.Cmd, so two tabs rendering at
// once are two goroutines reaching it.
func TestRenderMarkdownIsSafeUnderConcurrency(t *testing.T) {
	truecolor(t)

	var wg sync.WaitGroup
	for _, builtin := range theme.Builtins() {
		wg.Add(1)
		go func(p theme.Palette) {
			defer wg.Done()
			if _, err := renderMarkdown(codeBlockFixture, 80, p); err != nil {
				t.Errorf("renderMarkdown: %v", err)
			}
		}(builtin.Palette)
	}
	wg.Wait()
}

// rgbOfSGR reads the three channels out of a truecolor parameter run, so a
// colour in the output can be compared with one in a palette.
func rgbOfSGR(code string) ([3]int, bool) {
	var rgb [3]int
	fields := strings.Split(code, ";")
	if len(fields) != 5 || fields[1] != "2" {
		return rgb, false
	}
	for i, f := range fields[2:] {
		if _, err := fmt.Sscanf(f, "%d", &rgb[i]); err != nil {
			return rgb, false
		}
	}
	return rgb, true
}
