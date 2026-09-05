package theme

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestElephantPaletteFillsEverySemanticRole(t *testing.T) {
	p := Elephant()

	if p.Name != "elephant" {
		t.Fatalf("Name = %q, want %q", p.Name, "elephant")
	}

	roles := map[string]lipgloss.Color{
		"Base":      p.Base,
		"Surface":   p.Surface,
		"Overlay":   p.Overlay,
		"Text":      p.Text,
		"Subtext":   p.Subtext,
		"Primary":   p.Primary,
		"Accent":    p.Accent,
		"Highlight": p.Highlight,
		"Success":   p.Success,
		"Warning":   p.Warning,
		"Danger":    p.Danger,
		"Info":      p.Info,
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
}

func TestForegroundAndBackgroundStayReadable(t *testing.T) {
	p := Elephant()

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

func TestDefaultIsTheElephantPalette(t *testing.T) {
	if got := Default().Palette.Name; got != Elephant().Name {
		t.Fatalf("Default palette = %q, want %q", got, Elephant().Name)
	}
}
