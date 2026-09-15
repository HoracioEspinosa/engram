package theme

import "testing"

// specTokenHex is the palette table every registered theme is specified
// from, transcribed verbatim: one hex literal per semantic token, per
// palette. The four koi rows come from the workspace's own palette design,
// the three below them from the colour schemes engram adopts as-is.
//
// It exists only so TestPaletteFieldsMatchSpecTokens can compare a Palette
// field against the exact value the specification assigns its token, instead
// of against another field of the same struct — a check derived from the
// struct under test can never catch a field holding the wrong token's colour,
// only two fields holding different colours from each other.
//
// Two of those koi values are the design's, not the table's: showa's and
// ogon's overlays sit a few units above the neutral ramp the palette table
// names, because a panel border has to clear 3:1 against Base once a
// translucent terminal composites it over a bright desktop
// (TestKoiPalettesStayLegibleOverTranslucentBackgrounds measures exactly
// that). Every other value is the table's, verbatim.
var specTokenHex = map[string]struct {
	base, surface, overlay, text, subtext              string
	primary, secondary, success, warning, danger, info string
	accent, highlight                                  string
}{
	"koi-pond": {
		base: "#0d1b21", surface: "#16272f", overlay: "#57808c", text: "#e6edef", subtext: "#9fb6bd",
		primary: "#ff9e5e", secondary: "#f4a8c0", success: "#96cf7f", warning: "#e9b949", danger: "#f4787f", info: "#74bde0",
		accent: "#ecc369", highlight: "#7fe0d4",
	},
	"koi-day": {
		base: "#f6f3ec", surface: "#e6dfd1", overlay: "#7d7263", text: "#20252a", subtext: "#54595d",
		primary: "#9c4413", secondary: "#8f2f57", success: "#265c2a", warning: "#754b00", danger: "#a11f18", info: "#155273",
		accent: "#71510f", highlight: "#0b5f5b",
	},
	"showa": {
		base: "#0f0f11", surface: "#1c1c20", overlay: "#72727c", text: "#f2efe9", subtext: "#a8a49c",
		primary: "#ff8552", secondary: "#f5d6c6", success: "#9ec97e", warning: "#dcb43f", danger: "#ff7a86", info: "#7cb8dd",
		accent: "#e6b455", highlight: "#8ad7c8",
	},
	"ogon": {
		base: "#151009", surface: "#231a10", overlay: "#8a7248", text: "#f6ead2", subtext: "#bda884",
		primary: "#ffc247", secondary: "#f0d9a8", success: "#a5c96b", warning: "#f2a93b", danger: "#f4756a", info: "#8bbfc9",
		accent: "#e79a3c", highlight: "#a8d8b0",
	},
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

// TestPaletteFieldsMatchSpecTokens ties every Palette field to the literal
// hex value its specification assigns that token, per registered palette. It
// exists to catch a rotation: a palette whose field names no longer line up
// with the spec's tokens (e.g. the token "secondary" living under a field
// called Accent) trips neither the collision test
// (TestNoTwoDistinctRolesShareAColour) nor the contrast test
// (TestRegisteredPalettesAreLegible), because both compare fields to each
// other and a rotation moves whole values, contrast and all — it never
// produces a duplicate or an illegible pair. Only a check against the spec's
// own literal values, independent of this struct, can catch that.
func TestPaletteFieldsMatchSpecTokens(t *testing.T) {
	for _, name := range paletteNames() {
		t.Run(name, func(t *testing.T) {
			want, ok := specTokenHex[name]
			if !ok {
				t.Fatalf("specTokenHex has no entry for registered palette %q", name)
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
					t.Errorf("token %q: Palette field holds %s, the specification fixes it at %s", tc.token, tc.got, tc.want)
				}
			}
		})
	}
}
