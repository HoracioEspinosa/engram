// `engram theme` manages the palettes the TUI renders with.
//
// Themes live in the store, not in a configuration file: SQLite is the source
// of truth for everything else the interface remembers, and a palette somebody
// edits with `UPDATE themes SET palette = json_set(...)` has to be the palette
// the next start paints with. The binary's own palettes are seeded into that
// table on every read, which is what makes an upgrade able to ship a corrected
// colour without overwriting one somebody chose.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"
)

// themeSettingKey is where the chosen theme is remembered. It is the tier
// `--theme` and ENGRAM_TUI_THEME override and the one config.json falls behind.
const themeSettingKey = "tui.theme"

func cmdTheme(cfg store.Config) {
	args := os.Args[2:]
	if len(args) == 0 {
		printThemeUsage()
		exitFunc(1)
		return
	}

	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		cmdThemeList(cfg, rest)
	case "show":
		cmdThemeShow(cfg, rest)
	case "use":
		cmdThemeUse(cfg, rest)
	case "import":
		cmdThemeImport(cfg, rest)
	case "export":
		cmdThemeExport(cfg, rest)
	case "reset":
		cmdThemeReset(cfg, rest)
	case "help", "--help", "-h":
		printThemeUsage()
	default:
		fmt.Fprintf(os.Stderr, "engram: unknown theme subcommand %q\n", sub)
		printThemeUsage()
		exitFunc(1)
	}
}

func printThemeUsage() {
	fmt.Fprint(os.Stdout, `usage: engram theme <subcommand> [flags]

Themes are stored in SQLite, seeded from the palettes the binary ships. Every
subcommand accepts --json.

Subcommands:
  list                     Every theme, with its variant and where it came from
  show <name>              The thirteen roles, the logo gradient and the
                             contrast of each role against both planes
  use <name>               Remember this theme as the one the TUI opens on
  import <file.json>       Add a theme from a document
                             [--name N] [--force]
  export <name>            Write a theme as a document [--out <path>]
  reset <name>             Put a theme back the way the binary ships it

Examples:
  engram theme list --json
  engram theme show kanagawa
  engram theme use kanagawa
  engram theme export kanagawa --out kanagawa.json
  engram theme import kanagawa.json --name kanagawa-mine
  engram theme reset kanagawa
`)
}

// themeSeed registers the palettes the binary ships, idempotently.
//
// It runs before every read rather than at install time: a theme added by a
// later version has to appear without anybody re-running a setup step, and
// SeedBuiltinTheme leaves a row somebody has edited exactly as it is.
func themeSeed(s *store.Store) error {
	for _, builtin := range theme.Builtins() {
		palette, err := theme.MarshalTheme(builtin.Name, builtin.Variant, builtin.Palette)
		if err != nil {
			return err
		}
		if err := s.SeedBuiltinTheme(builtin.Name, builtin.Variant, palette); err != nil {
			return err
		}
	}
	return nil
}

// themeOpenStore opens the store and seeds the built-in palettes into it.
func themeOpenStore(cfg store.Config, jsonOut bool) (*store.Store, bool) {
	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
		return nil, false
	}
	if err := themeSeed(s); err != nil {
		s.Close()
		projFail(jsonOut, "theme_seed_failed", err.Error(), nil)
		return nil, false
	}
	return s, true
}

// themePrintJSON writes a theme result. A theme belongs to no project, so it
// carries no project envelope; errors still use the {"error","code"} shape
// every other command's --json uses.
func themePrintJSON(result any) {
	out, err := jsonMarshalIndent(result, "", "  ")
	if err != nil {
		fatal(err)
		return
	}
	fmt.Fprintln(os.Stdout, string(out))
}

// themeNames lists what is installed, for the hint a miss prints.
func themeNames(s *store.Store) []string {
	records, err := s.ListThemes()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(records))
	for _, record := range records {
		names = append(names, record.Name)
	}
	sort.Strings(names)
	return names
}

// themeNotFound reports a name no theme answers to, with the list of the ones
// that do: the next thing anybody asks is "so which are there?".
func themeNotFound(s *store.Store, jsonOut bool, name string) {
	names := themeNames(s)
	projFail(jsonOut, "unknown_theme", fmt.Sprintf("no theme named %q", name),
		map[string]any{"themes": names, "hint": "one of " + strings.Join(names, ", ")})
}

// ─── list ────────────────────────────────────────────────────────────────────

func cmdThemeList(cfg store.Config, args []string) {
	f := projNewFlags("engram theme list")
	jsonOut := f.fs.Bool("json", false, "print JSON")
	if !f.parse(args) {
		return
	}

	s, ok := themeOpenStore(cfg, *jsonOut)
	if !ok {
		return
	}
	defer s.Close()

	records, err := s.ListThemes()
	if err != nil {
		fatal(err)
		return
	}
	active, _, err := s.Setting(themeSettingKey)
	if err != nil {
		fatal(err)
		return
	}

	type row struct {
		Name    string `json:"name"`
		Variant string `json:"variant"`
		Builtin bool   `json:"builtin"`
		Source  string `json:"source"`
		Active  bool   `json:"active"`
		// Problems is what Validate found. A theme is listed whether or not it
		// is legible; the picker draws a broken one in Danger rather than
		// hiding it, and so does this.
		Problems  []string `json:"problems"`
		UpdatedAt string   `json:"updated_at"`
	}

	rows := make([]row, 0, len(records))
	for _, record := range records {
		item := row{
			Name: record.Name, Variant: record.Variant, Builtin: record.Builtin,
			Source: record.Source, Active: record.Name == active,
			Problems: []string{}, UpdatedAt: record.UpdatedAt,
		}
		if _, _, palette, err := theme.UnmarshalTheme(record.Palette); err != nil {
			item.Problems = append(item.Problems, err.Error())
		} else {
			for _, problem := range palette.Validate() {
				item.Problems = append(item.Problems, problem.Error())
			}
		}
		rows = append(rows, item)
	}

	if *jsonOut {
		themePrintJSON(map[string]any{"themes": rows, "active": active})
		return
	}
	table := &projTable{headers: []string{"", "THEME", "VARIANT", "SOURCE", "PROBLEMS"}}
	for _, item := range rows {
		marker := " "
		if item.Active {
			marker = "*"
		}
		problems := "-"
		if len(item.Problems) > 0 {
			problems = fmt.Sprintf("%d", len(item.Problems))
		}
		table.add(marker, item.Name, item.Variant, item.Source, problems)
	}
	table.render(os.Stdout)
	if active == "" {
		fmt.Printf("\nno theme chosen; the TUI opens on %s\n", theme.DefaultThemeName)
	}
}

// ─── show ────────────────────────────────────────────────────────────────────

func cmdThemeShow(cfg store.Config, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram theme show")
	jsonOut := f.fs.Bool("json", false, "print JSON")
	if !f.parse(rest) {
		return
	}
	if len(positional) == 0 {
		projFail(*jsonOut, "missing_field", "usage: engram theme show <name>", nil)
		return
	}
	name := positional[0]

	s, ok := themeOpenStore(cfg, *jsonOut)
	if !ok {
		return
	}
	defer s.Close()

	record, err := s.GetTheme(name)
	if errors.Is(err, store.ErrThemeNotFound) {
		themeNotFound(s, *jsonOut, name)
		return
	}
	if err != nil {
		fatal(err)
		return
	}

	_, variant, palette, err := theme.UnmarshalTheme(record.Palette)
	if err != nil {
		projFail(*jsonOut, "invalid_theme", err.Error(), map[string]any{"theme": record.Name})
		return
	}

	roles := themeRoleRows(palette)
	problems := []string{}
	for _, problem := range palette.Validate() {
		problems = append(problems, problem.Error())
	}

	if *jsonOut {
		themePrintJSON(map[string]any{
			"name": record.Name, "variant": variant, "builtin": record.Builtin,
			"source": record.Source, "roles": roles,
			"logo_gradient": themeGradient(palette), "problems": problems,
		})
		return
	}

	fmt.Printf("theme:   %s (%s · %s)\n", record.Name, variant, record.Source)
	table := &projTable{headers: []string{"", "ROLE", "HEX", "ON BASE", "ON SURFACE"}}
	for _, role := range roles {
		table.add(themeSwatch(role.Hex), role.Role, role.Hex,
			themeRatioCell(role.OnBase, role.Role), themeRatioCell(role.OnSurface, role.Role))
	}
	table.render(os.Stdout)

	fmt.Print("\nlogo:    ")
	for _, stop := range themeGradient(palette) {
		fmt.Print(themeSwatch(stop) + " ")
	}
	fmt.Println()
	for _, stop := range themeGradient(palette) {
		fmt.Printf("         %s\n", stop)
	}
	for _, problem := range problems {
		fmt.Printf("problem: %s\n", problem)
	}
}

// themeRole is one row of the show table.
type themeRole struct {
	Role string `json:"role"`
	Hex  string `json:"hex"`
	// OnBase and OnSurface are the contrast ratios against the two planes. A
	// plane against itself is 1:1 and reported as such rather than hidden: it
	// is the honest number.
	OnBase    float64 `json:"on_base"`
	OnSurface float64 `json:"on_surface"`
}

// themeRoleRows measures every role against both background planes, which is
// the matrix that decides whether a palette is usable.
func themeRoleRows(p theme.Palette) []themeRole {
	rows := make([]themeRole, 0, len(theme.Roles()))
	for _, role := range theme.Roles() {
		colour, ok := p.Role(role)
		if !ok {
			continue
		}
		row := themeRole{Role: role, Hex: string(colour)}
		if ratio, err := theme.ContrastRatio(colour, p.Base); err == nil {
			row.OnBase = ratio
		}
		if ratio, err := theme.ContrastRatio(colour, p.Surface); err == nil {
			row.OnSurface = ratio
		}
		rows = append(rows, row)
	}
	return rows
}

// themeGradient renders the five logo stops as hex strings.
func themeGradient(p theme.Palette) []string {
	out := make([]string, 0, len(p.LogoGradient))
	for _, stop := range p.LogoGradient {
		out = append(out, string(stop))
	}
	return out
}

// themeSwatch paints one cell in the colour it names, using a truecolor
// background escape. A terminal that does not do truecolor prints a space,
// which costs nothing; the hex is in the next column either way.
func themeSwatch(hex string) string {
	r, g, b, ok := themeHexRGB(hex)
	if !ok {
		return " "
	}
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm  \x1b[0m", r, g, b)
}

// themeHexRGB parses a #rrggbb literal into its channels.
func themeHexRGB(hex string) (int, int, int, bool) {
	if len(hex) != 7 || hex[0] != '#' {
		return 0, 0, 0, false
	}
	var r, g, b int
	if _, err := fmt.Sscanf(hex[1:], "%02x%02x%02x", &r, &g, &b); err != nil {
		return 0, 0, 0, false
	}
	return r, g, b, true
}

// themeRatioCell renders a contrast ratio, marking the ones that fall under
// the bar the role answers to.
func themeRatioCell(ratio float64, role string) string {
	if ratio == 0 {
		return "-"
	}
	cell := fmt.Sprintf("%.2f:1", ratio)
	switch {
	case themeIsTextRole(role) && ratio < theme.MinContrastRatio:
		return cell + " !"
	case role == theme.RoleOverlay && ratio < theme.MinOverlayContrastRatio:
		return cell + " !"
	default:
		return cell
	}
}

func themeIsTextRole(role string) bool {
	for _, candidate := range theme.TextRoles() {
		if candidate == role {
			return true
		}
	}
	return false
}

// ─── use ─────────────────────────────────────────────────────────────────────

func cmdThemeUse(cfg store.Config, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram theme use")
	jsonOut := f.fs.Bool("json", false, "print JSON")
	if !f.parse(rest) {
		return
	}
	if len(positional) == 0 {
		projFail(*jsonOut, "missing_field", "usage: engram theme use <name>", nil)
		return
	}
	name := positional[0]

	s, ok := themeOpenStore(cfg, *jsonOut)
	if !ok {
		return
	}
	defer s.Close()

	record, err := s.GetTheme(name)
	if errors.Is(err, store.ErrThemeNotFound) {
		themeNotFound(s, *jsonOut, name)
		return
	}
	if err != nil {
		fatal(err)
		return
	}
	if err := s.SetSetting(themeSettingKey, record.Name); err != nil {
		fatal(err)
		return
	}

	if *jsonOut {
		themePrintJSON(map[string]any{"theme": record.Name, "setting": themeSettingKey})
		return
	}
	fmt.Printf("the TUI now opens on %s\n", record.Name)
}

// ─── import ──────────────────────────────────────────────────────────────────

func cmdThemeImport(cfg store.Config, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram theme import")
	jsonOut := f.fs.Bool("json", false, "print JSON")
	rename := f.fs.String("name", "", "store the theme under this name instead of the document's")
	force := f.fs.Bool("force", false, "store a palette that does not pass validation")
	if !f.parse(rest) {
		return
	}
	if len(positional) == 0 {
		projFail(*jsonOut, "missing_field", "usage: engram theme import <file.json> [--name N] [--force]", nil)
		return
	}

	raw, err := os.ReadFile(positional[0])
	if err != nil {
		projFail(*jsonOut, "invalid_file", err.Error(), nil)
		return
	}
	name, variant, palette, err := theme.UnmarshalTheme(raw)
	if err != nil {
		projFail(*jsonOut, "invalid_theme", err.Error(), map[string]any{"file": positional[0]})
		return
	}
	if given := strings.TrimSpace(*rename); given != "" {
		if err := theme.ValidateThemeName(given); err != nil {
			projFail(*jsonOut, "invalid_theme", err.Error(), nil)
			return
		}
		name = given
	}

	problems := []string{}
	for _, problem := range palette.Validate() {
		problems = append(problems, problem.Error())
	}
	if len(problems) > 0 && !*force {
		projFail(*jsonOut, "invalid_palette",
			fmt.Sprintf("%s does not pass validation (%d problem(s))", name, len(problems)),
			map[string]any{
				"theme":    name,
				"problems": problems,
				"hint":     "fix the colours, or keep them with --force",
			})
		return
	}

	encoded, err := theme.MarshalTheme(name, variant, palette)
	if err != nil {
		projFail(*jsonOut, "invalid_theme", err.Error(), nil)
		return
	}

	s, ok := themeOpenStore(cfg, *jsonOut)
	if !ok {
		return
	}
	defer s.Close()

	if err := s.SaveTheme(store.ThemeRecord{
		Name: name, Variant: variant, Palette: encoded, Source: store.ThemeSourceJSON,
	}); err != nil {
		fatal(err)
		return
	}

	if *jsonOut {
		themePrintJSON(map[string]any{
			"theme": name, "variant": variant, "source": store.ThemeSourceJSON,
			"problems": problems, "forced": *force && len(problems) > 0,
		})
		return
	}
	fmt.Printf("imported %s (%s)\n", name, variant)
	for _, problem := range problems {
		fmt.Fprintf(os.Stderr, "engram: %s\n", problem)
	}
}

// ─── export ──────────────────────────────────────────────────────────────────

func cmdThemeExport(cfg store.Config, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram theme export")
	jsonOut := f.fs.Bool("json", false, "print JSON")
	out := f.fs.String("out", "", "write the document here instead of to stdout")
	if !f.parse(rest) {
		return
	}
	if len(positional) == 0 {
		projFail(*jsonOut, "missing_field", "usage: engram theme export <name> [--out <path>]", nil)
		return
	}
	name := positional[0]

	s, ok := themeOpenStore(cfg, *jsonOut)
	if !ok {
		return
	}
	defer s.Close()

	record, err := s.GetTheme(name)
	if errors.Is(err, store.ErrThemeNotFound) {
		themeNotFound(s, *jsonOut, name)
		return
	}
	if err != nil {
		fatal(err)
		return
	}

	// The row is re-encoded rather than copied out: a palette edited by hand
	// with json_set is still a valid document but need not be formatted like
	// one, and an export is what somebody else imports.
	_, variant, palette, err := theme.UnmarshalTheme(record.Palette)
	if err != nil {
		projFail(*jsonOut, "invalid_theme", err.Error(), map[string]any{"theme": record.Name})
		return
	}
	encoded, err := theme.MarshalTheme(record.Name, variant, palette)
	if err != nil {
		projFail(*jsonOut, "invalid_theme", err.Error(), nil)
		return
	}

	path := strings.TrimSpace(*out)
	if path == "" && !*jsonOut {
		fmt.Print(string(encoded))
		return
	}
	if path == "" {
		themePrintJSON(map[string]any{"theme": record.Name, "document": json.RawMessage(encoded)})
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		projFail(*jsonOut, "invalid_file", err.Error(), nil)
		return
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		projFail(*jsonOut, "invalid_file", err.Error(), nil)
		return
	}
	if *jsonOut {
		themePrintJSON(map[string]any{"theme": record.Name, "path": path})
		return
	}
	fmt.Printf("wrote %s to %s\n", record.Name, path)
}

// ─── reset ───────────────────────────────────────────────────────────────────

func cmdThemeReset(cfg store.Config, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram theme reset")
	jsonOut := f.fs.Bool("json", false, "print JSON")
	if !f.parse(rest) {
		return
	}
	if len(positional) == 0 {
		projFail(*jsonOut, "missing_field", "usage: engram theme reset <name>", nil)
		return
	}
	name := positional[0]

	s, ok := themeOpenStore(cfg, *jsonOut)
	if !ok {
		return
	}
	defer s.Close()

	// The compiled palette has to be handed over: the store keeps one copy of a
	// theme, so "the way it shipped" only exists in the binary. A theme this
	// build does not ship is removed instead of being given an invented past.
	var palette json.RawMessage
	if builtin, ok := theme.Builtin(name); ok {
		encoded, err := theme.MarshalTheme(builtin.Name, builtin.Variant, builtin.Palette)
		if err != nil {
			fatal(err)
			return
		}
		palette = encoded
	}

	err := s.ResetTheme(name, palette)
	if errors.Is(err, store.ErrThemeNotFound) {
		themeNotFound(s, *jsonOut, name)
		return
	}
	if err != nil {
		fatal(err)
		return
	}

	_, restored := theme.Builtin(name)
	if *jsonOut {
		themePrintJSON(map[string]any{"theme": name, "restored": restored, "removed": !restored})
		return
	}
	if restored {
		fmt.Printf("%s is back the way this build ships it\n", name)
		return
	}
	fmt.Printf("%s is not a theme this build ships, so it was removed\n", name)
}
