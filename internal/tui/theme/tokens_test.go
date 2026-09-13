package theme

import "testing"

// rfcTokenHex is rfc-tui.md §8.1's table, transcribed verbatim: one hex
// literal per semantic token, per registered palette. It exists only so
// TestPaletteFieldsMatchRFCTokens can compare a Palette field against the
// exact value the RFC assigns its token, instead of against another field of
// the same struct — a check derived from the struct under test can never
// catch a field holding the wrong token's colour, only two fields holding
// different colours from each other.
var rfcTokenHex = map[string]struct {
	base, surface, overlay, text, subtext              string
	primary, secondary, success, warning, danger, info string
	accent, highlight                                  string
}{
	"catppuccin-mocha": {
		base: "#1e1e2e", surface: "#313244", overlay: "#6c7086", text: "#cdd6f4", subtext: "#a6adc8",
		primary: "#b4befe", secondary: "#cba6f7", success: "#a6e3a1", warning: "#f9e2af", danger: "#f38ba8", info: "#89b4fa",
		accent: "#fab387", highlight: "#94e2d5",
	},
	"kanagawa": {
		base: "#1f1f28", surface: "#2a2a37", overlay: "#54546d", text: "#dcd7ba", subtext: "#727169",
		primary: "#7e9cd8", secondary: "#957fb8", success: "#98bb6c", warning: "#e6c384", danger: "#e82424", info: "#7fb4ca",
		accent: "#ffa066", highlight: "#7aa89f",
	},
	"elephant": {
		base: "#191724", surface: "#1f1d2e", overlay: "#6e6a86", text: "#e0def4", subtext: "#908caa",
		primary: "#c4a7e7", secondary: "#ebbcba", success: "#9ccfd8", warning: "#f1ca93", danger: "#eb6f92", info: "#31748f",
		accent: "#f6c177", highlight: "#9ccfd8",
	},
}

// TestPaletteFieldsMatchRFCTokens ties every Palette field to the literal
// hex value rfc-tui.md §8.1 assigns its token, per registered palette. It
// exists to catch a rotation: a palette whose field names no longer line up
// with the RFC's tokens (e.g. the token "secondary" living under a field
// called Accent) trips neither the collision test
// (TestNoTwoDistinctRolesShareAColour) nor the contrast test
// (TestRegisteredPalettesAreLegible), because both compare fields to each
// other and a rotation moves whole values, contrast and all — it never
// produces a duplicate or an illegible pair. Only a check against the RFC's
// own literal values, independent of this struct, can catch that.
func TestPaletteFieldsMatchRFCTokens(t *testing.T) {
	for _, name := range paletteNames() {
		t.Run(name, func(t *testing.T) {
			want, ok := rfcTokenHex[name]
			if !ok {
				t.Fatalf("rfcTokenHex has no entry for registered palette %q", name)
			}
			p := registry[name]()

			cases := []struct {
				token string
				got   string
				want  string
			}{
				{"base", string(p.Base), want.base},
				{"surface", string(p.Surface), want.surface},
				{"overlay", string(p.Overlay), want.overlay},
				{"text", string(p.Text), want.text},
				{"subtext", string(p.Subtext), want.subtext},
				{"primary", string(p.Primary), want.primary},
				{"secondary", string(p.Secondary), want.secondary},
				{"success", string(p.Success), want.success},
				{"warning", string(p.Warning), want.warning},
				{"danger", string(p.Danger), want.danger},
				{"info", string(p.Info), want.info},
				{"accent", string(p.Accent), want.accent},
				{"highlight", string(p.Highlight), want.highlight},
			}
			for _, tc := range cases {
				if tc.got != tc.want {
					t.Errorf("token %q: Palette field holds %s, rfc-tui.md §8.1 fixes it at %s", tc.token, tc.got, tc.want)
				}
			}
		})
	}
}
