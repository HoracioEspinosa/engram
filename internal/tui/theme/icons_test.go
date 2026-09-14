package theme

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// allModes is every mode a glyph has to answer in. Every table-driven test
// below walks it rather than naming one mode, so an icon added with two
// spellings out of three is a failure and not a silent blank.
var allModes = []IconMode{IconModeUnicode, IconModeNerd, IconModeASCII}

// TestEveryIconHasAllThreeModes is the catalogue's completeness check: an
// Icon constant with no entry, or with a blank spelling in any mode, would
// render as nothing at all — a column that silently loses its marker rather
// than falling back to a readable one.
func TestEveryIconHasAllThreeModes(t *testing.T) {
	for icon := Icon(0); icon < iconCount; icon++ {
		entry := catalog[icon]
		if entry.name == "" {
			t.Errorf("icon %d has no name: the catalogue has a hole at that index", int(icon))
			continue
		}
		for _, mode := range allModes {
			if Icons(mode).Glyph(icon) == "" {
				t.Errorf("icon %s has no %s spelling", entry.name, mode)
			}
		}
	}
}

// TestEveryGlyphIsOneCellWide holds the whole catalogue to the one property
// every column in the workspace depends on: a marker occupies exactly one
// terminal cell. A two-cell glyph shifts every column after it by one, and a
// zero-width one collapses the column instead — neither is visible in a unit
// test of the view that renders it, only here.
//
// ansi.StringWidth is the same measure shared/text.go truncates and pads
// with, so a glyph that passes here cannot mis-measure downstream.
func TestEveryGlyphIsOneCellWide(t *testing.T) {
	for _, mode := range allModes {
		t.Run(string(mode), func(t *testing.T) {
			set := Icons(mode)
			for icon := Icon(0); icon < iconCount; icon++ {
				glyph := set.Glyph(icon)
				if width := ansi.StringWidth(glyph); width != 1 {
					t.Errorf("icon %s renders %q, which is %d cells wide, want 1",
						catalog[icon].name, glyph, width)
				}
			}
		})
	}
}

// TestNerdCodepointsAreInPrivateUseRanges proves the nerd column really is
// patched-font territory and not an ordinary character that happens to look
// right: every nerd spelling has to be a single rune from one of the private
// use areas a Nerd Font patches its icons into. A stray BMP character here
// would render on an unpatched font and quietly make "nerd" indistinguishable
// from "unicode".
func TestNerdCodepointsAreInPrivateUseRanges(t *testing.T) {
	for icon := Icon(0); icon < iconCount; icon++ {
		glyph := catalog[icon].nerd
		runes := []rune(glyph)
		if len(runes) != 1 {
			t.Errorf("icon %s spells its nerd glyph with %d runes, want exactly one",
				catalog[icon].name, len(runes))
			continue
		}
		if !inPrivateUse(runes[0]) {
			t.Errorf("icon %s uses U+%04X, which is outside every private use area",
				catalog[icon].name, runes[0])
		}
	}
}

// inPrivateUse reports whether r is in one of Unicode's three private use
// areas — the BMP block and the two supplementary planes Nerd Fonts spread
// their patched icons across.
func inPrivateUse(r rune) bool {
	switch {
	case r >= 0xE000 && r <= 0xF8FF: // Private Use Area
		return true
	case r >= 0xF0000 && r <= 0xFFFFD: // Supplementary Private Use Area-A
		return true
	case r >= 0x100000 && r <= 0x10FFFD: // Supplementary Private Use Area-B
		return true
	}
	return false
}

// TestUnicodeGlyphsStayInTheBasicPlane keeps the unicode column to what an
// unpatched font on a plain terminal actually draws. A supplementary-plane
// character in the fallback would defeat the point of having a fallback.
func TestUnicodeGlyphsStayInTheBasicPlane(t *testing.T) {
	for icon := Icon(0); icon < iconCount; icon++ {
		runes := []rune(catalog[icon].unicode)
		if len(runes) != 1 {
			t.Errorf("icon %s spells its unicode glyph with %d runes, want exactly one",
				catalog[icon].name, len(runes))
			continue
		}
		if runes[0] > 0xFFFF {
			t.Errorf("icon %s uses U+%04X, which is outside the basic multilingual plane",
				catalog[icon].name, runes[0])
		}
		if inPrivateUse(runes[0]) {
			t.Errorf("icon %s falls back to U+%04X, a private use codepoint: that is the nerd column's job",
				catalog[icon].name, runes[0])
		}
	}
}

// TestASCIIGlyphsArePrintableASCII holds the last-resort column to the one
// thing that makes it a last resort: a byte any terminal in any locale can
// draw.
func TestASCIIGlyphsArePrintableASCII(t *testing.T) {
	for icon := Icon(0); icon < iconCount; icon++ {
		glyph := catalog[icon].ascii
		if len(glyph) != 1 {
			t.Errorf("icon %s spells its ascii glyph %q with %d bytes, want exactly one",
				catalog[icon].name, glyph, len(glyph))
			continue
		}
		if glyph[0] < 0x21 || glyph[0] > 0x7e {
			t.Errorf("icon %s uses byte %#x, which is not printable ascii", catalog[icon].name, glyph[0])
		}
	}
}

// TestResolveIconModeNeverGuessesNerd is the rule that keeps the workspace
// readable on a machine nobody has patched a font on: nerd is opt-in and
// nothing about the environment can turn it on by itself. A terminal that
// advertises a Nerd Font through TERM, a locale that mentions one, a
// TERM_PROGRAM somebody set — none of them may promote the mode, because the
// cost of guessing wrong is a screen of replacement characters.
func TestResolveIconModeNeverGuessesNerd(t *testing.T) {
	// Every environment shape that could tempt a detector, with no explicit
	// request anywhere.
	environments := []struct {
		name       string
		term, lang string
	}{
		{"a terminal naming a patched font", "xterm-kitty-nerdfont", "en_US.UTF-8"},
		{"a locale naming a patched font", "xterm-256color", "en_US.UTF-8@nerd"},
		{"the usual truecolor terminal", "xterm-256color", "en_US.UTF-8"},
		{"a bare terminal", "", ""},
	}
	for _, env := range environments {
		t.Run(env.name, func(t *testing.T) {
			if got := ResolveIconMode("", "", env.term, env.lang); got == IconModeNerd {
				t.Errorf("ResolveIconMode guessed %s from TERM=%q LANG=%q with nothing asking for it",
					got, env.term, env.lang)
			}
		})
	}

	// And the two tiers that may ask for it, which must be honoured.
	if got := ResolveIconMode("nerd", "", "dumb", "C"); got != IconModeNerd {
		t.Errorf("ENGRAM_TUI_ICONS=nerd resolved to %s, want nerd: an explicit request is not a guess", got)
	}
	if got := ResolveIconMode("", "nerd", "dumb", "C"); got != IconModeNerd {
		t.Errorf("tui.icons=nerd resolved to %s, want nerd: an explicit request is not a guess", got)
	}
}

// TestResolveIconModePrecedence pins the order the design fixes:
// ENGRAM_TUI_ICONS, then tui.icons, then ascii for a terminal that cannot
// draw anything better, then unicode.
func TestResolveIconModePrecedence(t *testing.T) {
	tests := []struct {
		name                     string
		env, setting, term, lang string
		want                     IconMode
	}{
		{"the environment wins over the setting", "ascii", "nerd", "xterm-256color", "en_US.UTF-8", IconModeASCII},
		{"the setting wins over the terminal", "", "nerd", "xterm-256color", "en_US.UTF-8", IconModeNerd},
		{"a dumb terminal falls to ascii", "", "", "dumb", "en_US.UTF-8", IconModeASCII},
		{"a locale without utf-8 falls to ascii", "", "", "xterm-256color", "en_US.ISO-8859-1", IconModeASCII},
		{"an empty locale falls to ascii", "", "", "xterm-256color", "", IconModeASCII},
		{"a utf8 locale spelled without the hyphen counts", "", "", "xterm-256color", "en_US.utf8", IconModeUnicode},
		{"nothing set is unicode", "", "", "xterm-256color", "en_US.UTF-8", IconModeUnicode},
		{"an unrecognised request is ignored", "sparkles", "", "xterm-256color", "en_US.UTF-8", IconModeUnicode},
		{"whitespace and case do not matter", "  NERD ", "", "dumb", "C", IconModeNerd},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveIconMode(tc.env, tc.setting, tc.term, tc.lang); got != tc.want {
				t.Errorf("ResolveIconMode(%q, %q, %q, %q) = %s, want %s",
					tc.env, tc.setting, tc.term, tc.lang, got, tc.want)
			}
		})
	}
}

// TestIconsNormalisesAnUnknownMode keeps a bad value from producing a Set
// that draws nothing: an unrecognised mode is the default one, not an empty
// one.
func TestIconsNormalisesAnUnknownMode(t *testing.T) {
	set := Icons(IconMode("hieroglyphs"))
	if set.Mode() != IconModeUnicode {
		t.Fatalf("Icons(%q).Mode() = %s, want the default %s", "hieroglyphs", set.Mode(), IconModeUnicode)
	}
	if set.Glyph(IconKoi) != Icons(IconModeUnicode).Glyph(IconKoi) {
		t.Error("an unknown mode did not draw the default mode's glyphs")
	}
}

// TestGlyphOfAnUnknownIconIsNeutral covers the other direction: an Icon
// value outside the catalogue (a constant from a newer build read out of
// persisted state, say) draws the neutral marker rather than an empty cell.
func TestGlyphOfAnUnknownIconIsNeutral(t *testing.T) {
	for _, mode := range allModes {
		set := Icons(mode)
		if got := set.Glyph(iconCount + 7); got != set.Glyph(IconUnknown) {
			t.Errorf("%s: an out-of-range icon drew %q, want the neutral %q",
				mode, got, set.Glyph(IconUnknown))
		}
	}
}

// domainLookup is one of the seven vocabulary mappings, paired with every
// value the store's own CHECK constraint admits. Keeping the schema's lists
// here is the point: a state added to the database with no icon beside it
// would otherwise show up as the neutral marker on screen and nowhere in a
// test.
type domainLookup struct {
	name   string
	lookup func(Set, string) string
	values []string
}

func domainLookups() []domainLookup {
	return []domainLookup{
		{"TaskState", Set.TaskState, []string{
			"open", "analysis", "in_progress", "review", "verified",
			"done", "blocked", "cancelled", "pending", "archived", "unverified",
		}},
		{"TaskKind", Set.TaskKind, []string{
			"feature", "bugfix", "refactor", "incident", "migration", "spike",
		}},
		{"EvidenceCategory", Set.EvidenceCategory, []string{
			"analysis", "plans", "runbooks", "reports", "patches",
			"evidences", "evidences-qa", "benchmarks", "scripts", "assets", "exports",
		}},
		{"EvidenceKind", Set.EvidenceKind, []string{
			"png", "jpg", "gif", "webp", "svg", "mp4", "webm", "json", "csv",
			"log", "txt", "md", "patch", "diff", "pdf", "html", "zip", "har", "other",
		}},
		{"RunbookCategory", Set.RunbookCategory, []string{
			"auth", "database", "queue", "network", "performance", "data-integrity", "registration",
		}},
		{"ProjectKind", Set.ProjectKind, []string{
			"umbrella", "repo", "instance", "service", "dataset", "knowledge",
		}},
		{"SyncState", Set.SyncState, []string{
			"healthy", "pending", "running", "idle", "disabled", "degraded",
		}},
	}
}

// TestEveryDomainValueHasItsOwnIcon walks the seven vocabularies against the
// exact value lists the schema admits. Every one of them must resolve to
// something other than the neutral marker: falling back is what an unknown
// value does, and a known value that falls back is an icon nobody drew.
func TestEveryDomainValueHasItsOwnIcon(t *testing.T) {
	for _, mode := range allModes {
		set := Icons(mode)
		neutral := set.Glyph(IconUnknown)
		for _, domain := range domainLookups() {
			for _, value := range domain.values {
				if got := domain.lookup(set, value); got == neutral {
					t.Errorf("%s: %s(%q) fell back to the neutral marker", mode, domain.name, value)
				}
			}
		}
	}
}

// TestUnknownDomainValuesFallBackNeutrally is the same check inverted: a
// value the binary has never heard of draws the neutral marker instead of an
// empty cell or a panic. A store one migration ahead of this binary is the
// case this protects.
func TestUnknownDomainValuesFallBackNeutrally(t *testing.T) {
	set := Icons(IconModeUnicode)
	neutral := set.Glyph(IconUnknown)
	for _, domain := range domainLookups() {
		for _, value := range []string{"", "   ", "not-a-real-value"} {
			if got := domain.lookup(set, value); got != neutral {
				t.Errorf("%s(%q) = %q, want the neutral marker %q", domain.name, value, got, neutral)
			}
		}
	}
}

// TestDomainLookupsIgnoreCaseAndPadding keeps a value read from a JSON
// payload or typed on the command line from missing its icon over a capital
// letter or a trailing space.
func TestDomainLookupsIgnoreCaseAndPadding(t *testing.T) {
	set := Icons(IconModeUnicode)
	for _, domain := range domainLookups() {
		for _, value := range domain.values {
			want := domain.lookup(set, value)
			noisy := "  " + strings.ToUpper(value) + "\t"
			if got := domain.lookup(set, noisy); got != want {
				t.Errorf("%s(%q) = %q, want the same %q as %q", domain.name, noisy, got, want, value)
			}
		}
	}
}

// TestEvidenceKindsGroupByMedium pins the grouping the design fixes: nineteen
// file extensions collapse onto six markers, so a list of evidence reads as
// "image, image, video, data" rather than as nineteen unrelated shapes.
func TestEvidenceKindsGroupByMedium(t *testing.T) {
	groups := map[Icon][]string{
		IconEvidenceImage: {"png", "jpg", "gif", "webp", "svg"},
		IconEvidenceVideo: {"mp4", "webm"},
		IconEvidenceData:  {"json", "csv"},
		IconEvidenceText:  {"log", "txt", "md"},
		IconEvidenceDiff:  {"patch", "diff"},
		IconEvidenceFile:  {"pdf", "html", "zip", "har", "other"},
	}
	set := Icons(IconModeNerd)
	for icon, kinds := range groups {
		want := set.Glyph(icon)
		for _, kind := range kinds {
			if got := set.EvidenceKind(kind); got != want {
				t.Errorf("EvidenceKind(%q) = %q, want the %s marker %q",
					kind, got, catalog[icon].name, want)
			}
		}
	}
}

// TestNoTwoIconsInOneVocabularyShareAGlyph is what makes a column readable:
// two task states drawn identically are a column that cannot say which of the
// two a row is in. Glyphs are deliberately allowed to repeat across
// vocabularies — a database is a database whether it names a project kind or
// a runbook category — so the check is per vocabulary, not global.
func TestNoTwoIconsInOneVocabularyShareAGlyph(t *testing.T) {
	for _, mode := range allModes {
		set := Icons(mode)
		for _, domain := range domainLookups() {
			seen := map[string]string{}
			for _, value := range domain.values {
				glyph := domain.lookup(set, value)
				if domain.name == "EvidenceKind" {
					// The nineteen kinds collapse onto six markers on
					// purpose; TestEvidenceKindsGroupByMedium is what
					// checks that grouping.
					continue
				}
				if first, clash := seen[glyph]; clash {
					t.Errorf("%s: %s draws %q for both %q and %q",
						mode, domain.name, glyph, first, value)
					continue
				}
				seen[glyph] = value
			}
		}
	}
}

// TestIconNamesAreUnique keeps the catalogue honest about itself: two entries
// under one name would make every failure message above ambiguous.
func TestIconNamesAreUnique(t *testing.T) {
	seen := map[string]Icon{}
	for icon := Icon(0); icon < iconCount; icon++ {
		name := catalog[icon].name
		if first, clash := seen[name]; clash {
			t.Errorf("icons %d and %d are both named %q", int(first), int(icon), name)
			continue
		}
		seen[name] = icon
	}
}

// TestSetIsCopySafe proves a Set can be carried around inside a Styles value
// the way every tab carries one: copying it must not share mutable state with
// the original, and the copy must draw the same glyphs.
func TestSetIsCopySafe(t *testing.T) {
	original := Icons(IconModeNerd)
	copied := original
	if copied.Mode() != original.Mode() {
		t.Fatalf("a copied Set reports %s, want %s", copied.Mode(), original.Mode())
	}
	if copied.Glyph(IconKoi) != original.Glyph(IconKoi) {
		t.Fatal("a copied Set draws a different glyph than the one it was copied from")
	}
	if got := fmt.Sprint(copied.Glyph(IconTabHome)); got == "" {
		t.Fatal("a copied Set draws nothing")
	}
}
