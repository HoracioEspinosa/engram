package app

import (
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/theme"
)

// TestNewAppliesTheResolvedPaletteToEveryTab pins rfc-tui.md §8.2's "every
// tab respects the theme": app.New must thread the styles it is given down
// into each of the five tabs, not leave them on the theme.Default() their
// own package-level New() constructors bake in before app.New ever touches
// them.
//
// This failed for real against this repo's code before this change: New
// received a Kanagawa-backed theme.Styles for the root chrome, but every
// tab still reported "catppuccin-mocha" (theme.Default()) because New never
// called each tab's (then-nonexistent) WithStyles. See this task's report
// for the literal `go test` failure this reproduced.
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
