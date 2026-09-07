// Package theme turns a colour palette into the concrete lipgloss styles the
// TUI renders with.
//
// Nothing outside this package names a hex value. Views ask for a semantic
// role — Primary, Danger, Subtext — so swapping a palette re-skins the whole
// workspace without touching a single view.
package theme

import "github.com/charmbracelet/lipgloss"

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
	// Accent titles sections and headings.
	Accent lipgloss.Color
	// Highlight tags classifications such as an observation type badge.
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

// Elephant is the palette engram has shipped since the TUI existed. It stays
// the default so an upgrade never surprises anyone with new colours.
func Elephant() Palette {
	var (
		base      = lipgloss.Color("#191724")
		surface   = lipgloss.Color("#1f1d2e")
		overlay   = lipgloss.Color("#6e6a86")
		text      = lipgloss.Color("#e0def4")
		subtext   = lipgloss.Color("#908caa")
		primary   = lipgloss.Color("#c4a7e7")
		accent    = lipgloss.Color("#ebbcba")
		highlight = lipgloss.Color("#f6c177")
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
		Accent:       accent,
		Highlight:    highlight,
		Success:      success,
		Warning:      warning,
		Danger:       danger,
		Info:         info,
		LogoGradient: [logoRows]lipgloss.Color{accent, primary, info, success, success},
	}
}

// CatppuccinMocha is the Catppuccin Mocha colour scheme.
func CatppuccinMocha() Palette {
	var (
		base      = lipgloss.Color("#1e1e2e")
		surface   = lipgloss.Color("#313244")
		overlay   = lipgloss.Color("#585b70")
		text      = lipgloss.Color("#cdd6f4")
		subtext   = lipgloss.Color("#a6adc8")
		primary   = lipgloss.Color("#89b4fa")
		accent    = lipgloss.Color("#f38ba8")
		highlight = lipgloss.Color("#f9e2af")
		success   = lipgloss.Color("#a6e3a1")
		warning   = lipgloss.Color("#f9e2af")
		danger    = lipgloss.Color("#f38ba8")
		info      = lipgloss.Color("#89dceb")
	)

	return Palette{
		Name:         "catppuccin-mocha",
		Base:         base,
		Surface:      surface,
		Overlay:      overlay,
		Text:         text,
		Subtext:      subtext,
		Primary:      primary,
		Accent:       accent,
		Highlight:    highlight,
		Success:      success,
		Warning:      warning,
		Danger:       danger,
		Info:         info,
		LogoGradient: [logoRows]lipgloss.Color{accent, primary, info, success, success},
	}
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
		Foreground(p.Accent).
		MarginBottom(1)

	s.ListItem = lipgloss.NewStyle().
		Foreground(p.Text).
		PaddingLeft(2)

	s.ListSelected = lipgloss.NewStyle().
		Foreground(p.Primary).
		Bold(true).
		PaddingLeft(1)

	s.TypeBadge = lipgloss.NewStyle().
		Foreground(p.Highlight).
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
		Foreground(p.Accent).
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
		Foreground(p.Success).
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

// Default is the style set the TUI uses when no theme has been selected.
func Default() Styles { return New(Elephant()) }
