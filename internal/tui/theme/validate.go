package theme

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// MinOverlayContrastRatio is the bar the separator colour has to clear against
// the ground: WCAG 2.1 Success Criterion 1.4.11 ("Non-text Contrast"), level
// AA — 3:1. Overlay is never painted as text, so holding it to the 4.5:1 of
// 1.4.3 would be the wrong criterion; letting it sit at whatever it likes
// would leave panel borders invisible.
// https://www.w3.org/WAI/WCAG21/Understanding/non-text-contrast.html
const MinOverlayContrastRatio = 3.0

// maxThemeNameLength mirrors the column the name is stored in.
const maxThemeNameLength = 32

// themeNamePattern is the kebab-case shape a theme name takes. It is a file
// name on export, an argument on the command line and a key in settings, and
// all three want the same thing.
var themeNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// hexPattern is the only colour literal a theme document may carry: six
// lowercase hex digits. Three-digit shorthand and uppercase are refused rather
// than normalised, so two documents that render identically cannot compare
// unequal — which is what makes "no two roles share a colour" checkable at all.
var hexPattern = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// ValidateThemeName reports whether a name is the kebab-case shape every
// surface that handles a theme agrees on.
func ValidateThemeName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errors.New("theme: a theme needs a name")
	}
	if len(trimmed) > maxThemeNameLength || !themeNamePattern.MatchString(trimmed) {
		return fmt.Errorf("theme: %q is not a theme name (lowercase letters, digits and single hyphens, up to %d characters)",
			name, maxThemeNameLength)
	}
	return nil
}

// Validate reports everything wrong with a palette, rather than the first
// thing.
//
// A person fixing a hand-written theme wants the whole list: told one problem
// at a time they edit, re-import, and are told the next one. The order is
// fixed — roles, then shape, then legibility, then duplication — so two runs
// over the same document read the same way.
//
// Nothing here is fatal on its own. A palette that fails is still a palette,
// and the caller decides whether to refuse it, keep it with a warning, or draw
// it in Danger in a picker.
func (p Palette) Validate() []error {
	var problems []error

	fields := paletteFields(&p)
	values := map[string]string{}
	for _, role := range Roles() {
		// The literal is checked exactly as the palette carries it. Lowercasing
		// first would accept a spelling the document format does not have, and
		// the importer is where a hand-written "#FFAA00" is normalised.
		value := strings.TrimSpace(string(*fields[role]))
		switch {
		case value == "":
			problems = append(problems, fmt.Errorf("role %s has no colour", role))
		case !hexPattern.MatchString(value):
			problems = append(problems, fmt.Errorf("role %s is %q, want a #rrggbb literal in lowercase", role, value))
		default:
			values[role] = value
		}
	}

	for i, stop := range p.LogoGradient {
		value := strings.TrimSpace(string(stop))
		if value == "" {
			problems = append(problems, fmt.Errorf("logo gradient stop %d has no colour", i))
			continue
		}
		if !hexPattern.MatchString(value) {
			problems = append(problems, fmt.Errorf("logo gradient stop %d is %q, want a #rrggbb literal in lowercase", i, value))
		}
	}

	problems = append(problems, p.legibilityProblems(values)...)
	problems = append(problems, duplicateRoleProblems(values)...)
	return problems
}

// legibilityProblems checks the ten text roles against both planes and the
// separator against the ground. A role whose hex did not parse is skipped: it
// was already reported, and a second complaint about the same character says
// nothing new.
func (p Palette) legibilityProblems(values map[string]string) []error {
	base, hasBase := values[RoleBase]
	surface, hasSurface := values[RoleSurface]

	var problems []error
	planes := []struct {
		name  string
		hex   string
		known bool
	}{
		{RoleBase, base, hasBase},
		{RoleSurface, surface, hasSurface},
	}

	for _, role := range TextRoles() {
		hex, ok := values[role]
		if !ok {
			continue
		}
		for _, plane := range planes {
			if !plane.known {
				continue
			}
			ratio, err := ContrastRatio(lipgloss.Color(hex), lipgloss.Color(plane.hex))
			if err != nil {
				continue
			}
			if ratio < MinContrastRatio {
				problems = append(problems, fmt.Errorf("%s on %s: contrast %.2f:1, want >= %.1f:1",
					role, plane.name, ratio, MinContrastRatio))
			}
		}
	}

	if overlay, ok := values[RoleOverlay]; ok && hasBase {
		ratio, err := ContrastRatio(lipgloss.Color(overlay), lipgloss.Color(base))
		if err == nil && ratio < MinOverlayContrastRatio {
			problems = append(problems, fmt.Errorf("%s on %s: contrast %.2f:1, want >= %.1f:1",
				RoleOverlay, RoleBase, ratio, MinOverlayContrastRatio))
		}
	}
	return problems
}

// duplicateRoleProblems reports two roles rendering identically. A section
// title the same colour as an error message is not a variant, it is a palette
// that cannot say which of the two a reader is looking at.
func duplicateRoleProblems(values map[string]string) []error {
	byColour := map[string][]string{}
	for role, hex := range values {
		byColour[hex] = append(byColour[hex], role)
	}

	hexes := make([]string, 0, len(byColour))
	for hex, roles := range byColour {
		if len(roles) > 1 {
			hexes = append(hexes, hex)
		}
	}
	sort.Strings(hexes)

	problems := make([]error, 0, len(hexes))
	for _, hex := range hexes {
		roles := byColour[hex]
		sort.Strings(roles)
		problems = append(problems, fmt.Errorf("roles %s all render as %s", strings.Join(roles, ", "), hex))
	}
	return problems
}
