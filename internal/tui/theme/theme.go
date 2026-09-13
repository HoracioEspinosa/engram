// Package theme turns a colour palette into the concrete lipgloss styles the
// TUI renders with.
//
// Nothing outside this package names a hex value. Views ask for a semantic
// role — Primary, Danger, Subtext — so swapping a palette re-skins the whole
// workspace without touching a single view.
package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// logoRows is the number of gradient stops the wordmark needs, one per row of
// the ASCII logo.
const logoRows = 5

// Palette is the semantic colour contract a theme fulfils.
//
// Roles, not hues: a palette that renames Danger to something green is a bug,
// not a variant.
type Palette struct {
	// Name identifies the palette in configuration and in the theme registry.
	Name string

	// Base and Surface are background planes: the terminal ground and the
	// panels that sit on it.
	Base    lipgloss.Color
	Surface lipgloss.Color
	// Overlay draws borders and separators.
	Overlay lipgloss.Color

	// Text is the default foreground; Subtext is de-emphasised copy such as
	// timestamps, hints and previews.
	Text    lipgloss.Color
	Subtext lipgloss.Color

	// Primary marks the focused element and the brand.
	Primary lipgloss.Color
	// Secondary titles sections and headings (rfc-tui.md §8.1's "secondary"
	// token: "títulos de sección").
	Secondary lipgloss.Color
	// Accent tags classifications such as an observation type badge
	// (rfc-tui.md §8.1's "accent" token: "badges de tipo").
	Accent lipgloss.Color
	// Highlight marks a search match inline (rfc-tui.md §8.1's "highlight"
	// token: "coincidencias de búsqueda"). On elephant it happens to equal
	// Success, which is why it is excluded from
	// TestNoTwoDistinctRolesShareAColour.
	Highlight lipgloss.Color

	// Success, Warning, Danger and Info carry state, in that order of
	// escalation.
	Success lipgloss.Color
	Warning lipgloss.Color
	Danger  lipgloss.Color
	Info    lipgloss.Color

	// LogoGradient colours the wordmark from its top row to its bottom row.
	LogoGradient [logoRows]lipgloss.Color
}

// Elephant is the palette engram shipped with before theming existed —
// rfc-tui.md §8 found these thirteen hex values hardcoded in styles.go under
// a comment claiming Catppuccin Mocha, when they are actually Rosé Pine. It
// stays selectable as "elephant" so an upgrade never surprises anyone who
// liked the original look; CatppuccinMocha, not this, is the default (ADR-028
// §5, rfc-tui.md §8.2).
func Elephant() Palette {
	var (
		base      = lipgloss.Color("#191724")
		surface   = lipgloss.Color("#1f1d2e")
		overlay   = lipgloss.Color("#6e6a86")
		text      = lipgloss.Color("#e0def4")
		subtext   = lipgloss.Color("#908caa")
		primary   = lipgloss.Color("#c4a7e7")
		secondary = lipgloss.Color("#ebbcba")
		accent    = lipgloss.Color("#f6c177")
		highlight = lipgloss.Color("#9ccfd8")
		success   = lipgloss.Color("#9ccfd8")
		warning   = lipgloss.Color("#f1ca93")
		danger    = lipgloss.Color("#eb6f92")
		info      = lipgloss.Color("#31748f")
	)

	return Palette{
		Name:         "elephant",
		Base:         base,
		Surface:      surface,
		Overlay:      overlay,
		Text:         text,
		Subtext:      subtext,
		Primary:      primary,
		Secondary:    secondary,
		Accent:       accent,
		Highlight:    highlight,
		Success:      success,
		Warning:      warning,
		Danger:       danger,
		Info:         info,
		LogoGradient: [logoRows]lipgloss.Color{secondary, primary, info, success, success},
	}
}

// CatppuccinMocha is the Catppuccin Mocha colour scheme and the default
// theme (ADR-028 §5, rfc-tui.md §8.2). Values are transcribed from
// rfc-tui.md §8.1's table, itself the project's canonical Catppuccin Mocha
// hex codes (https://catppuccin.com/palette — mocha: lavender #b4befe,
// mauve #cba6f7, peach #fab387, teal #94e2d5, blue #89b4fa), not composed.
func CatppuccinMocha() Palette {
	var (
		base      = lipgloss.Color("#1e1e2e")
		surface   = lipgloss.Color("#313244")
		overlay   = lipgloss.Color("#6c7086")
		text      = lipgloss.Color("#cdd6f4")
		subtext   = lipgloss.Color("#a6adc8")
		primary   = lipgloss.Color("#b4befe")
		secondary = lipgloss.Color("#cba6f7")
		accent    = lipgloss.Color("#fab387")
		highlight = lipgloss.Color("#94e2d5")
		success   = lipgloss.Color("#a6e3a1")
		warning   = lipgloss.Color("#f9e2af")
		danger    = lipgloss.Color("#f38ba8")
		info      = lipgloss.Color("#89b4fa")
	)

	return Palette{
		Name:         "catppuccin-mocha",
		Base:         base,
		Surface:      surface,
		Overlay:      overlay,
		Text:         text,
		Subtext:      subtext,
		Primary:      primary,
		Secondary:    secondary,
		Accent:       accent,
		Highlight:    highlight,
		Success:      success,
		Warning:      warning,
		Danger:       danger,
		Info:         info,
		LogoGradient: [logoRows]lipgloss.Color{secondary, primary, info, success, success},
	}
}

// Kanagawa is the Kanagawa "wave" colour scheme rfc-tui.md §8.1 specifies as
// the second selectable option. Values are transcribed from that table,
// itself Kanagawa's published hex codes
// (https://github.com/rebelot/kanagawa.nvim — wave: crystalBlue #7e9cd8,
// oniViolet #957fb8, springGreen #98bb6c, carpYellow #e6c384, samuraiRed
// #e82424, springBlue #7fb4ca, surimiOrange #ffa066, waveAqua2 #7aa89f), not
// composed.
func Kanagawa() Palette {
	var (
		base      = lipgloss.Color("#1f1f28")
		surface   = lipgloss.Color("#2a2a37")
		overlay   = lipgloss.Color("#54546d")
		text      = lipgloss.Color("#dcd7ba")
		subtext   = lipgloss.Color("#727169")
		primary   = lipgloss.Color("#7e9cd8")
		secondary = lipgloss.Color("#957fb8")
		accent    = lipgloss.Color("#ffa066")
		highlight = lipgloss.Color("#7aa89f")
		success   = lipgloss.Color("#98bb6c")
		warning   = lipgloss.Color("#e6c384")
		danger    = lipgloss.Color("#e82424")
		info      = lipgloss.Color("#7fb4ca")
	)

	return Palette{
		Name:         "kanagawa",
		Base:         base,
		Surface:      surface,
		Overlay:      overlay,
		Text:         text,
		Subtext:      subtext,
		Primary:      primary,
		Secondary:    secondary,
		Accent:       accent,
		Highlight:    highlight,
		Success:      success,
		Warning:      warning,
		Danger:       danger,
		Info:         info,
		LogoGradient: [logoRows]lipgloss.Color{secondary, primary, info, success, success},
	}
}

// registry lists every palette selectable by name (rfc-tui.md §8.2). The
// three keys are the only valid values for --theme, ENGRAM_TUI_THEME and
// tui.theme.
var registry = map[string]func() Palette{
	"catppuccin-mocha": CatppuccinMocha,
	"kanagawa":         Kanagawa,
	"elephant":         Elephant,
}

// DefaultThemeName is the palette rfc-tui.md §8.2 and ADR-028 §5 fix as the
// out-of-the-box theme.
const DefaultThemeName = "catppuccin-mocha"

// pickName returns the first non-blank candidate among flag, env and config,
// in that precedence order, or "" if all three are blank. Resolve and
// UnknownName both build on it so the two never disagree on which tier won.
func pickName(flag, env, config string) string {
	for _, candidate := range []string{flag, env, config} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// Resolve picks a palette by name from flag, env or config, in that order of
// precedence — the first non-blank one wins outright, with no fallthrough to
// a lower tier if it turns out invalid. rfc-tui.md §8.2: "flag `--theme` >
// ENGRAM_TUI_THEME > tui.theme > catppuccin-mocha". A blank or unrecognised
// name at the winning tier resolves to DefaultThemeName, never to a lower
// tier's value, so a typo in --theme cannot silently fall back to whatever
// ENGRAM_TUI_THEME happens to hold.
func Resolve(flag, env, config string) Palette {
	ctor, ok := registry[pickName(flag, env, config)]
	if !ok {
		ctor = registry[DefaultThemeName]
	}
	return ctor()
}

// UnknownName reports the winning candidate among flag, env and config when
// it is non-blank and not a registered palette, so a caller can warn before
// Resolve silently falls back to DefaultThemeName — rfc-tui.md §10.1's
// smoke test: "engram tui --theme desconocido cae al default con aviso". It
// returns "" when the winning candidate is blank, or already valid, in
// which case no warning is warranted.
func UnknownName(flag, env, config string) string {
	name := pickName(flag, env, config)
	if name == "" {
		return ""
	}
	if _, ok := registry[name]; ok {
		return ""
	}
	return name
}

// Styles is every style the TUI renders with, built once from a Palette and
// then copied by value into each tab.
//
// A lipgloss.Style is immutable in use: calling Width or Bold on one returns a
// copy, so sharing this struct across tabs is safe.
type Styles struct {
	// Palette is kept so a view that needs a raw colour — a gradient, an
	// inline border — reads it from here instead of redefining one.
	Palette Palette

	// Frame.
	App          lipgloss.Style
	Header       lipgloss.Style
	Help         lipgloss.Style
	Error        lipgloss.Style
	Notice       lipgloss.Style
	UpdateBanner lipgloss.Style

	// TabBar is the persistent tab bar's container (rfc-tui.md §7's chrome);
	// TabActive and TabInactive style, respectively, the tab under the
	// cursor and every other one, so the bar always paints with the active
	// palette instead of a default one.
	TabBar      lipgloss.Style
	TabActive   lipgloss.Style
	TabInactive lipgloss.Style

	// Dashboard.
	StatNumber   lipgloss.Style
	StatLabel    lipgloss.Style
	StatCard     lipgloss.Style
	MenuItem     lipgloss.Style
	MenuSelected lipgloss.Style
	Title        lipgloss.Style

	// Lists.
	ListItem          lipgloss.Style
	ListSelected      lipgloss.Style
	TypeBadge         lipgloss.Style
	StateWarningBadge lipgloss.Style
	StaleBadge        lipgloss.Style
	AttachedBadge     lipgloss.Style
	ID                lipgloss.Style
	Timestamp         lipgloss.Style
	Project           lipgloss.Style
	ContentPreview    lipgloss.Style

	// Detail views.
	SectionHeading lipgloss.Style
	DetailContent  lipgloss.Style
	DetailLabel    lipgloss.Style
	DetailValue    lipgloss.Style

	// Timeline.
	TimelineFocus     lipgloss.Style
	TimelineItem      lipgloss.Style
	TimelineConnector lipgloss.Style

	// Search.
	SearchInput     lipgloss.Style
	SearchHighlight lipgloss.Style
	NoResults       lipgloss.Style

	// Wordmark.
	LogoFrame    lipgloss.Style
	LogoAccent   lipgloss.Style
	LogoTagline  lipgloss.Style
	LogoGradient [logoRows]lipgloss.Style

	// Spinner is the throbber shown while a long operation runs.
	Spinner lipgloss.Style

	// Inline emphasis used inside composed lines.
	Emphasis      lipgloss.Style
	SuccessInline lipgloss.Style
	DangerInline  lipgloss.Style
}

// New builds the style set for a palette.
func New(p Palette) Styles {
	s := Styles{Palette: p}

	s.App = lipgloss.NewStyle().
		Foreground(p.Text).
		Padding(1, 2)

	s.Header = lipgloss.NewStyle().
		Bold(true).
		Foreground(p.Primary).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(p.Overlay).
		PaddingBottom(1).
		MarginBottom(1)

	s.Help = lipgloss.NewStyle().
		Foreground(p.Subtext).
		MarginTop(1)

	s.Error = lipgloss.NewStyle().
		Foreground(p.Danger).
		Bold(true).
		Padding(0, 1)

	s.Notice = lipgloss.NewStyle().
		Foreground(p.Success).
		Bold(true).
		Padding(0, 1)

	s.UpdateBanner = lipgloss.NewStyle().
		Foreground(p.Warning).
		Bold(true).
		Padding(0, 1)

	s.TabBar = lipgloss.NewStyle().
		MarginBottom(1)

	s.TabActive = lipgloss.NewStyle().
		Bold(true).
		Foreground(p.Primary)

	s.TabInactive = lipgloss.NewStyle().
		Foreground(p.Subtext)

	s.StatNumber = lipgloss.NewStyle().
		Bold(true).
		Foreground(p.Success).
		Width(8).
		Align(lipgloss.Right)

	s.StatLabel = lipgloss.NewStyle().
		Foreground(p.Text).
		PaddingLeft(2)

	s.StatCard = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(p.Overlay).
		Padding(1, 2).
		MarginBottom(1)

	s.MenuItem = lipgloss.NewStyle().
		Foreground(p.Text).
		PaddingLeft(2)

	s.MenuSelected = lipgloss.NewStyle().
		Foreground(p.Primary).
		Bold(true).
		PaddingLeft(1)

	s.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(p.Secondary).
		MarginBottom(1)

	s.ListItem = lipgloss.NewStyle().
		Foreground(p.Text).
		PaddingLeft(2)

	s.ListSelected = lipgloss.NewStyle().
		Foreground(p.Primary).
		Bold(true).
		PaddingLeft(1)

	s.TypeBadge = lipgloss.NewStyle().
		Foreground(p.Accent).
		Bold(true)

	s.StateWarningBadge = lipgloss.NewStyle().
		Foreground(p.Warning).
		Bold(true)

	s.StaleBadge = lipgloss.NewStyle().
		Foreground(p.Warning).
		Bold(true)

	s.AttachedBadge = lipgloss.NewStyle().
		Foreground(p.Success).
		Bold(true)

	s.ID = lipgloss.NewStyle().
		Foreground(p.Info)

	s.Timestamp = lipgloss.NewStyle().
		Foreground(p.Subtext).
		Italic(true)

	s.Project = lipgloss.NewStyle().
		Foreground(p.Warning)

	s.ContentPreview = lipgloss.NewStyle().
		Foreground(p.Subtext).
		PaddingLeft(4)

	s.SectionHeading = lipgloss.NewStyle().
		Bold(true).
		Foreground(p.Secondary).
		MarginTop(1).
		MarginBottom(1)

	s.DetailContent = lipgloss.NewStyle().
		Foreground(p.Text).
		PaddingLeft(2)

	s.DetailLabel = lipgloss.NewStyle().
		Foreground(p.Subtext).
		Width(14).
		Align(lipgloss.Right).
		PaddingRight(1)

	s.DetailValue = lipgloss.NewStyle().
		Foreground(p.Text)

	s.TimelineFocus = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(p.Primary).
		Padding(0, 1)

	s.TimelineItem = lipgloss.NewStyle().
		Foreground(p.Subtext).
		PaddingLeft(2)

	s.TimelineConnector = lipgloss.NewStyle().
		Foreground(p.Overlay)

	s.SearchInput = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(p.Primary).
		Foreground(p.Text).
		Padding(0, 1).
		MarginBottom(1)

	s.SearchHighlight = lipgloss.NewStyle().
		Foreground(p.Highlight).
		Bold(true)

	s.NoResults = lipgloss.NewStyle().
		Foreground(p.Subtext).
		Italic(true).
		PaddingLeft(2).
		MarginTop(1)

	s.LogoFrame = lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(p.Overlay).
		Padding(0, 1).
		MarginBottom(1)

	s.LogoAccent = lipgloss.NewStyle().
		Foreground(p.Primary).
		Bold(true)

	s.LogoTagline = lipgloss.NewStyle().
		Foreground(p.Subtext).
		Italic(true)

	for i, c := range p.LogoGradient {
		s.LogoGradient[i] = lipgloss.NewStyle().Foreground(c).Bold(true)
	}

	s.Spinner = lipgloss.NewStyle().
		Foreground(p.Primary)

	s.Emphasis = lipgloss.NewStyle().
		Bold(true).
		Foreground(p.Primary)

	s.SuccessInline = lipgloss.NewStyle().
		Bold(true).
		Foreground(p.Success)

	s.DangerInline = lipgloss.NewStyle().
		Bold(true).
		Foreground(p.Danger)

	return s
}

// Default is the style set the TUI uses when no theme has been selected —
// CatppuccinMocha, per ADR-028 §5 and rfc-tui.md §8.2's precedence chain
// bottoming out at "catppuccin-mocha". Callers that build a screen without
// going through theme.Resolve (a tab's own package tests, mostly) get this
// same default rather than a second, competing notion of "no theme chosen".
func Default() Styles { return New(CatppuccinMocha()) }
