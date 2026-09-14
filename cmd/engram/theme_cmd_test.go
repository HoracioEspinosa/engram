package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"
)

// runTheme invokes `engram theme …` with args and captures both streams.
func runTheme(t *testing.T, cfg store.Config, args ...string) (stdout, stderr string) {
	t.Helper()
	withArgs(t, append([]string{"engram", "theme"}, args...)...)
	return captureOutput(t, func() { cmdTheme(cfg) })
}

// decodeThemeJSON parses the plain result object `engram theme --json` prints.
// A theme belongs to no project, so it carries no project envelope.
func decodeThemeJSON(t *testing.T, out string) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	return result
}

// TestThemeListSeedsTheBuiltinPalettes pins that the palettes the binary ships
// appear without anybody running a setup step, and that listing twice does not
// duplicate them.
func TestThemeListSeedsTheBuiltinPalettes(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)

	stdout, _ := runTheme(t, cfg, "list", "--json")
	themes := decodeThemeJSON(t, stdout)["themes"].([]any)
	if len(themes) != len(theme.Builtins()) {
		t.Fatalf("listed %d themes, want the %d the binary ships", len(themes), len(theme.Builtins()))
	}
	for _, item := range themes {
		row := item.(map[string]any)
		if !row["builtin"].(bool) {
			t.Errorf("%v is not marked as a builtin", row["name"])
		}
		if row["source"] != store.ThemeSourceBuiltin {
			t.Errorf("%v has source %v, want builtin", row["name"], row["source"])
		}
	}

	again, _ := runTheme(t, cfg, "list", "--json")
	if len(decodeThemeJSON(t, again)["themes"].([]any)) != len(themes) {
		t.Fatal("seeding twice changed how many themes there are")
	}
}

// TestThemeShowReportsTheRolesAndTheirContrast pins what `show` is for: the
// thirteen roles, the gradient, and the contrast of each role against both
// planes.
func TestThemeShowReportsTheRolesAndTheirContrast(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)

	stdout, _ := runTheme(t, cfg, "show", theme.DefaultThemeName, "--json")
	result := decodeThemeJSON(t, stdout)
	if result["name"] != theme.DefaultThemeName {
		t.Fatalf("name = %v, want %s", result["name"], theme.DefaultThemeName)
	}
	roles := result["roles"].([]any)
	if len(roles) != len(theme.Roles()) {
		t.Fatalf("reported %d roles, want %d", len(roles), len(theme.Roles()))
	}
	first := roles[0].(map[string]any)
	if first["role"] != theme.RoleBase {
		t.Errorf("the first role is %v, want %s", first["role"], theme.RoleBase)
	}
	if _, ok := first["on_base"]; !ok {
		t.Error("a role carries no contrast against the ground")
	}
	if len(result["logo_gradient"].([]any)) != 5 {
		t.Fatalf("gradient = %v, want five stops", result["logo_gradient"])
	}

	text, _ := runTheme(t, cfg, "show", theme.DefaultThemeName)
	for _, role := range theme.Roles() {
		if !strings.Contains(text, role) {
			t.Errorf("the text rendering does not name the role %s", role)
		}
	}
}

// TestThemeUseRemembersTheChoice pins that choosing a theme writes the setting
// the TUI reads, which is what makes SQLite the source of truth rather than a
// configuration file.
func TestThemeUseRemembersTheChoice(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)

	runTheme(t, cfg, "use", "kanagawa")

	s := openTestStore(t, cfg)
	value, found, err := s.Setting(themeSettingKey)
	if err != nil {
		t.Fatalf("Setting: %v", err)
	}
	if !found || value != "kanagawa" {
		t.Fatalf("%s = %q (found %v), want kanagawa", themeSettingKey, value, found)
	}

	stdout, _ := runTheme(t, cfg, "list", "--json")
	result := decodeThemeJSON(t, stdout)
	if result["active"] != "kanagawa" {
		t.Fatalf("active = %v, want kanagawa", result["active"])
	}
}

// TestThemeUseUnknownExitsWithTheList pins the refusal: a name nothing answers
// to exits non-zero and says which names do, because that is the next thing
// anybody asks.
func TestThemeUseUnknownExitsWithTheList(t *testing.T) {
	cfg := testConfig(t)
	exited := stubExit(t)

	stdout, _ := runTheme(t, cfg, "use", "no-such-theme", "--json")
	result := decodeThemeJSON(t, stdout)
	if result["code"] != "unknown_theme" {
		t.Fatalf("code = %v, want unknown_theme", result["code"])
	}
	if len(result["themes"].([]any)) == 0 {
		t.Fatal("the refusal does not say which themes there are")
	}
	if !*exited {
		t.Error("an unknown theme must exit non-zero")
	}

	s := openTestStore(t, cfg)
	if _, found, err := s.Setting(themeSettingKey); err != nil || found {
		t.Fatalf("the refused name was remembered anyway (found %v, err %v)", found, err)
	}
}

// TestThemeExportThenImportRoundTrips pins that what `export` writes is what
// `import` accepts, under a new name.
func TestThemeExportThenImportRoundTrips(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	path := filepath.Join(t.TempDir(), "kanagawa.json")

	runTheme(t, cfg, "export", "kanagawa", "--out", path)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the export: %v", err)
	}
	name, variant, palette, err := theme.UnmarshalTheme(raw)
	if err != nil {
		t.Fatalf("the exported document does not parse: %v", err)
	}
	if name != "kanagawa" {
		t.Fatalf("the document names %q", name)
	}

	// Kanagawa carries known contrast debt, so importing it under a new name
	// needs the same --force a hand-written palette would.
	needsForce := len(palette.Validate()) > 0
	args := []string{"import", path, "--name", "kanagawa-mine", "--json"}
	if needsForce {
		args = append(args, "--force")
	}
	stdout, _ := runTheme(t, cfg, args...)
	result := decodeThemeJSON(t, stdout)
	if result["theme"] != "kanagawa-mine" {
		t.Fatalf("imported as %v, want kanagawa-mine", result["theme"])
	}
	if result["variant"] != variant {
		t.Fatalf("variant = %v, want %s", result["variant"], variant)
	}

	s := openTestStore(t, cfg)
	record, err := s.GetTheme("kanagawa-mine")
	if err != nil {
		t.Fatalf("GetTheme: %v", err)
	}
	if record.Source != store.ThemeSourceJSON {
		t.Fatalf("source = %q, want %q", record.Source, store.ThemeSourceJSON)
	}
	if record.Builtin {
		t.Error("an imported theme claims to be one the binary ships")
	}
}

// TestThemeExportWritesTheDocumentToStdout pins the default destination, so
// `engram theme export x | pbcopy` works without a temporary file.
func TestThemeExportWritesTheDocumentToStdout(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)

	stdout, _ := runTheme(t, cfg, "export", "elephant")
	if _, _, _, err := theme.UnmarshalTheme([]byte(stdout)); err != nil {
		t.Fatalf("stdout is not a theme document: %v\n%s", err, stdout)
	}
}

// TestThemeImportRejectsAnIllegiblePaletteWithoutForce pins the guard: a
// palette that fails validation is refused, the refusal lists what is wrong,
// and --force is what stores it anyway.
func TestThemeImportRejectsAnIllegiblePaletteWithoutForce(t *testing.T) {
	cfg := testConfig(t)
	exited := stubExit(t)
	path := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(path, []byte(`{
  "name": "invisible",
  "variant": "dark",
  "palette": {
    "base": "#000000", "surface": "#010101", "overlay": "#020202",
    "text": "#030303", "subtext": "#040404",
    "primary": "#050505", "secondary": "#060606", "accent": "#070707", "highlight": "#080808",
    "success": "#090909", "warning": "#0a0a0a", "danger": "#0b0b0b", "info": "#0c0c0c"
  },
  "logo_gradient": ["#030303", "#050505", "#070707", "#090909", "#0b0b0b"]
}`), 0o644); err != nil {
		t.Fatalf("write the broken theme: %v", err)
	}

	stdout, _ := runTheme(t, cfg, "import", path, "--json")
	result := decodeThemeJSON(t, stdout)
	if result["code"] != "invalid_palette" {
		t.Fatalf("code = %v, want invalid_palette", result["code"])
	}
	if len(result["problems"].([]any)) == 0 {
		t.Fatal("the refusal does not say what is wrong")
	}
	if !*exited {
		t.Error("a refused import must exit non-zero")
	}

	s := openTestStore(t, cfg)
	if _, err := s.GetTheme("invisible"); err == nil {
		t.Fatal("the refused palette was stored anyway")
	}

	stdout, _ = runTheme(t, cfg, "import", path, "--force", "--json")
	forced := decodeThemeJSON(t, stdout)
	if forced["theme"] != "invisible" {
		t.Fatalf("--force did not store the palette: %v", forced)
	}
	if !forced["forced"].(bool) {
		t.Error("the result does not record that the palette was forced in")
	}
	if _, err := s.GetTheme("invisible"); err != nil {
		t.Fatalf("GetTheme after --force: %v", err)
	}
}

// TestThemeImportRejectsADocumentThatIsNotATheme pins that the shape check
// happens before the taste check: a file missing a role is refused whether or
// not --force is given.
func TestThemeImportRejectsADocumentThatIsNotATheme(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	path := filepath.Join(t.TempDir(), "not-a-theme.json")
	if err := os.WriteFile(path, []byte(`{"name":"partial","palette":{"base":"#000000"}}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	stdout, _ := runTheme(t, cfg, "import", path, "--force", "--json")
	if code := decodeThemeJSON(t, stdout)["code"]; code != "invalid_theme" {
		t.Fatalf("code = %v, want invalid_theme", code)
	}
}

// TestThemeResetRestoresABuiltinAndRemovesTheRest pins both halves of reset: a
// palette the binary ships comes back the way it shipped, and one it does not
// is removed rather than given an invented previous state.
func TestThemeResetRestoresABuiltinAndRemovesTheRest(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	s := openTestStore(t, cfg)

	// Seed first, then edit a builtin the way somebody would with SQL.
	runTheme(t, cfg, "list", "--json")
	builtin, ok := theme.Builtin("elephant")
	if !ok {
		t.Fatal("elephant is not a registered palette")
	}
	edited := builtin.Palette
	edited.Primary = "#123456"
	encoded, err := theme.MarshalTheme("elephant", builtin.Variant, edited)
	if err != nil {
		t.Fatalf("MarshalTheme: %v", err)
	}
	if err := s.SaveTheme(store.ThemeRecord{
		Name: "elephant", Variant: builtin.Variant, Palette: encoded, Source: store.ThemeSourceSQL,
	}); err != nil {
		t.Fatalf("SaveTheme: %v", err)
	}

	stdout, _ := runTheme(t, cfg, "reset", "elephant", "--json")
	if restored := decodeThemeJSON(t, stdout)["restored"]; restored != true {
		t.Fatalf("restored = %v, want true", restored)
	}
	record, err := s.GetTheme("elephant")
	if err != nil {
		t.Fatalf("GetTheme: %v", err)
	}
	if record.Source != store.ThemeSourceBuiltin {
		t.Fatalf("source = %q, want builtin", record.Source)
	}
	_, _, palette, err := theme.UnmarshalTheme(record.Palette)
	if err != nil {
		t.Fatalf("UnmarshalTheme: %v", err)
	}
	if string(palette.Primary) != strings.ToLower(string(builtin.Palette.Primary)) {
		t.Fatalf("primary = %q, want the shipped %q", palette.Primary, builtin.Palette.Primary)
	}

	// A theme nobody ships has no previous state to be put back into.
	if err := s.SaveTheme(store.ThemeRecord{
		Name: "mine", Variant: "dark", Palette: encoded, Source: store.ThemeSourceJSON,
	}); err != nil {
		t.Fatalf("SaveTheme: %v", err)
	}
	stdout, _ = runTheme(t, cfg, "reset", "mine", "--json")
	if removed := decodeThemeJSON(t, stdout)["removed"]; removed != true {
		t.Fatalf("removed = %v, want true", removed)
	}
	if _, err := s.GetTheme("mine"); err == nil {
		t.Fatal("the theme was not removed")
	}
}

// TestThemeUnknownSubcommandExits pins that a typo is refused rather than
// doing something adjacent.
func TestThemeUnknownSubcommandExits(t *testing.T) {
	cfg := testConfig(t)
	exited := stubExit(t)

	_, stderr := runTheme(t, cfg, "paint")
	if !*exited {
		t.Fatal("`theme paint` must exit non-zero")
	}
	if !strings.Contains(stderr, "unknown theme subcommand") {
		t.Fatalf("stderr = %q", stderr)
	}
}

// TestThemeShowUnknownExitsWithTheList pins the same refusal `use` gives.
func TestThemeShowUnknownExitsWithTheList(t *testing.T) {
	cfg := testConfig(t)
	exited := stubExit(t)

	stdout, _ := runTheme(t, cfg, "show", "no-such-theme", "--json")
	if code := decodeThemeJSON(t, stdout)["code"]; code != "unknown_theme" {
		t.Fatalf("code = %v, want unknown_theme", code)
	}
	if !*exited {
		t.Error("an unknown theme must exit non-zero")
	}
}
