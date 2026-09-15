package theme

import "github.com/charmbracelet/lipgloss"

// The koi palettes are the workspace's own, composed rather than transcribed
// from somebody else's scheme, and they are held to two rules the three
// inherited palettes were not built for.
//
// The first is legibility with no help from the backgrounds: every one of the
// ten text roles clears 4.5:1 against Base *and* against Surface, and Overlay
// clears 3:1 against Base. That matters because nothing in this workspace
// paints a panel — a selected row is a cursor glyph in Primary, a panel is a
// border in Overlay — so a reader never gets the contrast boost a filled
// background would have given them. Overlay is measured against Base alone
// because Base is the only plane it is ever drawn on: it is a panel border and
// a horizontal rule, and both sit on the terminal's default background.
//
// The second is that the terminal underneath may be translucent. A pane at
// ninety percent opacity is the theme's Base mixed with whatever is behind the
// window, so the plane a role is actually read against is not the hex written
// here. Composite and the contrast tests measure that mixed plane against both
// extremes a desktop can be, and it is that second rule the separators of
// showa and ogon sit a few units above their neutral ramps for: a bright
// desktop behind the window washes a panel border out, and those few units are
// the difference between a findable border and an invisible one. The lift is
// imperceptible next to the ground it is drawn on.
//
// Four palettes, one idea: a koi pond. Orange fish, gold scales, lotus pink,
// water teal, moss green.

// KoiPond is the default: a pond at night, the ground the workspace was
// designed against.
func KoiPond() Palette {
	var (
		base      = lipgloss.Color("#0d1b21")
		surface   = lipgloss.Color("#16272f")
		overlay   = lipgloss.Color("#57808c")
		text      = lipgloss.Color("#e6edef")
		subtext   = lipgloss.Color("#9fb6bd")
		primary   = lipgloss.Color("#ff9e5e")
		secondary = lipgloss.Color("#f4a8c0")
		accent    = lipgloss.Color("#ecc369")
		highlight = lipgloss.Color("#7fe0d4")
		success   = lipgloss.Color("#96cf7f")
		warning   = lipgloss.Color("#e9b949")
		danger    = lipgloss.Color("#f4787f")
		info      = lipgloss.Color("#74bde0")
	)
	return koiPalette("koi-pond", base, surface, overlay, text, subtext,
		primary, secondary, accent, highlight, success, warning, danger, info)
}

// KoiDay is the same pond in daylight: the light variant, for a terminal on a
// pale ground.
func KoiDay() Palette {
	var (
		base      = lipgloss.Color("#f6f3ec")
		surface   = lipgloss.Color("#e6dfd1")
		overlay   = lipgloss.Color("#7d7263")
		text      = lipgloss.Color("#20252a")
		subtext   = lipgloss.Color("#54595d")
		primary   = lipgloss.Color("#9c4413")
		secondary = lipgloss.Color("#8f2f57")
		accent    = lipgloss.Color("#71510f")
		highlight = lipgloss.Color("#0b5f5b")
		success   = lipgloss.Color("#265c2a")
		warning   = lipgloss.Color("#754b00")
		danger    = lipgloss.Color("#a11f18")
		info      = lipgloss.Color("#155273")
	)
	return koiPalette("koi-day", base, surface, overlay, text, subtext,
		primary, secondary, accent, highlight, success, warning, danger, info)
}

// Showa is the red-and-white koi variety: a near-black ground with the orange
// pushed warmer and the secondary bleached towards the white of the fish.
func Showa() Palette {
	var (
		base      = lipgloss.Color("#0f0f11")
		surface   = lipgloss.Color("#1c1c20")
		overlay   = lipgloss.Color("#72727c")
		text      = lipgloss.Color("#f2efe9")
		subtext   = lipgloss.Color("#a8a49c")
		primary   = lipgloss.Color("#ff8552")
		secondary = lipgloss.Color("#f5d6c6")
		accent    = lipgloss.Color("#e6b455")
		highlight = lipgloss.Color("#8ad7c8")
		success   = lipgloss.Color("#9ec97e")
		warning   = lipgloss.Color("#dcb43f")
		danger    = lipgloss.Color("#ff7a86")
		info      = lipgloss.Color("#7cb8dd")
	)
	return koiPalette("showa", base, surface, overlay, text, subtext,
		primary, secondary, accent, highlight, success, warning, danger, info)
}

// Ogon is the metallic gold variety: a brown ground under a palette that
// leans entirely into yellow.
func Ogon() Palette {
	var (
		base      = lipgloss.Color("#151009")
		surface   = lipgloss.Color("#231a10")
		overlay   = lipgloss.Color("#8a7248")
		text      = lipgloss.Color("#f6ead2")
		subtext   = lipgloss.Color("#bda884")
		primary   = lipgloss.Color("#ffc247")
		secondary = lipgloss.Color("#f0d9a8")
		accent    = lipgloss.Color("#e79a3c")
		highlight = lipgloss.Color("#a8d8b0")
		success   = lipgloss.Color("#a5c96b")
		warning   = lipgloss.Color("#f2a93b")
		danger    = lipgloss.Color("#f4756a")
		info      = lipgloss.Color("#8bbfc9")
	)
	return koiPalette("ogon", base, surface, overlay, text, subtext,
		primary, secondary, accent, highlight, success, warning, danger, info)
}

// koiPalette assembles a koi palette and derives its wordmark gradient.
//
// The gradient is not a fourteenth set of colours. It runs the five hues the
// pond is named for, top to bottom, from roles the palette already carries:
// shiro (the white of the fish) is Text, ogon (its gold) is Accent, the orange
// of the koi is Primary, the red of a hi marking is Danger, and the water it
// swims in is Highlight. Deriving it is what keeps a re-tinted palette's logo
// in the same family as the rest of its screen.
func koiPalette(name string,
	base, surface, overlay, text, subtext lipgloss.Color,
	primary, secondary, accent, highlight lipgloss.Color,
	success, warning, danger, info lipgloss.Color,
) Palette {
	return Palette{
		Name:         name,
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
		LogoGradient: [logoRows]lipgloss.Color{text, accent, primary, danger, highlight},
	}
}
