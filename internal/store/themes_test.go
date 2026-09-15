package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const koiPondPalette = `{"base":"#0d1b21","text":"#e6edef","primary":"#ff9e5e"}`

func TestSeedBuiltinThemeDoesNotOverrideSqlEdits(t *testing.T) {
	s := newProjectsSchemaTestStore(t)

	if err := s.SeedBuiltinTheme("koi-pond", "dark", json.RawMessage(koiPondPalette)); err != nil {
		t.Fatalf("SeedBuiltinTheme: %v", err)
	}
	seeded, err := s.GetTheme("koi-pond")
	if err != nil {
		t.Fatalf("GetTheme: %v", err)
	}
	if !seeded.Builtin || seeded.Source != ThemeSourceBuiltin {
		t.Fatalf("expected a builtin theme, got %+v", seeded)
	}

	// Seeding again with a new palette is how a build ships a corrected colour,
	// and an untouched builtin takes it.
	const corrected = `{"base":"#0d1b21","text":"#e6edef","primary":"#ff8a3d"}`
	if err := s.SeedBuiltinTheme("koi-pond", "dark", json.RawMessage(corrected)); err != nil {
		t.Fatalf("SeedBuiltinTheme (corrected): %v", err)
	}
	updated, err := s.GetTheme("koi-pond")
	if err != nil {
		t.Fatalf("GetTheme: %v", err)
	}
	if !strings.Contains(string(updated.Palette), "#ff8a3d") {
		t.Fatalf("expected the reseed to carry the new palette, got %s", updated.Palette)
	}

	// Editing a builtin by hand marks it as the user's, and the next seed must
	// leave it alone: a theme edited in SQL surviving an upgrade is the whole
	// point of the source column.
	const handEdited = `{"base":"#000000","text":"#ffffff","primary":"#123456"}`
	if _, err := s.db.Exec(
		`UPDATE themes SET palette = ?, source = 'sql', updated_at = datetime('now') WHERE name = 'koi-pond'`,
		handEdited,
	); err != nil {
		t.Fatalf("hand edit: %v", err)
	}
	if err := s.SeedBuiltinTheme("koi-pond", "dark", json.RawMessage(koiPondPalette)); err != nil {
		t.Fatalf("SeedBuiltinTheme (after hand edit): %v", err)
	}
	after, err := s.GetTheme("koi-pond")
	if err != nil {
		t.Fatalf("GetTheme: %v", err)
	}
	if !strings.Contains(string(after.Palette), "#123456") {
		t.Fatalf("expected the hand-edited palette to survive the seed, got %s", after.Palette)
	}
	if after.Source != ThemeSourceSQL {
		t.Fatalf("expected source to stay %q, got %q", ThemeSourceSQL, after.Source)
	}
}

func TestSaveThemeRejectsInvalidName(t *testing.T) {
	s := newProjectsSchemaTestStore(t)

	for _, name := range []string{"", "Koi-Pond", "koi pond", "koi_pond", "-koi", "koi-", strings.Repeat("k", 33)} {
		err := s.SaveTheme(ThemeRecord{Name: name, Variant: "dark", Palette: json.RawMessage(koiPondPalette)})
		if !errors.Is(err, ErrInvalidThemeName) {
			t.Errorf("SaveTheme(%q) = %v, want ErrInvalidThemeName", name, err)
		}
	}

	if err := s.SaveTheme(ThemeRecord{
		Name: "koi-day", Variant: "sepia", Palette: json.RawMessage(koiPondPalette),
	}); !errors.Is(err, ErrInvalidThemeVariant) {
		t.Errorf("expected ErrInvalidThemeVariant, got %v", err)
	}

	if err := s.SaveTheme(ThemeRecord{
		Name: "koi-day", Variant: "light", Palette: json.RawMessage(`{"base":`),
	}); !errors.Is(err, ErrInvalidThemePalette) {
		t.Errorf("expected ErrInvalidThemePalette, got %v", err)
	}

	if err := s.SaveTheme(ThemeRecord{
		Name: "koi-day", Variant: "light", Palette: json.RawMessage(koiPondPalette), Source: "builtin",
	}); !errors.Is(err, ErrInvalidThemeSource) {
		t.Errorf("expected a saved theme to be refused the builtin source, got %v", err)
	}

	if err := s.SaveTheme(ThemeRecord{
		Name: "koi-day", Variant: "light", Palette: json.RawMessage(koiPondPalette),
	}); err != nil {
		t.Fatalf("SaveTheme (valid): %v", err)
	}
	saved, err := s.GetTheme("koi-day")
	if err != nil {
		t.Fatalf("GetTheme: %v", err)
	}
	if saved.Builtin || saved.Source != ThemeSourceJSON {
		t.Fatalf("expected a user theme defaulting to the json source, got %+v", saved)
	}

	if _, err := s.GetTheme("koi-nowhere"); !errors.Is(err, ErrThemeNotFound) {
		t.Fatalf("expected ErrThemeNotFound, got %v", err)
	}
}

func TestResetThemeRestoresBuiltin(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	if err := s.SeedBuiltinTheme("koi-pond", "dark", json.RawMessage(koiPondPalette)); err != nil {
		t.Fatalf("SeedBuiltinTheme: %v", err)
	}
	if _, err := s.db.Exec(
		`UPDATE themes SET palette = ?, source = 'sql' WHERE name = 'koi-pond'`,
		`{"base":"#000000"}`,
	); err != nil {
		t.Fatalf("hand edit: %v", err)
	}

	if err := s.ResetTheme("koi-pond", json.RawMessage(koiPondPalette)); err != nil {
		t.Fatalf("ResetTheme: %v", err)
	}
	restored, err := s.GetTheme("koi-pond")
	if err != nil {
		t.Fatalf("GetTheme: %v", err)
	}
	if restored.Source != ThemeSourceBuiltin || !strings.Contains(string(restored.Palette), "#ff9e5e") {
		t.Fatalf("expected the compiled palette back under the builtin source, got %+v", restored)
	}

	// A theme the code does not ship has nothing to be restored to, so reset
	// removes it rather than inventing a previous state.
	if err := s.SaveTheme(ThemeRecord{
		Name: "my-theme", Variant: "dark", Palette: json.RawMessage(koiPondPalette),
	}); err != nil {
		t.Fatalf("SaveTheme: %v", err)
	}
	if err := s.ResetTheme("my-theme", nil); err != nil {
		t.Fatalf("ResetTheme (custom): %v", err)
	}
	if _, err := s.GetTheme("my-theme"); !errors.Is(err, ErrThemeNotFound) {
		t.Fatalf("expected the custom theme to be gone, got %v", err)
	}

	if err := s.ResetTheme("koi-nowhere", nil); !errors.Is(err, ErrThemeNotFound) {
		t.Fatalf("expected ErrThemeNotFound, got %v", err)
	}
}

func TestDeleteThemeRefusesABuiltin(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	if err := s.SeedBuiltinTheme("koi-pond", "dark", json.RawMessage(koiPondPalette)); err != nil {
		t.Fatalf("SeedBuiltinTheme: %v", err)
	}
	if err := s.DeleteTheme("koi-pond"); !errors.Is(err, ErrBuiltinTheme) {
		t.Fatalf("expected ErrBuiltinTheme, got %v", err)
	}

	if err := s.SaveTheme(ThemeRecord{
		Name: "my-theme", Variant: "light", Palette: json.RawMessage(koiPondPalette),
	}); err != nil {
		t.Fatalf("SaveTheme: %v", err)
	}
	if err := s.DeleteTheme("my-theme"); err != nil {
		t.Fatalf("DeleteTheme: %v", err)
	}
	if err := s.DeleteTheme("my-theme"); !errors.Is(err, ErrThemeNotFound) {
		t.Fatalf("expected ErrThemeNotFound on the second delete, got %v", err)
	}
}

func TestListThemesIsOrderedAndComplete(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	for _, name := range []string{"showa", "koi-pond", "ogon"} {
		if err := s.SeedBuiltinTheme(name, "dark", json.RawMessage(koiPondPalette)); err != nil {
			t.Fatalf("SeedBuiltinTheme(%q): %v", name, err)
		}
	}
	themes, err := s.ListThemes()
	if err != nil {
		t.Fatalf("ListThemes: %v", err)
	}
	got := make([]string, 0, len(themes))
	for _, theme := range themes {
		got = append(got, theme.Name)
		if theme.UpdatedAt == "" || len(theme.Palette) == 0 {
			t.Errorf("incomplete theme record: %+v", theme)
		}
	}
	want := []string{"koi-pond", "ogon", "showa"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("expected %v in name order, got %v", want, got)
	}
}

// TestThemesAndSettingsAreLocalOnly pins the two decisions that make these
// tables different from every other table in the schema: they do not move the
// version stamp, and nothing about them replicates.
func TestThemesAndSettingsAreLocalOnly(t *testing.T) {
	s := newProjectsSchemaTestStore(t)

	status, err := s.ProjectsSchemaStatus()
	if err != nil {
		t.Fatalf("ProjectsSchemaStatus: %v", err)
	}
	if status.UserVersion != ProjectsSchemaVersion {
		t.Fatalf("expected user_version to stay at %d, got %d", ProjectsSchemaVersion, status.UserVersion)
	}

	if err := s.SeedBuiltinTheme("koi-pond", "dark", json.RawMessage(koiPondPalette)); err != nil {
		t.Fatalf("SeedBuiltinTheme: %v", err)
	}
	if err := s.SetSetting("tui.theme", "koi-pond"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	status, err = s.ProjectsSchemaStatus()
	if err != nil {
		t.Fatalf("ProjectsSchemaStatus: %v", err)
	}
	if status.Themes != 1 || status.Settings != 1 {
		t.Fatalf("expected the status to count both tables, got themes=%d settings=%d",
			status.Themes, status.Settings)
	}

	// What the screen looks like on this machine is not a fact about the work,
	// so neither table queues anything for another machine to apply.
	var queued int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM sync_mutations WHERE entity IN ('theme','setting')`,
	).Scan(&queued); err != nil {
		t.Fatalf("count queued mutations: %v", err)
	}
	if queued != 0 {
		t.Fatalf("expected themes and settings to queue nothing for sync, got %d rows", queued)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	s := newProjectsSchemaTestStore(t)

	if _, ok, err := s.Setting("tui.theme"); err != nil || ok {
		t.Fatalf("expected an unset key to report absent, got ok=%v err=%v", ok, err)
	}

	if err := s.SetSetting("tui.theme", "koi-pond"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	value, ok, err := s.Setting("tui.theme")
	if err != nil || !ok || value != "koi-pond" {
		t.Fatalf("Setting = %q ok=%v err=%v", value, ok, err)
	}

	// Writing the same key again replaces the value rather than failing on the
	// primary key: a setting is the current answer, not a log of answers.
	if err := s.SetSetting("tui.theme", "koi-day"); err != nil {
		t.Fatalf("SetSetting (replace): %v", err)
	}
	if value, _, _ := s.Setting("tui.theme"); value != "koi-day" {
		t.Fatalf("expected the replacement value, got %q", value)
	}

	for _, key := range []string{"tui.icons", "tui.last_project", "cloud.enabled"} {
		if err := s.SetSetting(key, "x"); err != nil {
			t.Fatalf("SetSetting(%q): %v", key, err)
		}
	}
	scoped, err := s.Settings("tui.")
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if len(scoped) != 3 {
		t.Fatalf("expected the prefix to select the three tui keys, got %v", scoped)
	}
	if _, found := scoped["cloud.enabled"]; found {
		t.Fatalf("expected the prefix to exclude other namespaces, got %v", scoped)
	}

	all, err := s.Settings("")
	if err != nil {
		t.Fatalf("Settings(all): %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("expected every key with no prefix, got %v", all)
	}

	if err := s.DeleteSetting("tui.theme"); err != nil {
		t.Fatalf("DeleteSetting: %v", err)
	}
	if _, ok, _ := s.Setting("tui.theme"); ok {
		t.Fatal("expected the deleted key to be gone")
	}
	// Deleting what is already gone is the state the caller asked for.
	if err := s.DeleteSetting("tui.theme"); err != nil {
		t.Fatalf("DeleteSetting (again): %v", err)
	}
}

func TestSetSettingRejectsUppercaseKey(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	for _, key := range []string{"TUI.theme", "tui.Theme", "", " tui.theme", "tui theme", "tui/theme", strings.Repeat("k", 65)} {
		if err := s.SetSetting(key, "koi-pond"); !errors.Is(err, ErrInvalidSettingKey) {
			t.Errorf("SetSetting(%q) = %v, want ErrInvalidSettingKey", key, err)
		}
	}
	if _, _, err := s.Setting("TUI.theme"); !errors.Is(err, ErrInvalidSettingKey) {
		t.Errorf("expected a read of an invalid key to be refused too, got %v", err)
	}
}
