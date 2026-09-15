package theme

import "strings"

// Selection is everything that has an opinion about which palette the
// workspace opens on: the four tiers that can name one, and the palettes the
// store has to offer.
//
// It is a struct rather than four arguments because the tiers only mean
// anything together — a caller that passes them in the wrong order has
// silently changed the precedence, and four bare strings at a call site cannot
// say which is which.
type Selection struct {
	// Flag is an explicit --theme.
	Flag string
	// Env is ENGRAM_TUI_THEME.
	Env string
	// Setting is settings['tui.theme'], the tier the interface writes when
	// somebody picks a theme. SQLite is the source of truth for what the
	// workspace remembers, so this sits above the configuration file rather
	// than beside it.
	Setting string
	// Config is the tui.theme key in config.json, kept as a read-only legacy
	// tier for installations that set it before settings existed.
	Config string

	// Stored are the palettes read from the themes table, by name. A palette
	// somebody edited with `UPDATE themes SET palette = json_set(...)` is in
	// here and the compiled one is not, which is what makes a SQL edit
	// survive to the next start. Leave it nil — no database, or a read that
	// failed — and the compiled registry answers alone.
	Stored map[string]Palette
}

// pickName returns the first non-blank tier, in precedence order, or "" if
// every tier is blank. Resolve and UnknownName both build on it so the two
// never disagree about which tier won.
func (s Selection) pickName() string {
	for _, candidate := range []string{s.Flag, s.Env, s.Setting, s.Config} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// lookup finds a palette by name, preferring what the store holds over what
// the binary compiled in.
func (s Selection) lookup(name string) (Palette, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return Palette{}, false
	}
	if palette, ok := s.Stored[name]; ok {
		// The name a row is filed under is the name that identifies it,
		// whatever the document inside happens to claim.
		palette.Name = name
		return palette, true
	}
	if ctor, ok := registry[name]; ok {
		return ctor(), true
	}
	return Palette{}, false
}

// Resolve picks the palette the workspace opens on.
//
// The first non-blank tier wins outright, with no fallthrough to a lower one
// if the name turns out to be invalid: a typo in --theme must not silently
// hand control to whatever ENGRAM_TUI_THEME happens to hold. A blank or
// unrecognised name resolves to DefaultThemeName, which is itself looked up
// in the store first, so an edited default is still the default.
func (s Selection) Resolve() Palette {
	if palette, ok := s.lookup(s.pickName()); ok {
		return palette
	}
	if fallback, ok := s.lookup(DefaultThemeName); ok {
		return fallback
	}
	return registry[DefaultThemeName]()
}

// UnknownName reports the winning tier's name when it is non-blank and names
// no palette, so a caller can warn before Resolve falls back to the default.
// It returns "" when the winning name is blank or already valid, in which case
// no warning is warranted.
func (s Selection) UnknownName() string {
	name := s.pickName()
	if name == "" {
		return ""
	}
	if _, ok := s.lookup(name); ok {
		return ""
	}
	return name
}

// PalettesFromDocuments decodes stored theme documents into palettes by name,
// and reports the names it could not read.
//
// A row that is not a theme is skipped rather than fatal. The themes table is
// editable by hand — that is the point of it — so a malformed document is a
// thing a person does, not a corruption, and it must cost them that one theme
// rather than the ability to open the workspace at all. The caller decides
// whether to mention the skipped names; Resolve simply never sees them and
// falls back to the compiled palette of the same name if there is one.
func PalettesFromDocuments(documents map[string][]byte) (map[string]Palette, []string) {
	palettes := make(map[string]Palette, len(documents))
	var unreadable []string
	for name, document := range documents {
		key := strings.ToLower(strings.TrimSpace(name))
		_, _, palette, err := UnmarshalTheme(document)
		if err != nil {
			unreadable = append(unreadable, key)
			continue
		}
		palette.Name = key
		palettes[key] = palette
	}
	return palettes, unreadable
}
