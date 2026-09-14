package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"
)

// TestThemeListIncludesKoiPalettes pins that the palettes this workspace
// designed reach the table, not only the compiled registry. A palette that
// never gets seeded cannot be chosen in the picker, edited with SQL, or
// exported — the three things the table exists for.
func TestThemeListIncludesKoiPalettes(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)

	stdout, _ := runTheme(t, cfg, "list", "--json")
	listed := map[string]map[string]any{}
	for _, item := range decodeThemeJSON(t, stdout)["themes"].([]any) {
		row := item.(map[string]any)
		listed[row["name"].(string)] = row
	}

	for _, want := range []struct{ name, variant string }{
		{"koi-pond", "dark"},
		{"koi-day", "light"},
		{"showa", "dark"},
		{"ogon", "dark"},
	} {
		row, ok := listed[want.name]
		if !ok {
			t.Errorf("%s is not listed; got %v", want.name, listed)
			continue
		}
		if row["variant"] != want.variant {
			t.Errorf("%s has variant %v, want %s", want.name, row["variant"], want.variant)
		}
	}

	// The default has to be one of them, or `engram tui` opens on a palette
	// the table does not hold.
	if _, ok := listed[theme.DefaultThemeName]; !ok {
		t.Errorf("the default theme %q is not among the seeded ones", theme.DefaultThemeName)
	}
}

// TestTuiSeedsBuiltinThemesIdempotently pins that opening the workspace is
// itself the setup step: the palettes appear on a database that has never seen
// them, running again changes nothing, and a row somebody edited survives.
//
// The last part is the one that matters. `UPDATE themes SET palette =
// json_set(...)` is a documented way to re-tint a theme, and a start that
// silently repainted it would be a bug that looks like the edit never
// happened.
func TestTuiSeedsBuiltinThemesIdempotently(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)
	withArgs(t, "engram", "tui")
	t.Setenv("ENGRAM_TUI_THEME", "")

	captureOutput(t, func() { cmdTUI(cfg) })

	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	defer s.Close()

	first, err := s.ListThemes()
	if err != nil {
		t.Fatalf("list themes: %v", err)
	}
	if len(first) != len(theme.Builtins()) {
		t.Fatalf("seeded %d themes, want the %d the binary ships", len(first), len(theme.Builtins()))
	}

	// Re-tint the default the way the documentation says to, marking the row
	// as edited.
	edited := findTheme(t, first, theme.DefaultThemeName)
	var document map[string]any
	if err := json.Unmarshal(edited.Palette, &document); err != nil {
		t.Fatalf("decode the seeded palette: %v", err)
	}
	document["palette"].(map[string]any)["primary"] = "#ff8a3d"
	repainted, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode the edited palette: %v", err)
	}
	if err := s.SaveTheme(store.ThemeRecord{
		Name:    edited.Name,
		Variant: edited.Variant,
		Palette: repainted,
		Source:  store.ThemeSourceSQL,
	}); err != nil {
		t.Fatalf("save the edited palette: %v", err)
	}
	s.Close()

	// A second start seeds again and must leave the edit alone.
	captureOutput(t, func() { cmdTUI(cfg) })

	s2, err := store.New(cfg)
	if err != nil {
		t.Fatalf("reopen the store: %v", err)
	}
	defer s2.Close()

	second, err := s2.ListThemes()
	if err != nil {
		t.Fatalf("list themes again: %v", err)
	}
	if len(second) != len(first) {
		t.Errorf("a second start changed the theme count from %d to %d", len(first), len(second))
	}
	survivor := findTheme(t, second, theme.DefaultThemeName)
	if !strings.Contains(string(survivor.Palette), "#ff8a3d") {
		t.Errorf("the edited palette did not survive the second start: %s", survivor.Palette)
	}
	if survivor.Source != store.ThemeSourceSQL {
		t.Errorf("the edited row came back as source %q, want %q", survivor.Source, store.ThemeSourceSQL)
	}
}

// TestTuiOpensOnTheRememberedTheme is the other half of the picker's promise:
// what it wrote to settings is what the next start paints with, above
// config.json and below the flag and the environment.
func TestTuiOpensOnTheRememberedTheme(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)

	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	if err := s.SetSetting(themeSettingKey, "showa"); err != nil {
		t.Fatalf("remember a theme: %v", err)
	}
	s.Close()

	// A stale config.json naming another theme must lose to the setting.
	configPath := filepath.Join(cfg.DataDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"tui":{"theme":"kanagawa"}}`), 0o644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(configPath) })

	got := paletteCmdTUIOpensWith(t, cfg, []string{"engram", "tui"}, "")
	if got != "showa" {
		t.Fatalf("opened on %q, want the remembered %q", got, "showa")
	}

	// And the two tiers above it still win.
	if got := paletteCmdTUIOpensWith(t, cfg, []string{"engram", "tui"}, "ogon"); got != "ogon" {
		t.Errorf("ENGRAM_TUI_THEME lost to the setting: opened on %q", got)
	}
	if got := paletteCmdTUIOpensWith(t, cfg, []string{"engram", "tui", "--theme", "koi-day"}, "ogon"); got != "koi-day" {
		t.Errorf("--theme lost to a lower tier: opened on %q", got)
	}
}

// TestTuiOpensOnAPaletteEditedInTheTable proves the store is actually read for
// colours and not only for names: a re-tinted koi-pond has to reach the screen.
func TestTuiOpensOnAPaletteEditedInTheTable(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)

	// Seed first, then edit, the same order a real installation goes through.
	withArgs(t, "engram", "tui")
	t.Setenv("ENGRAM_TUI_THEME", "")
	captureOutput(t, func() { cmdTUI(cfg) })

	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	seeded, err := s.ListThemes()
	if err != nil {
		t.Fatalf("list themes: %v", err)
	}
	record := findTheme(t, seeded, theme.DefaultThemeName)
	var document map[string]any
	if err := json.Unmarshal(record.Palette, &document); err != nil {
		t.Fatalf("decode the seeded palette: %v", err)
	}
	document["palette"].(map[string]any)["primary"] = "#ff8a3d"
	repainted, _ := json.Marshal(document)
	if err := s.SaveTheme(store.ThemeRecord{
		Name: record.Name, Variant: record.Variant, Palette: repainted, Source: store.ThemeSourceSQL,
	}); err != nil {
		t.Fatalf("save the edited palette: %v", err)
	}
	s.Close()

	var painted theme.Palette
	oldNewTUIModel := newTUIModel
	t.Cleanup(func() { newTUIModel = oldNewTUIModel })
	newTUIModel = func(_ *store.Store, _ string, palette theme.Palette) tui.Model {
		painted = palette
		return tui.New(nil, "", "", palette)
	}
	captureOutput(t, func() { cmdTUI(cfg) })

	if got := string(painted.Primary); got != "#ff8a3d" {
		t.Fatalf("opened with Primary %q, want the edited %q", got, "#ff8a3d")
	}
}

// TestTuiIgnoresAThemeRowThatIsNotAThemeDocument keeps a hand-broken row from
// taking the workspace down with it.
func TestTuiIgnoresAThemeRowThatIsNotAThemeDocument(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)

	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	if err := s.SaveTheme(store.ThemeRecord{
		Name:    "half-done",
		Variant: "dark",
		Palette: json.RawMessage(`{"name":"half-done","variant":"dark","palette":{},"logo_gradient":[]}`),
		Source:  store.ThemeSourceSQL,
	}); err != nil {
		t.Fatalf("save the broken row: %v", err)
	}
	s.Close()

	withArgs(t, "engram", "tui")
	t.Setenv("ENGRAM_TUI_THEME", "")
	_, stderr, recovered := captureOutputAndRecover(t, func() { cmdTUI(cfg) })
	if recovered != nil {
		t.Fatalf("a broken theme row panicked the workspace: %v", recovered)
	}
	if !strings.Contains(stderr, "half-done") {
		t.Errorf("stderr never mentioned the unreadable theme:\n%s", stderr)
	}
}

// paletteCmdTUIOpensWith runs cmdTUI with the given arguments and environment
// and reports the palette it handed the workspace.
func paletteCmdTUIOpensWith(t *testing.T, cfg store.Config, args []string, env string) string {
	t.Helper()
	withArgs(t, args...)
	t.Setenv("ENGRAM_TUI_THEME", env)

	var name string
	oldNewTUIModel := newTUIModel
	defer func() { newTUIModel = oldNewTUIModel }()
	newTUIModel = func(_ *store.Store, _ string, palette theme.Palette) tui.Model {
		name = palette.Name
		return tui.New(nil, "", "", palette)
	}
	captureOutput(t, func() { cmdTUI(cfg) })
	return name
}

// findTheme picks one row out of a listing, failing loudly rather than
// returning a zero value a later assertion would misread.
func findTheme(t *testing.T, records []store.ThemeRecord, name string) store.ThemeRecord {
	t.Helper()
	for _, record := range records {
		if record.Name == name {
			return record
		}
	}
	t.Fatalf("%q is not among the listed themes", name)
	return store.ThemeRecord{}
}
