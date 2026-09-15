package app

import (
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/theme"
)

// TestNewAppliesTheResolvedPaletteToEveryTab pins that every tab respects the
// resolved theme: app.New threads the styles it is given down into each tab
// it builds, through their WithStyles, instead of leaving them on the
// theme.Default() their own package-level New() constructors bake in.
//
// Without that thread the root chrome renders the resolved palette while
// every tab underneath still reports theme.Default()'s name.
func TestNewAppliesTheResolvedPaletteToEveryTab(t *testing.T) {
	resolved := theme.New(theme.Kanagawa())
	m := New(nil, nil, nil, nil, nil, "", resolved, "")

	cases := []struct {
		tab  string
		name string
	}{
		{"memory", m.memory.Styles().Palette.Name},
		{"tasks", m.tasks.Styles().Palette.Name},
		{"evidence", m.evidence.Styles().Palette.Name},
		{"runbooks", m.runbooks.Styles().Palette.Name},
		{"settings", m.settings.Styles().Palette.Name},
	}
	for _, tc := range cases {
		if tc.name != "kanagawa" {
			t.Errorf("%s tab styles.Palette.Name = %q, want %q", tc.tab, tc.name, "kanagawa")
		}
	}
}
