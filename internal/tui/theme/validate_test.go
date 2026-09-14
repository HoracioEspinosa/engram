package theme

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestValidateAcceptsALegiblePalette pins the baseline the table below bends:
// a palette that breaks no rule reports nothing.
func TestValidateAcceptsALegiblePalette(t *testing.T) {
	if problems := legiblePalette().Validate(); len(problems) != 0 {
		t.Fatalf("a palette built to pass reports %v", problems)
	}
}

// TestValidateAcceptsEveryKoiPalette turns the validator on the palettes this
// workspace designed. They are the ones held to the whole rule set — thirteen
// lowercase hex roles, five gradient stops, every text role legible against
// both planes, a separator visible against the ground, and no two roles
// rendering alike — so a problem reported here is a palette to fix rather than
// a rule to loosen.
//
// The three inherited palettes are deliberately not in this list: they carry
// declared contrast debt (knownContrastDebt) and would fail a check that has
// no notion of debt.
func TestValidateAcceptsEveryKoiPalette(t *testing.T) {
	for _, name := range koiPaletteNames {
		t.Run(name, func(t *testing.T) {
			if problems := registry[name]().Validate(); len(problems) != 0 {
				for _, problem := range problems {
					t.Error(problem)
				}
			}
		})
	}
}

// TestValidateReportsOneProblemPerRule walks the rules one at a time. Each
// case bends exactly one thing about a palette that otherwise passes, so the
// problem it reports can only have come from the bend.
func TestValidateReportsOneProblemPerRule(t *testing.T) {
	cases := []struct {
		name string
		bend func(*Palette)
		want string
	}{
		{
			name: "a role with no colour",
			bend: func(p *Palette) { p.Accent = "" },
			want: "role accent has no colour",
		},
		{
			name: "a colour that is not a hex literal",
			bend: func(p *Palette) { p.Warning = "yellow" },
			want: `role warning is "yellow"`,
		},
		{
			name: "a hex literal in uppercase",
			bend: func(p *Palette) { p.Info = "#74BDE0" },
			want: "want a #rrggbb literal in lowercase",
		},
		{
			name: "three-digit shorthand",
			bend: func(p *Palette) { p.Danger = "#f78" },
			want: "role danger",
		},
		{
			name: "a gradient stop that is not a hex literal",
			bend: func(p *Palette) { p.LogoGradient[2] = "orange" },
			want: "logo gradient stop 2",
		},
		{
			name: "a gradient stop left empty",
			bend: func(p *Palette) { p.LogoGradient[4] = "" },
			want: "logo gradient stop 4 has no colour",
		},
		{
			name: "text that cannot be read on the ground",
			bend: func(p *Palette) { p.Subtext = "#0a0a0a" },
			want: "subtext on base",
		},
		{
			name: "text that cannot be read on a panel",
			bend: func(p *Palette) { p.Surface = "#fefefe" },
			want: "on surface",
		},
		{
			name: "a separator that disappears into the ground",
			bend: func(p *Palette) { p.Overlay = "#0b0b0b" },
			want: "overlay on base",
		},
		{
			name: "two roles rendering identically",
			bend: func(p *Palette) { p.Accent = "#96cf7f" },
			want: "roles accent, success all render as #96cf7f",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := legiblePalette()
			tc.bend(&p)
			problems := p.Validate()
			if len(problems) == 0 {
				t.Fatal("the bent palette reported nothing")
			}
			found := false
			for _, problem := range problems {
				if strings.Contains(problem.Error(), tc.want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("problems = %v, want one mentioning %q", problems, tc.want)
			}
		})
	}
}

// TestValidateReportsEveryProblemAtOnce pins the reason Validate returns a
// slice: somebody fixing a hand-written theme should see the whole list rather
// than edit, re-import, and be told the next one.
func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	p := legiblePalette()
	p.Accent = "chartreuse"
	p.Subtext = "#0a0a0a"
	p.LogoGradient[0] = "nope"

	problems := p.Validate()
	if len(problems) < 3 {
		t.Fatalf("problems = %v, want all three reported at once", problems)
	}
}

// TestValidateDoesNotComplainTwiceAboutOneColour pins that a role whose hex
// did not parse is reported once: a second complaint about its contrast says
// nothing a reader can act on.
func TestValidateDoesNotComplainTwiceAboutOneColour(t *testing.T) {
	p := legiblePalette()
	p.Primary = "chartreuse"

	count := 0
	for _, problem := range p.Validate() {
		if strings.Contains(problem.Error(), "primary") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("primary was reported %d times, want once", count)
	}
}

// TestValidateThemeName walks the shape every surface that handles a theme
// agrees on.
func TestValidateThemeName(t *testing.T) {
	cases := []struct {
		name  string
		valid bool
	}{
		{"koi-pond", true},
		{"kanagawa", true},
		{"theme2", true},
		{"a", true},
		{"", false},
		{"   ", false},
		{"Koi-Pond", false},
		{"koi pond", false},
		{"koi_pond", false},
		{"-koi", false},
		{"koi-", false},
		{"koi--pond", false},
		{"koi.pond", false},
		{strings.Repeat("a", maxThemeNameLength), true},
		{strings.Repeat("a", maxThemeNameLength+1), false},
	}

	for _, tc := range cases {
		err := ValidateThemeName(tc.name)
		if tc.valid && err != nil {
			t.Errorf("ValidateThemeName(%q) = %v, want it accepted", tc.name, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("ValidateThemeName(%q) was accepted, want it refused", tc.name)
		}
	}
}

// TestValidateHoldsOverlayToTheNonTextBar pins that the separator answers to
// 3:1 and not to the 4.5:1 of text: holding a border to a text criterion would
// reject palettes whose borders are perfectly visible.
func TestValidateHoldsOverlayToTheNonTextBar(t *testing.T) {
	p := legiblePalette()
	// A grey that clears 3:1 against black but not 4.5:1.
	p.Overlay = "#6a6a6a"

	ratio, err := ContrastRatio(p.Overlay, p.Base)
	if err != nil {
		t.Fatalf("ContrastRatio: %v", err)
	}
	if ratio < MinOverlayContrastRatio || ratio >= MinContrastRatio {
		t.Fatalf("the probe colour is at %.2f:1, which does not sit between the two bars", ratio)
	}
	for _, problem := range p.Validate() {
		if strings.Contains(problem.Error(), "overlay") {
			t.Fatalf("overlay was held to the text bar: %v", problem)
		}
	}
}

// TestValidateReportsAPaletteWithNothingInIt pins the far end: a zero palette
// is every role missing, not a panic.
func TestValidateReportsAPaletteWithNothingInIt(t *testing.T) {
	problems := Palette{}.Validate()
	if len(problems) < len(Roles())+logoRows {
		t.Fatalf("an empty palette reported %d problems, want one per role and per gradient stop", len(problems))
	}
}

// TestValidateIsStableAcrossRuns pins that two runs over one palette report the
// same problems in the same order, so a message a person is reading does not
// reshuffle between edits.
func TestValidateIsStableAcrossRuns(t *testing.T) {
	p := legiblePalette()
	p.Accent = "#96cf7f"
	p.Secondary = lipgloss.Color(string(p.Info))

	first := p.Validate()
	second := p.Validate()
	if len(first) != len(second) {
		t.Fatalf("two runs reported %d and %d problems", len(first), len(second))
	}
	for i := range first {
		if first[i].Error() != second[i].Error() {
			t.Fatalf("problem %d differs between runs:\n%v\n%v", i, first[i], second[i])
		}
	}
}
