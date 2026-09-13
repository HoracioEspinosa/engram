package runbooks

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	glamourstyles "github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// defaultRenderWidth is used when the tab has not yet received a
// tea.WindowSizeMsg (a markdown load raced ahead of the first resize).
const defaultRenderWidth = 80

// absoluteVaultPath resolves a runbook_index.vault_path — relative to
// shared.VaultRoot(), per rfc-tui.md §9.1's documented contract — to a real
// filesystem path. It mirrors tabs/evidence/model.go's absolutePath: reading
// a runbook's Markdown file is not part of data.RunbookReader (see its doc
// comment), the same way tabs/evidence keeps manifest.json off
// data.EvidenceReader — plain filesystem access with nothing to query the
// store for belongs to the tab, not the reader.
func absoluteVaultPath(vaultPath string) string {
	return filepath.Join(shared.VaultRoot(), vaultPath)
}

// readRunbookMarkdown reads vaultPath's file relative to shared.VaultRoot().
//
// A missing file is not an error — rfc-tui.md §9.4 documents it as the
// normal state of a checkout of cd-knowledge-mcp that was never cloned
// locally — it is reported through exists=false so S9 can show the index
// fields plus the instruction to clone instead of a raw I/O error.
func readRunbookMarkdown(vaultPath string) (content string, exists bool, err error) {
	raw, err := os.ReadFile(absoluteVaultPath(vaultPath))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	return string(raw), true, nil
}

// renderMarkdown converts source into the ANSI-styled text glamour produces,
// wrapped to width (rfc-tui.md §9.4: "la vista Markdown... lo renderiza con
// glamour"), styled from palette instead of glamour's own automatic
// light/dark detection (rfc-tui.md §8.2: "El estilo de glamour se genera
// desde la misma paleta... para que el Markdown de runbooks no rompa la
// coherencia visual").
//
// Two determinism fixes make this call, together, actually colourless under
// a non-terminal stdout — T-10.07's teatest golden files need that, the
// same way golden_test.go's own comment documents lipgloss falling back to
// the Ascii profile automatically when `go test` attaches no TTY:
//
//  1. glamour.WithColorProfile(lipgloss.ColorProfile()) pins glamour to the
//     exact profile lipgloss's own default renderer detected, instead of
//     glamour.NewTermRenderer's undocumented default: it hardcodes
//     ansiOptions.ColorProfile to termenv.TrueColor unless told otherwise,
//     unlike lipgloss it never checks whether stdout is a terminal at all.
//  2. Even with that pin, glamour v1.0.0's ansi.renderText (baseelement.go)
//     builds its termenv.Style via the package-level termenv.String(s) —
//     hardcoded to profile ANSI — instead of the profile-aware
//     ctx.options.ColorProfile.String(s); the ColorProfile argument only
//     reaches Foreground/Background (so colour is correctly suppressed
//     under Ascii), never Bold/Italic/Underline/Reverse/Blink/CrossOut/
//     Overline (verified against the vendored source: those escapes are
//     emitted unconditionally). Confirmed against v1.0.0, the version this
//     module pins — an upstream fix could remove the need for this. Rather
//     than vendor a patched glamour for one call site, ansi.Strip removes
//     whatever glamour still emitted once the resolved profile is Ascii,
//     which is exactly the condition under which every other screen's
//     lipgloss output is already escape-free.
func renderMarkdown(source string, width int, palette theme.Palette) (string, error) {
	if width <= 0 {
		width = defaultRenderWidth
	}
	profile := lipgloss.ColorProfile()
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(glamourStyleConfig(palette)),
		glamour.WithColorProfile(profile),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return source, err
	}
	out, err := renderer.Render(source)
	if err != nil {
		return source, err
	}
	if profile == termenv.Ascii {
		out = xansi.Strip(out)
	}
	return out, nil
}

// glamourStyleConfig derives a glamour ansi.StyleConfig from palette,
// starting from glamour's own DarkStyleConfig (for its prefixes, indents and
// bullet/blockquote/table formatting, none of which rfc-tui.md §8 assigns a
// semantic token to) and overriding only the colour fields the palette
// actually has an opinion about. Chroma (code block syntax highlighting) is
// left at DarkStyleConfig's own scheme: rfc-tui.md §8.1's token table has no
// entry for source-code tokens, so recolouring them would be this package's
// own invention rather than something the RFC specifies.
//
// This mapping — which glamour element gets which Palette field — is this
// task's own composition; rfc-tui.md §8.2 says only that glamour.WithStyles
// must be built "desde la misma paleta", not which element gets which role.
func glamourStyleConfig(p theme.Palette) ansi.StyleConfig {
	cfg := glamourstyles.DarkStyleConfig

	cfg.Document.Color = hexPtr(p.Text)
	cfg.Heading.Color = hexPtr(p.Accent)
	cfg.H1.Color = hexPtr(p.Base)
	cfg.H1.BackgroundColor = hexPtr(p.Primary)
	cfg.H2.Color = hexPtr(p.Accent)
	cfg.H3.Color = hexPtr(p.Accent)
	cfg.H4.Color = hexPtr(p.Accent)
	cfg.H5.Color = hexPtr(p.Accent)
	cfg.H6.Color = hexPtr(p.Accent)
	cfg.Link.Color = hexPtr(p.Info)
	cfg.LinkText.Color = hexPtr(p.Primary)
	cfg.Code.Color = hexPtr(p.Highlight)
	cfg.Code.BackgroundColor = hexPtr(p.Surface)
	cfg.CodeBlock.Color = hexPtr(p.Subtext)
	cfg.HorizontalRule.Color = hexPtr(p.Overlay)

	return cfg
}

// hexPtr adapts a Palette colour (always a "#rrggbb" lipgloss.Color, since
// every registered palette is truecolor) to the *string ansi.StyleConfig's
// fields want.
func hexPtr(c lipgloss.Color) *string {
	s := string(c)
	return &s
}
