package theme

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestResolvePrecedence pins the precedence chain: flag beats env beats the
// setting beats the configuration file beats the default.
func TestResolvePrecedence(t *testing.T) {
	tests := []struct {
		name                       string
		flag, env, setting, config string
		want                       string
	}{
		{"flag wins alone", "kanagawa", "", "", "", "kanagawa"},
		{"env is used without a flag", "", "kanagawa", "", "", "kanagawa"},
		{"the setting is used without a flag or env", "", "", "showa", "", "showa"},
		{"config is used when nothing above it is set", "", "", "", "elephant", "elephant"},
		{"flag overrides env", "kanagawa", "elephant", "", "", "kanagawa"},
		{"flag overrides the setting", "kanagawa", "", "showa", "", "kanagawa"},
		{"env overrides the setting", "", "kanagawa", "showa", "", "kanagawa"},
		{"the setting overrides config", "", "", "showa", "elephant", "showa"},
		{"nothing set falls back to the default", "", "", "", "", DefaultThemeName},
		{"whitespace-only tiers count as unset", "  ", "\t", " ", "", DefaultThemeName},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sel := Selection{Flag: tc.flag, Env: tc.env, Setting: tc.setting, Config: tc.config}
			if got := sel.Resolve().Name; got != tc.want {
				t.Errorf("Resolve() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestResolvePrefersSettingsOverConfigFile is the one precedence question the
// store answers differently from how the configuration file used to: SQLite is
// where the workspace records what somebody chose, and config.json is a legacy
// tier that reads but is never written. A setting losing to a stale
// config.json would mean a theme picked in the interface did not survive a
// restart.
func TestResolvePrefersSettingsOverConfigFile(t *testing.T) {
	sel := Selection{Setting: "showa", Config: "kanagawa"}
	if got := sel.Resolve().Name; got != "showa" {
		t.Fatalf("Resolve() = %q, want the setting's %q rather than config.json's %q", got, "showa", "kanagawa")
	}
}

// TestResolveRejectsUnknownNameWithoutFallingThrough pins that an invalid
// winning tier resolves straight to the default rather than silently trying
// the next tier down — a typo'd --theme must not hand control to
// ENGRAM_TUI_THEME.
func TestResolveRejectsUnknownNameWithoutFallingThrough(t *testing.T) {
	sel := Selection{Flag: "not-a-real-theme", Env: "kanagawa", Setting: "showa", Config: "elephant"}
	if got := sel.Resolve().Name; got != DefaultThemeName {
		t.Fatalf("Resolve() with an invalid flag = %q, want the default %q", got, DefaultThemeName)
	}
}

// TestResolveEveryRegisteredName confirms every name the registry exposes
// round-trips through Resolve to the palette of that name.
func TestResolveEveryRegisteredName(t *testing.T) {
	for _, name := range paletteNames() {
		t.Run(name, func(t *testing.T) {
			if got := (Selection{Flag: name}).Resolve().Name; got != name {
				t.Errorf("Resolve() = %q, want %q", got, name)
			}
		})
	}
}

// TestResolveFallsBackToRegistryWithoutStore is what keeps the workspace
// openable on a machine with no database, or after a read that failed: with
// Stored nil, every compiled palette still resolves and the default still
// answers.
func TestResolveFallsBackToRegistryWithoutStore(t *testing.T) {
	for _, name := range paletteNames() {
		sel := Selection{Flag: name, Stored: nil}
		if got := sel.Resolve().Name; got != name {
			t.Errorf("with no store, Resolve(%q) = %q", name, got)
		}
	}
	if got := (Selection{Stored: nil}).Resolve().Name; got != DefaultThemeName {
		t.Errorf("with no store and no tier set, Resolve() = %q, want %q", got, DefaultThemeName)
	}
	if got := (Selection{Stored: map[string]Palette{}}).Resolve().Name; got != DefaultThemeName {
		t.Errorf("with an empty store, Resolve() = %q, want %q", got, DefaultThemeName)
	}
}

// TestResolvePrefersAStoredPaletteOverTheCompiledOne is the whole reason the
// store is consulted at all: a palette edited with `UPDATE themes SET palette
// = json_set(...)` has to be the palette the next start paints with, or the
// edit was decoration.
func TestResolvePrefersAStoredPaletteOverTheCompiledOne(t *testing.T) {
	edited := KoiPond()
	edited.Primary = lipgloss.Color("#ff8a3d")

	sel := Selection{Flag: "koi-pond", Stored: map[string]Palette{"koi-pond": edited}}
	got := sel.Resolve()
	if got.Primary != edited.Primary {
		t.Errorf("Resolve() painted Primary %s, want the edited %s", got.Primary, edited.Primary)
	}
	if got.Name != "koi-pond" {
		t.Errorf("Resolve().Name = %q, want the name the row is filed under", got.Name)
	}
}

// TestResolveFindsAPaletteThatOnlyExistsInTheStore covers a theme somebody
// imported: it is not in the registry, so it has to come from the table or
// not at all.
func TestResolveFindsAPaletteThatOnlyExistsInTheStore(t *testing.T) {
	imported := KoiDay()
	imported.Name = "mine"
	sel := Selection{Flag: "mine", Stored: map[string]Palette{"mine": imported}}
	if got := sel.Resolve().Name; got != "mine" {
		t.Fatalf("Resolve() = %q, want the imported %q", got, "mine")
	}
	if got := sel.UnknownName(); got != "" {
		t.Errorf("UnknownName() = %q, want nothing: the store knows this theme", got)
	}
}

// TestResolveFallsBackToAnEditedDefault checks the fallback path goes through
// the store too: somebody who has re-tinted koi-pond and then typos a theme
// name should land on their koi-pond, not on the compiled one.
func TestResolveFallsBackToAnEditedDefault(t *testing.T) {
	edited := KoiPond()
	edited.Primary = lipgloss.Color("#ff8a3d")

	sel := Selection{Flag: "not-a-real-theme", Stored: map[string]Palette{DefaultThemeName: edited}}
	if got := sel.Resolve().Primary; got != edited.Primary {
		t.Fatalf("the fallback painted Primary %s, want the edited default's %s", got, edited.Primary)
	}
}

// TestUnknownName pins what a caller warns about: the winning tier's name,
// when nothing can resolve it.
func TestUnknownName(t *testing.T) {
	tests := []struct {
		name                       string
		flag, env, setting, config string
		want                       string
	}{
		{"a registered flag reports nothing", "kanagawa", "", "", "", ""},
		{"an unregistered flag is reported", "not-a-real-theme", "", "", "", "not-a-real-theme"},
		{"an unregistered env is reported below a blank flag", "", "not-a-real-theme", "", "", "not-a-real-theme"},
		{"an unregistered setting is reported", "", "", "not-a-real-theme", "", "not-a-real-theme"},
		{"an unregistered config is reported last", "", "", "", "not-a-real-theme", "not-a-real-theme"},
		{"a valid flag hides an invalid lower tier", "kanagawa", "not-a-real-theme", "", "", ""},
		{"nothing set reports nothing", "", "", "", "", ""},
		{"whitespace-only counts as nothing set", "  ", "", "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sel := Selection{Flag: tc.flag, Env: tc.env, Setting: tc.setting, Config: tc.config}
			if got := sel.UnknownName(); got != tc.want {
				t.Errorf("UnknownName() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestPalettesFromDocumentsReadsWhatItCanAndNamesWhatItCannot pins the policy
// that keeps a hand-edited table from locking somebody out: one broken
// document costs that one theme and nothing else.
func TestPalettesFromDocumentsReadsWhatItCanAndNamesWhatItCannot(t *testing.T) {
	good, err := MarshalTheme("koi-pond", ThemeVariantDark, KoiPond())
	if err != nil {
		t.Fatalf("MarshalTheme: %v", err)
	}
	documents := map[string][]byte{
		"KOI-Pond":  good,
		"half-done": []byte(`{"name":"half-done","variant":"dark","palette":{"base":"#000000"},"logo_gradient":[]}`),
		"garbage":   []byte(`not json at all`),
	}

	palettes, unreadable := PalettesFromDocuments(documents)
	if _, ok := palettes["koi-pond"]; !ok {
		t.Errorf("a readable document was not decoded, or not filed under its folded name: %v", palettes)
	}
	if len(palettes) != 1 {
		t.Errorf("decoded %d palettes, want only the readable one", len(palettes))
	}
	if len(unreadable) != 2 {
		t.Errorf("reported %v as unreadable, want both broken documents", unreadable)
	}
}

// TestPalettesFromDocumentsHandlesNothing keeps the no-database path free of
// special cases at the call site.
func TestPalettesFromDocumentsHandlesNothing(t *testing.T) {
	palettes, unreadable := PalettesFromDocuments(nil)
	if len(palettes) != 0 || len(unreadable) != 0 {
		t.Fatalf("decoding nothing gave %v / %v, want two empties", palettes, unreadable)
	}
}
