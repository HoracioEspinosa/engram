package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Where a theme's current palette came from. The distinction is what lets an
// upgrade ship a corrected colour without overwriting one somebody chose:
// only a row still marked builtin is reseeded.
const (
	// ThemeSourceBuiltin means the palette is the one the binary ships.
	ThemeSourceBuiltin = "builtin"
	// ThemeSourceJSON means it was imported or saved through the application.
	ThemeSourceJSON = "json"
	// ThemeSourceSQL means somebody edited the row directly.
	ThemeSourceSQL = "sql"
)

// Theme variants. A palette declares which of the two it was built for, so the
// picker can group them and a light terminal does not open on a dark theme.
const (
	ThemeVariantDark  = "dark"
	ThemeVariantLight = "light"
)

var (
	// ErrThemeNotFound is returned for a theme name no row answers to.
	ErrThemeNotFound = errors.New("theme not found")
	// ErrBuiltinTheme is returned when a theme the binary ships is asked to be
	// deleted. It would come back on the next start; ResetTheme is the verb
	// for putting it back the way it shipped.
	ErrBuiltinTheme = errors.New("a builtin theme cannot be deleted")
	// ErrInvalidThemeName is returned for a name outside the kebab-case shape
	// the CLI, the picker and the file name all have to agree on.
	ErrInvalidThemeName = errors.New("invalid theme name")
	// ErrInvalidThemeVariant is returned for a variant that is neither dark nor
	// light.
	ErrInvalidThemeVariant = errors.New("invalid theme variant")
	// ErrInvalidThemePalette is returned when the palette is not valid JSON.
	// What the colours mean is the interface's business; that this is a
	// document at all is the store's.
	ErrInvalidThemePalette = errors.New("invalid theme palette")
	// ErrInvalidThemeSource is returned when a saved theme claims to be
	// builtin. Only seeding writes that source: a theme that claimed it would
	// be silently replaced by the next upgrade.
	ErrInvalidThemeSource = errors.New("invalid theme source")
)

// themeNamePattern is the kebab-case shape a theme name has to take: it is used
// as a file name on export, as an argument on the command line and as a key in
// settings, and all three want the same thing.
var themeNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// maxThemeNameLength mirrors the CHECK on the column.
const maxThemeNameLength = 32

// ThemeRecord is one row of themes: a named palette, the variant it was built
// for, and where its current contents came from.
type ThemeRecord struct {
	Name string `json:"name"`
	// Variant is "dark" or "light".
	Variant string `json:"variant"`
	// Palette is the colour document, kept raw: the store guarantees it is
	// JSON and leaves what the roles mean to the interface that draws them.
	Palette json.RawMessage `json:"palette"`
	// Builtin reports whether the binary ships a theme by this name.
	Builtin bool `json:"builtin"`
	// Source is builtin, json or sql.
	Source    string `json:"source"`
	UpdatedAt string `json:"updated_at"`
}

const themeSelectColumns = `name, variant, palette, builtin, source, updated_at`

func scanTheme(row interface{ Scan(dest ...any) error }) (ThemeRecord, error) {
	var t ThemeRecord
	var palette string
	var builtin int
	if err := row.Scan(&t.Name, &t.Variant, &palette, &builtin, &t.Source, &t.UpdatedAt); err != nil {
		return ThemeRecord{}, err
	}
	t.Palette = json.RawMessage(palette)
	t.Builtin = builtin == 1
	return t, nil
}

// ListThemes returns every theme, by name, so a picker renders in a stable
// order rather than in whatever order the rows happen to sit in.
func (s *Store) ListThemes() ([]ThemeRecord, error) {
	rows, err := s.queryHook(s.readDB(), `SELECT `+themeSelectColumns+` FROM themes ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("engram-projects: list themes: %w", err)
	}
	defer rows.Close()

	themes := make([]ThemeRecord, 0)
	for rows.Next() {
		theme, err := scanTheme(rows)
		if err != nil {
			return nil, fmt.Errorf("engram-projects: scan theme: %w", err)
		}
		themes = append(themes, theme)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("engram-projects: list themes: %w", err)
	}
	return themes, nil
}

// GetTheme returns one theme, or ErrThemeNotFound.
func (s *Store) GetTheme(name string) (ThemeRecord, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	theme, err := scanTheme(s.queryRowHook(s.readDB(),
		`SELECT `+themeSelectColumns+` FROM themes WHERE name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return ThemeRecord{}, fmt.Errorf("%w: %s", ErrThemeNotFound, name)
	}
	if err != nil {
		return ThemeRecord{}, fmt.Errorf("engram-projects: get theme: %w", err)
	}
	return theme, nil
}

// SaveTheme writes a theme somebody chose: imported from a file, or edited in
// the application. It never writes the builtin source — that belongs to
// SeedBuiltinTheme alone, and a row that claimed it would be quietly replaced
// by the next upgrade.
func (s *Store) SaveTheme(rec ThemeRecord) error {
	name, err := validThemeName(rec.Name)
	if err != nil {
		return err
	}
	variant := strings.ToLower(strings.TrimSpace(rec.Variant))
	if variant == "" {
		variant = ThemeVariantDark
	}
	if variant != ThemeVariantDark && variant != ThemeVariantLight {
		return fmt.Errorf("%w: %q", ErrInvalidThemeVariant, rec.Variant)
	}
	if !json.Valid(rec.Palette) {
		return fmt.Errorf("%w: %s is not a JSON document", ErrInvalidThemePalette, name)
	}
	source := strings.ToLower(strings.TrimSpace(rec.Source))
	if source == "" {
		source = ThemeSourceJSON
	}
	if source != ThemeSourceJSON && source != ThemeSourceSQL {
		return fmt.Errorf("%w: %q (a saved theme is json or sql)", ErrInvalidThemeSource, rec.Source)
	}

	// builtin is left to whatever the row already says: whether the binary
	// ships a theme by this name is a fact about the build, not about this
	// write, and overwriting a builtin's palette does not stop it being one.
	if _, err := s.execHook(s.db, `
		INSERT INTO themes (name, variant, palette, builtin, source, updated_at)
		VALUES (?, ?, ?, 0, ?, datetime('now'))
		ON CONFLICT(name) DO UPDATE SET
			variant    = excluded.variant,
			palette    = excluded.palette,
			source     = excluded.source,
			updated_at = datetime('now')`,
		name, variant, string(rec.Palette), source,
	); err != nil {
		return fmt.Errorf("engram-projects: save theme %s: %w", name, err)
	}
	return nil
}

// SeedBuiltinTheme registers a palette the binary ships. It is idempotent by
// design and runs on every start.
//
// The WHERE clause on the upsert is the whole point: a row still marked builtin
// takes the palette the new build carries, and one somebody has edited — which
// the edit marked 'sql' or 'json' — is left exactly as it is. An upgrade that
// silently repainted a chosen theme would be a bug that looks like a feature.
func (s *Store) SeedBuiltinTheme(name, variant string, palette json.RawMessage) error {
	validName, err := validThemeName(name)
	if err != nil {
		return err
	}
	variant = strings.ToLower(strings.TrimSpace(variant))
	if variant == "" {
		variant = ThemeVariantDark
	}
	if variant != ThemeVariantDark && variant != ThemeVariantLight {
		return fmt.Errorf("%w: %q", ErrInvalidThemeVariant, variant)
	}
	if !json.Valid(palette) {
		return fmt.Errorf("%w: %s is not a JSON document", ErrInvalidThemePalette, validName)
	}

	if _, err := s.execHook(s.db, `
		INSERT INTO themes (name, variant, palette, builtin, source, updated_at)
		VALUES (?, ?, ?, 1, 'builtin', datetime('now'))
		ON CONFLICT(name) DO UPDATE SET
			palette    = excluded.palette,
			variant    = excluded.variant,
			builtin    = 1,
			updated_at = datetime('now')
		WHERE themes.source = 'builtin'`,
		validName, variant, string(palette),
	); err != nil {
		return fmt.Errorf("engram-projects: seed builtin theme %s: %w", validName, err)
	}
	return nil
}

// ResetTheme puts a theme back the way the binary ships it. The caller passes
// the compiled palette, because the store does not hold a second copy of it.
//
// A theme the binary does not ship has nothing to be restored to, so it is
// removed rather than left in an invented previous state.
func (s *Store) ResetTheme(name string, palette json.RawMessage) error {
	existing, err := s.GetTheme(name)
	if err != nil {
		return err
	}
	if !existing.Builtin {
		return s.deleteThemeRow(existing.Name)
	}
	if palette == nil {
		palette = existing.Palette
	}
	if !json.Valid(palette) {
		return fmt.Errorf("%w: %s is not a JSON document", ErrInvalidThemePalette, existing.Name)
	}
	if _, err := s.execHook(s.db, `
		UPDATE themes SET palette = ?, source = 'builtin', updated_at = datetime('now')
		WHERE name = ?`, string(palette), existing.Name,
	); err != nil {
		return fmt.Errorf("engram-projects: reset theme %s: %w", existing.Name, err)
	}
	return nil
}

// DeleteTheme removes a theme somebody added. A builtin is refused: it would be
// back on the next start, and ResetTheme is the verb for undoing changes to one.
func (s *Store) DeleteTheme(name string) error {
	existing, err := s.GetTheme(name)
	if err != nil {
		return err
	}
	if existing.Builtin {
		return fmt.Errorf("%w: %s", ErrBuiltinTheme, existing.Name)
	}
	return s.deleteThemeRow(existing.Name)
}

func (s *Store) deleteThemeRow(name string) error {
	if _, err := s.execHook(s.db, `DELETE FROM themes WHERE name = ?`, name); err != nil {
		return fmt.Errorf("engram-projects: delete theme %s: %w", name, err)
	}
	return nil
}

func validThemeName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if len(trimmed) > maxThemeNameLength || !themeNamePattern.MatchString(trimmed) {
		return "", fmt.Errorf("%w: %q (lowercase letters, digits and single hyphens, up to %d characters)",
			ErrInvalidThemeName, name, maxThemeNameLength)
	}
	return trimmed, nil
}
