package theme

import (
	"sort"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// paletteNames lists the registry in a fixed order so table-driven tests
// produce stable subtest names regardless of Go's randomised map iteration.
func paletteNames() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestEveryPaletteFillsEverySemanticRole(t *testing.T) {
	for _, name := range paletteNames() {
		t.Run(name, func(t *testing.T) {
			p := registry[name]()

			if p.Name != name {
				t.Fatalf("Name = %q, want %q", p.Name, name)
			}

			roles := map[string]lipgloss.Color{
				"Base":            p.Base,
				"Surface":         p.Surface,
				"Overlay":         p.Overlay,
				"Text":            p.Text,
				"Subtext":         p.Subtext,
				"Primary":         p.Primary,
				"Accent":          p.Accent,
				"Highlight":       p.Highlight,
				"SearchHighlight": p.SearchHighlight,
				"Success":         p.Success,
				"Warning":         p.Warning,
				"Danger":          p.Danger,
				"Info":            p.Info,
			}
			for role, color := range roles {
				if color == "" {
					t.Errorf("role %s has no colour", role)
				}
			}

			for i, stop := range p.LogoGradient {
				if stop == "" {
					t.Errorf("logo gradient stop %d has no colour", i)
				}
			}
		})
	}
}

func TestForegroundAndBackgroundStayReadable(t *testing.T) {
	for _, name := range paletteNames() {
		t.Run(name, func(t *testing.T) {
			p := registry[name]()

			if p.Text == p.Base {
				t.Fatal("Text and Base are the same colour: body copy would be invisible")
			}
			if p.Subtext == p.Base {
				t.Fatal("Subtext and Base are the same colour: hints would be invisible")
			}
			if p.Primary == p.Text {
				t.Fatal("Primary and Text are the same colour: the cursor would not stand out")
			}
			if p.Danger == p.Success {
				t.Fatal("Danger and Success are the same colour: state would be unreadable")
			}
		})
	}
}

// distinctRoles are the "brand and state" roles rfc-tui.md §8.1 assigns one
// meaning each. SearchHighlight is deliberately excluded: it is allowed to
// coincide with Success (elephant does, by design — see
// Palette.SearchHighlight's doc comment). LogoGradient reuses these colours
// on purpose and is not part of this check either.
var distinctRoles = []string{"Primary", "Accent", "Highlight", "Success", "Warning", "Danger", "Info"}

// TestNoTwoDistinctRolesShareAColour catches a palette where two roles with
// different meanings render identically — e.g. a section title the same
// colour as an error message. This failed for real against this repo's own
// code before this change: CatppuccinMocha() had Accent and Danger both at
// "#f38ba8", and Highlight and Warning both at "#f9e2af" (titles
// indistinguishable from errors, and type badges from stale warnings). See
// this task's report for the exact `go test` output that reproduced it.
func TestNoTwoDistinctRolesShareAColour(t *testing.T) {
	for _, name := range paletteNames() {
		t.Run(name, func(t *testing.T) {
			p := registry[name]()
			colors := map[string]lipgloss.Color{
				"Primary":   p.Primary,
				"Accent":    p.Accent,
				"Highlight": p.Highlight,
				"Success":   p.Success,
				"Warning":   p.Warning,
				"Danger":    p.Danger,
				"Info":      p.Info,
			}

			byColor := map[lipgloss.Color][]string{}
			for _, role := range distinctRoles {
				byColor[colors[role]] = append(byColor[colors[role]], role)
			}
			for color, roles := range byColor {
				if len(roles) > 1 {
					sort.Strings(roles)
					t.Errorf("colour %s is shared by %v — distinct semantic roles must render distinctly", color, roles)
				}
			}
		})
	}
}

func TestNewBindsEveryStyleToThePalette(t *testing.T) {
	p := Elephant()
	s := New(p)

	if s.Palette.Name != p.Name {
		t.Fatalf("Palette.Name = %q, want %q", s.Palette.Name, p.Name)
	}

	cases := []struct {
		name  string
		style lipgloss.Style
		want  lipgloss.Color
	}{
		{"App", s.App, p.Text},
		{"Header", s.Header, p.Primary},
		{"Help", s.Help, p.Subtext},
		{"Error", s.Error, p.Danger},
		{"Notice", s.Notice, p.Success},
		{"UpdateBanner", s.UpdateBanner, p.Warning},
		{"MenuSelected", s.MenuSelected, p.Primary},
		{"Title", s.Title, p.Accent},
		{"TypeBadge", s.TypeBadge, p.Highlight},
		{"SearchHighlight", s.SearchHighlight, p.SearchHighlight},
		{"ID", s.ID, p.Info},
		{"Project", s.Project, p.Warning},
		{"TimelineConnector", s.TimelineConnector, p.Overlay},
		{"Spinner", s.Spinner, p.Primary},
		{"SuccessInline", s.SuccessInline, p.Success},
		{"DangerInline", s.DangerInline, p.Danger},
	}
	for _, tc := range cases {
		if got := tc.style.GetForeground(); got != tc.want {
			t.Errorf("%s foreground = %v, want %v", tc.name, got, tc.want)
		}
	}

	for i, stop := range s.LogoGradient {
		if got := stop.GetForeground(); got != p.LogoGradient[i] {
			t.Errorf("logo gradient style %d = %v, want %v", i, got, p.LogoGradient[i])
		}
	}
}

func TestStylesAreValuesNotSharedState(t *testing.T) {
	s := Default()

	widened := s.DetailContent.Width(40)
	if s.DetailContent.GetWidth() == widened.GetWidth() {
		t.Fatal("deriving a style mutated the shared style set")
	}
}

// TestDefaultIsTheCatppuccinMochaPalette pins ADR-028 §5 and rfc-tui.md
// §8.2's decision: catppuccin-mocha is the out-of-the-box theme, not
// elephant (the palette that shipped before theming existed).
func TestDefaultIsTheCatppuccinMochaPalette(t *testing.T) {
	if got := Default().Palette.Name; got != CatppuccinMocha().Name {
		t.Fatalf("Default palette = %q, want %q", got, CatppuccinMocha().Name)
	}
	if got := Default().Palette.Name; got != DefaultThemeName {
		t.Fatalf("Default palette = %q, want DefaultThemeName %q", got, DefaultThemeName)
	}
}
