package theme

import (
	"fmt"
	"math"
	"strconv"

	"github.com/charmbracelet/lipgloss"
)

// MinContrastRatio is the numeric bar a foreground/background pair must clear
// to count as legible: WCAG 2.1 Success Criterion 1.4.3 ("Contrast
// (Minimum)"), level AA, for normal-weight text — 4.5:1. The TUI never
// declares a "large text" exemption (WCAG's relaxed 3:1 tier for text at
// 18pt+/14pt+bold, which a terminal cell grid has no equivalent scale for),
// so every semantic foreground role is held to the stricter ratio uniformly.
// https://www.w3.org/WAI/WCAG21/Understanding/contrast-minimum.html
const MinContrastRatio = 4.5

// ContrastRatio computes the WCAG relative-luminance contrast ratio between
// fg and bg, in the range [1, 21]. Both colours must be "#rrggbb" hex
// literals — every Palette field is one, since the three registered
// palettes are truecolor — anything else is an error rather than a silent
// guess.
func ContrastRatio(fg, bg lipgloss.Color) (float64, error) {
	lf, err := relativeLuminance(fg)
	if err != nil {
		return 0, fmt.Errorf("foreground: %w", err)
	}
	lb, err := relativeLuminance(bg)
	if err != nil {
		return 0, fmt.Errorf("background: %w", err)
	}

	lighter, darker := lf, lb
	if darker > lighter {
		lighter, darker = darker, lighter
	}
	return (lighter + 0.05) / (darker + 0.05), nil
}

// relativeLuminance implements WCAG 2.1's formula for sRGB relative
// luminance: https://www.w3.org/WAI/WCAG21/Understanding/contrast-minimum.html
func relativeLuminance(c lipgloss.Color) (float64, error) {
	r, g, b, err := hexToRGB(string(c))
	if err != nil {
		return 0, err
	}
	rl, gl, bl := srgbToLinear(r), srgbToLinear(g), srgbToLinear(b)
	return 0.2126*rl + 0.7152*gl + 0.0722*bl, nil
}

func srgbToLinear(channel float64) float64 {
	if channel <= 0.03928 {
		return channel / 12.92
	}
	return math.Pow((channel+0.055)/1.055, 2.4)
}

// hexToRGB parses a "#rrggbb" literal into three [0, 1] channel values.
func hexToRGB(hex string) (r, g, b float64, err error) {
	if len(hex) != 7 || hex[0] != '#' {
		return 0, 0, 0, fmt.Errorf("%q is not a #rrggbb hex colour", hex)
	}
	ri, err := strconv.ParseUint(hex[1:3], 16, 8)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("%q: invalid red channel: %w", hex, err)
	}
	gi, err := strconv.ParseUint(hex[3:5], 16, 8)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("%q: invalid green channel: %w", hex, err)
	}
	bi, err := strconv.ParseUint(hex[5:7], 16, 8)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("%q: invalid blue channel: %w", hex, err)
	}
	return float64(ri) / 255, float64(gi) / 255, float64(bi) / 255, nil
}
