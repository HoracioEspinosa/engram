package theme

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// themeDocument is what a theme looks like on disk and in the themes table: a
// name, the variant it was built for, the thirteen roles by name, and the five
// gradient stops of the wordmark.
//
// The roles are a map rather than thirteen fields so a document missing one
// says which one, instead of decoding into a zero value that renders as an
// invisible colour. Validation is what turns that map back into a contract.
type themeDocument struct {
	Name         string            `json:"name"`
	Variant      string            `json:"variant"`
	Palette      map[string]string `json:"palette"`
	LogoGradient []string          `json:"logo_gradient"`
}

// The role names a theme document spells. They are the serialised form of the
// Palette fields, kept lowercase because they are also the keys somebody edits
// by hand with json_set.
const (
	RoleBase      = "base"
	RoleSurface   = "surface"
	RoleOverlay   = "overlay"
	RoleText      = "text"
	RoleSubtext   = "subtext"
	RolePrimary   = "primary"
	RoleSecondary = "secondary"
	RoleAccent    = "accent"
	RoleHighlight = "highlight"
	RoleSuccess   = "success"
	RoleWarning   = "warning"
	RoleDanger    = "danger"
	RoleInfo      = "info"
)

// Roles returns the thirteen role names a palette answers to, in the order a
// document writes them: the two planes, the separator, the two copy weights,
// the four brand roles, then the four state roles.
func Roles() []string {
	return []string{
		RoleBase, RoleSurface, RoleOverlay,
		RoleText, RoleSubtext,
		RolePrimary, RoleSecondary, RoleAccent, RoleHighlight,
		RoleSuccess, RoleWarning, RoleDanger, RoleInfo,
	}
}

// TextRoles returns the ten roles that are painted as text. They are the ones
// held to the legibility bar against both background planes; the planes
// themselves and the separator are not text and answer to other rules.
func TextRoles() []string {
	return []string{
		RoleText, RoleSubtext,
		RolePrimary, RoleSecondary, RoleAccent, RoleHighlight,
		RoleSuccess, RoleWarning, RoleDanger, RoleInfo,
	}
}

// paletteFields maps each role onto the field of a Palette that holds it. One
// table serves both directions, so a role can never be readable and not
// writable.
func paletteFields(p *Palette) map[string]*lipgloss.Color {
	return map[string]*lipgloss.Color{
		RoleBase:      &p.Base,
		RoleSurface:   &p.Surface,
		RoleOverlay:   &p.Overlay,
		RoleText:      &p.Text,
		RoleSubtext:   &p.Subtext,
		RolePrimary:   &p.Primary,
		RoleSecondary: &p.Secondary,
		RoleAccent:    &p.Accent,
		RoleHighlight: &p.Highlight,
		RoleSuccess:   &p.Success,
		RoleWarning:   &p.Warning,
		RoleDanger:    &p.Danger,
		RoleInfo:      &p.Info,
	}
}

// Role returns the colour a palette holds for one of the thirteen role names,
// and whether that name is a role at all.
//
// It is the read half of the same table the document is built from, so a
// caller that has to walk the roles — a picker, a contrast report — never
// spells the thirteen fields a second time.
func (p Palette) Role(name string) (lipgloss.Color, bool) {
	fields := paletteFields(&p)
	colour, ok := fields[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return "", false
	}
	return *colour, true
}

// BuiltinTheme is a palette the binary ships, with the variant it was built
// for. It is what a caller seeds the themes table from.
type BuiltinTheme struct {
	Name    string
	Variant string
	Palette Palette
}

// builtinVariants records which variant each registered palette was built for.
// A palette that is not listed is dark: all but one of the palettes the binary
// ships are, and a light one that forgot to say so would open a light terminal
// on a dark theme, which is the failure worth defaulting away from.
var builtinVariants = map[string]string{
	"koi-day": ThemeVariantLight,
}

// The two variants a theme document may declare. They are the same two strings
// the themes table's CHECK constraint admits.
const (
	ThemeVariantDark  = "dark"
	ThemeVariantLight = "light"
)

// Builtins returns every registered palette, by name, so a caller can seed all
// of them without knowing which ones exist.
func Builtins() []BuiltinTheme {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]BuiltinTheme, 0, len(names))
	for _, name := range names {
		variant := builtinVariants[name]
		if variant == "" {
			variant = ThemeVariantDark
		}
		out = append(out, BuiltinTheme{Name: name, Variant: variant, Palette: registry[name]()})
	}
	return out
}

// Builtin returns one registered palette by name.
func Builtin(name string) (BuiltinTheme, bool) {
	// The name is folded once and then used for both lookups. Looking the
	// registry up folded and the variant up raw is how a palette answers to
	// "KOI-DAY" and comes back claiming to be dark.
	name = strings.ToLower(strings.TrimSpace(name))
	ctor, ok := registry[name]
	if !ok {
		return BuiltinTheme{}, false
	}
	variant := builtinVariants[name]
	if variant == "" {
		variant = ThemeVariantDark
	}
	return BuiltinTheme{Name: name, Variant: variant, Palette: ctor()}, true
}

// MarshalTheme renders a palette as the theme document, indented, so a file
// somebody is meant to edit reads as one.
//
// The name on the document wins over the one the palette carries: a palette
// exported under a new name is that theme, and leaving the old name inside the
// file is how two themes end up claiming to be the same one.
func MarshalTheme(name, variant string, p Palette) ([]byte, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(p.Name))
	}
	if err := ValidateThemeName(name); err != nil {
		return nil, err
	}
	variant = strings.ToLower(strings.TrimSpace(variant))
	if variant == "" {
		variant = ThemeVariantDark
	}
	if variant != ThemeVariantDark && variant != ThemeVariantLight {
		return nil, fmt.Errorf("theme: variant %q is neither dark nor light", variant)
	}

	fields := paletteFields(&p)
	doc := themeDocument{
		Name:         name,
		Variant:      variant,
		Palette:      make(map[string]string, len(fields)),
		LogoGradient: make([]string, 0, logoRows),
	}
	for _, role := range Roles() {
		doc.Palette[role] = strings.ToLower(string(*fields[role]))
	}
	for _, stop := range p.LogoGradient {
		doc.LogoGradient = append(doc.LogoGradient, strings.ToLower(string(stop)))
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("theme: encode %s: %w", name, err)
	}
	return append(out, '\n'), nil
}

// UnmarshalTheme reads a theme document back into a palette.
//
// It checks the document's shape — the name, the variant, all thirteen roles,
// five gradient stops — and not what the colours look like. Whether a palette
// is legible is Palette.Validate's question, and keeping the two apart is what
// lets a caller import a palette it knows is imperfect with --force while
// still refusing a file that is not a theme at all.
func UnmarshalTheme(data []byte) (string, string, Palette, error) {
	var doc themeDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", "", Palette{}, fmt.Errorf("theme: parse document: %w", err)
	}

	name := strings.ToLower(strings.TrimSpace(doc.Name))
	if err := ValidateThemeName(name); err != nil {
		return "", "", Palette{}, err
	}
	variant := strings.ToLower(strings.TrimSpace(doc.Variant))
	if variant == "" {
		variant = ThemeVariantDark
	}
	if variant != ThemeVariantDark && variant != ThemeVariantLight {
		return "", "", Palette{}, fmt.Errorf("theme: variant %q is neither dark nor light", variant)
	}
	if len(doc.LogoGradient) != logoRows {
		return "", "", Palette{}, fmt.Errorf("theme: %s has %d gradient stop(s), want %d",
			name, len(doc.LogoGradient), logoRows)
	}

	palette := Palette{Name: name}
	fields := paletteFields(&palette)
	var missing []string
	for _, role := range Roles() {
		value, ok := doc.Palette[role]
		if !ok || strings.TrimSpace(value) == "" {
			missing = append(missing, role)
			continue
		}
		*fields[role] = lipgloss.Color(strings.ToLower(strings.TrimSpace(value)))
	}
	if len(missing) > 0 {
		return "", "", Palette{}, fmt.Errorf("theme: %s is missing the role(s) %s",
			name, strings.Join(missing, ", "))
	}
	for i, stop := range doc.LogoGradient {
		palette.LogoGradient[i] = lipgloss.Color(strings.ToLower(strings.TrimSpace(stop)))
	}
	return name, variant, palette, nil
}
