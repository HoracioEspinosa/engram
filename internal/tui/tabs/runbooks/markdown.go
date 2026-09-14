package runbooks

import (
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sync"

	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
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
//
// ok is false when ENGRAM_VAULT_ROOT is not configured, in which case path
// is empty. There is no default to fall back to (ADR-053 §6): joining an
// empty root would silently resolve against the process's working
// directory, which is not the vault and would make readRunbookMarkdown read
// the wrong file (or none) without saying why.
func absoluteVaultPath(vaultPath string) (path string, ok bool) {
	root, ok := shared.VaultRoot()
	if !ok {
		return "", false
	}
	return filepath.Join(root, vaultPath), true
}

// readRunbookMarkdown reads vaultPath's file relative to shared.VaultRoot().
//
// Neither a missing file nor an unconfigured ENGRAM_VAULT_ROOT is reported
// as err: a missing file is documented by rfc-tui.md §9.4 as the normal
// state of a checkout of cd-knowledge-mcp that was never cloned locally, and
// an unconfigured variable is a configuration gap S9 must name rather than
// an I/O failure. Both come back through exists=false — the caller tells
// them apart by calling shared.VaultRoot() itself, the same source of truth
// this function consulted, which is exactly what lets S9 render the correct
// one of the two instructions instead of a raw I/O error.
func readRunbookMarkdown(vaultPath string) (content string, exists bool, err error) {
	abs, ok := absoluteVaultPath(vaultPath)
	if !ok {
		return "", false, nil
	}
	raw, err := os.ReadFile(abs)
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
//
// The chroma formatter is chosen from the same resolved profile, so a code
// block quantises its colours exactly as far as the rest of the screen does.
func renderMarkdown(source string, width int, palette theme.Palette) (string, error) {
	if width <= 0 {
		width = defaultRenderWidth
	}
	profile := lipgloss.ColorProfile()

	// The style is registered before the lock below is taken, not inside it:
	// chromaStyleMu is not reentrant, and nothing ever removes an entry from
	// chroma's registry, so a style registered here is still there to render
	// with.
	cfg := glamourStyleConfig(palette, profile)

	options := []glamour.TermRendererOption{
		glamour.WithStyles(cfg),
		glamour.WithColorProfile(profile),
		glamour.WithWordWrap(width),
	}
	if profile != termenv.Ascii {
		options = append(options, glamour.WithChromaFormatter(chromaFormatterFor(profile)))
	}

	// The render itself is serialised for the same reason the registration is:
	// with CodeBlock.Chroma nil, glamour reaches chroma's unsynchronised
	// registry map by name from inside Render, outside any lock of its own.
	// Markdown is loaded from a tea.Cmd, so two tabs rendering at once would
	// be two goroutines reading that map while a third writes it.
	chromaStyleMu.Lock()
	defer chromaStyleMu.Unlock()

	renderer, err := glamour.NewTermRenderer(options...)
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
// semantic token to) and overriding the colour fields the palette has an
// opinion about — the code block's syntax scheme included.
//
// Syntax colours have to come from the palette because glamour's own are
// written for a dark terminal and a code block is drawn straight onto the
// terminal ground: chroma's TTY formatters clear the style's background
// before emitting anything (formatters/tty_indexed.go and tty_truecolour.go
// both open with clearBackground), so there is no panel under the block to
// lift its contrast. Against a light palette's Base those fixed colours read
// down to 1.02:1 — a block nobody can read.
//
// profile decides whether a syntax scheme is attached at all. Under Ascii the
// theme name stays empty so glamour falls back to rendering the block as
// plain text in CodeBlock.Color, which is what keeps a run with no terminal
// attached free of escape sequences.
//
// This mapping — which glamour element gets which Palette field — is this
// task's own composition; rfc-tui.md §8.2 says only that glamour.WithStyles
// must be built "desde la misma paleta", not which element gets which role.
func glamourStyleConfig(p theme.Palette, profile termenv.Profile) ansi.StyleConfig {
	cfg := glamourstyles.DarkStyleConfig

	cfg.Document.Color = hexPtr(p.Text)
	cfg.Heading.Color = hexPtr(p.Secondary)
	cfg.H1.Color = hexPtr(p.Base)
	cfg.H1.BackgroundColor = hexPtr(p.Primary)
	cfg.H2.Color = hexPtr(p.Secondary)
	cfg.H3.Color = hexPtr(p.Secondary)
	cfg.H4.Color = hexPtr(p.Secondary)
	cfg.H5.Color = hexPtr(p.Secondary)
	cfg.H6.Color = hexPtr(p.Secondary)
	cfg.Link.Color = hexPtr(p.Info)
	cfg.LinkText.Color = hexPtr(p.Primary)
	cfg.Code.Color = hexPtr(p.Accent)
	cfg.Code.BackgroundColor = hexPtr(p.Surface)
	cfg.CodeBlock.Color = hexPtr(p.Text)
	cfg.HorizontalRule.Color = hexPtr(p.Overlay)

	// The scheme is handed over by name rather than through CodeBlock.Chroma.
	// glamour derives a chroma style from that field but registers it under
	// one fixed name and only when that name is free, so the first palette to
	// draw a code block would own every code block for the life of the
	// process — and this workspace swaps palettes while it runs.
	cfg.CodeBlock.Chroma = nil
	if profile != termenv.Ascii {
		cfg.CodeBlock.Theme = ensureChromaStyle(p)
	} else {
		cfg.CodeBlock.Theme = ""
	}

	return cfg
}

// chromaStyleMu guards chroma's style registry, which is a plain map with no
// synchronisation of its own (styles/api.go), and the renders that read it.
var chromaStyleMu sync.Mutex

// ensureChromaStyle registers palette's syntax scheme if it is not registered
// already and returns the name to ask for it by.
//
// The name is derived from the palette's colours rather than from its name
// because a row in the themes table shadows a builtin while keeping its name
// (theme.Selection.Stored): two different palettes answering to "koi-pond"
// would otherwise share one registered style, and the one that got there
// first would win.
func ensureChromaStyle(p theme.Palette) string {
	name := chromaStyleName(p)

	chromaStyleMu.Lock()
	defer chromaStyleMu.Unlock()
	if _, ok := styles.Registry[name]; !ok {
		styles.Register(chroma.MustNewStyle(name, chromaStyleEntries(p)))
	}
	return name
}

// chromaStyleName hashes the palette's colours into a registry key. The hash
// only has to separate palettes that differ, so the cheapest one in the
// standard library is the right one.
func chromaStyleName(p theme.Palette) string {
	h := fnv.New32a()
	for _, c := range []lipgloss.Color{
		p.Base, p.Surface, p.Overlay, p.Text, p.Subtext,
		p.Primary, p.Secondary, p.Accent, p.Highlight,
		p.Success, p.Warning, p.Danger, p.Info,
	} {
		_, _ = h.Write([]byte(c))
		_, _ = h.Write([]byte{0})
	}
	return fmt.Sprintf("engram-%08x", h.Sum32())
}

// chromaStyleEntries maps every token type glamour styles onto a palette role.
//
// Which role a token gets is this package's composition — rfc-tui.md §8.1
// assigns no token to a keyword or a string literal — but it is not free
// choice: a code block is drawn on Base with no panel under it, so every
// colour here has to be one the palette already keeps legible against Base.
// Info is deliberately unused for that reason, and the emphasis glamour's own
// scheme gives a token is kept.
//
// No entry carries a background. chroma's TTY formatters strip backgrounds
// before emitting, so a "bg:" here would be a colour nobody ever sees.
func chromaStyleEntries(p theme.Palette) chroma.StyleEntries {
	var (
		text      = string(p.Text)
		subtext   = string(p.Subtext)
		primary   = string(p.Primary)
		secondary = string(p.Secondary)
		accent    = string(p.Accent)
		highlight = string(p.Highlight)
		success   = string(p.Success)
		danger    = string(p.Danger)
	)
	return chroma.StyleEntries{
		// Background is the root every token without an entry inherits from,
		// so it carries the default foreground rather than a ground.
		chroma.Background:          text,
		chroma.Text:                text,
		chroma.Name:                text,
		chroma.NameOther:           text,
		chroma.Punctuation:         text,
		chroma.Operator:            text,
		chroma.Comment:             subtext + " italic",
		chroma.CommentPreproc:      subtext,
		chroma.Keyword:             primary + " bold",
		chroma.KeywordReserved:     primary,
		chroma.KeywordNamespace:    primary,
		chroma.KeywordType:         primary,
		chroma.Literal:             success,
		chroma.LiteralString:       success,
		chroma.LiteralStringEscape: success,
		chroma.LiteralNumber:       success,
		chroma.LiteralDate:         success,
		chroma.NameFunction:        secondary,
		chroma.NameClass:           secondary + " bold underline",
		chroma.NameBuiltin:         accent,
		chroma.NameConstant:        accent,
		chroma.NameDecorator:       accent,
		chroma.NameException:       accent,
		chroma.NameTag:             highlight,
		chroma.NameAttribute:       highlight,
		chroma.Error:               danger + " bold",
		chroma.GenericDeleted:      danger,
		chroma.GenericInserted:     success,
		chroma.GenericEmph:         text + " italic",
		chroma.GenericStrong:       text + " bold",
		chroma.GenericSubheading:   secondary,
	}
}

// chromaFormatterFor picks the chroma formatter that matches the profile
// lipgloss detected, so a code block is quantised exactly as far as the rest
// of the screen is and no further. Naming a truecolor formatter on a terminal
// that reports 256 colours is the coupling WithColorProfile exists to avoid.
func chromaFormatterFor(profile termenv.Profile) string {
	switch profile {
	case termenv.TrueColor:
		return "terminal16m"
	case termenv.ANSI256:
		return "terminal256"
	default:
		return "terminal16"
	}
}

// hexPtr adapts a Palette colour (always a "#rrggbb" lipgloss.Color, since
// every registered palette is truecolor) to the *string ansi.StyleConfig's
// fields want.
func hexPtr(c lipgloss.Color) *string {
	s := string(c)
	return &s
}
